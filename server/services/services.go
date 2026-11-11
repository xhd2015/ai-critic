package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xhd2015/ai-critic/server/auth"
	"github.com/xhd2015/ai-critic/server/cloudflare"
	"github.com/xhd2015/ai-critic/server/config"
	"github.com/xhd2015/ai-critic/server/procenv"
	"github.com/xhd2015/ai-critic/server/proxy/portforward"
	"github.com/xhd2015/ai-critic/server/services/authproxy"
	"github.com/xhd2015/ai-critic/server/streaming/progress"
	"github.com/xhd2015/dot-pkgs/go-pkgs/service"
)

const (
	StatusStarting = "starting"
	StatusRunning  = "running"
	StatusStopped  = "stopped"
	StatusError    = "error"
	StatusUnknown  = "unknown"
)

const (
	AuthTokenModeShared = "shared"
	AuthTokenModeCustom = "custom"
)

const (
	restartBackoffBase   = 1 * time.Second
	restartBackoffMax    = 5 * time.Minute
	stableRunResetWindow = 30 * time.Second
)

type ServicePortForward struct {
	Port       int    `json:"port"`
	Label      string `json:"label,omitempty"`
	Provider   string `json:"provider,omitempty"`
	BaseDomain string `json:"baseDomain,omitempty"`
	Subdomain  string `json:"subdomain,omitempty"`
}

type ServiceDefinition struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Command    string            `json:"command"`
	WorkingDir string            `json:"workingDir,omitempty"`
	ExtraEnv   map[string]string `json:"extraEnv,omitempty"`
	// ExtraPath is a PATH-style directory list prepended to the service PATH on
	// top of whatever the process/shell env already provides. It is for tool
	// shims (nvm, volta, ~/bin) without rewriting PATH wholesale.
	ExtraPath string `json:"extraPath,omitempty"`
	// InheritInteractiveShellEnv merges the environment of an interactive login
	// shell ($SHELL -lic 'env -0') into the service env, so tools exported from
	// ~/.zshrc or ~/.bashrc resolve like they do in a terminal. It is resolved on
	// every start; a probe failure is reported, never fatal.
	InheritInteractiveShellEnv bool                `json:"inheritInteractiveShellEnv,omitempty"`
	PortForward                *ServicePortForward `json:"portForward,omitempty"`
	// UpgradeTarget remembers the remote binary path used by remote-agent
	// service upgrade. It does not affect the running command itself.
	UpgradeTarget string `json:"upgradeTarget,omitempty"`
	// UpgradePreStopCmds are remote shell steps run by `service upgrade`
	// *before* the service is stopped, so a build or fetch runs while the old
	// process still serves. The first failing step aborts the upgrade and
	// leaves the service untouched.
	UpgradePreStopCmds []string `json:"upgradePreStopCmds,omitempty"`
	// UpgradePostStopCmds are remote shell steps run *after* the service is
	// stopped and after an uploaded binary is moved into place. A failure
	// aborts the upgrade but restarts the service before reporting.
	UpgradePostStopCmds []string `json:"upgradePostStopCmds,omitempty"`
	// UpgradeTimeoutSeconds bounds each upgrade step. Nil means the default;
	// an explicit 0 disables the timeout.
	UpgradeTimeoutSeconds *int `json:"upgradeTimeoutSeconds,omitempty"`
	// Enabled controls boot auto-start and daemon reconcile. Defaults to true
	// when absent. Disable/enable do not immediately stop or start processes.
	Enabled *bool `json:"enabled,omitempty"`
	// RequireAuth gates the public URL with an in-process cookie login hop.
	RequireAuth   bool   `json:"requireAuth,omitempty"`
	AuthUser      string `json:"authUser,omitempty"`
	AuthTokenMode string `json:"authTokenMode,omitempty"`
	AuthToken     string `json:"authToken,omitempty"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

type ServicePortForwardStatus struct {
	Port       int    `json:"port"`
	Label      string `json:"label,omitempty"`
	Provider   string `json:"provider,omitempty"`
	BaseDomain string `json:"baseDomain,omitempty"`
	Subdomain  string `json:"subdomain,omitempty"`
	PublicURL  string `json:"publicUrl,omitempty"`
	Status     string `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
	Active     bool   `json:"active"`
}

type ServiceStatus struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Kind        ServiceKind       `json:"kind"`
	Description string            `json:"description,omitempty"`
	Command     string            `json:"command"`
	WorkingDir  string            `json:"workingDir,omitempty"`
	ExtraEnv    map[string]string `json:"extraEnv,omitempty"`
	ExtraPath   string            `json:"extraPath,omitempty"`
	// InheritInteractiveShellEnv mirrors the definition so the GUI can show the
	// toggle's state; ShellEnvError reports the last start-time probe failure.
	InheritInteractiveShellEnv bool                      `json:"inheritInteractiveShellEnv,omitempty"`
	ShellEnvError              string                    `json:"shellEnvError,omitempty"`
	EffectivePath              string                    `json:"effectivePath,omitempty"`
	LogPath                    string                    `json:"logPath"`
	Status                     string                    `json:"status"`
	PID                        int                       `json:"pid"`
	LastStartedAt              string                    `json:"lastStartedAt,omitempty"`
	LastExitedAt               string                    `json:"lastExitedAt,omitempty"`
	LastExitError              string                    `json:"lastExitError,omitempty"`
	DesiredRunning             bool                      `json:"desiredRunning"`
	Enabled                    bool                      `json:"enabled"`
	PortForward                *ServicePortForwardStatus `json:"portForward,omitempty"`
	UpgradeTarget              string                    `json:"upgradeTarget,omitempty"`
	// UpgradePreStopCmds and UpgradePostStopCmds mirror the definition so a
	// service update round-trip preserves them.
	UpgradePreStopCmds    []string `json:"upgradePreStopCmds,omitempty"`
	UpgradePostStopCmds   []string `json:"upgradePostStopCmds,omitempty"`
	UpgradeTimeoutSeconds *int     `json:"upgradeTimeoutSeconds,omitempty"`
	RequireAuth           bool     `json:"requireAuth,omitempty"`
	AuthUser              string   `json:"authUser,omitempty"`
	AuthTokenMode         string   `json:"authTokenMode,omitempty"`
	AuthToken             string   `json:"authToken,omitempty"`
	AuthTokens            []string `json:"authTokens,omitempty"`

	// System-service detail. Empty for user services.
	Detail    string `json:"detail,omitempty"`
	PublicURL string `json:"publicUrl,omitempty"`
	Port      int    `json:"port,omitempty"`
	Mocked    bool   `json:"mocked,omitempty"`
	// AutoStartSwitch reports that the subsystem exposes its own auto-start
	// flag, so the GUI offers enable/disable. Always false for user services.
	AutoStartSwitch bool `json:"autoStartSwitch,omitempty"`
	// Edge is the upstream a proxying system service publishes through.
	Edge string `json:"edge,omitempty"`
	// Hosts lists the public hostnames a system service publishes, each with
	// its live dial count.
	Hosts []SystemHostStatus `json:"hosts,omitempty"`
	// Actions lists the actions a system service supports ("start", "stop",
	// "restart"). Empty for user services, which always offer the full set.
	Actions []string `json:"actions,omitempty"`
	// OperationStream marks a system service whose start/stop/restart can run
	// through /api/services/system/stream with live logs.
	OperationStream bool `json:"operationStream,omitempty"`
}

type ServiceActionResponse struct {
	Status  string         `json:"status"`
	Message string         `json:"message"`
	Service *ServiceStatus `json:"service"`
}

const (
	msgDisableRunning = "The server won't stop immediately unless you manually stop it"
	msgDisableStopped = "Server is already stopped"
	msgEnableRunning  = "Server is already running"
	msgEnableStopped  = "The server won't start immediately until daemon checks at next time"
)

type ServiceUpgradeRequest struct {
	ID string `json:"id"`
	// TmpPath and LocalBase describe an uploaded binary. When both are empty
	// the upgrade runs the service's configured steps only, with no artifact
	// to move.
	TmpPath   string `json:"tmpPath,omitempty"`
	LocalBase string `json:"localBase,omitempty"`
	Target    string `json:"target,omitempty"`
	// PreStopCmds, PostStopCmds and TimeoutSeconds override the stored
	// definition for this run only. Nil keeps the configured value.
	PreStopCmds    []string `json:"preStopCmds,omitempty"`
	PostStopCmds   []string `json:"postStopCmds,omitempty"`
	TimeoutSeconds *int     `json:"timeoutSeconds,omitempty"`
}

