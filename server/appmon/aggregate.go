package appmon

import (
	"net/url"
	"sort"
	"strings"

	"github.com/xhd2015/ai-critic/server/procmon"
)

// AppView is one installed app joined with its live processes.
type AppView struct {
	App
	// IconURL is empty when the app has no icon; the UI then draws a tile.
	IconURL string `json:"icon_url,omitempty"`
	// ProcessCount counts the live processes attributed to this app.
	ProcessCount int `json:"process_count"`
	// PIDs lists those processes, ordered like the system page (CPU, then RSS).
	PIDs []int `json:"pids,omitempty"`
	// CPUPercent and RSSBytes are the sum over all attributed processes, as a
	// percent of all cores (same unit as the process list).
	CPUPercent float64 `json:"cpu_percent"`
	RSSBytes   uint64  `json:"rss_bytes"`
	// Processes carries the rows the UI expands into; omitted when idle.
	Processes []procmon.Process `json:"processes,omitempty"`
}

// View is the joined payload served to the Apps page.
type View struct {
	ScannedAt   string    `json:"scanned_at"`
	SampledAt   string    `json:"sampled_at,omitempty"`
	Platform    string    `json:"platform"`
	Dirs        []string  `json:"dirs"`
	Unsupported bool      `json:"unsupported,omitempty"`
	Note        string    `json:"note,omitempty"`
	Running     int       `json:"running"`
	Apps        []AppView `json:"apps"`
}

// GetView returns the inventory joined with the current process snapshot. The
// process table comes from procmon's 1 s cache, so polling this endpoint never
// adds extra `ps` forks of its own.
func GetView() (*View, error) {
	inv, err := Get()
	if err != nil {
		return nil, err
	}
	return join(inv, procmon.Get), nil
}

// join attaches process data to every app. snapshotFn is injectable for tests.
func join(inv *Inventory, snapshotFn func() (*procmon.Snapshot, error)) *View {
	view := &View{
		ScannedAt:   inv.ScannedAt,
		Platform:    inv.Platform,
		Dirs:        inv.Dirs,
		Unsupported: inv.Unsupported,
		Note:        inv.Note,
		Apps:        make([]AppView, 0, len(inv.Apps)),
	}
	var procs []procmon.Process
	if snapshotFn != nil {
		if snap, err := snapshotFn(); err == nil && snap != nil {
			procs = snap.Processes
			view.SampledAt = snap.SampledAt
		}
	}

	byBundle := map[string][]procmon.Process{}
	for _, proc := range procs {
		if proc.BundlePath == "" {
			continue
		}
		byBundle[proc.BundlePath] = append(byBundle[proc.BundlePath], proc)
	}

	for _, app := range inv.Apps {
		item := AppView{App: app, CPUPercent: 0}
		if app.HasIcon {
			item.IconURL = "/api/server/apps/icon?path=" + url.QueryEscape(app.Path)
		}
		matched := byBundle[app.Path]
		if len(matched) == 0 && app.Exec != "" {
			matched = matchByExec(procs, app.Exec)
		}
		if len(matched) > 0 {
			item.Processes = matched
			item.ProcessCount = len(matched)
			for _, proc := range matched {
				item.PIDs = append(item.PIDs, proc.PID)
				item.CPUPercent += proc.CPUPercent
				item.RSSBytes += proc.RSSBytes
			}
			item.CPUPercent = round1(item.CPUPercent)
			view.Running++
		}
		view.Apps = append(view.Apps, item)
	}

	// Running apps first, then heaviest, then alphabetical from the scan order.
	sort.SliceStable(view.Apps, func(i, j int) bool {
		a, b := view.Apps[i], view.Apps[j]
		if (a.ProcessCount > 0) != (b.ProcessCount > 0) {
			return a.ProcessCount > 0
		}
		if a.CPUPercent != b.CPUPercent {
			return a.CPUPercent > b.CPUPercent
		}
		return false
	})
	return view
}

// matchByExec attributes processes on platforms without bundle paths, where the
// only stable link is the executable name from .desktop Exec= or DisplayIcon.
func matchByExec(procs []procmon.Process, exec string) []procmon.Process {
	exec = strings.ToLower(strings.TrimSpace(exec))
	if exec == "" {
		return nil
	}
	var out []procmon.Process
	for _, proc := range procs {
		if proc.BundlePath != "" {
			continue
		}
		if strings.ToLower(proc.Name) == exec {
			out = append(out, proc)
		}
	}
	return out
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}
