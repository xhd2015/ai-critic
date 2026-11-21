package wsclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// defaultProbeURL is the egress check used by streamed starts. It is a public
// host so the probe exercises the tunnel end to end, not a local shortcut.
const defaultProbeURL = "https://example.com"

// RegisterAPI mounts the client CRUD and lifecycle endpoints. They live under
// the same /api/ws-proxy prefix as the server half, so the GUI drives both from
// one page.
func RegisterAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ws-proxy/clients", handleList)
	mux.HandleFunc("POST /api/ws-proxy/clients", handleCreate)
	mux.HandleFunc("PUT /api/ws-proxy/clients/{id}", handleUpdate)
	mux.HandleFunc("DELETE /api/ws-proxy/clients/{id}", handleDelete)
	mux.HandleFunc("POST /api/ws-proxy/clients/{id}/start", handleStart)
	mux.HandleFunc("POST /api/ws-proxy/clients/{id}/start/stream", handleStartStream)
	mux.HandleFunc("POST /api/ws-proxy/clients/{id}/stop", handleStop)
	mux.HandleFunc("POST /api/ws-proxy/clients/{id}/stop/stream", handleStopStream)
	mux.HandleFunc("GET /api/ws-proxy/clients/{id}/probe", handleProbe)
	mux.HandleFunc("GET /api/ws-proxy/clients/{id}/pac", handlePAC)
}

// clientPayload is the API's view of a client. Token is write-only: requests
// may set it, responses never echo it.
type clientPayload struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Source     string   `json:"source"`
	BaseURL    string   `json:"base_url"`
	Instance   string   `json:"instance"`
	Token      string   `json:"token,omitempty"`
	VMessLink  string   `json:"vmess_link,omitempty"`
	HTTPPort   int      `json:"http_port"`
	SocksPort  int      `json:"socks_port"`
	AutoStart  bool     `json:"auto_start"`
	PacDomains []string `json:"pac_domains,omitempty"`
	HasToken   bool     `json:"has_token,omitempty"`
}

func toPayload(c Client) clientPayload {
	return clientPayload{
		ID: c.ID, Name: c.Name, Source: string(c.Source),
		BaseURL: c.BaseURL, Instance: c.Instance,
		// The vmess link is not secret in the same way a token is, but echoing
		// it is still noise; the GUI shows it only when the user asks.
		HTTPPort: c.HTTPPort, SocksPort: c.SocksPort,
		AutoStart: c.AutoStart, PacDomains: c.PacDomains,
		HasToken: c.Token != "",
	}
}

// raw converts the payload without applying defaults, so mergeFrom can still
// tell "field omitted" apart from "field set to the default value".
func (p clientPayload) raw() Client {
	return Client{
		ID: p.ID, Name: p.Name, Source: SourceKind(p.Source),
		BaseURL: p.BaseURL, Instance: p.Instance, Token: p.Token,
		VMessLink: p.VMessLink,
		HTTPPort:  p.HTTPPort, SocksPort: p.SocksPort,
		AutoStart: p.AutoStart, PacDomains: p.PacDomains,
	}
}

func (p clientPayload) toClient() Client {
	c := p.raw()
	c.Normalized()
	return c
}

