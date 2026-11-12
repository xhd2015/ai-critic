package services

import (
	"testing"

	"github.com/xhd2015/dot-pkgs/go-pkgs/service"
)

// Detached and Enabled are start-time settings: saving them must never bounce a
// running process, or the "restart to apply" hint would be a lie.
func TestDefinitionChangedIgnoresStartTimeFlags(t *testing.T) {
	base := ServiceDefinition{ID: "svc-web", Name: "web", Command: "run"}

	disabled := false
	cases := []struct {
		name   string
		mutate func(*ServiceDefinition)
	}{
		{name: "detached", mutate: func(def *ServiceDefinition) { def.Detached = true }},
		{name: "enabled", mutate: func(def *ServiceDefinition) { def.Enabled = &disabled }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := base
			tc.mutate(&next)
			if definitionChanged(base, next) {
				t.Fatalf("changing %s must not count as a definition change", tc.name)
			}
		})
	}
}

// The lifetime that counts is the one the process actually runs with, so a
// detached process must survive Shutdown and be reported by detachedIDs.
func TestShutdownKeepsDetachedServiceRunning(t *testing.T) {
	useTempServicesConfig(t)

	sup := service.NewSupervisor(service.Options{
		DataDir: t.TempDir(),
		Logf:    func(string, ...any) {},
	})
	t.Cleanup(func() { sup.Shutdown() })

	def := service.Definition{
		ID:       "svc-sleeper",
		Name:     "sleeper",
		Command:  "sleep 60",
		Detached: true,
	}
	if err := sup.Define([]service.Definition{def}); err != nil {
		t.Fatalf("Define: %v", err)
	}
	if err := sup.Start(def.ID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	proc, ok := sup.Process(def.ID)
	if !ok || proc.PID <= 0 {
		t.Fatalf("Process = %#v, %v; want a running process", proc, ok)
	}
	pid := proc.PID
	t.Cleanup(func() { _ = sup.Stop(def.ID) })

	m := &Manager{
		sup:  sup,
		defs: []ServiceDefinition{{ID: def.ID, Name: def.Name, Command: def.Command, Detached: true}},
		hops: make(map[string]*forwardHop),
	}
	if !m.detachedIDs()[def.ID] {
		t.Fatal("detachedIDs() did not report the detached process")
	}

	m.Shutdown()

	if !service.Alive(pid) {
		t.Fatalf("Shutdown killed detached process pid %d", pid)
	}
}

// A service whose Detached flag is saved while it runs attached still stops on
// quit: the running process never picked the new lifetime up.
func TestShutdownStopsServiceThatIsRunningAttached(t *testing.T) {
	useTempServicesConfig(t)

	sup := service.NewSupervisor(service.Options{
		DataDir: t.TempDir(),
		Logf:    func(string, ...any) {},
	})
	t.Cleanup(func() { sup.Shutdown() })

	def := service.Definition{ID: "svc-sleeper", Name: "sleeper", Command: "sleep 60"}
	if err := sup.Define([]service.Definition{def}); err != nil {
		t.Fatalf("Define: %v", err)
	}
	if err := sup.Start(def.ID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	proc, ok := sup.Process(def.ID)
	if !ok || proc.PID <= 0 {
		t.Fatalf("Process = %#v, %v; want a running process", proc, ok)
	}
	pid := proc.PID

	m := &Manager{
		sup:  sup,
		defs: []ServiceDefinition{{ID: def.ID, Name: def.Name, Command: def.Command, Detached: true}},
		hops: make(map[string]*forwardHop),
	}
	if m.detachedIDs()[def.ID] {
		t.Fatal("detachedIDs() reported an attached process as detached")
	}

	m.Shutdown()

	if service.Alive(pid) {
		t.Fatalf("Shutdown left attached process pid %d running", pid)
	}
}
