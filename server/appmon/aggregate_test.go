package appmon

import (
	"testing"

	"github.com/xhd2015/ai-critic/server/procmon"
)

func viewFixture() *Inventory {
	return &Inventory{
		ScannedAt: "2026-11-13T07:00:00Z",
		Platform:  "darwin",
		Dirs:      []string{"/Applications"},
		Apps: []App{
			{Name: "Alpha", Path: "/Applications/Alpha.app", HasIcon: true},
			{Name: "Bravo", Path: "/Applications/Bravo.app"},
			{Name: "Charlie", Path: "/Applications/Charlie.app", Exec: "charlie-bin"},
			{Name: "Delta", Path: "/Applications/Delta.app"},
		},
	}
}

func snapFn(procs ...procmon.Process) func() (*procmon.Snapshot, error) {
	return func() (*procmon.Snapshot, error) {
		return &procmon.Snapshot{SampledAt: "2026-11-13T07:00:01Z", Processes: procs}, nil
	}
}

func TestJoinAttributesAndOrders(t *testing.T) {
	procs := []procmon.Process{
		{PID: 10, Name: "Alpha", BundlePath: "/Applications/Alpha.app", CPUPercent: 2.5, RSSBytes: 100},
		{PID: 11, Name: "Alpha Helper", BundlePath: "/Applications/Alpha.app", CPUPercent: 1.2, RSSBytes: 50},
		{PID: 20, Name: "charlie-bin", CPUPercent: 9, RSSBytes: 900},
		{PID: 30, Name: "unrelated", CPUPercent: 50, RSSBytes: 5000},
	}
	view := join(viewFixture(), snapFn(procs...))

	if view.SampledAt != "2026-11-13T07:00:01Z" {
		t.Errorf("sampled_at=%q", view.SampledAt)
	}
	if view.Running != 2 {
		t.Errorf("running=%d", view.Running)
	}

	byName := map[string]AppView{}
	var order []string
	for _, app := range view.Apps {
		byName[app.Name] = app
		order = append(order, app.Name)
	}
	// Running apps first, heaviest CPU first; idle ones keep scan order.
	if want := []string{"Charlie", "Alpha", "Bravo", "Delta"}; !equalStrings(order, want) {
		t.Fatalf("order=%v want %v", order, want)
	}

	alpha := byName["Alpha"]
	if alpha.ProcessCount != 2 || len(alpha.PIDs) != 2 || alpha.PIDs[0] != 10 || alpha.PIDs[1] != 11 {
		t.Errorf("alpha procs=%+v", alpha)
	}
	if alpha.CPUPercent != 3.7 {
		t.Errorf("alpha cpu=%v", alpha.CPUPercent)
	}
	if alpha.RSSBytes != 150 {
		t.Errorf("alpha rss=%d", alpha.RSSBytes)
	}
	if alpha.IconURL != "/api/server/apps/icon?path=%2FApplications%2FAlpha.app" {
		t.Errorf("icon_url=%q", alpha.IconURL)
	}

	// Exec fallback only applies to processes with no bundle path.
	if charlie := byName["Charlie"]; charlie.ProcessCount != 1 {
		t.Errorf("charlie=%+v", charlie)
	}
	if delta := byName["Delta"]; delta.ProcessCount != 0 || delta.PIDs != nil || delta.IconURL != "" {
		t.Errorf("delta=%+v", delta)
	}
	if bravo := byName["Bravo"]; bravo.CPUPercent != 0 || bravo.RSSBytes != 0 {
		t.Errorf("bravo=%+v", bravo)
	}
}

func TestJoinWithoutSnapshot(t *testing.T) {
	view := join(viewFixture(), nil)
	if len(view.Apps) != 4 || view.Running != 0 {
		t.Fatalf("apps=%d running=%d", len(view.Apps), view.Running)
	}
	for _, app := range view.Apps {
		if app.ProcessCount != 0 || len(app.PIDs) != 0 {
			t.Errorf("unexpected procs: %+v", app)
		}
	}
}

func TestJoinSnapshotErrorDegrades(t *testing.T) {
	fn := func() (*procmon.Snapshot, error) { return nil, errAny }
	view := join(viewFixture(), fn)
	if len(view.Apps) != 4 || view.Running != 0 || view.SampledAt != "" {
		t.Fatalf("view=%+v", view)
	}
}

var errAny = &testError{"boom"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
