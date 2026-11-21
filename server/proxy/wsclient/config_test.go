package wsclient

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestClientNormalizedFillsDefaults(t *testing.T) {
	c := Client{Name: "Work Mac", BaseURL: "https://mac.example.com/"}
	c.Normalized()

	if c.ID != "work-mac" {
		t.Fatalf("ID = %q, want work-mac", c.ID)
	}
	if c.BaseURL != "https://mac.example.com" {
		t.Fatalf("BaseURL = %q, want trailing slash trimmed", c.BaseURL)
	}
	if c.Source != SourceRemote {
		t.Fatalf("Source = %q, want remote", c.Source)
	}
	if c.HTTPPort != defaultHTTPPort || c.SocksPort != defaultSocksPort {
		t.Fatalf("ports = %d/%d, want %d/%d", c.HTTPPort, c.SocksPort, defaultHTTPPort, defaultSocksPort)
	}
}

func TestClientValidate(t *testing.T) {
	base := func() Client {
		c := Client{ID: "c1", Name: "c1", Source: SourceRemote, BaseURL: "https://x.example.com"}
		c.Normalized()
		return c
	}

	tests := []struct {
		name    string
		mutate  func(*Client)
		wantErr string
	}{
		{"valid remote", func(*Client) {}, ""},
		{"missing base url", func(c *Client) { c.BaseURL = "" }, "base_url is required"},
		{"bad scheme", func(c *Client) { c.BaseURL = "ftp://x" }, "http:// or https://"},
		{"vmess without link", func(c *Client) { c.Source = SourceVMess }, "vmess_link is required"},
		{"unknown source", func(c *Client) { c.Source = "carrier-pigeon" }, "unknown source"},
		{"same ports", func(c *Client) { c.SocksPort = c.HTTPPort }, "must differ"},
		{"port out of range", func(c *Client) { c.HTTPPort = 70000 }, "between 1 and 65535"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mutate(&c)
			err := c.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	SetConfigDir(dir)
	t.Cleanup(func() { SetConfigDir("") })

	st := &Store{Clients: []Client{
		{ID: "a", Name: "A", Source: SourceRemote, BaseURL: "https://a.example.com", Token: "secret-a", AutoStart: true,
			PacDomains: []string{"Example.COM", " example.org "}},
		{ID: "b", Name: "B", Source: SourceVMess, VMessLink: "vmess://x"},
	}}
	if err := SaveStore(st); err != nil {
		t.Fatalf("SaveStore: %v", err)
	}

	info, err := os.Stat(configPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("config perm = %o, want 600 (it holds server tokens)", perm)
	}

	got, err := LoadStore()
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if len(got.Clients) != 2 {
		t.Fatalf("len = %d, want 2", len(got.Clients))
	}
	if got.Clients[0].Token != "secret-a" || !got.Clients[0].AutoStart {
		t.Fatalf("client a round-trip lost fields: %#v", got.Clients[0])
	}
	if want := []string{"example.com", "example.org"}; strings.Join(got.Clients[0].PacDomains, ",") != strings.Join(want, ",") {
		t.Fatalf("PacDomains = %v, want %v", got.Clients[0].PacDomains, want)
	}
	if got.Clients[1].HTTPPort != defaultHTTPPort {
		t.Fatalf("defaults not applied on load: %#v", got.Clients[1])
	}
}

func TestLoadStoreMissingFileIsEmpty(t *testing.T) {
	SetConfigDir(t.TempDir())
	t.Cleanup(func() { SetConfigDir("") })

	st, err := LoadStore()
	if err != nil {
		t.Fatalf("LoadStore on missing file = %v, want nil", err)
	}
	if len(st.Clients) != 0 {
		t.Fatalf("len = %d, want 0", len(st.Clients))
	}
}

func TestSlug(t *testing.T) {
	tests := map[string]string{
		"Work Mac":      "work-mac",
		"  a  b  ":      "a-b",
		"already-slug":  "already-slug",
		"UPPER_case-99": "upper-case-99",
		"→→→":           "",
	}
	for in, want := range tests {
		if got := slug(in); got != want {
			t.Fatalf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseVMessLink(t *testing.T) {
	body := map[string]any{
		"v": "2", "ps": "ws-on-mac", "add": "ws.example.com", "port": 443,
		"id": "279037c1-2583-4839-bfc4-31a34d14c591", "aid": 0,
		"net": "ws", "type": "none", "host": "ws.example.com", "path": "/ws", "tls": "tls",
	}
	raw, _ := json.Marshal(body)
	link := "vmess://" + base64.StdEncoding.EncodeToString(raw)

	p, err := parseVMessLink(link)
	if err != nil {
		t.Fatalf("parseVMessLink: %v", err)
	}
	if p.Host != "ws.example.com" || p.Port != "443" || p.Path != "/ws" || p.TLS != "tls" {
		t.Fatalf("parsed = %#v", p)
	}
	if p.SNI != "ws.example.com" {
		t.Fatalf("SNI = %q, want host fallback", p.SNI)
	}
}

func TestParseVMessLinkNumericPortAndDefaults(t *testing.T) {
	// port as a JSON number and no path/tls: defaults must fill in.
	body := `{"add":"h.example.com","port":8443,"id":"uuid","net":"ws"}`
	link := "vmess://" + base64.StdEncoding.EncodeToString([]byte(body))

	p, err := parseVMessLink(link)
	if err != nil {
		t.Fatalf("parseVMessLink: %v", err)
	}
	if p.Port != "8443" {
		t.Fatalf("Port = %q, want 8443", p.Port)
	}
	if p.Path != "/ws" {
		t.Fatalf("Path = %q, want /ws default", p.Path)
	}
}

func TestParseVMessLinkErrors(t *testing.T) {
	tests := map[string]string{
		"empty":       "vmess://",
		"not base64":  "vmess://!!!!",
		"not json":    "vmess://" + base64.StdEncoding.EncodeToString([]byte("hello")),
		"missing id":  "vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"h","port":1}`)),
		"missing add": "vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"port":1,"id":"u"}`)),
	}
	for name, link := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseVMessLink(link); err == nil {
				t.Fatalf("parseVMessLink(%q) = nil error, want failure", link)
			}
		})
	}
}

func TestXrayConfigFor(t *testing.T) {
	p := &VMessParams{Host: "ws.example.com", Port: "443", UUID: "uuid-1", Network: "ws", Path: "/ws", TLS: "tls"}
	data, err := xrayConfigFor(p, 10809, 10808)
	if err != nil {
		t.Fatalf("xrayConfigFor: %v", err)
	}
	var cfg struct {
		Inbounds []struct {
			Tag      string `json:"tag"`
			Port     int    `json:"port"`
			Protocol string `json:"protocol"`
		} `json:"inbounds"`
		Outbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("config is not JSON: %v\n%s", err, data)
	}
	if len(cfg.Inbounds) != 2 {
		t.Fatalf("inbounds = %d, want 2 (http + socks)", len(cfg.Inbounds))
	}
	if cfg.Inbounds[0].Protocol != "http" || cfg.Inbounds[0].Port != 10809 {
		t.Fatalf("first inbound = %#v, want http on 10809", cfg.Inbounds[0])
	}
	if cfg.Inbounds[1].Protocol != "socks" || cfg.Inbounds[1].Port != 10808 {
		t.Fatalf("second inbound = %#v, want socks on 10808", cfg.Inbounds[1])
	}
	if cfg.Outbounds[0].Protocol != "vmess" {
		t.Fatalf("first outbound = %#v, want vmess", cfg.Outbounds[0])
	}
	if !strings.Contains(string(data), `"serverName": "ws.example.com"`) {
		t.Fatalf("tls serverName missing:\n%s", data)
	}
}

func TestXrayConfigForRejectsIncomplete(t *testing.T) {
	if _, err := xrayConfigFor(nil, 1, 2); err == nil {
		t.Fatal("nil params must fail")
	}
	if _, err := xrayConfigFor(&VMessParams{Host: ""}, 1, 2); err == nil {
		t.Fatal("missing host must fail")
	}
}

// TestRenderPAC pins the generated script's shape: tunnel domains, explicit
// fallback (PAC has no "system proxy" value), and a suffix match that rejects
// lookalike hosts.
func TestRenderPAC(t *testing.T) {
	script := RenderPAC(10809, 10808, []string{"example.com", "example.org"}, "proxy.internal:8897")

	for _, want := range []string{
		`var TUNNEL_PROXY = "PROXY 127.0.0.1:10809; SOCKS5 127.0.0.1:10808; DIRECT";`,
		`var FALLBACK_PROXY = "PROXY proxy.internal:8897";`,
		`"example.com", "example.org"`,
		"function FindProxyForURL(url, host)",
		"function inDomains(host, domains)",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("PAC missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "dnsResolve") || strings.Contains(script, "isInNet") {
		t.Fatalf("PAC must not resolve DNS locally:\n%s", script)
	}
}

func TestRenderPACFallbackDefaultsToDirect(t *testing.T) {
	script := RenderPAC(10809, 0, nil, "")
	if !strings.Contains(script, `var FALLBACK_PROXY = "DIRECT";`) {
		t.Fatalf("empty fallback must be DIRECT:\n%s", script)
	}
	if strings.Contains(script, "SOCKS5") {
		t.Fatalf("socks port 0 must omit the SOCKS5 line:\n%s", script)
	}
}

func TestRenderPACAcceptsBareHostPort(t *testing.T) {
	script := RenderPAC(10809, 10808, nil, "127.0.0.1:8897")
	if !strings.Contains(script, `var FALLBACK_PROXY = "PROXY 127.0.0.1:8897";`) {
		t.Fatalf("bare host:port must gain the PROXY prefix:\n%s", script)
	}
}

func TestXrayConfigPathPerClient(t *testing.T) {
	SetConfigDir(t.TempDir())
	t.Cleanup(func() { SetConfigDir("") })

	a, b := xrayConfigPath("one"), xrayConfigPath("two")
	if a == b {
		t.Fatalf("config paths collide: %q", a)
	}
	if filepath.Base(a) != "ws-client-one.json" {
		t.Fatalf("path = %q, want ws-client-one.json", a)
	}
}

func TestEnsurePortFree(t *testing.T) {
	// Occupy a port, then confirm the check reports it and names the port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	err = ensurePortFree(port)
	if err == nil {
		t.Fatalf("ensurePortFree(%d) = nil, want failure", port)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Fatalf("error must name the port: %v", err)
	}

	// A free port must pass.
	other, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	free := other.Addr().(*net.TCPAddr).Port
	other.Close()
	if err := ensurePortFree(free); err != nil {
		t.Fatalf("ensurePortFree(free %d) = %v, want nil", free, err)
	}
}
