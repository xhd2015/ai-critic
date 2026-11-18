package hostservices

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xhd2015/ai-critic/server/config"
)

func TestServiceKeyCollapsesSuffix(t *testing.T) {
	cases := map[string]string{
		"llm-proxy.service":    "llm-proxy",
		"llm-proxy":            "llm-proxy",
		"  LLM-Proxy.Service ": "llm-proxy",
		"":                     "",
	}
	for in, want := range cases {
		if got := serviceKey(in); got != want {
			t.Fatalf("serviceKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCommandBinaryStripsPath(t *testing.T) {
	cases := map[string]string{
		"/root/.local/bin/llm-proxy --config /root/x.json": "llm-proxy",
		"cloudflared tunnel --url http://x":                "cloudflared",
		"":                                                 "",
	}
	for in, want := range cases {
		if got := commandBinary(in); got != want {
			t.Fatalf("commandBinary(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsProtectedRefusesServerUnit(t *testing.T) {
	for _, u := range []string{"ai-critic-server", "ai-critic-server.service"} {
		if !IsProtected(u) {
			t.Fatalf("IsProtected(%q) = false, want true", u)
		}
	}
	if IsProtected("cloudflared-ai-critic") {
		t.Fatal("cloudflared-ai-critic reported protected, want false")
	}
}

func TestResolveOwner(t *testing.T) {
	cases := []struct {
		svc  Service
		want Owner
	}{
		{Service{Active: "active", ManagedByAICritic: ""}, OwnerSystemd},
		{Service{Active: "inactive"}, OwnerUnused},
		{Service{Active: "active", ManagedByAICritic: "llm-proxy"}, OwnerBoth},
	}
	for _, c := range cases {
		if got := resolveOwner(c.svc); got != c.want {
			t.Fatalf("resolveOwner(%+v) = %q, want %q", c.svc, got, c.want)
		}
	}
}

// A service switched off in ai-critic must stop counting as a second owner;
// otherwise the panel keeps reporting a duplicate that is already stood down.
func TestDisabledAICriticDefinitionIsNotAnOwner(t *testing.T) {
	dir := t.TempDir()
	orig := config.DataDir
	config.DataDir = dir
	defer func() { config.DataDir = orig }()

	body := `[{"name":"llm-proxy","command":"/root/.local/bin/llm-proxy","enabled":false},
	          {"name":"dsh-go","command":"/root/.local/bin/dsh-go"}]`
	if err := os.WriteFile(filepath.Join(dir, "services.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write services.json: %v", err)
	}

	names := aiCriticServiceNames()
	if _, ok := names[serviceKey("llm-proxy")]; ok {
		t.Fatal("disabled llm-proxy still counted as an ai-critic owner")
	}
	if _, ok := names[serviceKey("dsh-go")]; !ok {
		t.Fatal("dsh-go (enabled by default) missing from ai-critic owners")
	}
}

// A malformed services.json must degrade to "no overlap", not to an error.
func TestMalformedServicesFileIsIgnored(t *testing.T) {
	dir := t.TempDir()
	orig := config.DataDir
	config.DataDir = dir
	defer func() { config.DataDir = orig }()

	if err := os.WriteFile(filepath.Join(dir, "services.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write services.json: %v", err)
	}
	if names := aiCriticServiceNames(); len(names) != 0 {
		t.Fatalf("malformed file yielded %d owners, want 0", len(names))
	}
}