// ServiceUpgradeStep records one executed shell step of an upgrade.
type ServiceUpgradeStep struct {
	Phase      string `json:"phase"`
	Index      int    `json:"index"`
	Total      int    `json:"total"`
	Command    string `json:"command"`
	ExitCode   int    `json:"exitCode"`
	DurationMs int64  `json:"durationMs"`
}

type ServiceUpgradeResult struct {
	Status           string               `json:"status"`
	TmpPath          string               `json:"tmpPath,omitempty"`
	TargetPath       string               `json:"targetPath,omitempty"`
	RememberedTarget string               `json:"rememberedTarget,omitempty"`
	Service          *ServiceStatus       `json:"service,omitempty"`
	Steps            []ServiceUpgradeStep `json:"steps,omitempty"`
}

// Upgrade phases, used in errors and step records.
const (
	UpgradePhasePreStop  = "pre-stop"
	UpgradePhasePostStop = "post-stop"
)

// DefaultUpgradeStepTimeout bounds each upgrade step unless the service sets
// upgradeTimeoutSeconds (an explicit 0 disables the bound).
const DefaultUpgradeStepTimeout = 15 * time.Minute

// UpgradeError describes a failed upgrade precisely enough for a caller to
// explain which step broke and what happened to the service.
type UpgradeError struct {
	Phase     string
	Index     int
	Total     int
	Command   string
	ExitCode  int
	TimedOut  bool
	Tail      []string
	Resumed   bool
	ResumeErr error
	// ServiceDown reports that the service is stopped with no successful
	// resume, which is the only outcome callers must escalate loudly.
	ServiceDown bool
	Cause       error
}

func (e *UpgradeError) Error() string {
	var b strings.Builder
	switch {
	case e.Phase == UpgradePhasePreStop || e.Phase == UpgradePhasePostStop:
		fmt.Fprintf(&b, "%s command %d/%d", e.Phase, e.Index, e.Total)
	case e.Phase != "":
		fmt.Fprintf(&b, "%s", e.Phase)
	default:
		b.WriteString("upgrade")
	}
	switch {
	case e.TimedOut:
		b.WriteString(" timed out")
	case e.ExitCode >= 0 && (e.Phase == UpgradePhasePreStop || e.Phase == UpgradePhasePostStop):
		fmt.Fprintf(&b, " failed with exit code %d", e.ExitCode)
	}
	if e.Cause != nil {
		fmt.Fprintf(&b, ": %v", e.Cause)
	}
	if len(e.Tail) > 0 {
		fmt.Fprintf(&b, "\n%s", strings.Join(e.Tail, "\n"))
	}
	switch {
	case e.ServiceDown:
		b.WriteString("\nand restarting the service also failed")
		if e.ResumeErr != nil {
			fmt.Fprintf(&b, ": %v", e.ResumeErr)
		}
		b.WriteString(" — the service is STOPPED")
	case e.Resumed:
		b.WriteString("\nthe service was resumed and is running")
	case e.Phase == UpgradePhasePreStop:
		b.WriteString("\nthe service was never stopped")
	}
	return b.String()
}

func (e *UpgradeError) Unwrap() error { return e.Cause }

type serviceUpgradeTargetSelection struct {
	Input      string
	Path       string
	Remembered string
}

// forwardHop is the per-service port-forward/auth-proxy state the Manager owns.
// Process lifetime lives in the shared service.Supervisor; only the forwarding
// bookkeeping is host-specific.
type forwardHop struct {
	authProxy    *authproxy.Server
	tunneledPort int
	ownedForward bool
}

type Manager struct {
	mu     sync.Mutex
	defs   []ServiceDefinition
	system *service.SystemRegistry
	// bootAutostartIDs remembers which services were enabled at boot so the
	// health check never resurrects one the user disabled mid-run.
	bootAutostartIDs   []string
	bootAutostartSet   bool
	hops               map[string]*forwardHop
	sup                *service.Supervisor
	portForwardManager *portforward.Manager
	// Optional isolation for L2 tests; empty → package defaults.
	configPath string
	dataDir    string
}

var (
	defaultManager     = NewManager()
	servicesConfigPath = filepath.Join(config.DataDir, "services.json")
)

func NewManager() *Manager {
	m := newManagerAt("", "")
	m.loadDefinitionsLocked()
	m.syncSupervisor()
	return m
}

// NewManagerFromDefinitions builds an in-memory Manager that never loads or
// saves services.json. Safe for parallel List/ListAll tests.
func NewManagerFromDefinitions(defs []ServiceDefinition) *Manager {
	normalized := make([]ServiceDefinition, 0, len(defs))
	for _, d := range defs {
		d.WorkingDir = normalizeWorkingDir(d.WorkingDir)
		d.ExtraEnv = normalizeExtraEnv(d.ExtraEnv)
		d.UpgradeTarget = strings.TrimSpace(d.UpgradeTarget)
		normalizeAuthFields(&d)
		normalized = append(normalized, d)
	}
	m := newManagerAt("", "")
	m.mu.Lock()
	m.defs = normalized
	m.bootAutostartSet = true
	m.mu.Unlock()
	m.syncSupervisor()
	return m
}

// NewManagerAt loads definitions from configDir/services.json and writes
// process logs under configDir/services/. Isolated for L2 doctests.
func NewManagerAt(configDir string) *Manager {
	configDir = strings.TrimSpace(configDir)
	if configDir == "" {
		return NewManager()
	}
	m := newManagerAt(filepath.Join(configDir, "services.json"), configDir)
	m.loadDefinitionsLocked()
	m.syncSupervisor()
	return m
}

// newManagerAt wires a Manager to a shared service.Supervisor. configPath == ""
// means "use the package default"; dataDir == "" means "use the package
// default data dir" for logs, runtime state, and the auth-proxy secret.
func newManagerAt(configPath, dataDir string) *Manager {
	m := &Manager{
		hops:               make(map[string]*forwardHop),
		portForwardManager: portforward.GetDefaultManager(),
		configPath:         configPath,
		dataDir:            dataDir,
	}
	m.sup = service.NewSupervisor(service.Options{
		DataDir: dataDir,
		LogDir:  filepath.Join(m.servicesDataDir(), "services"),
		Prepare: m.prepareStart,
		Started: m.started,
		Env:     m.buildEnv,
	})
	m.system = m.sup.Registry()
	return m
}

func GetDefaultManager() *Manager {
	return defaultManager
}

func RegisterAPI(mux *http.ServeMux) {
	RegisterAPIWithManager(mux, GetDefaultManager())
}

// RegisterAPIWithManager mounts service endpoints bound to m (L2 doctests).
func RegisterAPIWithManager(mux *http.ServeMux, m *Manager) {
	if m == nil {
		m = GetDefaultManager()
	}
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		handleServicesWith(m, w, r)
	})
	mux.HandleFunc("/api/services/presets", func(w http.ResponseWriter, r *http.Request) {
		handleServicePresets(w, r)
	})
	mux.HandleFunc("/api/services/start", func(w http.ResponseWriter, r *http.Request) {
		handleStartServiceWith(m, w, r)
	})
	mux.HandleFunc("/api/services/stop", func(w http.ResponseWriter, r *http.Request) {
		handleStopServiceWith(m, w, r)
	})
	mux.HandleFunc("/api/services/restart", func(w http.ResponseWriter, r *http.Request) {
		handleRestartServiceWith(m, w, r)
	})
	mux.HandleFunc("/api/services/disable", func(w http.ResponseWriter, r *http.Request) {
		handleDisableServiceWith(m, w, r)
	})
	mux.HandleFunc("/api/services/enable", func(w http.ResponseWriter, r *http.Request) {
		handleEnableServiceWith(m, w, r)
	})
	mux.HandleFunc("/api/services/upgrade", func(w http.ResponseWriter, r *http.Request) {
		handleUpgradeServiceWith(m, w, r)
	})
	mux.HandleFunc("/api/services/upgrade/stream", func(w http.ResponseWriter, r *http.Request) {
		handleUpgradeServiceStreamWith(m, w, r)
	})
	registerSystemStreamAPI(mux, m)
}

func StartHealthCheck() {
	defaultManager.StartHealthCheck()
}

func AutoStartConfiguredServices() {
	defaultManager.AutoStartConfiguredServices()
}

