package wsclient

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/xhd2015/ai-critic/server/proxy/wsproxy"
)

// xrayConfigFor renders the xray config for one client: HTTP + SOCKS inbounds
// both routing to the VMess/WS outbound. It mirrors the CLI's sidecar config so
// a hand-started client and a service-managed one behave identically.
func xrayConfigFor(p *VMessParams, httpPort, socksPort int) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("nil vmess params")
	}
	if p.Host == "" || p.UUID == "" {
		return nil, fmt.Errorf("vmess params are missing host or id")
	}
	port := p.Port
	if port == "" {
		port = "443"
	}
	network := p.Network
	if network == "" {
		network = "ws"
	}
	path := p.Path
	if path == "" {
		path = "/ws"
	}
	security := "tls"
	if p.TLS == "" || jequals(p.TLS, "none") {
		security = "none"
	}
	sni := p.SNI
	if sni == "" {
		sni = p.Host
	}

	stream := map[string]any{
		"network": network,
		"wsSettings": map[string]any{
			"path":    path,
			"headers": map[string]string{"Host": p.Host},
		},
	}
	if security == "tls" {
		stream["security"] = "tls"
		stream["tlsSettings"] = map[string]any{"serverName": sni}
	} else {
		stream["security"] = "none"
	}

	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []map[string]any{
			{
				"tag":      "http-in",
				"listen":   "127.0.0.1",
				"port":     httpPort,
				"protocol": "http",
				"settings": map[string]any{},
			},
			{
				"tag":      "socks-in",
				"listen":   "127.0.0.1",
				"port":     socksPort,
				"protocol": "socks",
				"settings": map[string]any{"udp": true, "auth": "noauth"},
			},
		},
		"outbounds": []map[string]any{
			{
				"tag":      "vmess-ws",
				"protocol": "vmess",
				"settings": map[string]any{
					"vnext": []map[string]any{{
						"address": p.Host,
						"port":    portOf(port),
						"users": []map[string]any{{
							"id":       p.UUID,
							"alterId":  0,
							"security": "auto",
						}},
					}},
				},
				"streamSettings": stream,
			},
			{"tag": "direct", "protocol": "freedom", "settings": map[string]any{}},
		},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []map[string]any{
				{"type": "field", "network": "tcp,udp", "outboundTag": "vmess-ws"},
			},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// portOf keeps the port numeric in JSON when it is a plain number, matching the
// xray configs this repo already writes.
func portOf(port string) any {
	var n int
	if _, err := fmt.Sscanf(port, "%d", &n); err == nil && fmt.Sprintf("%d", n) == port {
		return n
	}
	return port
}

func jequals(v, want string) bool {
	if len(v) != len(want) {
		return false
	}
	for i := 0; i < len(v); i++ {
		a, b := v[i], want[i]
		if 'A' <= a && a <= 'Z' {
			a += 'a' - 'A'
		}
		if 'A' <= b && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

// xrayConfigPath is per client so several clients can coexist.
func xrayConfigPath(id string) string {
	dir := wsproxy.XrayDir()
	if _testConfigDir != "" {
		return filepath.Join(_testConfigDir, "ws-client-"+id+".json")
	}
	return filepath.Join(dir, "ws-client-"+id+".json")
}

// writeXrayConfig writes the rendered config, 0600 since it carries the UUID.
func writeXrayConfig(id string, data []byte) error {
	path := xrayConfigPath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create xray dir: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// ensureXray resolves the xray binary: the shared downloaded copy first, then
// one already on PATH, and only then a download. Preferring the cached copy keeps
// client and server on the same build; the PATH fallback means a machine that
// already installed xray (brew, apt) works offline.
func ensureXray() (string, error) {
	if path := filepath.Join(wsproxy.XrayDir(), xrayBinaryName); fileExists(path) {
		return path, nil
	}
	if path, err := exec.LookPath(xrayBinaryName); err == nil {
		return path, nil
	}
	return wsproxy.EnsureXrayBinary()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
