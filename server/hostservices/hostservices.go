// Package hostservices reports and manages host-level systemd services so the
// operator can see which units are the "real" owners of a port or tunnel.
//
// ai-critic's own service table (server/services) only knows processes it
// spawned. A unit can be boot-enabled and still be shadowed by an ai-critic
// child (exactly the cloudflared-ai-critic case), and a service can be defined
// in both places at once. Nothing surfaced that overlap until this package.
package hostservices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xhd2015/ai-critic/server/config"
)

// Owner describes who actually runs the process behind a unit.
type Owner string

const (
	// OwnerSystemd means systemd owns the main process and it is alive.
	OwnerSystemd Owner = "systemd"
	// OwnerAICritic means an ai-critic child service carries the same name.
	OwnerAICritic Owner = "ai-critic"
	// OwnerBoth means a unit is boot-enabled AND ai-critic defines it too.
	OwnerBoth Owner = "both"
	// OwnerUnused means the unit is boot-enabled but nothing is running.
	OwnerUnused Owner = "unused"
)

// Service is one boot-enabled systemd unit.
type Service struct {
	Unit string `json:"unit"`
	// Boot is the systemd enable state, e.g. "enabled".
	Boot string `json:"boot"`
	// Active is systemd's runtime state: "active", "inactive", "failed".
	Active string `json:"active"`
	// Sub is the finer-grained state, e.g. "running", "dead".
	Sub string `json:"sub,omitempty"`
	// Description is systemd's unit description.
	Description string `json:"description,omitempty"`
	// Owner is the resolved ownership verdict.
	Owner Owner `json:"owner"`
	// ManagedByAICritic is the ai-critic service name when it defines this unit.
	ManagedByAICritic string `json:"managed_by_ai_critic,omitempty"`
	// Protected units must never be disabled from the UI (e.g. the parent
	// process that runs ai-critic itself).
	Protected bool `json:"protected,omitempty"`
}

// List returns every boot-enabled systemd service with its ownership verdict.
func List(ctx context.Context) ([]Service, error) {
	units, err := listEnabledUnits(ctx)
	if err != nil {
		return nil, err
	}
	active, err := listActiveStates(ctx)
	if err != nil {
		return nil, err
	}
	ours := aiCriticServiceNames()

	out := make([]Service, 0, len(units))
	for _, u := range units {
		st := active[u.Unit]
		svc := Service{
			Unit:        u.Unit,
			Boot:        u.Boot,
			Active:      st.Active,
			Sub:         st.Sub,
			Description: u.Description,
		}
		if name, ok := ours[serviceKey(u.Unit)]; ok {
			svc.ManagedByAICritic = name
		}
		svc.Owner = resolveOwner(svc)
		svc.Protected = IsProtected(u.Unit)
		out = append(out, svc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Unit < out[j].Unit })
	return out, nil
}

func resolveOwner(s Service) Owner {
	switch {
	case s.ManagedByAICritic != "" && s.Active == "active":
		return OwnerBoth
	case s.ManagedByAICritic != "":
		return OwnerBoth
	case s.Active == "active":
		return OwnerSystemd
	default:
		return OwnerUnused
	}
}

// protectedUnits may never be disabled through this API: stopping them breaks
// the process that serves the request, and ai-critic must keep running for the
// tunnel and its children to exist.
var protectedUnits = map[string]struct{}{
	"ai-critic-server.service": {},
}

// IsProtected reports whether disabling the unit would cut the branch it sits on.
func IsProtected(unit string) bool {
	_, ok := protectedUnits[normalizeUnit(unit)]
	return ok
}

func normalizeUnit(unit string) string {
	u := strings.TrimSpace(unit)
	if u == "" {
		return ""
	}
	if !strings.Contains(u, ".") {
		u += ".service"
	}
	return u
}

// DisableResult reports what changed.
type DisableResult struct {
	Unit    string `json:"unit"`
	Boot    string `json:"boot"`
	Masked  bool   `json:"masked"`
	Message string `json:"message,omitempty"`
}

// Disable turns off boot activation for a unit. mask additionally blocks a
// manual `systemctl start` by symlinking the unit to /dev/null.
//
// The running process, when one exists, is left alone: callers disable units
// that are already shadowed by another owner, so stopping them is not implied.
func Disable(ctx context.Context, unit string, mask bool) (*DisableResult, error) {
	u := normalizeUnit(unit)
	if u == "" {
		return nil, errors.New("unit is required")
	}
	if IsProtected(u) {
		return nil, fmt.Errorf("%s runs this server; disabling it is refused", u)
	}

	if out, err := runSystemctl(ctx, "disable", u); err != nil {
		return nil, fmt.Errorf("disable %s: %v: %s", u, err, strings.TrimSpace(out))
	}
	res := &DisableResult{Unit: u, Boot: "disabled"}

	if mask {
		if err := maskUnit(ctx, u); err != nil {
			return nil, err
		}
		res.Masked = true
	}

	// Report the post-state rather than echoing intent.
	if states, err := listActiveStates(ctx); err == nil {
		if st, ok := states[u]; ok {
			res.Boot = st.Boot
			if !mask {
				res.Message = "boot disabled; any running process was left untouched"
			}
		}
	}
	return res, nil
}

