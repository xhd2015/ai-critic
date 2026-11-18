package portforward

import (
	"fmt"
	"strings"

	pkgcf "github.com/xhd2015/dot-pkgs/go-pkgs/cloudflare"

	"github.com/xhd2015/ai-critic/server/domains"
)

func init() {
	// domains cannot import this package (it would be a cycle), so register the
	// restart hook here instead.
	domains.RestartOwnedConnectorFunc = RestartOwnedConnector
}

// RestartOwnedConnector applies the currently configured edge protocol to the
// managed-tunnel connector and recycles it.
//
// Unlike the unified-tunnel groups, this connector is spawned by
// dot-pkgs/cloudflare (managed-tunnels/<name>/) and takes its protocol from
// TunnelState.Protocol. Attach reconciles that field from the configured value
// and restarts the connector when it changes, so a recycle is all that is
// needed here.
//
// It is a no-op when no managed tunnel is configured or nothing is running.
func RestartOwnedConnector() error {
	cfg, err := domains.LoadDomains()
	if err != nil || cfg == nil {
		return nil
	}
	name := strings.TrimSpace(cfg.TunnelName)
	if name == "" {
		return nil
	}
	configDir, err := pkgcf.DefaultConfigDir()
	if err != nil {
		return fmt.Errorf("owned tunnel config dir: %w", err)
	}
	dir, err := pkgcf.ManagedTunnelDir(configDir, name)
	if err != nil {
		return err
	}
	st, err := pkgcf.LoadTunnelState(dir)
	if err != nil || st == nil {
		return nil
	}

	// Do NOT persist here. Attach owns both the state write and the supervisor
	// restart, and it only restarts when it sees the protocol differ. Writing
	// state first would make the change look like a no-op and leave the old
	// connector (and its respawning supervisor) running on the old protocol.
	if !pidAlive(st.ConnectorPID) {
		// Nothing running; the next attach or watchdog tick persists it.
		return nil
	}
	return recycleConnector(st, configDir)
}