func Shutdown() {
	defaultManager.Shutdown()
}

func (m *Manager) servicesFile() string {
	if m != nil && m.configPath != "" {
		return m.configPath
	}
	return servicesConfigPath
}

func (m *Manager) servicesDataDir() string {
	if m != nil && m.dataDir != "" {
		return m.dataDir
	}
	return config.DataDir
}

func (m *Manager) loadDefinitionsLocked() {
	data, err := os.ReadFile(m.servicesFile())
	if err != nil {
		if os.IsNotExist(err) {
			m.defs = []ServiceDefinition{}
		}
		return
	}

	var defs []ServiceDefinition
	if err := json.Unmarshal(data, &defs); err != nil {
		fmt.Printf("[services] failed to parse services config: %v\n", err)
		return
	}

	for i := range defs {
		// Legacy projectDir in services.json is ignored (field removed).
		defs[i].WorkingDir = normalizeWorkingDir(defs[i].WorkingDir)
		defs[i].ExtraEnv = normalizeExtraEnv(defs[i].ExtraEnv)
		defs[i].UpgradeTarget = strings.TrimSpace(defs[i].UpgradeTarget)
		normalizeAuthFields(&defs[i])
	}
	m.defs = defs
	if !m.bootAutostartSet {
		for _, def := range defs {
			if serviceEnabled(def) {
				m.bootAutostartIDs = append(m.bootAutostartIDs, def.ID)
			}
		}
		m.bootAutostartSet = true
	}
}

func (m *Manager) saveDefinitionsLocked() error {
	dir := m.servicesDataDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.defs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.servicesFile(), data, 0600)
}

// syncSupervisor mirrors the persisted definitions into the shared supervisor.
// It never starts or stops anything: live processes keep running, and only
// definitions that disappeared are torn down by the supervisor itself.
func (m *Manager) syncSupervisor() {
	if m.sup == nil {
		return
	}
	m.mu.Lock()
	defs := make([]service.Definition, 0, len(m.defs))
	for _, def := range m.defs {
		defs = append(defs, toServiceDefinition(def))
	}
	m.mu.Unlock()
	if err := m.sup.Define(defs); err != nil {
		fmt.Printf("[services] failed to sync service definitions: %v\n", err)
	}
}

func (m *Manager) StartHealthCheck() {
	m.sup.StartHealthCheck(0)
}

func (m *Manager) AutoStartConfiguredServices() {
	// Reclaim processes a previous instance left behind before starting
	// anything: an orphan still holds its port, so its replacement would die
	// with "address already in use".
	if reclaimed := m.ReconcileOrphans(); len(reclaimed) > 0 {
		fmt.Printf("[services] reclaimed %d orphaned service process(es)\n", len(reclaimed))
	}

	// Defer briefly so early API calls (e.g. disable before autostart) can
	// persist policy changes before the boot snapshot is applied.
	time.Sleep(2 * time.Second)

	m.mu.Lock()
	ids := append([]string(nil), m.bootAutostartIDs...)
	m.mu.Unlock()

	for _, id := range ids {
		m.mu.Lock()
		def, ok := m.findDefinitionLocked(id)
		enabled := ok && serviceEnabled(def)
		name := ""
		if ok {
			name = def.Name
		}
		m.mu.Unlock()
		if !enabled {
			continue
		}
		if _, err := m.Start(id); err != nil {
			fmt.Printf("[services] failed to autostart %s: %v\n", name, err)
		}
	}
}

// Shutdown stops every managed service. Stops run concurrently: each waits up
// to 5s for its process to die, so stopping a fleet sequentially can outlast the
// caller's cleanup budget and leave the last services running when the process
// is replaced.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.defs))
	for _, def := range m.defs {
		ids = append(ids, def.ID)
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = m.stop(id, true, true)
		}(id)
	}
	wg.Wait()
	// Adopted orphans have no saved definition, so sweep whatever the supervisor
	// still tracks and stop the health check.
	m.sup.Shutdown()
}

// ForceStopAll kills any managed service process that is still alive, returning
// the names it had to kill. It is the backstop for an interrupted shutdown: a
// survivor keeps its port, and the next instance's copy then dies with
// "address already in use".
func ForceStopAll() []string {
	return defaultManager.forceStopAll()
}

func (m *Manager) forceStopAll() []string {
	procs := m.sup.Processes()
	m.mu.Lock()
	names := make([]string, 0, len(procs))
	for _, proc := range procs {
		if proc.PID <= 0 || !service.Alive(proc.PID) {
			continue
		}
		name := proc.Definition.Name
		if name == "" {
			name = proc.Definition.ID
		}
		names = append(names, name)
	}
	m.mu.Unlock()
	if len(names) == 0 {
		return nil
	}
	m.sup.ForceStopAll()
	return names
}

func (m *Manager) CreateOrUpdate(def ServiceDefinition) (*ServiceStatus, error) {
	return m.createOrUpdate(def, true)
}

func (m *Manager) CreateOrUpdateNoRestart(def ServiceDefinition) (*ServiceStatus, error) {
	return m.createOrUpdate(def, false)
}

func (m *Manager) createOrUpdate(def ServiceDefinition, restartChanged bool) (*ServiceStatus, error) {
	def.WorkingDir = normalizeWorkingDir(def.WorkingDir)
	def.ExtraEnv = normalizeExtraEnv(def.ExtraEnv)
	normalizedExtraPath, err := procenv.NormalizeExtraPath(def.ExtraPath)
	if err != nil {
		return nil, fmt.Errorf("extra path: %w", err)
	}
	def.ExtraPath = normalizedExtraPath
	def.UpgradeTarget = strings.TrimSpace(def.UpgradeTarget)
	def.UpgradePreStopCmds = normalizeUpgradeCmds(def.UpgradePreStopCmds)
	def.UpgradePostStopCmds = normalizeUpgradeCmds(def.UpgradePostStopCmds)
	if def.UpgradeTimeoutSeconds != nil && *def.UpgradeTimeoutSeconds < 0 {
		return nil, fmt.Errorf("upgrade timeout must not be negative")
	}
	normalizeAuthFields(&def)
	// A write addressed at a system service must not create a user service with
	// the same id, which would shadow the registered one. Checked before
	// validation so the error names the real problem.
	if err := m.rejectSystemServiceEdit(strings.TrimSpace(def.ID), "edited"); err != nil {
		return nil, err
	}
	if err := validateDefinition(def); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var existing *ServiceDefinition
	var shouldRestart bool

	m.mu.Lock()
	if def.ID == "" {
		def.ID = fmt.Sprintf("svc-%d", time.Now().UnixNano())
		def.CreatedAt = now
		def.UpdatedAt = now
		m.defs = append(m.defs, def)
	} else {
		for i := range m.defs {
			if m.defs[i].ID != def.ID {
				continue
			}
			existingCopy := m.defs[i]
			existing = &existingCopy
			if def.UpgradeTarget == "" {
				def.UpgradeTarget = m.defs[i].UpgradeTarget
			}
			if def.Enabled == nil {
				def.Enabled = m.defs[i].Enabled
			}
			def.CreatedAt = m.defs[i].CreatedAt
			def.UpdatedAt = now
			m.defs[i] = def
			break
		}
		if existing == nil {
			def.CreatedAt = now
			def.UpdatedAt = now
			m.defs = append(m.defs, def)
		}
	}
	if err := m.saveDefinitionsLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}

	wasRunning := m.serviceRunningLocked(def.ID)
	m.mu.Unlock()

	m.syncSupervisor()
	if wasRunning && restartChanged {
		shouldRestart = existing == nil || definitionChanged(*existing, def)
	}

	if shouldRestart {
		// Definition is already on disk. A restart failure (zombie PID, CF
		// bounce mid-request, etc.) must not roll back the save or make the
		// UI treat a successful domain/command edit as failed.
		if err := m.Restart(def.ID); err != nil {
			fmt.Printf("[services] saved %s (%s) but restart failed: %v\n", def.ID, def.Name, err)
			m.setRuntimeError(def.ID, "saved; restart failed: "+err.Error())
		}
	}

	if status, ok := m.statusByID(def.ID); ok {
		return status, nil
	}
	return nil, fmt.Errorf("service %s not found after save", def.ID)
}