// maskUnit makes `systemctl start` fail for a unit.
//
// `systemctl mask` only works when the unit is not already an administrator file
// in /etc/systemd/system: it refuses to overwrite it. That is the common case
// for units an operator hand-wrote (as here), so fall back to the documented
// equivalent — move the file aside and link /dev/null in its place.
func maskUnit(ctx context.Context, unit string) error {
	if out, err := runSystemctl(ctx, "mask", unit); err == nil {
		return nil
	} else if !strings.Contains(out, "already exists") {
		return fmt.Errorf("mask %s: %v: %s", unit, err, strings.TrimSpace(out))
	}

	adminPath := filepath.Join("/etc/systemd/system", unit)
	stashed := adminPath + ".unmasked"
	if _, err := os.Stat(stashed); err == nil {
		return fmt.Errorf("mask %s: %s already exists; unmask first", unit, stashed)
	}
	if err := os.Rename(adminPath, stashed); err != nil {
		return fmt.Errorf("mask %s: stash unit file: %w", unit, err)
	}
	if err := os.Symlink(os.DevNull, adminPath); err != nil {
		// Put the original back rather than leaving the host without the unit.
		_ = os.Rename(stashed, adminPath)
		return fmt.Errorf("mask %s: link %s: %w", unit, adminPath, err)
	}
	if out, err := runSystemctl(ctx, "daemon-reload"); err != nil {
		return fmt.Errorf("mask %s: daemon-reload: %v: %s", unit, err, strings.TrimSpace(out))
	}
	return nil
}

// ---- systemd probing ----

type unitFile struct {
	Unit        string
	Boot        string
	Description string
}

// listEnabledUnits reads `systemctl list-unit-files --state=enabled`.
// Only *.service units are reported; sockets/timers are not actionable here.
func listEnabledUnits(ctx context.Context) ([]unitFile, error) {
	out, err := runSystemctl(ctx, "list-unit-files", "--type=service",
		"--state=enabled", "--no-legend", "--no-pager", "--plain")
	if err != nil {
		return nil, fmt.Errorf("list enabled units: %v: %s", err, strings.TrimSpace(out))
	}
	var units []unitFile
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasSuffix(fields[0], ".service") {
			continue
		}
		units = append(units, unitFile{Unit: fields[0], Boot: fields[1]})
	}
	return units, nil
}

type activeState struct {
	Active string
	Sub    string
	Boot   string
}

// listActiveStates reads one JSON blob instead of one call per unit.
func listActiveStates(ctx context.Context) (map[string]activeState, error) {
	out, err := runSystemctl(ctx, "list-units", "--type=service", "--all",
		"--no-legend", "--no-pager", "--output=json")
	if err != nil {
		return nil, fmt.Errorf("list service units: %v: %s", err, strings.TrimSpace(out))
	}
	var raw []struct {
		Unit        string `json:"unit"`
		Load        string `json:"load"`
		Active      string `json:"active"`
		Sub         string `json:"sub"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parse systemctl json: %w", err)
	}
	states := make(map[string]activeState, len(raw))
	for _, r := range raw {
		states[r.Unit] = activeState{Active: r.Active, Sub: r.Sub, Boot: r.Load}
	}
	return states, nil
}

// aiCriticServiceNames maps a unit-ish key to the ai-critic service name, so a
// unit defined in both places is reported as "both" rather than "systemd".
func aiCriticServiceNames() map[string]string {
	names := map[string]string{}
	for _, def := range loadAICriticDefinitions() {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		// A disabled definition is not a competing owner: reporting it as a
		// duplicate would keep flagging a set of services already stood down.
		if !def.participatesInAutoStart() {
			continue
		}
		names[serviceKey(name)] = name
		if bin := commandBinary(def.Command); bin != "" {
			names[serviceKey(bin)] = name
		}
	}
	return names
}

// aiCriticDefinition is the subset of services.json this package needs.
// Enabled defaults to true when absent, matching the service manager: only an
// explicit false removes a definition from auto-start.
type aiCriticDefinition struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Enabled *bool  `json:"enabled"`
}

// participatesInAutoStart reports whether ai-critic still manages this service.
func (d aiCriticDefinition) participatesInAutoStart() bool {
	return d.Enabled == nil || *d.Enabled
}

// loadAICriticDefinitions reads the ai-critic service table straight off disk.
// Importing server/services would drag the whole process supervisor in for one
// field, and a missing or malformed file simply means "nothing overlaps".
func loadAICriticDefinitions() []aiCriticDefinition {
	if config.DataDir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(config.DataDir, "services.json"))
	if err != nil {
		return nil
	}
	var defs []aiCriticDefinition
	if err := json.Unmarshal(data, &defs); err != nil {
		return nil
	}
	return defs
}

// serviceKey normalizes a unit name or service name for comparison:
// "llm-proxy.service" and "llm-proxy" must collide.
func serviceKey(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.TrimSuffix(v, ".service")
	return v
}

func commandBinary(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	bin := fields[0]
	if i := strings.LastIndex(bin, "/"); i >= 0 {
		bin = bin[i+1:]
	}
	return bin
}

// ---- process helpers ----

var systemctlTimeout = 15 * time.Second

func runSystemctl(ctx context.Context, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, systemctlTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	return string(out), err
}

// ---- HTTP API ----

// RegisterAPI mounts the host service endpoints.
func RegisterAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/host/services", handleList)
	mux.HandleFunc("/api/host/services/disable", handleDisable)
}

func handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	services, err := List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	total := len(services)
	unused := 0
	both := 0
	for _, s := range services {
		switch s.Owner {
		case OwnerUnused:
			unused++
		case OwnerBoth:
			both++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"services": services,
		"summary": map[string]int{
			"total":  total,
			"unused": unused,
			"both":   both,
		},
	})
}

func handleDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Unit string `json:"unit"`
		Mask bool   `json:"mask"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	res, err := Disable(r.Context(), req.Unit, req.Mask)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
