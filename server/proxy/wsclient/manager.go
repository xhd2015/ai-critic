package wsclient

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xhd2015/ai-critic/server/proxy/wsproxy"
	"github.com/xhd2015/ai-critic/server/streaming/progress"
	"github.com/xhd2015/ai-critic/server/subprocess"
)

// RuntimeStatus is one client's live state.
type RuntimeStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Source    string `json:"source"`
	Server    string `json:"server,omitempty"`
	Instance  string `json:"instance,omitempty"`
	HTTPPort  int    `json:"http_port"`
	SocksPort int    `json:"socks_port"`
	AutoStart bool   `json:"auto_start"`

	Running   bool   `json:"running"`
	PID       int    `json:"pid,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	Uptime    int    `json:"uptime_secs,omitempty"`
	LastError string `json:"last_error,omitempty"`
	XrayPath  string `json:"xray_path,omitempty"`
}

// Manager owns every client's xray process. One manager per process; clients
// are identified by their config id.
type Manager struct {
	mu      sync.Mutex
	running map[string]*clientRun
	logf    func(string, ...any)
}

type clientRun struct {
	procID    string
	startedAt time.Time
	httpPort  int
	socksPort int
	err       string
}

// procID namespaces the subprocess manager entry.
func procID(id string) string { return "ws-client-" + id }

// GetManager returns the process-wide manager.
func GetManager() *Manager {
	managerOnce.Do(func() {
		manager = &Manager{running: map[string]*clientRun{}, logf: func(format string, args ...any) {
			fmt.Printf("[ws-client] "+format+"\n", args...)
		}}
	})
	return manager
}

var (
	managerOnce sync.Once
	manager     *Manager
)

// SetLogf redirects manager logging (tests capture it).
func (m *Manager) SetLogf(f func(string, ...any)) { m.mu.Lock(); m.logf = f; m.mu.Unlock() }

func (m *Manager) log(format string, args ...any) {
	if m.logf != nil {
		m.logf(format, args...)
	}
}

// Start brings one client up: resolve VMess → render config → spawn xray →
// wait for the HTTP inbound. Every failure path leaves no process behind.
func (m *Manager) Start(c Client, logFn func(string)) error {
	log := func(msg string) {
		if logFn != nil {
			logFn(msg)
		}
		m.log("%s", msg)
	}
	c.Normalized()
	if err := c.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	if run, ok := m.running[c.ID]; ok && subprocess.GetManager().IsRunning(run.procID) {
		m.mu.Unlock()
		log("client " + c.ID + ": already running")
		return nil
	}
	m.mu.Unlock()

	log(fmt.Sprintf("client %s: start (%s source)", c.ID, c.Source))
	vmess, err := resolveVMess(&c)
	if err != nil {
		m.recordErr(c.ID, err)
		return err
	}
	log(fmt.Sprintf("vmess → %s:%s path %s", vmess.Host, vmess.Port, vmess.Path))

	if err := ensurePortFree(c.HTTPPort); err != nil {
		m.recordErr(c.ID, err)
		return err
	}
	if err := ensurePortFree(c.SocksPort); err != nil {
		m.recordErr(c.ID, err)
		return err
	}

	xrayPath, err := ensureXray()
	if err != nil {
		m.recordErr(c.ID, fmt.Errorf("setup xray: %w", err))
		return err
	}
	log("xray binary: " + xrayPath)

	data, err := xrayConfigFor(vmess, c.HTTPPort, c.SocksPort)
	if err != nil {
		m.recordErr(c.ID, err)
		return err
	}
	cfgPath := xrayConfigPath(c.ID)
	if err := writeXrayConfig(c.ID, data); err != nil {
		m.recordErr(c.ID, err)
		return err
	}
	log("config: " + cfgPath)

	logFile, err := openLog()
	if err != nil {
		m.recordErr(c.ID, err)
		return err
	}

	cmd := exec.Command(xrayPath, "run", "-c", cfgPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	proc, err := subprocess.GetManager().StartProcess(procID(c.ID), "ws-client-"+c.ID, cmd, func() bool {
		return portListening(c.HTTPPort)
	})
	if err != nil {
		logFile.Close()
		m.recordErr(c.ID, fmt.Errorf("start xray: %w", err))
		return err
	}
	if !proc.WaitForRunning(20 * time.Second) {
		_ = subprocess.GetManager().StopProcess(procID(c.ID))
		logFile.Close()
		err := fmt.Errorf("xray did not open 127.0.0.1:%d within 20s", c.HTTPPort)
		m.recordErr(c.ID, err)
		return err
	}
	log(fmt.Sprintf("xray up pid %d · http 127.0.0.1:%d · socks 127.0.0.1:%d",
		cmd.Process.Pid, c.HTTPPort, c.SocksPort))

	m.mu.Lock()
	m.running[c.ID] = &clientRun{procID: procID(c.ID), startedAt: time.Now(), httpPort: c.HTTPPort, socksPort: c.SocksPort}
	m.mu.Unlock()

	log("client " + c.ID + ": running")
	return nil
}

// StartWithProbe starts a client and then verifies the tunnel actually carries
// traffic, so "running" never means "xray is up but the tunnel is dead".
func (m *Manager) StartWithProbe(c Client, logFn func(string), probeURL string) error {
	if err := m.Start(c, logFn); err != nil {
		return err
	}
	if probeURL == "" {
		return nil
	}
	if err := ProbeEgress(c.HTTPPort, probeURL); err != nil {
		m.recordErr(c.ID, err)
		if logFn != nil {
			logFn("egress probe failed: " + err.Error())
		}
		return err
	}
	if logFn != nil {
		logFn("egress probe ok: " + probeURL)
	}
	return nil
}

// Stop tears one client down.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	run, ok := m.running[id]
	delete(m.running, id)
	m.mu.Unlock()

	if !ok || !subprocess.GetManager().IsRunning(run.procID) {
		return nil
	}
	if err := subprocess.GetManager().StopProcess(run.procID); err != nil {
		return fmt.Errorf("stop client %s: %w", id, err)
	}
	m.log("client %s: stopped", id)
	return nil
}

// StopAll tears every running client down.
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.running))
	for id := range m.running {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Stop(id)
	}
}

// Statuses reports every configured client plus its live state.
func (m *Manager) Statuses() ([]RuntimeStatus, error) {
	st, err := LoadStore()
	if err != nil {
		return nil, err
	}
	out := make([]RuntimeStatus, 0, len(st.Clients))
	for _, c := range st.Clients {
		out = append(out, m.StatusOf(c))
	}
	return out, nil
}

// StatusOf reports one client's state.
func (m *Manager) StatusOf(c Client) RuntimeStatus {
	c.Normalized()
	rs := RuntimeStatus{
		ID: c.ID, Name: c.Name, Source: string(c.Source),
		Server: c.BaseURL, Instance: c.Instance,
		HTTPPort: c.HTTPPort, SocksPort: c.SocksPort, AutoStart: c.AutoStart,
	}
	m.mu.Lock()
	run, ok := m.running[c.ID]
	m.mu.Unlock()
	if !ok {
		return rs
	}
	rs.LastError = run.err
	if !subprocess.GetManager().IsRunning(run.procID) {
		return rs
	}
	rs.Running = true
	rs.StartedAt = run.startedAt.Format(time.RFC3339)
	rs.Uptime = int(time.Since(run.startedAt).Seconds())
	if p := subprocess.GetManager().GetProcess(run.procID); p != nil && p.Cmd != nil && p.Cmd.Process != nil {
		rs.PID = p.Cmd.Process.Pid
	}
	if xray, _ := ensureXrayQuiet(); xray != "" {
		rs.XrayPath = xray
	}
	return rs
}

func (m *Manager) recordErr(id string, err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	run, ok := m.running[id]
	if !ok {
		run = &clientRun{procID: procID(id)}
		m.running[id] = run
	}
	run.err = err.Error()
	m.mu.Unlock()
}

// BootAutoStart brings every auto-start client up. It reports the aggregate
// failure so a caller that is itself an explicit user action (StartSystem) can
// surface it; the boot path ignores the result.
func (m *Manager) BootAutoStart() error {
	st, err := LoadStore()
	if err != nil {
		m.log("boot auto-start: %v", err)
		return err
	}
	var failures []string
	for _, c := range st.Clients {
		if !c.AutoStart {
			continue
		}
		if err := m.Start(c, func(msg string) { m.log("boot: %s", msg) }); err != nil {
			m.log("boot auto-start %s failed: %v", c.ID, err)
			failures = append(failures, fmt.Sprintf("%s: %v", c.ID, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("client auto-start failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

// BootAutoStart starts the auto-start clients of the process-wide manager.
// Never fatal at boot: the error is logged by the manager.
func BootAutoStart() { _ = GetManager().BootAutoStart() }

// StreamStart runs a start with SSE progress, ending in done or error.
func (m *Manager) StreamStart(w http.ResponseWriter, id string, probeURL string) error {
	pw := progress.NewWriter(w)
	if pw == nil {
		return fmt.Errorf("streaming unsupported")
	}
	st, err := LoadStore()
	if err != nil {
		_ = pw.EmitError(err.Error())
		return nil
	}
	c := st.Find(id)
	if c == nil {
		_ = pw.EmitError("client not found: " + id)
		return nil
	}
	_ = pw.EmitSection("client " + c.ID)
	logFn := func(msg string) { _ = pw.EmitLog(msg, false) }
	if err := m.Start(*c, logFn); err != nil {
		_ = pw.EmitError(err.Error())
		return nil
	}
	if probeURL != "" {
		_ = pw.EmitLog("probing egress via 127.0.0.1:"+fmt.Sprint(c.HTTPPort), false)
		if err := ProbeEgress(c.HTTPPort, probeURL); err != nil {
			_ = pw.EmitError("egress probe: " + err.Error())
			return nil
		}
		_ = pw.EmitLog("egress probe ok: "+probeURL, false)
	}
	_ = pw.EmitDone(map[string]any{"status": "running", "id": c.ID, "http_port": c.HTTPPort})
	return nil
}

// StreamStop runs a stop with SSE progress.
func (m *Manager) StreamStop(w http.ResponseWriter, id string) error {
	pw := progress.NewWriter(w)
	if pw == nil {
		return fmt.Errorf("streaming unsupported")
	}
	_ = pw.EmitSection("client " + id)
	if err := m.Stop(id); err != nil {
		_ = pw.EmitError(err.Error())
		return nil
	}
	_ = pw.EmitLog("stopped", false)
	_ = pw.EmitDone(map[string]any{"status": "stopped", "id": id})
	return nil
}

// ProbeEgress sends one request through the client's HTTP inbound. The probe
// bypasses any ambient proxies so it tests the tunnel, not the machine.
func ProbeEgress(httpPort int, target string) error {
	proxy, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", httpPort))
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout:   25 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxy)},
	}
	resp, err := client.Get(target)
	if err != nil {
		return fmt.Errorf("GET %s via 127.0.0.1:%d: %w", target, httpPort, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("GET %s via 127.0.0.1:%d: HTTP %d", target, httpPort, resp.StatusCode)
	}
	return nil
}

func ensurePortFree(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("port %d is already in use: %w", port, err)
	}
	return ln.Close()
}

func portListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func openLog() (*os.File, error) {
	path := LogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	return f, nil
}

// ensureXrayQuiet reports the binary path without triggering a download.
func ensureXrayQuiet() (string, error) {
	path := filepath.Join(wsproxy.XrayDir(), xrayBinaryName)
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

// xrayBinaryName keeps the binary name consistent with the ws-proxy server half.
const xrayBinaryName = "xray"

// sanitizeID keeps a log line safe when the id came from a URL.
func sanitizeID(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, id)
}