func (m *Manager) Delete(id string) error {
	if err := m.rejectSystemServiceEdit(id, "removed"); err != nil {
		return err
	}
	if err := m.stop(id, true, true); err != nil {
		return err
	}

	m.mu.Lock()
	filtered := make([]ServiceDefinition, 0, len(m.defs))
	found := false
	for _, def := range m.defs {
		if def.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, def)
	}
	if !found {
		m.mu.Unlock()
		return fmt.Errorf("service not found")
	}
	m.defs = filtered
	err := m.saveDefinitionsLocked()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.syncSupervisor()
	return nil
}

func (m *Manager) Start(id string) (*ServiceStatus, error) {
	if svc, ok := m.systemService(id); ok {
		lifecycle, ok := svc.Controller.(SystemLifecycle)
		if !ok {
			return nil, errSystemActionUnsupported(svc, SystemActionStart)
		}
		if err := lifecycle.StartSystem(); err != nil {
			return nil, err
		}
		if status, ok := m.statusByID(id); ok {
			return status, nil
		}
		return nil, fmt.Errorf("system service %s not found after start", id)
	}
	if err := m.start(id, true); err != nil {
		return nil, err
	}
	if status, ok := m.statusByID(id); ok {
		return status, nil
	}
	return nil, fmt.Errorf("service not found")
}

// Stop stops one service. System services delegate to their controller.
func (m *Manager) Stop(id string) error {
	if svc, ok := m.systemService(id); ok {
		lifecycle, ok := svc.Controller.(SystemLifecycle)
		if !ok {
			return errSystemActionUnsupported(svc, SystemActionStop)
		}
		return lifecycle.StopSystem()
	}
	return m.stop(id, true, true)
}

func (m *Manager) Restart(id string) error {
	if svc, ok := m.systemService(id); ok {
		if restarter, ok := svc.Controller.(SystemRestarter); ok {
			return restarter.RestartSystem()
		}
		lifecycle, ok := svc.Controller.(SystemLifecycle)
		if !ok {
			return errSystemActionUnsupported(svc, SystemActionRestart)
		}
		if err := lifecycle.StopSystem(); err != nil {
			return err
		}
		return lifecycle.StartSystem()
	}
	if err := m.stop(id, false, true); err != nil {
		return err
	}
	return m.start(id, true)
}

func (m *Manager) Disable(id string) (*ServiceActionResponse, error) {
	if svc, ok := m.systemService(id); ok {
		return m.setSystemAutoStart(svc, false)
	}
	running, err := m.setServiceEnabled(id, false)
	if err != nil {
		return nil, err
	}

	message := msgDisableStopped
	if running {
		message = msgDisableRunning
	}
	return m.buildServiceActionResponse(id, message)
}

func (m *Manager) Enable(id string) (*ServiceActionResponse, error) {
	if svc, ok := m.systemService(id); ok {
		return m.setSystemAutoStart(svc, true)
	}
	m.mu.Lock()
	def, ok := m.findDefinitionLocked(id)
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("service not found")
	}

	running := m.serviceRunningLocked(id)
	enabled := true
	def.Enabled = &enabled
	def.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	for i := range m.defs {
		if m.defs[i].ID == id {
			m.defs[i] = def
			break
		}
	}
	if err := m.saveDefinitionsLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}

	m.mu.Unlock()
	// The supervisor re-reads definitions on its own; enabling only restores the
	// auto-start policy, so a stopped service is not started here.
	m.syncSupervisor()

	message := msgEnableStopped
	if running {
		message = msgEnableRunning
	}
	return m.buildServiceActionResponse(id, message)
}

func (m *Manager) setServiceEnabled(id string, enabled bool) (running bool, err error) {
	m.mu.Lock()

	def, ok := m.findDefinitionLocked(id)
	if !ok {
		m.mu.Unlock()
		return false, fmt.Errorf("service not found")
	}

	running = m.serviceRunningLocked(id)
	def.Enabled = &enabled
	def.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	for i := range m.defs {
		if m.defs[i].ID == id {
			m.defs[i] = def
			break
		}
	}
	err = m.saveDefinitionsLocked()
	m.mu.Unlock()
	if err != nil {
		return running, err
	}
	m.syncSupervisor()
	return running, nil
}

func (m *Manager) buildServiceActionResponse(id string, message string) (*ServiceActionResponse, error) {
	status, ok := m.statusByID(id)
	if !ok {
		return nil, fmt.Errorf("service %s not found after action", id)
	}
	return &ServiceActionResponse{
		Status:  "ok",
		Message: message,
		Service: status,
	}, nil
}

// List returns the user services followed by the server-owned system services.
func (m *Manager) List() []ServiceStatus {
	return m.ListAll()
}

// ListAll is an alias of List kept for callers and tests that used the old
// cross-scope API name.
func (m *Manager) ListAll() []ServiceStatus {
	// The supervisor owns process state and has its own lock; snapshot it
	// before taking m.mu so neither lock is held under the other.
	procs := m.supervisorProcesses()

	m.mu.Lock()
	defer m.mu.Unlock()
	result := m.buildStatusListLocked(append([]ServiceDefinition(nil), m.defs...), procs)
	return append(result, m.buildSystemStatusListLocked()...)
}

func (m *Manager) supervisorProcesses() map[string]service.Process {
	procs := make(map[string]service.Process)
	if m.sup == nil {
		return procs
	}
	for _, proc := range m.sup.Processes() {
		procs[proc.Definition.ID] = proc
	}
	return procs
}

func (m *Manager) buildStatusListLocked(defs []ServiceDefinition, procs map[string]service.Process) []ServiceStatus {
	portMap := make(map[int]portforward.PortForward)
	for _, pf := range m.getPortForwardManager().List() {
		if pf.Target == nil || pf.ID == config.LocalMappingID(pf.LocalPort) {
			portMap[pf.LocalPort] = pf
		}
	}

	result := make([]ServiceStatus, 0, len(defs))
	for _, def := range defs {
		proc, tracked := procs[def.ID]
		hop := m.hops[def.ID]
		logPath := m.serviceLogPath(def.ID)
		status := StatusStopped
		pid := 0
		lastStartedAt := ""
		lastExitedAt := ""
		lastExitError := ""
		shellEnvError := ""
		effectivePath := ""
		desired := true
		ownedForward := false
		if tracked {
			status = string(proc.State)
			pid = proc.PID
			lastStartedAt = proc.LastStartedAt
			lastExitedAt = proc.LastExitedAt
			lastExitError = proc.LastExitError
			shellEnvError = proc.ShellEnvError
			effectivePath = proc.EffectivePath
			desired = proc.Desired
			if proc.LogPath != "" {
				logPath = proc.LogPath
			}
			if pid > 0 && !processAlive(pid) {
				pid = 0
				if status == StatusRunning {
					status = StatusStopped
				}
			}
		}
		if hop != nil {
			ownedForward = hop.ownedForward
		}
		if effectivePath == "" {
			// No start yet: show the static PATH layers. The interactive shell
			// env is deliberately not probed here; this runs on every list poll.
			effectivePath = lookupEnvValue(buildServiceEnv(def), "PATH")
		}

		var pfStatus *ServicePortForwardStatus
		if def.PortForward != nil && def.PortForward.Port > 0 {
			pfStatus = &ServicePortForwardStatus{
				Port:       def.PortForward.Port,
				Label:      def.PortForward.Label,
				Provider:   normalizeProvider(def.PortForward.Provider),
				BaseDomain: def.PortForward.BaseDomain,
				Subdomain:  def.PortForward.Subdomain,
			}
			lookupPort := def.PortForward.Port
			if hop != nil && hop.tunneledPort > 0 {
				lookupPort = hop.tunneledPort
			}
			if active, ok := portMap[lookupPort]; ok {
				pfStatus.PublicURL = active.PublicURL
				pfStatus.Status = active.Status
				pfStatus.Error = active.Error
				pfStatus.Active = active.Status == portforward.StatusActive || ownedForward
				if active.Provider != "" {
					pfStatus.Provider = active.Provider
				}
				if active.Label != "" && pfStatus.Label == "" {
					pfStatus.Label = active.Label
				}
			}
		}

		result = append(result, ServiceStatus{
			ID:                         def.ID,
			Name:                       def.Name,
			Kind:                       ServiceKindUser,
			Command:                    def.Command,
			WorkingDir:                 def.WorkingDir,
			ExtraEnv:                   cloneStringMap(def.ExtraEnv),
			ExtraPath:                  def.ExtraPath,
			InheritInteractiveShellEnv: def.InheritInteractiveShellEnv,
			ShellEnvError:              shellEnvError,
			EffectivePath:              effectivePath,
			LogPath:                    logPath,
			Status:                     status,
			PID:                        pid,
			LastStartedAt:              lastStartedAt,
			LastExitedAt:               lastExitedAt,
			LastExitError:              lastExitError,
			DesiredRunning:             desired,
			Enabled:                    serviceEnabled(def),
			PortForward:                pfStatus,
			UpgradeTarget:              def.UpgradeTarget,
			UpgradePreStopCmds:         cloneCmdList(def.UpgradePreStopCmds),
			UpgradePostStopCmds:        cloneCmdList(def.UpgradePostStopCmds),
			UpgradeTimeoutSeconds:      cloneIntPtr(def.UpgradeTimeoutSeconds),
			RequireAuth:                def.RequireAuth,
			AuthUser:                   def.AuthUser,
			AuthTokenMode:              def.AuthTokenMode,
			AuthToken:                  def.AuthToken,
			AuthTokens:                 inspectAuthTokens(def),
		})
	}
	return result
}

