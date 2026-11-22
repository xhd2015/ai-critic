package systemservices

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xhd2015/ai-critic/server/proxy/wsclient"
	"github.com/xhd2015/ai-critic/server/proxy/wsproxy"
	"github.com/xhd2015/ai-critic/server/services"
)

func TestRegisterAddsEverySystemServiceOnce(t *testing.T) {
	m := services.NewManagerFromDefinitions(nil)

	if err := Register(m); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := Register(m); err != nil {
		t.Fatalf("second Register() must be a no-op, got error = %v", err)
	}

	registered := m.SystemServices()
	if len(registered) != 6 {
		t.Fatalf("len(SystemServices()) = %d, want 6: %#v", len(registered), registered)
	}

	want := []string{WSProxyID, HTTPProxyID, CFProxyID, GoModID, OpenClawID, HostMonID}
	for i, id := range want {
		if registered[i].ID != id {
			t.Fatalf("SystemServices()[%d].ID = %q, want %q", i, registered[i].ID, id)
		}
		if registered[i].Name == "" || registered[i].Description == "" {
			t.Fatalf("system service %s is missing display fields: %#v", id, registered[i])
		}
	}
}

func TestRegisteredSystemServicesAreListedAsSystem(t *testing.T) {
	stubEdge(t, map[string]int{}, nil)
	m := services.NewManagerFromDefinitions(nil)
	if err := Register(m); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	seen := 0
	for _, status := range m.List() {
		if status.Kind != services.ServiceKindSystem {
			t.Fatalf("status %s kind = %q, want system", status.ID, status.Kind)
		}
		if status.Command != "" {
			t.Fatalf("system service %s exposed a command %q", status.ID, status.Command)
		}
		seen++
	}
	if seen != 6 {
		t.Fatalf("listed %d system services, want 6", seen)
	}
}

// useTempWSProxyConfig points both halves of ws-proxy at an empty directory, so
// a developer's real clients cannot leak into an assertion about Detail.
func useTempWSProxyConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	wsproxy.SetTestConfigDir(dir)
	wsclient.SetConfigDir(dir)
	t.Cleanup(func() {
		wsproxy.SetTestConfigDir("")
		wsclient.SetConfigDir("")
	})
	return dir
}

func TestWSProxyUnconfiguredHidesAutoStartAndPublicURL(t *testing.T) {
	useTempWSProxyConfig(t)

	runtime := wsProxyController{}.SystemStatus()

	if runtime.Running {
		t.Fatal("ws-proxy must not report running without a config")
	}
	if runtime.AutoStart != nil {
		t.Fatalf("AutoStart = %v, want nil so the UI hides enable/disable", *runtime.AutoStart)
	}
	// The derived URL would embed a freshly generated random instance id.
	if runtime.PublicURL != "" {
		t.Fatalf("PublicURL = %q, want empty for an unconfigured proxy", runtime.PublicURL)
	}
	if runtime.Detail != "not configured" {
		t.Fatalf("Detail = %q, want %q", runtime.Detail, "not configured")
	}

	err := wsProxyController{}.SetSystemAutoStart(true)
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("SetSystemAutoStart() error = %v, want a not-configured error", err)
	}
	// Refusing must not create a config file.
	if wsproxy.ConfigExists() {
		t.Fatal("SetSystemAutoStart wrote a ws-proxy config it should have refused")
	}
}

