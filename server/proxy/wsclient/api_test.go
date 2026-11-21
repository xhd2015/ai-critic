package wsclient

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) (*http.ServeMux, func()) {
	t.Helper()
	dir := t.TempDir()
	SetConfigDir(dir)
	mux := http.NewServeMux()
	RegisterAPI(mux)
	return mux, func() { SetConfigDir("") }
}

func do(t *testing.T, mux *http.ServeMux, method, target, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

func TestAPICreateListAndTokenNeverEchoed(t *testing.T) {
	mux, done := newTestServer(t)
	defer done()

	w, _ := do(t, mux, http.MethodPost, "/api/ws-proxy/clients",
		`{"id":"work-mac","name":"Work Mac","source":"remote","base_url":"https://mac.example.com","instance":"ws-a","token":"super-secret","http_port":10809,"socks_port":10808}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "super-secret") {
		t.Fatal("create response must not echo the token")
	}

	w, out := do(t, mux, http.MethodGet, "/api/ws-proxy/clients", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "super-secret") {
		t.Fatal("list response must not echo the token")
	}
	clients, _ := out["clients"].([]any)
	if len(clients) != 1 {
		t.Fatalf("clients = %d, want 1", len(clients))
	}
	first, _ := clients[0].(map[string]any)
	if first["has_token"] != true {
		t.Fatalf("has_token = %v, want true", first["has_token"])
	}
}

func TestAPICreateRejectsDuplicateAndPortClash(t *testing.T) {
	mux, done := newTestServer(t)
	defer done()

	body := `{"id":"a","name":"A","source":"vmess","vmess_link":"vmess://e30=","http_port":10809,"socks_port":10808}`
	if w, _ := do(t, mux, http.MethodPost, "/api/ws-proxy/clients", body); w.Code != http.StatusOK {
		t.Fatalf("first create = %d: %s", w.Code, w.Body.String())
	}
	if w, _ := do(t, mux, http.MethodPost, "/api/ws-proxy/clients", body); w.Code != http.StatusConflict {
		t.Fatalf("duplicate create = %d, want 409", w.Code)
	}

	clash := `{"id":"b","name":"B","source":"vmess","vmess_link":"vmess://e30=","http_port":10809,"socks_port":20808}`
	if w, _ := do(t, mux, http.MethodPost, "/api/ws-proxy/clients", clash); w.Code != http.StatusConflict {
		t.Fatalf("port clash create = %d, want 409: %s", w.Code, w.Body.String())
	}
}

func TestAPIUpdateKeepsOmittedToken(t *testing.T) {
	mux, done := newTestServer(t)
	defer done()

	do(t, mux, http.MethodPost, "/api/ws-proxy/clients",
		`{"id":"a","name":"A","source":"remote","base_url":"https://x.example.com","token":"keep-me","http_port":10809,"socks_port":10808}`)

	// An edit that does not retype the token must not erase it.
	w, _ := do(t, mux, http.MethodPut, "/api/ws-proxy/clients/a", `{"name":"A renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", w.Code, w.Body.String())
	}
	st, err := LoadStore()
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	c := st.Find("a")
	if c == nil {
		t.Fatal("client a disappeared")
	}
	if c.Token != "keep-me" {
		t.Fatalf("Token = %q, want it preserved", c.Token)
	}
	if c.Name != "A renamed" {
		t.Fatalf("Name = %q, want renamed", c.Name)
	}
	if c.HTTPPort != 10809 {
		t.Fatalf("HTTPPort = %d, want it preserved", c.HTTPPort)
	}
}

func TestAPIDelete(t *testing.T) {
	mux, done := newTestServer(t)
	defer done()

	do(t, mux, http.MethodPost, "/api/ws-proxy/clients",
		`{"id":"a","name":"A","source":"vmess","vmess_link":"vmess://e30=","http_port":10809,"socks_port":10808}`)

	if w, _ := do(t, mux, http.MethodDelete, "/api/ws-proxy/clients/a", ""); w.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", w.Code, w.Body.String())
	}
	if w, _ := do(t, mux, http.MethodDelete, "/api/ws-proxy/clients/a", ""); w.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d, want 404", w.Code)
	}
}