func (m *Manager) statusByID(id string) (*ServiceStatus, bool) {
	for _, item := range m.List() {
		if item.ID == id {
			copy := item
			return &copy, true
		}
	}
	return nil, false
}

func (m *Manager) selectServiceUpgradeTarget(id string, localBase string, targetFlag string) (*serviceUpgradeTargetSelection, error) {
	id = strings.TrimSpace(id)
	localBase = normalizeUpgradeLocalBase(localBase)
	targetFlag = strings.TrimSpace(targetFlag)
	if id == "" {
		return nil, fmt.Errorf("service id is required")
	}
	if localBase == "" {
		return nil, fmt.Errorf("local binary basename is required")
	}

	var remembered string

	m.mu.Lock()
	idx := -1
	for i := range m.defs {
		if m.defs[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		m.mu.Unlock()
		return nil, fmt.Errorf("service not found")
	}

	remembered = strings.TrimSpace(m.defs[idx].UpgradeTarget)
	m.mu.Unlock()

	input := targetFlag
	if input == "" {
		input = remembered
	}
	if input == "" {
		input = localBase
	}

	targetPath, err := resolveServiceUpgradeTargetPath(input, localBase)
	if err != nil {
		return nil, err
	}
	if targetFlag != "" {
		m.mu.Lock()
		idx := -1
		for i := range m.defs {
			if m.defs[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			m.mu.Unlock()
			return nil, fmt.Errorf("service not found")
		}
		m.defs[idx].UpgradeTarget = targetFlag
		remembered = targetFlag
		if err := m.saveDefinitionsLocked(); err != nil {
			m.mu.Unlock()
			return nil, err
		}
		m.mu.Unlock()
	}
	return &serviceUpgradeTargetSelection{
		Input:      input,
		Path:       targetPath,
		Remembered: remembered,
	}, nil
}

// start launches a service through the shared supervisor. force bypasses the
// restart backoff ladder, which is what an explicit Start action wants.
func (m *Manager) start(id string, force bool) error {
	m.mu.Lock()
	_, ok := m.findDefinitionLocked(id)
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("service not found")
	}
	if force {
		return m.sup.StartForce(id)
	}
	return m.sup.Start(id)
}

// prepareStart is the supervisor's Prepare hook: free the port the service is
// configured to publish before the child binds it.
func (m *Manager) prepareStart(def service.Definition) error {
	m.mu.Lock()
	saved, ok := m.findDefinitionLocked(def.ID)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return killConfiguredPortOwner(saved.PortForward)
}

// started is the supervisor's Started hook. A forwarding failure is recorded on
// the status instead of failing the start, matching the pre-extraction
// behavior: the process is already healthy and killing it would be worse.
func (m *Manager) started(def service.Definition, _ int, logFile *os.File) error {
	m.mu.Lock()
	saved, ok := m.findDefinitionLocked(def.ID)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	if err := m.ensurePortForward(saved.ID, saved); err != nil {
		if logFile != nil {
			_, _ = logFile.WriteString(fmt.Sprintf("[%s] failed to ensure port forwarding: %v\n", time.Now().Format(time.RFC3339), err))
		}
		m.setRuntimeError(saved.ID, err.Error())
	}
	return nil
}

// buildEnv is the supervisor's Env hook. The saved definition is re-read under
// the lock so a concurrent save cannot make the login-shell probe disagree with
// the command that just started.
func (m *Manager) buildEnv(def service.Definition) ([]string, error) {
	m.mu.Lock()
	saved, ok := m.findDefinitionLocked(def.ID)
	m.mu.Unlock()
	if ok {
		def = toServiceDefinition(saved)
	}
	env, shellErr := buildServiceEnvForStart(fromServiceDefinition(def))
	if shellErr != "" {
		return env, errors.New(shellErr)
	}
	return env, nil
}

// toServiceDefinition converts a saved definition into the shared supervisor's
// type. Host-only fields (port forwarding, upgrades, auth) stay in ai-critic.
func toServiceDefinition(def ServiceDefinition) service.Definition {
	return service.Definition{
		ID:                         def.ID,
		Name:                       def.Name,
		Command:                    def.Command,
		WorkingDir:                 def.WorkingDir,
		ExtraEnv:                   def.ExtraEnv,
		ExtraPath:                  def.ExtraPath,
		InheritInteractiveShellEnv: def.InheritInteractiveShellEnv,
		Enabled:                    def.Enabled,
	}
}

// fromServiceDefinition is the inverse of toServiceDefinition. It is only used
// where a hook receives a supervisor definition and needs the saved record.
func fromServiceDefinition(def service.Definition) ServiceDefinition {
	return ServiceDefinition{
		ID:                         def.ID,
		Name:                       def.Name,
		Command:                    def.Command,
		WorkingDir:                 def.WorkingDir,
		ExtraEnv:                   def.ExtraEnv,
		ExtraPath:                  def.ExtraPath,
		InheritInteractiveShellEnv: def.InheritInteractiveShellEnv,
		Enabled:                    def.Enabled,
	}
}

// stop takes a service down. removeForward tears down the auth proxy and the
// port forward the service owns.
func (m *Manager) stop(id string, removeForward bool, _ bool) error {
	m.mu.Lock()
	_, exists := m.findDefinitionLocked(id)
	if !exists {
		if _, tracked := m.sup.Process(id); !tracked {
			m.mu.Unlock()
			return fmt.Errorf("service not found")
		}
	}
	hop := m.hops[id]
	if removeForward {
		delete(m.hops, id)
	}
	m.mu.Unlock()

	// Forward teardown stays ahead of the signal so the local tunnel closes
	// while the process it published is still up.
	if removeForward && hop != nil {
		if hop.authProxy != nil {
			_ = hop.authProxy.Close()
		}
		if hop.ownedForward && hop.tunneledPort > 0 {
			_ = m.getPortForwardManager().RemoveSelector(config.LocalMappingID(hop.tunneledPort))
		}
	}
	return m.sup.Stop(id)
}

// CloudflareOwnedForwardHosts returns the hostnames user services intend to
// publish via cloudflare_owned, from their saved definitions. It takes the
// manager lock, so it must not be called while that lock is held.
func (m *Manager) CloudflareOwnedForwardHosts() []SystemHost {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cloudflareOwnedHostsLocked()
}

func (m *Manager) cloudflareOwnedHostsLocked() []SystemHost {
	out := make([]SystemHost, 0)
	for _, def := range m.defs {
		if def.PortForward == nil {
			continue
		}
		if normalizeProvider(def.PortForward.Provider) != portforward.ProviderCloudflareOwned {
			continue
		}
		host := resolveForwardLabel(def.PortForward)
		if host == "" || strings.HasPrefix(host, "Port ") {
			continue
		}
		out = append(out, SystemHost{Host: host, ServiceID: def.ID})
	}
	return out
}

// RepublishForward tears down one service's port forward and re-establishes it.
//
// It exists for the Cloudflare Proxy system service: a forward can keep
// claiming "active" while its dial pool is gone, and ensurePortForward reuses
// an active entry, so the entry must be dropped first. The service's own
// process is left alone.
func (m *Manager) RepublishForward(id string) error {
	m.mu.Lock()
	def, ok := m.findDefinitionLocked(id)
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("service not found")
	}
	if def.PortForward == nil || def.PortForward.Port <= 0 {
		m.mu.Unlock()
		return fmt.Errorf("service %q has no port forwarding", id)
	}
	tunnelPort := def.PortForward.Port
	if hop := m.hops[id]; hop != nil && hop.tunneledPort > 0 {
		tunnelPort = hop.tunneledPort
	}
	desiredDef := def
	m.mu.Unlock()

	// Drop the stale entry so ensurePortForward rebuilds it instead of reusing it.
	if err := m.getPortForwardManager().RemoveSelector(config.LocalMappingID(tunnelPort)); err != nil &&
		!strings.Contains(err.Error(), "not being forwarded") {
		return err
	}
	return m.ensurePortForward(id, desiredDef)
}

