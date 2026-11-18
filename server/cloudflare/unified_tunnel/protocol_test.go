package unified_tunnel

import (
	"os"
	"testing"
)

func TestNormalizeProtocol(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"  ", "", false},
		{"http2", ProtocolHTTP2, false},
		{"HTTP2", ProtocolHTTP2, false},
		{" quic ", ProtocolQUIC, false},
		{"Quic", ProtocolQUIC, false},
		{"tcp", "", true},
		{"auto", "", true},
		{"quic ", ProtocolQUIC, false},
		{"https", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeProtocol(c.in)
		if c.wantErr {
			if err == nil {
				t.Fatalf("NormalizeProtocol(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("NormalizeProtocol(%q) unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("NormalizeProtocol(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// An unset value must resolve to the historical default so an install that
// never touches the setting behaves exactly as before.
func TestEffectiveProtocolDefaultsToHTTP2(t *testing.T) {
	if got := EffectiveProtocol(""); got != DefaultTunnelProtocol {
		t.Fatalf("EffectiveProtocol(\"\") = %q, want %q", got, DefaultTunnelProtocol)
	}
	if DefaultTunnelProtocol != ProtocolHTTP2 {
		t.Fatalf("DefaultTunnelProtocol = %q, want %q", DefaultTunnelProtocol, ProtocolHTTP2)
	}
}

// A corrupted value in server-domains.json must not brick boot: fall back.
func TestEffectiveProtocolFallsBackOnGarbage(t *testing.T) {
	if got := EffectiveProtocol("gopher"); got != DefaultTunnelProtocol {
		t.Fatalf("EffectiveProtocol(garbage) = %q, want %q", got, DefaultTunnelProtocol)
	}
}

func TestSetConfiguredProtocolReportsChange(t *testing.T) {
	restore := SetConfiguredProtocolForTest()
	defer restore()

	changed, err := SetConfiguredProtocol("quic")
	if err != nil {
		t.Fatalf("SetConfiguredProtocol(quic) error: %v", err)
	}
	if !changed {
		t.Fatal("SetConfiguredProtocol(quic) reported no change, want true")
	}
	if got := ConfiguredProtocol(); got != ProtocolQUIC {
		t.Fatalf("ConfiguredProtocol() = %q, want quic", got)
	}

	// Same value twice must not look like a change, or a no-op save would
	// bounce a healthy connector.
	changed, err = SetConfiguredProtocol("quic")
	if err != nil {
		t.Fatalf("SetConfiguredProtocol(quic) again error: %v", err)
	}
	if changed {
		t.Fatal("re-setting quic reported a change, want false")
	}

	if _, err := SetConfiguredProtocol("tcp"); err == nil {
		t.Fatal("SetConfiguredProtocol(tcp) accepted an invalid protocol")
	}
	if got := ConfiguredProtocol(); got != ProtocolQUIC {
		t.Fatalf("invalid input mutated state: ConfiguredProtocol() = %q, want quic", got)
	}
}

// Mapped addr form must also be a change: http2 -> "" is a real switch back
// to the default only when the default differs from the previous value.
func TestSetConfiguredProtocolEmptyAfterQuic(t *testing.T) {
	restore := SetConfiguredProtocolForTest()
	defer restore()

	if _, err := SetConfiguredProtocol("quic"); err != nil {
		t.Fatalf("set quic: %v", err)
	}
	changed, err := SetConfiguredProtocol("")
	if err != nil {
		t.Fatalf("set empty: %v", err)
	}
	if !changed {
		t.Fatal(`quic -> "" reported no change, want true (falls back to http2)`)
	}
	if got := ConfiguredProtocol(); got != ProtocolHTTP2 {
		t.Fatalf("ConfiguredProtocol() = %q, want http2", got)
	}
}

// SetConfiguredProtocolForTest snapshots and restores the package state.
func SetConfiguredProtocolForTest() func() {
	protocolMu.Lock()
	prev := configuredProtocol
	protocolMu.Unlock()
	return func() {
		protocolMu.Lock()
		configuredProtocol = prev
		protocolMu.Unlock()
	}
}

// SetConfiguredProtocol must NOT touch the process environment: the protocol
// now travels to every connector as an argv flag. A process-wide variable would
// leak into unrelated children (llm-proxy, dsh, quick tunnels).
func TestSetConfiguredProtocolDoesNotPolluteEnv(t *testing.T) {
	restore := SetConfiguredProtocolForTest()
	defer restore()

	const envKey = "TUNNEL_TRANSPORT_PROTOCOL"
	prev, had := os.LookupEnv(envKey)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(envKey, prev)
		} else {
			_ = os.Unsetenv(envKey)
		}
	})
	_ = os.Unsetenv(envKey)

	if _, err := SetConfiguredProtocol("quic"); err != nil {
		t.Fatalf("set quic: %v", err)
	}
	if got, ok := os.LookupEnv(envKey); ok {
		t.Fatalf("%s was exported as %q; the setting must not use the environment", envKey, got)
	}
}