func TestAPIPACEndpoint(t *testing.T) {
	mux, done := newTestServer(t)
	defer done()

	do(t, mux, http.MethodPost, "/api/ws-proxy/clients",
		`{"id":"a","name":"A","source":"vmess","vmess_link":"vmess://e30=","http_port":10809,"socks_port":10808,"pac_domains":["example.com"]}`)

	w, _ := do(t, mux, http.MethodGet, "/api/ws-proxy/clients/a/pac?fallback=proxy.internal:8897", "")
	if w.Code != http.StatusOK {
		t.Fatalf("pac = %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "proxy-autoconfig") {
		t.Fatalf("Content-Type = %q, want the PAC mime type", ct)
	}
	for _, want := range []string{"PROXY 127.0.0.1:10809", `"example.com"`, `"PROXY proxy.internal:8897"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("PAC missing %q:\n%s", want, body)
		}
	}
}

func TestAPIUnknownClientIs404(t *testing.T) {
	mux, done := newTestServer(t)
	defer done()

	for _, tc := range []struct{ method, target string }{
		{http.MethodPost, "/api/ws-proxy/clients/nope/start"},
		{http.MethodPost, "/api/ws-proxy/clients/nope/stop"},
		{http.MethodGet, "/api/ws-proxy/clients/nope/pac"},
		{http.MethodDelete, "/api/ws-proxy/clients/nope"},
	} {
		if w, _ := do(t, mux, tc.method, tc.target, ""); w.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404", tc.method, tc.target, w.Code)
		}
	}
}

// TestManagerStopIsIdempotent: stopping a client that never started must be a
// no-op, not an error, so Delete and StopAll stay safe.
func TestManagerStopIsIdempotent(t *testing.T) {
	SetConfigDir(t.TempDir())
	t.Cleanup(func() { SetConfigDir("") })

	m := GetManager()
	if err := m.Stop("never-started"); err != nil {
		t.Fatalf("Stop on unknown client = %v, want nil", err)
	}
	m.StopAll()
}

// TestStatusOfStoppedClient reports config fields even when no process exists.
func TestStatusOfStoppedClient(t *testing.T) {
	SetConfigDir(t.TempDir())
	t.Cleanup(func() { SetConfigDir("") })

	c := Client{ID: "a", Name: "A", Source: SourceVMess, VMessLink: "vmess://e30=", AutoStart: true}
	rs := GetManager().StatusOf(c)
	if rs.Running {
		t.Fatal("a fresh client must not report running")
	}
	if rs.HTTPPort != defaultHTTPPort || rs.SocksPort != defaultSocksPort {
		t.Fatalf("status ports = %d/%d, want defaults", rs.HTTPPort, rs.SocksPort)
	}
	if !rs.AutoStart {
		t.Fatal("AutoStart must survive normalization")
	}
}

// TestStartFailsOnOccupiedPort proves the failure is reported before any xray
// process is spawned.
func TestStartFailsOnOccupiedPort(t *testing.T) {
	SetConfigDir(t.TempDir())
	t.Cleanup(func() { SetConfigDir("") })

	ln, port, err := listenEphemeral()
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// A vmess source avoids a network fetch, so the only possible failure is
	// the port check.
	c := Client{
		ID: "a", Name: "A", Source: SourceVMess,
		VMessLink: "vmess://" + testVMessLinkB64(),
		HTTPPort:  port, SocksPort: port + 1,
	}
	err = GetManager().Start(c, nil)
	if err == nil {
		t.Fatalf("Start on occupied port %d = nil, want failure", port)
	}
	if !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("error = %v, want an 'already in use' message", err)
	}
}

// listenEphemeral returns a listener bound to a free 127.0.0.1 port.
func listenEphemeral() (net.Listener, int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	return ln, ln.Addr().(*net.TCPAddr).Port, nil
}

// testVMessLinkB64 is a minimal decodable vmess link body.
func testVMessLinkB64() string {
	body := `{"add":"ws.example.com","port":443,"id":"uuid","net":"ws","path":"/ws","tls":"tls"}`
	return base64.StdEncoding.EncodeToString([]byte(body))
}

func TestProbeFromQuery(t *testing.T) {
	tests := []struct{ raw, want string }{
		{"", defaultProbeURL},
		{"-", ""},
		{"https://x.example.com", "https://x.example.com"},
	}
	for _, tc := range tests {
		r := httptest.NewRequest(http.MethodPost, "/?probe="+tc.raw, nil)
		if got := probeFromQuery(r); got != tc.want {
			t.Fatalf("probeFromQuery(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
