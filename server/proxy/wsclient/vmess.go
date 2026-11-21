package wsclient

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// VMessParams are the fields an xray VMess/WS client needs. They match the
// serving instance's /vmess-link payload and the vmess:// link body, so either
// source produces the same struct.
type VMessParams struct {
	Host    string `json:"host"`
	Port    string `json:"port"`
	UUID    string `json:"uuid"`
	AlterID string `json:"alter_id,omitempty"`
	Network string `json:"network"`
	Path    string `json:"path"`
	TLS     string `json:"tls"`
	SNI     string `json:"sni,omitempty"`
	// VMessLink is the raw link when the source produced one.
	VMessLink string `json:"vmess_link,omitempty"`
}

// vmessLinkBody is the JSON carried inside a base64 vmess:// link.
type vmessLinkBody struct {
	V    string `json:"v"`
	PS   string `json:"ps"`
	Add  string `json:"add"`
	Port any    `json:"port"`
	ID   string `json:"id"`
	AID  any    `json:"aid"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Host string `json:"host"`
	Path string `json:"path"`
	TLS  string `json:"tls"`
	SNI  string `json:"sni"`
}

// parseVMessLink decodes a vmess:// link into params. The link is the only
// carrier of the port (the serving API omits it), so it is parsed rather than
// re-derived.
func parseVMessLink(link string) (*VMessParams, error) {
	raw := strings.TrimSpace(link)
	raw = strings.TrimPrefix(raw, "vmess://")
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return nil, fmt.Errorf("empty vmess link")
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		// Some clients emit URL-safe base64 without padding.
		decoded, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(raw, "="))
		if err != nil {
			return nil, fmt.Errorf("vmess link is not valid base64: %w", err)
		}
	}
	var body vmessLinkBody
	if err := json.Unmarshal(decoded, &body); err != nil {
		return nil, fmt.Errorf("vmess link body is not JSON: %w", err)
	}
	port := scalarString(body.Port)
	if body.Add == "" || port == "" || body.ID == "" {
		return nil, fmt.Errorf("vmess link is missing address, port, or id")
	}
	p := &VMessParams{
		Host:      body.Add,
		Port:      port,
		UUID:      body.ID,
		AlterID:   scalarString(body.AID),
		Network:   body.Net,
		Path:      body.Path,
		TLS:       body.TLS,
		SNI:       body.SNI,
		VMessLink: link,
	}
	p.normalized()
	return p, nil
}

// scalarString renders a JSON field that may be a string or a number.
func scalarString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	case json.Number:
		return t.String()
	default:
		return ""
	}
}

// normalized fills the defaults an incomplete but usable link may omit.
func (p *VMessParams) normalized() {
	if p.Port == "" {
		p.Port = "443"
	}
	if p.Network == "" {
		p.Network = "ws"
	}
	if p.Path == "" {
		p.Path = "/ws"
	}
	if p.SNI == "" {
		p.SNI = p.Host
	}
}

// vmessLinkResponse is what the serving instance returns from
// GET /api/ws-proxy/instances/{id}/vmess-link.
type vmessLinkResponse struct {
	VMessLink string `json:"vmess_link"`
	Host      string `json:"host"`
	UUID      string `json:"uuid"`
	Path      string `json:"path"`
	TLS       string `json:"tls"`
	Error     string `json:"error"`
}

var fetchHTTPClient = &http.Client{Timeout: 20 * time.Second}

// fetchRemoteVMess pulls the vmess link from the serving ai-critic instance.
// The link is authoritative: it carries the port, which the JSON payload does
// not.
func fetchRemoteVMess(c *Client) (*VMessParams, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("base_url is empty")
	}
	path := "/api/ws-proxy/vmess-link"
	if c.Instance != "" {
		path = "/api/ws-proxy/instances/" + c.Instance + "/vmess-link"
	}
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build vmess request: %w", err)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := fetchHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch vmess from %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	var payload vmessLinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode vmess response (HTTP %d): %w", resp.StatusCode, err)
	}
	if payload.Error != "" {
		return nil, fmt.Errorf("fetch vmess from %s: %s", c.BaseURL, payload.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch vmess from %s: HTTP %d", c.BaseURL, resp.StatusCode)
	}
	if payload.VMessLink == "" {
		return nil, fmt.Errorf("serving instance returned no vmess link (is %q running?)", c.Instance)
	}
	return parseVMessLink(payload.VMessLink)
}

// resolveVMess turns a client definition into typed params, from whichever
// source the client selected.
func resolveVMess(c *Client) (*VMessParams, error) {
	switch c.Source {
	case SourceVMess:
		return parseVMessLink(c.VMessLink)
	case SourceRemote, "":
		return fetchRemoteVMess(c)
	default:
		return nil, fmt.Errorf("unknown source %q", c.Source)
	}
}
