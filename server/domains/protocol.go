package domains

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/xhd2015/ai-critic/server/cloudflare/unified_tunnel"
)

// RestartOwnedConnectorFunc bounces the dot-pkgs managed-tunnel connector.
// Registered by portforward because that package imports domains, not the
// other way around.
var RestartOwnedConnectorFunc func() error

// ProtocolInfo is the GET/POST payload for the edge transport setting.
type ProtocolInfo struct {
	// Protocol is the configured value: "http2", "quic", or "" when unset.
	Protocol string `json:"protocol"`
	// Effective is what cloudflared is actually launched with.
	Effective string `json:"effective"`
	// Default is the built-in fallback, so the UI can label "default".
	Default string `json:"default"`
}

// LoadTunnelProtocol reads the persisted edge protocol.
func LoadTunnelProtocol() (ProtocolInfo, error) {
	cfg, err := LoadDomains()
	if err != nil {
		return ProtocolInfo{}, err
	}
	return ProtocolInfo{
		Protocol:  cfg.Protocol,
		Effective: unified_tunnel.EffectiveProtocol(cfg.Protocol),
		Default:   unified_tunnel.DefaultTunnelProtocol,
	}, nil
}

// SaveTunnelProtocol validates, persists and applies the edge protocol.
// It returns the resulting info plus whether reconnecting is required.
// restart forces a connector restart so the new transport takes effect now.
func SaveTunnelProtocol(protocol string, restart bool) (info ProtocolInfo, restarted bool, err error) {
	normalized, err := unified_tunnel.NormalizeProtocol(protocol)
	if err != nil {
		return ProtocolInfo{}, false, err
	}

	persistTunnelMu.Lock()
	cfg, err := LoadDomains()
	if err != nil {
		persistTunnelMu.Unlock()
		return ProtocolInfo{}, false, err
	}
	previous := unified_tunnel.EffectiveProtocol(cfg.Protocol)
	cfg.Protocol = normalized
	if err := SaveDomains(cfg); err != nil {
		persistTunnelMu.Unlock()
		return ProtocolInfo{}, false, err
	}
	persistTunnelMu.Unlock()

	changed, err := unified_tunnel.SetConfiguredProtocol(normalized)
	if err != nil {
		return ProtocolInfo{}, false, err
	}

	info, err = LoadTunnelProtocol()
	if err != nil {
		return ProtocolInfo{}, false, err
	}

	// Only reconnect when the wire transport actually changes; re-saving the
	// same value must not bounce a healthy tunnel.
	if !restart || !changed || previous == info.Effective {
		return info, false, nil
	}

	// The managed-tunnel connector (managed-tunnels/<name>/) is spawned by
	// dot-pkgs, which takes the protocol from TunnelState, so it needs its own
	// persist + bounce to apply the new value.
	var restartErr error
	if RestartOwnedConnectorFunc != nil {
		if err := RestartOwnedConnectorFunc(); err != nil {
			restartErr = fmt.Errorf("restart managed connector: %w", err)
		} else {
			restarted = true
		}
	}

	// The tunnel may also be carried by either group: configured domains live on
	// the core group, port mappings on the extension group. Restart whichever is up.
	tgm := unified_tunnel.GetTunnelGroupManager()
	for _, group := range []string{unified_tunnel.GroupCore, unified_tunnel.GroupExtension} {
		tg := tgm.GetGroup(group)
		if tg == nil || !tg.IsRunning() {
			continue
		}
		if err := tg.RestartNow(); err != nil {
			restartErr = fmt.Errorf("restart %s tunnel: %w", group, err)
			continue
		}
		restarted = true
	}
	if restartErr != nil {
		return info, restarted, restartErr
	}
	return info, restarted, nil
}

// ApplyConfiguredProtocol pushes the persisted protocol into the tunnel
// package. Called at startup before any connector is spawned.
func ApplyConfiguredProtocol() {
	cfg, err := LoadDomains()
	if err != nil {
		return
	}
	_, _ = unified_tunnel.SetConfiguredProtocol(cfg.Protocol)
}

func handleTunnelProtocol(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		info, err := LoadTunnelProtocol()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, info)

	case http.MethodPost:
		var req struct {
			Protocol  string `json:"protocol"`
			NoRestart bool   `json:"no_restart"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		info, restarted, err := SaveTunnelProtocol(req.Protocol, !req.NoRestart)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]any{
			"status":    "ok",
			"protocol":  info.Protocol,
			"effective": info.Effective,
			"default":   info.Default,
			"restarted": restarted,
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
