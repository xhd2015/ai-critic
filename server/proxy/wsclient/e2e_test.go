package wsclient

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestEndToEndTunnelThroughLocalXray proves the whole client path with a real
// xray on both ends: the client's HTTP inbound tunnels VMess/WS to a local
// server, whose freedom outbound reaches the test HTTP server. Skipped when no
// xray binary is available (CI without the download) or under -short.
func TestEndToEndTunnelThroughLocalXray(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end tunnel needs a real xray binary")
	}
	// Resolve only an already-present binary: a download would make the test
	// slow and network-dependent, so a machine without xray skips instead.
	xrayBin, err := ensureXrayQuiet()
	if err != nil {
		if found, lookErr := exec.LookPath(xrayBinaryName); lookErr == nil {
			xrayBin = found
		} else {
			t.Skip("no xray binary in cache or PATH")
		}
	}

	serverPort := freePort(t)
	wsPath := "/ws"
	uuid := "8b7c1f52-1111-4a2b-9c3d-000000000001"

	// Server side: VMess + WebSocket inbound, freedom outbound.
	serverCfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []map[string]any{{
			"listen": "127.0.0.1", "port": serverPort, "protocol": "vmess",
			"settings": map[string]any{
				"clients": []map[string]any{{"id": uuid, "alterId": 0}},
			},
			"streamSettings": map[string]any{
				"network":    "ws",
				"wsSettings": map[string]any{"path": wsPath},
			},
		}},
		"outbounds": []map[string]any{{"protocol": "freedom"}},
	}
	serverCfgPath := filepath.Join(t.TempDir(), "server.json")
	writeJSONFile(t, serverCfgPath, serverCfg)

	serverCmd := exec.Command(xrayBin, "run", "-c", serverCfgPath)
	serverCmd.Stdout, serverCmd.Stderr = os.Stderr, os.Stderr
	if err := serverCmd.Start(); err != nil {
		t.Fatalf("start server xray: %v", err)
	}
	t.Cleanup(func() {
		_ = serverCmd.Process.Kill()
		_, _ = serverCmd.Process.Wait()
	})
	waitForPort(t, serverPort, 15*time.Second)

	// Origin the tunnel should reach.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("through-the-tunnel"))
	}))
	defer origin.Close()

	// Client side: vmess link pointing at the local server.
	linkBody, _ := json.Marshal(map[string]any{
		"v": "2", "ps": "local", "add": "127.0.0.1", "port": serverPort,
		"id": uuid, "aid": 0, "net": "ws", "type": "none",
		"host": "127.0.0.1", "path": wsPath, "tls": "none",
	})
	link := "vmess://" + base64.StdEncoding.EncodeToString(linkBody)

	SetConfigDir(t.TempDir())
	t.Cleanup(func() { SetConfigDir("") })

	httpPort := freePort(t)
	socksPort := freePort(t)
	c := Client{
		ID: "e2e", Name: "e2e", Source: SourceVMess, VMessLink: link,
		HTTPPort: httpPort, SocksPort: socksPort,
	}
	if err := GetManager().Start(c, func(msg string) { t.Log(msg) }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = GetManager().Stop("e2e") })

	if !GetManager().StatusOf(c).Running {
		t.Fatal("StatusOf must report running after Start")
	}

	if err := ProbeEgress(httpPort, origin.URL); err != nil {
		t.Fatalf("ProbeEgress: %v", err)
	}

	// A second Start must not spawn another process.
	before := GetManager().StatusOf(c).PID
	if err := GetManager().Start(c, nil); err != nil {
		t.Fatalf("second Start = %v, want nil (idempotent)", err)
	}
	if after := GetManager().StatusOf(c).PID; after != before {
		t.Fatalf("pid changed %d → %d; Start spawned a second xray", before, after)
	}

	if err := GetManager().Stop("e2e"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if GetManager().StatusOf(c).Running {
		t.Fatal("client still running after Stop")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForPort(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("port %d never opened within %s", port, timeout)
}

func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
