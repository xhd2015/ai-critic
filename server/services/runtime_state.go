package services

import "github.com/xhd2015/dot-pkgs/go-pkgs/service"

// ReconcileOrphans stops processes left behind by a previous instance, so a
// restart does not fail with "address already in use". The shared supervisor
// owns the runtime file and only trusts a record whose pid still runs the
// recorded command.
func ReconcileOrphans() []string {
	return defaultManager.ReconcileOrphans()
}

func (m *Manager) ReconcileOrphans() []string {
	if m.sup == nil {
		return nil
	}
	return m.sup.Reclaim(service.ReclaimStop).Reclaimed
}