func (m *Manager) ensurePortForward(id string, def ServiceDefinition) error {
	if def.PortForward == nil || def.PortForward.Port <= 0 {
		m.teardownAuthHop(id)
		return nil
	}

	pf := def.PortForward
	manager := m.getPortForwardManager()
	desiredProvider := normalizeProvider(pf.Provider)
	desiredLabel := resolveForwardLabel(pf)

	tunnelPort := pf.Port
	var proxy *authproxy.Server
	if def.RequireAuth {
		reused, err := m.reuseAuthHop(id, def)
		if err != nil {
			return err
		}
		if reused != nil {
			proxy = reused
			tunnelPort = reused.Port()
		} else {
			m.teardownAuthHop(id)
			started, err := m.startAuthProxy(def)
			if err != nil {
				return err
			}
			proxy = started
			tunnelPort = started.Port()
			if pf.Port != tunnelPort {
				_ = manager.RemoveSelector(config.LocalMappingID(pf.Port))
			}
		}
	} else {
		m.teardownAuthHop(id)
		tunnelPort = pf.Port
	}

	for _, existing := range manager.List() {
		if existing.LocalPort != tunnelPort || (existing.Target != nil && existing.ID != config.LocalMappingID(tunnelPort)) {
			continue
		}
		// Only reuse a live forward. Stuck "connecting"/"error" entries (e.g. after
		// a failed qemu upsert) must be replaced or the UI keeps the old error forever.
		if existing.Provider == desiredProvider && existing.Label == desiredLabel &&
			existing.Status == portforward.StatusActive && existing.PublicURL != "" {
			m.rememberAuthHop(id, proxy, tunnelPort, true)
			return nil
		}
		if existing.Type != portforward.PortForwardTypePortForward {
			if proxy != nil && proxy != m.currentAuthProxy(id) {
				_ = proxy.Close()
			}
			return fmt.Errorf("port %d is already forwarded by %s as %q; expected %s %q",
				tunnelPort, existing.Provider, existing.Label, desiredProvider, desiredLabel)
		}
		fmt.Printf("[services] replacing stale port forward for service %s: port=%d old=%s/%q status=%s new=%s/%q\n",
			id, tunnelPort, existing.Provider, existing.Label, existing.Status, desiredProvider, desiredLabel)
		if err := manager.RemoveSelector(config.LocalMappingID(tunnelPort)); err != nil && !strings.Contains(err.Error(), "not being forwarded") {
			if proxy != nil && proxy != m.currentAuthProxy(id) {
				_ = proxy.Close()
			}
			return fmt.Errorf("failed to remove stale port forward on port %d: %w", tunnelPort, err)
		}
		break
	}

	if _, err := manager.Add(tunnelPort, desiredLabel, desiredProvider); err != nil {
		if proxy != nil && proxy != m.currentAuthProxy(id) {
			_ = proxy.Close()
		}
		return err
	}

	m.rememberAuthHop(id, proxy, tunnelPort, true)
	return nil
}

func (m *Manager) startAuthProxy(def ServiceDefinition) (*authproxy.Server, error) {
	if def.PortForward == nil {
		return nil, fmt.Errorf("require auth requires port forwarding")
	}
	return authproxy.Start(authproxy.Config{
		BackendPort: def.PortForward.Port,
		AuthUser:    def.AuthUser,
		TokenMode:   def.AuthTokenMode,
		CustomToken: def.AuthToken,
		SecretPath:  filepath.Join(m.servicesDataDir(), "service-auth-secret"),
	})
}

func (m *Manager) reuseAuthHop(id string, def ServiceDefinition) (*authproxy.Server, error) {
	if def.PortForward == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	hop := m.hops[id]
	if hop == nil || hop.authProxy == nil || !hop.authProxy.Alive() {
		return nil, nil
	}
	if !hop.authProxy.Matches(def.PortForward.Port, def.AuthUser, def.AuthTokenMode, def.AuthToken) {
		return nil, nil
	}
	return hop.authProxy, nil
}

func (m *Manager) currentAuthProxy(id string) *authproxy.Server {
	m.mu.Lock()
	defer m.mu.Unlock()
	if hop := m.hops[id]; hop != nil {
		return hop.authProxy
	}
	return nil
}

func (m *Manager) rememberAuthHop(id string, proxy *authproxy.Server, tunnelPort int, owned bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hop := m.hops[id]
	if hop == nil {
		// A forward can be published before the process entry exists (republish,
		// or a service whose child already exited); keep tracking it so it can be
		// torn down again.
		hop = &forwardHop{}
		m.hops[id] = hop
	}
	hop.authProxy = proxy
	if proxy != nil {
		hop.tunneledPort = tunnelPort
	} else {
		hop.tunneledPort = 0
	}
	hop.ownedForward = owned
}

func (m *Manager) teardownAuthHop(id string) {
	m.mu.Lock()
	hop := m.hops[id]
	if hop == nil || hop.authProxy == nil {
		m.mu.Unlock()
		return
	}
	proxy := hop.authProxy
	port := hop.tunneledPort
	owned := hop.ownedForward
	hop.authProxy = nil
	hop.tunneledPort = 0
	if owned {
		hop.ownedForward = false
	}
	m.mu.Unlock()

	_ = proxy.Close()
	if owned && port > 0 {
		_ = m.getPortForwardManager().RemoveSelector(config.LocalMappingID(port))
	}
}

func (m *Manager) getPortForwardManager() *portforward.Manager {
	if m.portForwardManager != nil {
		return m.portForwardManager
	}
	return portforward.GetDefaultManager()
}

func (m *Manager) setError(id string, err error) {
	m.setRuntimeError(id, err.Error())
}

// setRuntimeError records a host-side failure on the service status. The
// supervisor owns the backoff ladder and the process fields.
func (m *Manager) setRuntimeError(id string, msg string) {
	if m.sup == nil {
		return
	}
	m.sup.SetRuntimeError(id, msg)
}

func (m *Manager) findDefinitionLocked(id string) (ServiceDefinition, bool) {
	for _, def := range m.defs {
		if def.ID == id {
			return def, true
		}
	}
	return ServiceDefinition{}, false
}

func serviceEnabled(def ServiceDefinition) bool {
	if def.Enabled == nil {
		return true
	}
	return *def.Enabled
}

// serviceRunningLocked reports whether id currently has a live child. The
// supervisor keeps its own lock, so the process snapshot is read without m.mu
// (callers hold it, and taking another lock under it would invert lock order).
func (m *Manager) serviceRunningLocked(id string) bool {
	if m.sup == nil {
		return false
	}
	proc, ok := m.sup.Process(id)
	return ok && proc.PID > 0
}

func normalizeProvider(provider string) string {
	if provider == "" {
		return portforward.ProviderLocaltunnel
	}
	return provider
}

func validateDefinition(def ServiceDefinition) error {
	if strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("service name is required")
	}
	if strings.TrimSpace(def.Command) == "" {
		return fmt.Errorf("service command is required")
	}
	for key := range def.ExtraEnv {
		if !isValidEnvKey(key) {
			return fmt.Errorf("invalid environment variable name: %s", key)
		}
	}
	if _, err := procenv.NormalizeExtraPath(def.ExtraPath); err != nil {
		return fmt.Errorf("extra path: %w", err)
	}
	if def.PortForward != nil {
		if def.PortForward.Port <= 0 || def.PortForward.Port > 65535 {
			return fmt.Errorf("service port must be between 1 and 65535")
		}
	}
	if def.RequireAuth {
		if def.PortForward == nil || def.PortForward.Port <= 0 {
			return fmt.Errorf("require auth requires port forwarding")
		}
		if def.AuthTokenMode == AuthTokenModeCustom && def.AuthToken == "" {
			return fmt.Errorf("custom auth token must be non-empty")
		}
	}
	return nil
}

