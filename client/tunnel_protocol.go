package client

import "fmt"

// TunnelProtocolInfo describes the cloudflared edge transport.
type TunnelProtocolInfo struct {
	// Protocol is the persisted value: "http2", "quic", or "" when unset.
	Protocol string `json:"protocol"`
	// Effective is what cloudflared is actually launched with.
	Effective string `json:"effective"`
	// Default is the built-in fallback.
	Default string `json:"default"`
}

// TunnelProtocolResult is returned by SetTunnelProtocol.
type TunnelProtocolResult struct {
	TunnelProtocolInfo
	// Restarted reports whether the connector was bounced to apply the change.
	Restarted bool `json:"restarted"`
}

// GetTunnelProtocol reads the edge transport setting.
func (c *Client) GetTunnelProtocol() (*TunnelProtocolInfo, error) {
	var out TunnelProtocolInfo
	if err := c.getJSON("/api/domains/tunnel-protocol", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetTunnelProtocol persists the edge transport and, unless restart is false,
// bounces the connector so the new transport is used immediately.
func (c *Client) SetTunnelProtocol(protocol string, restart bool) (*TunnelProtocolResult, error) {
	body := map[string]any{"protocol": protocol, "no_restart": !restart}
	var out TunnelProtocolResult
	if err := c.postJSON("/api/domains/tunnel-protocol", body, &out); err != nil {
		return nil, fmt.Errorf("set tunnel protocol: %w", err)
	}
	return &out, nil
}