// mergeFrom keeps write-only and append-only fields when the request omits them,
// so a GUI edit that does not retype the token does not erase it. Defaults are
// applied after the merge, never before.
func (p clientPayload) mergeFrom(old Client) Client {
	c := p.raw()
	c.ID = old.ID
	if c.Token == "" {
		c.Token = old.Token
	}
	if c.VMessLink == "" {
		c.VMessLink = old.VMessLink
	}
	if c.Source == "" {
		c.Source = old.Source
	}
	if c.BaseURL == "" {
		c.BaseURL = old.BaseURL
	}
	if c.Instance == "" {
		c.Instance = old.Instance
	}
	if c.HTTPPort == 0 {
		c.HTTPPort = old.HTTPPort
	}
	if c.SocksPort == 0 {
		c.SocksPort = old.SocksPort
	}
	if c.PacDomains == nil {
		c.PacDomains = old.PacDomains
	}
	c.Normalized()
	return c
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func handleList(w http.ResponseWriter, _ *http.Request) {
	statuses, err := GetManager().Statuses()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Merge the write-only flag so the GUI can render "token set" without ever
	// receiving the token.
	st, _ := LoadStore()
	payloads := make([]clientPayload, 0, len(statuses))
	for _, rs := range statuses {
		var src Client
		if st != nil {
			if c := st.Find(rs.ID); c != nil {
				src = *c
			}
		}
		pa := toPayload(src)
		pa.ID, pa.Name = rs.ID, rs.Name
		payloads = append(payloads, pa)
	}
	writeJSON(w, map[string]any{
		"clients":  payloads,
		"statuses": statuses,
	})
}

func handleCreate(w http.ResponseWriter, r *http.Request) {
	var p clientPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c := p.toClient()
	if err := c.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := LoadStore()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if st.Find(c.ID) != nil {
		writeErr(w, http.StatusConflict, fmt.Sprintf("client %q already exists", c.ID))
		return
	}
	for _, other := range st.Clients {
		if other.HTTPPort == c.HTTPPort || other.SocksPort == c.HTTPPort ||
			other.HTTPPort == c.SocksPort || other.SocksPort == c.SocksPort {
			writeErr(w, http.StatusConflict, fmt.Sprintf("port %d or %d already used by client %q",
				c.HTTPPort, c.SocksPort, other.ID))
			return
		}
	}
	st.Clients = append(st.Clients, c)
	if err := SaveStore(st); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"client": toPayload(c)})
}

func handleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var p clientPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	st, err := LoadStore()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	old := st.Find(id)
	if old == nil {
		writeErr(w, http.StatusNotFound, "client not found: "+id)
		return
	}
	updated := p.mergeFrom(*old)
	if err := updated.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// A running client keeps serving its old config; restart is explicit.
	*old = updated
	if err := SaveStore(st); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"client": toPayload(updated), "restart_required": true})
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st, err := LoadStore()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if st.Find(id) == nil {
		writeErr(w, http.StatusNotFound, "client not found: "+id)
		return
	}
	if err := GetManager().Stop(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	kept := st.Clients[:0]
	for _, c := range st.Clients {
		if c.ID != id {
			kept = append(kept, c)
		}
	}
	st.Clients = kept
	if err := SaveStore(st); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"deleted": id})
}

func handleStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := loadClient(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if err := GetManager().StartWithProbe(*c, nil, probeFromQuery(r)); err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": GetManager().StatusOf(*c)})
}

func handleStartStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := GetManager().StreamStart(w, id, probeFromQuery(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func handleStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := loadClient(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if err := GetManager().Stop(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": GetManager().StatusOf(*c)})
}

func handleStopStream(w http.ResponseWriter, r *http.Request) {
	if err := GetManager().StreamStop(w, r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

// handleProbe runs the egress check on a running client without restarting it.
func handleProbe(w http.ResponseWriter, r *http.Request) {
	c, err := loadClient(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	target := strings.TrimSpace(r.URL.Query().Get("url"))
	if target == "" {
		target = defaultProbeURL
	}
	if err := ProbeEgress(c.HTTPPort, target); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "url": target})
}

// handlePAC renders a PAC script that sends PacDomains through this client and
// everything else to fallbackProxy (or DIRECT).
func handlePAC(w http.ResponseWriter, r *http.Request) {
	c, err := loadClient(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	fallback := strings.TrimSpace(r.URL.Query().Get("fallback"))
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	_, _ = w.Write([]byte(RenderPAC(c.HTTPPort, c.SocksPort, c.PacDomains, fallback)))
}

// probeFromQuery resolves the ?probe= parameter: absent means the default URL,
// "-" disables the egress check, anything else is the URL to fetch.
func probeFromQuery(r *http.Request) string {
	probe := strings.TrimSpace(r.URL.Query().Get("probe"))
	switch probe {
	case "-":
		return ""
	case "":
		return defaultProbeURL
	default:
		return probe
	}
}

func loadClient(id string) (*Client, error) {
	st, err := LoadStore()
	if err != nil {
		return nil, err
	}
	c := st.Find(id)
	if c == nil {
		return nil, fmt.Errorf("client not found: %s", id)
	}
	return c, nil
}
