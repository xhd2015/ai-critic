// Package wsclient runs the client half of a WS proxy: a local Xray process
// whose inbounds (HTTP + SOCKS) forward through a remote ws-proxy instance's
// VMess/WebSocket endpoint. The server owns it as a system service, so a
// machine that only consumes the tunnel does not have to keep a hand-started
// `xray run -c …` alive across reboots.
package wsclient

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xhd2015/ai-critic/server/config"
)

const (
	configFileName = "ws-proxy-clients.json"
	logFileName    = "ws-proxy-client.log"

	defaultHTTPPort  = 10809
	defaultSocksPort = 10808
)

// SourceKind says where a client's VMess parameters come from. A remote source
// re-fetches from the serving instance on every start, so a changed domain or
// rotated UUID follows without editing this machine; a literal vmess link keeps
// working when the server is unreachable but has to be updated by hand.
type SourceKind string

const (
	// SourceRemote fetches /api/ws-proxy/instances/{instance}/vmess-link.
	SourceRemote SourceKind = "remote"
	// SourceVMess parses a stored vmess:// link.
	SourceVMess SourceKind = "vmess"
)

// Client is one configured tunnel consumer.
type Client struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Source selects how VMess parameters are resolved; empty means remote.
	Source SourceKind `json:"source,omitempty"`
	// BaseURL is the serving ai-critic root, e.g. https://mac-agent.example.com.
	BaseURL string `json:"base_url,omitempty"`
	// Instance names the serving instance; empty falls back to the legacy
	// single-instance endpoint.
	Instance string `json:"instance,omitempty"`
	// Token authorizes the vmess-link fetch. Never echoed back by the API.
	Token string `json:"token,omitempty"`
	// VMessLink is the literal vmess:// link for SourceVMess.
	VMessLink string `json:"vmess_link,omitempty"`

	HTTPPort  int `json:"http_port,omitempty"`
	SocksPort int `json:"socks_port,omitempty"`

	// AutoStart brings this client up with the server process.
	AutoStart bool `json:"auto_start,omitempty"`
	// PacDomains are the domains routed through the tunnel in the generated
	// PAC script. Empty means "every host".
	PacDomains []string `json:"pac_domains,omitempty"`
}

// Normalized fills defaults in place so loaded and newly created clients look
// the same to the rest of the package.
func (c *Client) Normalized() {
	c.ID = strings.TrimSpace(c.ID)
	c.Name = strings.TrimSpace(c.Name)
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.Instance = strings.TrimSpace(c.Instance)
	if c.Source == "" {
		c.Source = SourceRemote
	}
	if c.HTTPPort <= 0 {
		c.HTTPPort = defaultHTTPPort
	}
	if c.SocksPort <= 0 {
		c.SocksPort = defaultSocksPort
	}
	if c.ID == "" {
		c.ID = slug(c.Name)
	}
	if c.Name == "" {
		c.Name = c.ID
	}
	for i, d := range c.PacDomains {
		c.PacDomains[i] = strings.TrimSpace(strings.ToLower(d))
	}
}

// Validate rejects a client that cannot start, so the API fails at write time
// instead of leaving a definition that never comes up.
func (c *Client) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("client id is required")
	}
	switch c.Source {
	case SourceRemote:
		if c.BaseURL == "" {
			return fmt.Errorf("base_url is required for a remote source")
		}
		if !strings.HasPrefix(c.BaseURL, "http://") && !strings.HasPrefix(c.BaseURL, "https://") {
			return fmt.Errorf("base_url must start with http:// or https://")
		}
	case SourceVMess:
		if strings.TrimSpace(c.VMessLink) == "" {
			return fmt.Errorf("vmess_link is required for a vmess source")
		}
	default:
		return fmt.Errorf("unknown source %q: want remote or vmess", c.Source)
	}
	if c.HTTPPort <= 0 || c.HTTPPort > 65535 {
		return fmt.Errorf("http_port must be between 1 and 65535")
	}
	if c.SocksPort <= 0 || c.SocksPort > 65535 {
		return fmt.Errorf("socks_port must be between 1 and 65535")
	}
	if c.HTTPPort == c.SocksPort {
		return fmt.Errorf("http_port and socks_port must differ")
	}
	return nil
}

// Store is the persisted client list.
type Store struct {
	Clients []Client `json:"clients"`
}

// Find returns the client with id, or nil.
func (s *Store) Find(id string) *Client {
	for i := range s.Clients {
		if s.Clients[i].ID == id {
			return &s.Clients[i]
		}
	}
	return nil
}

var _testConfigDir string

// SetConfigDir redirects persistence for tests.
func SetConfigDir(dir string) { _testConfigDir = dir }

func configPath() string {
	if _testConfigDir != "" {
		return filepath.Join(_testConfigDir, configFileName)
	}
	return filepath.Join(config.DataDir, configFileName)
}

// LogPath is the shared xray log for every client on this machine.
func LogPath() string {
	if _testConfigDir != "" {
		return filepath.Join(_testConfigDir, logFileName)
	}
	return filepath.Join(config.DataDir, logFileName)
}

// LoadStore reads the client list; a missing file is an empty list.
func LoadStore() (*Store, error) {
	data, err := os.ReadFile(configPath())
	if os.IsNotExist(err) {
		return &Store{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read ws-proxy clients: %w", err)
	}
	var st Store
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse ws-proxy clients: %w", err)
	}
	for i := range st.Clients {
		st.Clients[i].Normalized()
	}
	return &st, nil
}

// SaveStore writes the client list, 0600 because it carries server tokens.
func SaveStore(st *Store) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ws-proxy clients: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

// slug turns a display name into an id-safe token.
func slug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash && b.Len() > 0:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
