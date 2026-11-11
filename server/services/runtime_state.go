package services

import (
	"fmt"

	"github.com/xhd2015/dot-pkgs/go-pkgs/service"
)

// ReconcileOrphans stops processes left behind by a previous instance, so a
// restart does not fail with "address already in use", but adopts the ones that
// were started detached instead: those were meant to outlive the previous host.
// The shared supervisor owns the runtime file and only trusts a record whose pid
// still runs the recorded command.
func ReconcileOrphans() []string {
	return defaultManager.ReconcileOrphans()
}

func (m *Manager) ReconcileOrphans() []string {
	if m.sup == nil {
		return nil
	}
	result := m.sup.Reclaim(service.ReclaimStop)
	for _, id := range result.Adopted {
		proc, ok := m.sup.Process(id)
		if !ok {
			continue
		}
		fmt.Printf("[services] adopted detached service %q (pid %d) left by a previous instance\n", id, proc.PID)
		// The port forward (and its auth hop) belonged to the previous host, so
		// it is gone while the process still serves: without this the service
		// would look running but its public URL would stay broken.
		m.mu.Lock()
		saved, defined := m.findDefinitionLocked(id)
		m.mu.Unlock()
		if !defined {
			continue
		}
		if err := m.ensurePortForward(saved.ID, saved); err != nil {
			fmt.Printf("[services] adopted %q but port forwarding failed: %v\n", id, err)
			m.setRuntimeError(id, err.Error())
		}
	}
	return result.Reclaimed
}