func normalizeAuthFields(def *ServiceDefinition) {
	if def == nil {
		return
	}
	def.AuthUser = strings.TrimSpace(def.AuthUser)
	def.AuthToken = strings.TrimSpace(def.AuthToken)
	mode := strings.ToLower(strings.TrimSpace(def.AuthTokenMode))
	switch mode {
	case AuthTokenModeCustom, AuthTokenModeShared:
		def.AuthTokenMode = mode
	default:
		def.AuthTokenMode = ""
	}
	if !def.RequireAuth {
		return
	}
	if def.AuthTokenMode == AuthTokenModeCustom || (def.AuthTokenMode == "" && def.AuthToken != "") {
		def.AuthTokenMode = AuthTokenModeCustom
		return
	}
	def.AuthTokenMode = AuthTokenModeShared
	def.AuthToken = ""
}

func inspectAuthTokens(def ServiceDefinition) []string {
	if !def.RequireAuth {
		return nil
	}
	if def.AuthTokenMode == AuthTokenModeCustom {
		if def.AuthToken == "" {
			return nil
		}
		return []string{def.AuthToken}
	}
	tokens, err := auth.ExportCredentials()
	if err != nil || len(tokens) == 0 {
		return nil
	}
	sort.Strings(tokens)
	return tokens
}

func definitionChanged(oldDef ServiceDefinition, newDef ServiceDefinition) bool {
	if oldDef.Name != newDef.Name || oldDef.Command != newDef.Command || oldDef.WorkingDir != newDef.WorkingDir {
		return true
	}
	if !stringMapEqual(oldDef.ExtraEnv, newDef.ExtraEnv) {
		return true
	}
	if oldDef.ExtraPath != newDef.ExtraPath {
		return true
	}
	if oldDef.InheritInteractiveShellEnv != newDef.InheritInteractiveShellEnv {
		return true
	}
	if oldDef.RequireAuth != newDef.RequireAuth ||
		oldDef.AuthUser != newDef.AuthUser ||
		oldDef.AuthTokenMode != newDef.AuthTokenMode ||
		oldDef.AuthToken != newDef.AuthToken {
		return true
	}

	oldPF := oldDef.PortForward
	newPF := newDef.PortForward
	if (oldPF == nil) != (newPF == nil) {
		return true
	}
	if oldPF == nil {
		return false
	}
	return oldPF.Port != newPF.Port ||
		oldPF.Label != newPF.Label ||
		oldPF.Provider != newPF.Provider ||
		oldPF.BaseDomain != newPF.BaseDomain ||
		oldPF.Subdomain != newPF.Subdomain
}

func resolveForwardLabel(pf *ServicePortForward) string {
	if pf == nil {
		return ""
	}
	if pf.Label != "" {
		return pf.Label
	}
	if pf.Subdomain != "" {
		if pf.BaseDomain != "" {
			return fmt.Sprintf("%s.%s", pf.Subdomain, pf.BaseDomain)
		}
		ownedDomains := cloudflare.GetOwnedDomains()
		if len(ownedDomains) > 0 {
			return fmt.Sprintf("%s.%s", pf.Subdomain, ownedDomains[0])
		}
	}
	return fmt.Sprintf("Port %d", pf.Port)
}

func killConfiguredPortOwner(pf *ServicePortForward) error {
	if pf == nil || pf.Port <= 0 {
		return nil
	}
	pids, err := findListeningPIDs(pf.Port)
	if err != nil {
		return err
	}
	for _, pid := range pids {
		if pid == os.Getpid() {
			return fmt.Errorf("refusing to kill current server process on port %d", pf.Port)
		}
		if err := stopProcessGroup(pid); err != nil {
			return fmt.Errorf("failed to free port %d: %w", pf.Port, err)
		}
	}
	return nil
}

func findListeningPIDs(port int) ([]int, error) {
	cmd := exec.Command("lsof", "-tiTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-n", "-P")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) == 0 {
			return nil, nil
		}
		if execErr, ok := err.(*exec.Error); ok && execErr.Err == exec.ErrNotFound {
			return nil, fmt.Errorf("lsof not installed: required for service port management")
		}
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var pids []int
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, convErr := strconv.Atoi(line)
		if convErr == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

func processGroupID(pid int) (int, error) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return 0, err
	}
	return pgid, nil
}

func stopProcessGroup(pid int) error {
	if pid <= 0 {
		return nil
	}

	pgid, err := processGroupID(pid)
	if err != nil {
		// Fall back to signaling the process directly when PGID lookup fails.
		return signalProcessUntilExit(pid, pid, 3*time.Second, 2*time.Second)
	}
	return signalProcessUntilExit(pid, pgid, 3*time.Second, 2*time.Second)
}

