package unified_tunnel

import (
	"fmt"
	"strings"
	"sync"
)

// Edge transports understood by cloudflared's --protocol flag.
const (
	// ProtocolHTTP2 multiplexes every stream over one TCP connection. It is the
	// default because QUIC needs UDP/7844, which many networks drop.
	ProtocolHTTP2 = "http2"
	// ProtocolQUIC runs over UDP. Independent streams mean one lossy path no
	// longer collapses the connection's congestion window.
	ProtocolQUIC = "quic"
)

var (
	protocolMu         sync.RWMutex
	configuredProtocol string
)

// NormalizeProtocol validates a user-supplied edge protocol.
// An empty value means "not configured" and is returned as "", not an error.
func NormalizeProtocol(p string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(p))
	switch v {
	case "":
		return "", nil
	case ProtocolHTTP2, ProtocolQUIC:
		return v, nil
	default:
		return "", fmt.Errorf("unknown edge protocol %q (want %s|%s)", p, ProtocolHTTP2, ProtocolQUIC)
	}
}

// EffectiveProtocol resolves a configured value, falling back to the pinned
// default. An unparseable value also falls back so a bad file cannot brick boot.
func EffectiveProtocol(p string) string {
	v, err := NormalizeProtocol(p)
	if err != nil || v == "" {
		return DefaultTunnelProtocol
	}
	return v
}

// ConfiguredProtocol is the process-wide edge transport used for every
// cloudflared spawn and for the generated config.yml.
func ConfiguredProtocol() string {
	protocolMu.RLock()
	defer protocolMu.RUnlock()
	return EffectiveProtocol(configuredProtocol)
}

// SetConfiguredProtocol records the edge transport and reports whether it
// actually changed. Invalid input is rejected so callers can surface an error
// before persisting anything.
//
// This only updates the value read by this package's own spawn path. The
// managed-tunnel connector is launched by dot-pkgs/cloudflare and receives the
// protocol as an explicit --protocol flag (AttachOptions.Protocol /
// backend.Route.Protocol); it is deliberately not propagated through the
// process environment, which would leak into every unrelated child.
func SetConfiguredProtocol(p string) (bool, error) {
	if _, err := NormalizeProtocol(p); err != nil {
		return false, err
	}
	protocolMu.Lock()
	defer protocolMu.Unlock()
	if EffectiveProtocol(configuredProtocol) == EffectiveProtocol(p) {
		configuredProtocol = p
		return false, nil
	}
	configuredProtocol = p
	return true, nil
}