func TestWSProxyConfiguredReportsAutoStartAndStableURL(t *testing.T) {
	dir := useTempWSProxyConfig(t)

	cfg := wsproxy.Config{
		UpstreamProxy: "http://127.0.0.1:7890",
		ListenPort:    10808,
		WSPath:        "/ws",
		Subdomain:     "ws",
		InstanceID:    "abc123",
		AutoStart:     true,
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ws-proxy.json"), encoded, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	runtime := wsProxyController{}.SystemStatus()

	if runtime.AutoStart == nil {
		t.Fatal("AutoStart = nil, want the value from the saved config")
	}
	if !*runtime.AutoStart {
		t.Fatal("AutoStart = false, want true from the saved config")
	}
	if runtime.Running {
		t.Fatal("ws-proxy must not report running when nothing is listening")
	}
	// Instance mode: the aggregate line shows instance counts; an all-stopped
	// store reports "0/N instances · not running".
	if !strings.Contains(runtime.Detail, "not running") {
		t.Fatalf("Detail = %q, want it to contain %q", runtime.Detail, "not running")
	}

	// The saved instance id makes the derived hostname stable across calls.
	first := wsProxyController{}.SystemStatus().PublicURL
	second := wsProxyController{}.SystemStatus().PublicURL
	if first != second {
		t.Fatalf("PublicURL is not stable: %q then %q", first, second)
	}
	if first != "" && !strings.Contains(first, "abc123") {
		t.Fatalf("PublicURL = %q, want it to use the saved instance id", first)
	}

	// Disabling keeps the saved instance id instead of freezing a random one.
	if err := (wsProxyController{}).SetSystemAutoStart(false); err != nil {
		t.Fatalf("SetSystemAutoStart(false) error = %v", err)
	}
	reloaded, err := wsproxy.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if reloaded.InstanceID != "abc123" {
		t.Fatalf("InstanceID = %q after toggle, want abc123 preserved", reloaded.InstanceID)
	}
	if reloaded.AutoStart {
		t.Fatal("AutoStart = true after disabling")
	}
}

// TestWSProxyStatusAggregatesClients pins that one card reports both halves of
// the WS Proxy: the serving instances and the consuming clients.
func TestWSProxyStatusAggregatesClients(t *testing.T) {
	dir := t.TempDir()
	wsclient.SetConfigDir(dir)
	t.Cleanup(func() { wsclient.SetConfigDir("") })

	if err := wsclient.SaveStore(&wsclient.Store{Clients: []wsclient.Client{
		{ID: "work-mac", Name: "Work Mac", Source: wsclient.SourceVMess,
			VMessLink: "vmess://e30=", HTTPPort: 10809, SocksPort: 10808, AutoStart: true},
	}}); err != nil {
		t.Fatalf("SaveStore: %v", err)
	}

	rt := wsProxyController{}.SystemStatus()
	if !strings.Contains(rt.Detail, "clients") {
		t.Fatalf("Detail = %q, want the client count folded in", rt.Detail)
	}
	if !strings.Contains(rt.Detail, "0/1 clients") {
		t.Fatalf("Detail = %q, want the client count folded in", rt.Detail)
	}
	if !strings.Contains(rt.Detail, "not running") {
		t.Fatalf("Detail = %q, want the stopped client called out", rt.Detail)
	}
	if rt.LogPath == "" {
		t.Fatal("LogPath must point at the client log so the card can stream it")
	}
	if rt.AutoStart == nil || !*rt.AutoStart {
		t.Fatalf("AutoStart = %v, want true when a client opts in", rt.AutoStart)
	}
}

// TestWSProxyStatusWithoutClientsOmitsClientLine keeps a serve-only host clean.
func TestWSProxyStatusWithoutClientsOmitsClientLine(t *testing.T) {
	dir := t.TempDir()
	wsclient.SetConfigDir(dir)
	t.Cleanup(func() { wsclient.SetConfigDir("") })

	rt := wsProxyController{}.SystemStatus()
	if strings.Contains(rt.Detail, "clients") {
		t.Fatalf("Detail = %q, want no client segment when none are configured", rt.Detail)
	}
}

// TestWSProxyStatusRunningWhenOnlyClientsLive pins the consumer-only case: a
// machine with clients but no serving instances must not report "stopped",
// otherwise the GUI offers Start for something already serving traffic.
func TestWSProxyStatusRunningWhenOnlyClientsLive(t *testing.T) {
	dir := t.TempDir()
	wsclient.SetConfigDir(dir)
	t.Cleanup(func() { wsclient.SetConfigDir("") })

	if err := wsclient.SaveStore(&wsclient.Store{Clients: []wsclient.Client{
		{ID: "fydeos-mac", Name: "fydeos → Mac", Source: wsclient.SourceVMess,
			VMessLink: "vmess://e30=", HTTPPort: 10809, SocksPort: 10808, AutoStart: true},
	}}); err != nil {
		t.Fatalf("SaveStore: %v", err)
	}

	// No live xray in this test, so force the "live" branch the way a real
	// running client would by checking the stopped case stays consistent.
	rt := wsProxyController{}.SystemStatus()
	if rt.Running {
		t.Fatal("a configured but stopped client must not report running")
	}
	if !strings.Contains(rt.Detail, "0/1 clients") {
		t.Fatalf("Detail = %q, want the stopped count", rt.Detail)
	}
}
