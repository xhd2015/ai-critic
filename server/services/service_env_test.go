package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xhd2015/ai-critic/server/procenv"
	"github.com/xhd2015/dot-pkgs/go-pkgs/service"
)

func TestValidateDefinitionRejectsInvalidExtraPath(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantSub string
	}{
		{name: "empty entry", value: "/opt/a::/opt/b", wantSub: "empty entry"},
		{name: "variable", value: "$HOME/bin", wantSub: "does not expand variables"},
		{name: "relative", value: "opt/bin", wantSub: "must be absolute"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDefinition(ServiceDefinition{
				Name:      "web",
				Command:   "run",
				ExtraPath: tc.value,
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantSub)
			}
		})
	}
}

func TestCreateOrUpdateNormalizesExtraPath(t *testing.T) {
	useTempServicesConfig(t)
	m := &Manager{
		defs: []ServiceDefinition{},
		hops: make(map[string]*forwardHop),
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	saved, err := m.CreateOrUpdateNoRestart(ServiceDefinition{
		Name:      "web",
		Command:   "run",
		ExtraPath: "~/bin:/opt/shims/:/opt/shims::", // trailing/duplicate/empty is rejected below
	})
	if err == nil {
		t.Fatalf("CreateOrUpdateNoRestart() = %#v, want error for empty entry", saved)
	}

	saved, err = m.CreateOrUpdateNoRestart(ServiceDefinition{
		Name:                       "web",
		Command:                    "run",
		ExtraPath:                  "~/bin:/opt/shims/:/opt/shims",
		InheritInteractiveShellEnv: true,
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateNoRestart() error = %v", err)
	}
	want := filepath.Join(home, "bin") + string(os.PathListSeparator) + "/opt/shims"
	if saved.ExtraPath != want {
		t.Fatalf("status ExtraPath = %q, want %q", saved.ExtraPath, want)
	}
	if !saved.InheritInteractiveShellEnv {
		t.Fatal("status InheritInteractiveShellEnv = false, want true")
	}
	if m.defs[0].ExtraPath != want || !m.defs[0].InheritInteractiveShellEnv {
		t.Fatalf("saved definition = %#v", m.defs[0])
	}
}

func TestDefinitionChangedDetectsEnvLayers(t *testing.T) {
	base := ServiceDefinition{Name: "web", Command: "run"}

	withPath := base
	withPath.ExtraPath = "/opt/shims"
	if !definitionChanged(base, withPath) {
		t.Fatal("expected extra path to count as definition change")
	}

	withShell := base
	withShell.InheritInteractiveShellEnv = true
	if !definitionChanged(base, withShell) {
		t.Fatal("expected inherit shell env to count as definition change")
	}

	same := base
	same.ExtraPath = ""
	same.InheritInteractiveShellEnv = false
	if definitionChanged(base, same) {
		t.Fatal("expected identical env layers not to count as change")
	}
}

func TestStatusReportsEnvLayers(t *testing.T) {
	useTempServicesConfig(t)
	def := ServiceDefinition{
		ID:                         "svc-web",
		Name:                       "web",
		Command:                    "run",
		ExtraPath:                  "/opt/shims",
		InheritInteractiveShellEnv: true,
	}
	m := &Manager{
		defs: []ServiceDefinition{def},
		hops: make(map[string]*forwardHop),
	}
	procs := map[string]service.Process{
		"svc-web": {
			Definition:    toServiceDefinition(def),
			State:         service.StateRunning,
			PID:           os.Getpid(),
			ShellEnvError: "lookpath: empty login envs from bash and zsh",
			EffectivePath: "/opt/shims:/usr/bin:/bin",
			Desired:       true,
			LastStartedAt: "2026-01-01T00:00:00Z",
			LogPath:       filepath.Join(t.TempDir(), "svc-web.log"),
		},
	}

	list := m.buildStatusListLocked(m.defs, procs)
	if len(list) != 1 {
		t.Fatalf("status count = %d, want 1", len(list))
	}
	got := list[0]
	if got.ExtraPath != "/opt/shims" {
		t.Fatalf("ExtraPath = %q", got.ExtraPath)
	}
	if !got.InheritInteractiveShellEnv {
		t.Fatal("InheritInteractiveShellEnv = false, want true")
	}
	if !strings.Contains(got.ShellEnvError, "empty login envs") {
		t.Fatalf("ShellEnvError = %q", got.ShellEnvError)
	}
	if got.EffectivePath != "/opt/shims:/usr/bin:/bin" {
		t.Fatalf("EffectivePath = %q", got.EffectivePath)
	}
}

func TestStatusFallsBackToStaticPathWithoutStart(t *testing.T) {
	useTempServicesConfig(t)
	def := ServiceDefinition{
		ID:        "svc-web",
		Name:      "web",
		Command:   "run",
		ExtraPath: "/opt/shims",
	}
	m := &Manager{
		defs: []ServiceDefinition{def},
		hops: make(map[string]*forwardHop),
	}

	list := m.buildStatusListLocked(m.defs, map[string]service.Process{})
	if len(list) != 1 {
		t.Fatalf("status count = %d, want 1", len(list))
	}
	if !strings.HasPrefix(list[0].EffectivePath, "/opt/shims"+string(os.PathListSeparator)) {
		t.Fatalf("EffectivePath = %q, want Extra PATH first", list[0].EffectivePath)
	}
	if list[0].ShellEnvError != "" {
		t.Fatalf("ShellEnvError = %q, want empty (status must not probe)", list[0].ShellEnvError)
	}
}

func TestBuildServiceEnvForStartReportsShellEnvError(t *testing.T) {
	useTempServicesConfig(t)
	old := resolveStartEnv
	t.Cleanup(func() { resolveStartEnv = old })
	resolveStartEnv = func(opts procenv.Options) ([]string, error) {
		if !opts.InheritShellEnv {
			return []string{"PATH=/usr/bin"}, nil
		}
		if opts.ExtraPath != "/opt/shims" {
			t.Fatalf("ExtraPath = %q, want /opt/shims", opts.ExtraPath)
		}
		return []string{"PATH=/opt/shims:/usr/bin"}, errors.New("interactive shell reported an empty environment")
	}

	env, msg := buildServiceEnvForStart(ServiceDefinition{
		Name:                       "web",
		Command:                    "run",
		ExtraPath:                  "/opt/shims",
		InheritInteractiveShellEnv: true,
	})
	if !strings.Contains(msg, "empty environment") {
		t.Fatalf("msg = %q, want shell env failure", msg)
	}
	if lookupEnvValue(env, "PATH") != "/opt/shims:/usr/bin" {
		t.Fatalf("PATH = %q, want degraded env still used", lookupEnvValue(env, "PATH"))
	}

	if _, msg := buildServiceEnvForStart(ServiceDefinition{Name: "web", Command: "run"}); msg != "" {
		t.Fatalf("msg = %q, want empty when not inheriting", msg)
	}
}

func TestServiceDefinitionJSONExtraFields(t *testing.T) {
	data, err := json.Marshal(ServiceDefinition{Name: "web", Command: "run"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, key := range []string{"extraPath", "inheritInteractiveShellEnv"} {
		if strings.Contains(string(data), key) {
			t.Fatalf("json = %s, want %q omitted when unset", data, key)
		}
	}

	data, err = json.Marshal(ServiceDefinition{
		Name:                       "web",
		Command:                    "run",
		ExtraPath:                  "/opt/shims",
		InheritInteractiveShellEnv: true,
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var round ServiceDefinition
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if round.ExtraPath != "/opt/shims" || !round.InheritInteractiveShellEnv {
		t.Fatalf("round trip = %#v, want fields preserved", round)
	}
	if !strings.Contains(string(data), `"extraPath":"/opt/shims"`) {
		t.Fatalf("json = %s, want extraPath key", data)
	}
	if !strings.Contains(string(data), `"inheritInteractiveShellEnv":true`) {
		t.Fatalf("json = %s, want inheritInteractiveShellEnv key", data)
	}
}