func signalProcessUntilExit(pid, signalTarget int, termTimeout, killTimeout time.Duration) error {
	_ = syscall.Kill(-signalTarget, syscall.SIGTERM)
	deadline := time.Now().Add(termTimeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	_ = syscall.Kill(-signalTarget, syscall.SIGKILL)
	if signalTarget != pid {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	deadline = time.Now().Add(killTimeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("process %d did not exit", pid)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// Zombie PIDs still succeed kill(pid, 0) on Linux until the parent reaps
	// them. Treating them as alive makes stop/restart fail with
	// "process N did not exit" (e.g. service domain save on /service).
	if processZombie(pid) {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// processZombie reports whether pid exists as a zombie (Linux /proc).
// Non-Linux or unreadable /proc yields false (caller falls back to kill(0)).
func processZombie(pid int) bool {
	state, ok := processState(pid)
	return ok && state == 'Z'
}

// processState reads the state char from /proc/<pid>/stat ("R","S","Z",…).
func processState(pid int) (state byte, ok bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, false
	}
	return parseProcStatState(data)
}

// parseProcStatState extracts the state field from a /proc/<pid>/stat line.
// Format: pid (comm) state ... — comm may contain spaces and parentheses.
func parseProcStatState(stat []byte) (state byte, ok bool) {
	s := string(stat)
	i := strings.LastIndex(s, ") ")
	if i < 0 || i+2 >= len(s) {
		return 0, false
	}
	return s[i+2], true
}

func (m *Manager) serviceLogPath(id string) string {
	return filepath.Join(m.servicesDataDir(), "services", id+".log")
}

func serviceLogPath(id string) string {
	// Package default path (default manager / production).
	return filepath.Join(config.DataDir, "services", id+".log")
}

// EnsureServiceWorkingDir creates workingDir (including parents) when non-empty.
// Used by start and L2 doctests that isolate the mkdir contract.
func EnsureServiceWorkingDir(workingDir string) error {
	return ensureServiceWorkingDir(workingDir)
}

func ensureServiceWorkingDir(workingDir string) error {
	workingDir = strings.TrimSpace(workingDir)
	if workingDir == "" {
		return nil
	}
	if err := os.MkdirAll(workingDir, 0755); err != nil {
		return fmt.Errorf("create working directory %s: %w", workingDir, err)
	}
	return nil
}

func normalizeWorkingDir(workingDir string) string {
	workingDir = strings.TrimSpace(workingDir)
	if workingDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			workingDir = strings.TrimSpace(home)
		}
		if workingDir == "" {
			if cwd, err := os.Getwd(); err == nil {
				workingDir = cwd
			}
		}
	}
	if workingDir == "" {
		return ""
	}
	abs, err := filepath.Abs(workingDir)
	if err != nil {
		return workingDir
	}
	return abs
}

func normalizeExtraEnv(extraEnv map[string]string) map[string]string {
	if len(extraEnv) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(extraEnv))
	for key, value := range extraEnv {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		normalized[key] = value
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// buildServiceEnv builds the static PATH/env layers for a service definition:
// process env, Extra PATH prepended, Extra Env overrides, lookup-path fill-in.
// It never probes an interactive shell, so it stays cheap enough for status
// polling (buildStatusListLocked runs on every list request).
func buildServiceEnv(def ServiceDefinition) []string {
	env, _ := procenv.BuildManagedEnvWithOptions(procenv.Options{
		Extra:     def.ExtraEnv,
		ExtraPath: def.ExtraPath,
	})
	return env
}

// resolveStartEnv is the procenv entry point for real starts. It is a variable
// so tests can exercise the interactive-shell failure path without spawning a
// login shell.
var resolveStartEnv = procenv.BuildManagedEnvWithOptions

// buildServiceEnvForStart builds the env a real service start (or upgrade step)
// uses. Unlike buildServiceEnv it honors InheritInteractiveShellEnv, which runs
// `$SHELL -lic 'env -0'` once per start. A probe failure is returned as a
// human-readable message while the env stays usable: every other layer applies
// and bash -lc still reads login profiles on its own.
func buildServiceEnvForStart(def ServiceDefinition) ([]string, string) {
	env, err := resolveStartEnv(procenv.Options{
		Extra:           def.ExtraEnv,
		ExtraPath:       def.ExtraPath,
		InheritShellEnv: def.InheritInteractiveShellEnv,
	})
	if err != nil {
		return env, err.Error()
	}
	return env, ""
}

func lookupEnvValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func isValidEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, ch := range key {
		if i == 0 {
			if ch != '_' && !(ch >= 'A' && ch <= 'Z') && !(ch >= 'a' && ch <= 'z') {
				return false
			}
			continue
		}
		if ch != '_' && !(ch >= 'A' && ch <= 'Z') && !(ch >= 'a' && ch <= 'z') && !(ch >= '0' && ch <= '9') {
			return false
		}
	}
	return true
}

func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func shellQuote(s string) string {
	safe := true
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '/' || c == '.' || c == '-' || c == '_') {
			safe = false
			break
		}
	}
	if safe && s != "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func normalizeUpgradeLocalBase(localBase string) string {
	localBase = strings.TrimSpace(localBase)
	if localBase == "" {
		return ""
	}
	base := filepath.Base(localBase)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func resolveServiceUpgradeTargetPath(targetInput string, localBase string) (string, error) {
	localBase = normalizeUpgradeLocalBase(localBase)
	if localBase == "" {
		return "", fmt.Errorf("local binary basename is required")
	}

	targetInput = strings.TrimSpace(targetInput)
	if targetInput == "" {
		targetInput = localBase
	}
	if targetInput == "~" {
		targetInput = "~/" + localBase
	} else if strings.HasSuffix(targetInput, "/") {
		targetInput += localBase
	}

	if strings.HasPrefix(targetInput, "~") && targetInput != "~" && !strings.HasPrefix(targetInput, "~/") {
		return "", fmt.Errorf("unsupported upgrade target path %q: only ~ or ~/... are supported", targetInput)
	}
	if filepath.IsAbs(targetInput) {
		return filepath.Clean(targetInput), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	home = strings.TrimRight(strings.TrimSpace(home), string(filepath.Separator))
	if home == "" {
		return "", fmt.Errorf("home dir is empty")
	}
	if strings.HasPrefix(targetInput, "~/") {
		return filepath.Clean(filepath.Join(home, strings.TrimPrefix(targetInput, "~/"))), nil
	}
	return filepath.Clean(filepath.Join(home, targetInput)), nil
}

func moveServiceUpgradeFile(tmpPath string, targetPath string) error {
	tmpPath = strings.TrimSpace(tmpPath)
	targetPath = strings.TrimSpace(targetPath)
	if tmpPath == "" {
		return fmt.Errorf("temporary upload path is required")
	}
	if targetPath == "" {
		return fmt.Errorf("target path is required")
	}
	info, err := os.Stat(tmpPath)
	if err != nil {
		return fmt.Errorf("stat temporary upload path: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("temporary upload path is a directory: %s", tmpPath)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err == nil {
		return os.Chmod(targetPath, 0755)
	} else if copyErr := copyServiceUpgradeFileIntoPlace(tmpPath, targetPath); copyErr != nil {
		return fmt.Errorf("move %s to %s: rename: %v; copy fallback: %w", tmpPath, targetPath, err, copyErr)
	}
	if err := os.Remove(tmpPath); err != nil {
		return fmt.Errorf("remove temporary upload path after copy: %w", err)
	}
	return nil
}

func copyServiceUpgradeFileIntoPlace(src string, dst string) error {
	dir := filepath.Dir(dst)
	base := filepath.Base(dst)
	tmpDst := filepath.Join(dir, fmt.Sprintf(".%s.remote-agent-upgrade-%d", base, time.Now().UnixNano()))
	defer os.Remove(tmpDst)

	if err := copyServiceUpgradeFile(src, tmpDst); err != nil {
		return err
	}
	if err := os.Chmod(tmpDst, 0755); err != nil {
		return err
	}
	return os.Rename(tmpDst, dst)
}

func copyServiceUpgradeFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func handleServices(w http.ResponseWriter, r *http.Request) {
	handleServicesWith(GetDefaultManager(), w, r)
}

func handleServicesWith(manager *Manager, w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		// project_dir and all=1 are accepted but ignored; services are global.
		_ = json.NewEncoder(w).Encode(manager.List())

	case http.MethodPost, http.MethodPut:
		var req ServiceDefinition
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		restartChanged := serviceSaveShouldRestart(r)
		saved, err := manager.createOrUpdate(req, restartChanged)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		_ = json.NewEncoder(w).Encode(saved)

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}
		if err := manager.Delete(id); err != nil {
			status := http.StatusNotFound
			if errors.Is(err, ErrSystemServiceImmutable) {
				status = http.StatusBadRequest
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func serviceSaveShouldRestart(r *http.Request) bool {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("restart")))
	switch value {
	case "", "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func handleServicePresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Presets())
}

func handleStartService(w http.ResponseWriter, r *http.Request) {
	handleStartServiceWith(GetDefaultManager(), w, r)
}

func handleStartServiceWith(manager *Manager, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	status, err := manager.Start(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func handleStopService(w http.ResponseWriter, r *http.Request) {
	handleStopServiceWith(GetDefaultManager(), w, r)
}

func handleStopServiceWith(manager *Manager, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := manager.Stop(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleRestartService(w http.ResponseWriter, r *http.Request) {
	handleRestartServiceWith(GetDefaultManager(), w, r)
}

func handleRestartServiceWith(manager *Manager, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := manager.Restart(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleDisableService(w http.ResponseWriter, r *http.Request) {
	handleDisableServiceWith(GetDefaultManager(), w, r)
}

func handleDisableServiceWith(manager *Manager, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	result, err := manager.Disable(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func handleEnableService(w http.ResponseWriter, r *http.Request) {
	handleEnableServiceWith(GetDefaultManager(), w, r)
}

func handleEnableServiceWith(manager *Manager, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	result, err := manager.Enable(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func handleUpgradeService(w http.ResponseWriter, r *http.Request) {
	handleUpgradeServiceWith(GetDefaultManager(), w, r)
}

func handleUpgradeServiceWith(manager *Manager, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req ServiceUpgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	result, err := manager.Upgrade(req)
	if err != nil {
		http.Error(w, err.Error(), upgradeErrorStatus(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// handleUpgradeServiceStreamWith streams an upgrade as SSE: one section frame
// per phase, verbatim log frames for step output, then a summary and done.
func handleUpgradeServiceStreamWith(manager *Manager, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req ServiceUpgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	pw := progress.NewWriter(w)
	if pw == nil {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Step output is forwarded from the command's pipe-copying goroutine, so
	// frame writes are serialized here.
	var emitMu sync.Mutex
	emit := func(event UpgradeEvent) {
		emitMu.Lock()
		defer emitMu.Unlock()
		switch event.Kind {
		case UpgradeEventSection:
			_ = pw.EmitSection(event.Message)
		case UpgradeEventLog:
			_ = pw.EmitLog(event.Message, true)
		}
	}

	result, err := manager.UpgradeWithEmitter(req, emit)
	if err != nil {
		emitMu.Lock()
		defer emitMu.Unlock()
		_ = pw.EmitError(err.Error())
		return
	}

	emitMu.Lock()
	defer emitMu.Unlock()
	for _, line := range upgradeSummaryLines(result) {
		_ = pw.EmitLog(line, true)
	}
	summary := map[string]any{
		"status":  result.Status,
		"steps":   len(result.Steps),
		"tmpPath": result.TmpPath,
	}
	if result.TargetPath != "" {
		summary["targetPath"] = result.TargetPath
	}
	if result.Service != nil {
		summary["service"] = result.Service
		summary["pid"] = result.Service.PID
	}
	_ = pw.EmitDone(summary)
}

// upgradeErrorStatus maps an upgrade failure to an HTTP status: validation
// problems stay 400, while a valid request whose pipeline failed is a conflict.
func upgradeErrorStatus(err error) int {
	var upgradeErr *UpgradeError
	if errors.As(err, &upgradeErr) {
		return http.StatusConflict
	}
	return http.StatusBadRequest
}
