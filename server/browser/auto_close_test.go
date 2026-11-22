package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func autoCloseNow() time.Time {
	return time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
}

// autoCloseManager is a three-window browser: 900 is the current, focused
// window; 910 has been idle for 90 minutes; 920 is only just behind at 10.
func autoCloseManager(t *testing.T, f *fakeClient) (*Manager, time.Time) {
	t.Helper()
	m := newCloseManager(t, f)
	now := autoCloseNow()
	m.autoNow = func() time.Time { return now }
	m.autoClose = AutoCloseConfig{Enabled: true, IdleSeconds: 3600}
	m.autoActive = map[int64]*windowActivity{
		900: {lastActiveAt: now},
		910: {lastActiveAt: now.Add(-90 * time.Minute), title: "Docs", tabs: 4},
		920: {lastActiveAt: now.Add(-10 * time.Minute), title: "Search", tabs: 8},
	}
	return m, now
}

func autoClosePayload(open ...windowSnapshot) *windowsPayload {
	return sweepPayload(open...)
}

func autoCloseFake(rows []sessionRow, payload *windowsPayload) *fakeClient {
	return newSweepFake(rows, payload)
}

func TestAutoCloseTickClosesOnlyWindowsPastTheInterval(t *testing.T) {
	payload := autoClosePayload(
		sweepWindow(900, true, []string{"s1"}, sweepTab(901, sweepSessionURL+"s1")),
		sweepWindow(910, false, []string{"s2"}, sweepTab(911, "https://docs.example/")),
		sweepWindow(920, false, nil, sweepTab(921, "https://search.example/")),
	)
	m, now := autoCloseManager(t, autoCloseFake(sweepRows(), payload))

	closed, err := m.AutoCloseTick(context.Background())
	if err != nil {
		t.Fatalf("AutoCloseTick: %v", err)
	}
	if len(closed) != 1 || closed[0].WindowID != 910 {
		t.Fatalf("closed = %+v, want window 910 only", closed)
	}
	if got := closed[0].IdleSeconds; got != 5400 {
		t.Fatalf("idle_seconds = %d, want 5400", got)
	}
	if got := closed[0].Tabs; got != 1 {
		t.Fatalf("tabs = %d, want 1", got)
	}
	if closed[0].ClosedAt != now.Format(time.RFC3339) {
		t.Fatalf("closed_at = %q", closed[0].ClosedAt)
	}
	calls := m.d.(*fakeClient).all("window")
	if len(calls) != 1 {
		t.Fatalf("window jobs = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].Params["op"] != "close" || calls[0].Params["window_id"] != int64(910) {
		t.Fatalf("close job = %+v", calls[0])
	}
	status := m.AutoCloseStatus()
	if len(status.Records) != 1 || status.Records[0].WindowID != 910 {
		t.Fatalf("records = %+v", status.Records)
	}
}

// No exemptions: the focused window and the window ai-critic is driving are
// closed like any other once they pass the interval.
func TestAutoCloseClosesFocusedAndCurrentWindows(t *testing.T) {
	payload := autoClosePayload(
		// 900 is the window ai-critic drives and the focused one at that.
		sweepWindow(900, true, []string{"s1"}, sweepTab(901, sweepSessionURL+"s1")),
		sweepWindow(910, true, []string{"s2"}, sweepTab(911, "https://docs.example/")),
		sweepWindow(920, false, nil, sweepTab(921, "https://search.example/")),
	)
	m, now := autoCloseManager(t, autoCloseFake(sweepRows(), payload))
	m.mu.Lock()
	m.sessionID = "s1"
	m.windowID = 900
	m.mu.Unlock()
	m.autoActive[900] = &windowActivity{lastActiveAt: now.Add(-3 * time.Hour)}
	m.autoActive[910] = &windowActivity{lastActiveAt: now.Add(-3 * time.Hour)}
	m.autoActive[920] = &windowActivity{lastActiveAt: now.Add(-3 * time.Hour)}

	closed, err := m.AutoCloseTick(context.Background())
	if err != nil {
		t.Fatalf("AutoCloseTick: %v", err)
	}
	if len(closed) != 3 {
		t.Fatalf("closed = %+v, want all three windows", closed)
	}
	got := map[int64]bool{}
	for _, rec := range closed {
		got[rec.WindowID] = true
	}
	for _, want := range []int64{900, 910, 920} {
		if !got[want] {
			t.Fatalf("window %d not closed: %+v", want, closed)
		}
	}
	// The manager must stop claiming a window it just closed.
	if m.windowIDValue() != 0 || m.Snapshot().Session.Connected {
		t.Fatalf("manager still points at the closed window: id=%d connected=%v",
			m.windowIDValue(), m.Snapshot().Session.Connected)
	}
}

func TestAutoCloseTickDoesNothingWhenDisabled(t *testing.T) {
	payload := autoClosePayload(
		sweepWindow(900, true, []string{"s1"}),
		sweepWindow(910, false, []string{"s2"}),
	)
	m, _ := autoCloseManager(t, autoCloseFake(sweepRows(), payload))
	m.autoClose.Enabled = false

	closed, err := m.AutoCloseTick(context.Background())
	if err != nil {
		t.Fatalf("AutoCloseTick: %v", err)
	}
	if len(closed) != 0 {
		t.Fatalf("closed = %+v, want none", closed)
	}
	if calls := m.d.(*fakeClient).all("window"); len(calls) != 0 {
		t.Fatalf("window jobs = %d, want 0", len(calls))
	}
}

// The last window is closed too: Chrome quits with it, and the next explicit
// action reopens one. Keeping it alive was the exemption that made a stale idle
// window immortal.
func TestAutoCloseClosesTheLastWindow(t *testing.T) {
	payload := autoClosePayload(sweepWindow(910, false, nil))
	m, _ := autoCloseManager(t, autoCloseFake(sweepRows(), payload))
	m.autoActive = map[int64]*windowActivity{
		910: {lastActiveAt: autoCloseNow().Add(-3 * time.Hour)},
	}
	closed, err := m.AutoCloseTick(context.Background())
	if err != nil {
		t.Fatalf("AutoCloseTick: %v", err)
	}
	if len(closed) != 1 || closed[0].WindowID != 910 {
		t.Fatalf("closed = %+v, want the only window 910", closed)
	}
}

func TestIdleCandidatesRespectsThresholdBoundary(t *testing.T) {
	now := autoCloseNow()
	payload := autoClosePayload(
		sweepWindow(900, true, []string{"s1"}),
		sweepWindow(910, false, []string{"s2"}),
		sweepWindow(920, false, nil),
	)
	active := map[int64]*windowActivity{
		900: {lastActiveAt: now.Add(-2 * time.Hour)},
		910: {lastActiveAt: now.Add(-time.Hour)},
		920: {lastActiveAt: now.Add(-time.Hour + time.Second)},
	}
	got := idleCandidates(payload, active, time.Hour, now)
	if len(got) != 2 {
		t.Fatalf("candidates = %+v, want 900 and 910 (the exact boundary is due)", got)
	}
	// Oldest first: 900 before 910; 920 is one second short and stays.
	if got[0].win.WindowID != 900 || got[1].win.WindowID != 910 {
		t.Fatalf("candidates = %+v, want [900 910]", got)
	}
}

func TestAutoCloseRecordKeepsNewestTen(t *testing.T) {
	m := &Manager{autoActive: map[int64]*windowActivity{}}
	now := autoCloseNow()
	for i := 1; i <= 12; i++ {
		m.recordAutoClose(autoCloseCandidate{
			win:  &windowSnapshot{WindowID: int64(100 + i)},
			idle: time.Hour,
		}, now)
	}
	status := m.AutoCloseStatus()
	if len(status.Records) != 10 {
		t.Fatalf("records = %d, want 10", len(status.Records))
	}
	if status.Records[0].WindowID != 112 || status.Records[9].WindowID != 103 {
		t.Fatalf("records order = %d..%d, want 112..103", status.Records[0].WindowID, status.Records[9].WindowID)
	}
	if status.Records[0].ID <= status.Records[1].ID {
		t.Fatalf("record ids are not increasing: %+v", status.Records[:2])
	}
}

func TestSetAutoCloseValidatesAndPersists(t *testing.T) {
	m := newTestManager(t, &fakeClient{})
	m.autoNow = time.Now

	if _, err := m.SetAutoClose(true, 120); err == nil {
		t.Fatal("SetAutoClose(120s) = nil error, want range error")
	}
	status, err := m.SetAutoClose(true, 3600)
	if err != nil {
		t.Fatalf("SetAutoClose: %v", err)
	}
	if !status.Enabled || status.IdleSeconds != 3600 {
		t.Fatalf("status = %+v", status)
	}
	cfg := loadConfig()
	if cfg.AutoClose == nil || !cfg.AutoClose.Enabled || cfg.AutoClose.IdleSeconds != 3600 {
		t.Fatalf("persisted auto_close = %+v", cfg.AutoClose)
	}

	// Toggling off without a new interval keeps the saved one.
	status, err = m.SetAutoClose(false, 0)
	if err != nil {
		t.Fatalf("SetAutoClose off: %v", err)
	}
	if status.Enabled || status.IdleSeconds != 3600 {
		t.Fatalf("off status = %+v", status)
	}
}

func TestAutoCloseConfigNormalizesCorruptValues(t *testing.T) {
	if got := (AutoCloseConfig{}).normalized(); got.IdleSeconds != 3600 {
		t.Fatalf("zero interval = %d, want 3600", got.IdleSeconds)
	}
	if got := (AutoCloseConfig{IdleSeconds: 1}).normalized(); got.IdleSeconds != 300 {
		t.Fatalf("tiny interval = %d, want 300", got.IdleSeconds)
	}
	if got := (AutoCloseConfig{IdleSeconds: 999999}).normalized(); got.IdleSeconds != 86400 {
		t.Fatalf("huge interval = %d, want 86400", got.IdleSeconds)
	}
}

// Focus is not activity: a window left in front ages exactly like any other, so
// the rule can retire it. Only real input through the mirror resets a clock.
func TestObserveWindowsSeedsAndAgesEveryWindow(t *testing.T) {
	m := newTestManager(t, &fakeClient{})
	now := autoCloseNow()
	m.autoNow = func() time.Time { return now }

	payload := autoClosePayload(
		sweepWindow(900, true, []string{"s1"}),
		sweepWindow(910, false, []string{"s2"}),
	)
	m.observeWindows(payload)
	if got := m.idleSecondsFor(900, now); got != 0 {
		t.Fatalf("focused idle = %d, want 0", got)
	}
	if got := m.idleSecondsFor(910, now); got != 0 {
		t.Fatalf("new window idle = %d, want 0", got)
	}

	// A later tick ages both windows, focused or not.
	later := now.Add(2 * time.Hour)
	m.autoNow = func() time.Time { return later }
	m.observeWindows(payload)
	if got := m.idleSecondsFor(900, later); got != 7200 {
		t.Fatalf("focused idle = %d, want 7200 (focus is not activity)", got)
	}
	if got := m.idleSecondsFor(910, later); got != 7200 {
		t.Fatalf("idle = %d, want 7200", got)
	}

	// Input through the mirror is what resets a clock.
	m.touchWindow(900)
	if got := m.idleSecondsFor(900, later); got != 0 {
		t.Fatalf("touched idle = %d, want 0", got)
	}

	// A vanished window drops out of the ledger entirely.
	m.observeWindows(autoClosePayload(sweepWindow(900, true, []string{"s1"})))
	if got := m.idleSecondsFor(910, later); got != 0 {
		t.Fatalf("removed window idle = %d, want 0", got)
	}
}

func TestAutoCloseIgnoresPassiveCapture(t *testing.T) {
	m := &Manager{autoActive: map[int64]*windowActivity{}}
	if m.beginBrowserJob("screenshot") {
		t.Fatal("screenshot counted as an interactive job")
	}
	if m.beginBrowserJob("info") {
		t.Fatal("info counted as an interactive job")
	}
	if !m.beginBrowserJob("eval") {
		t.Fatal("eval was not counted as an interactive job")
	}
	m.finishBrowserJob()
	if m.jobsInFlight() != 0 {
		t.Fatalf("jobs in flight = %d, want 0", m.jobsInFlight())
	}
}

// An in-flight job is not an exemption either: the window still goes when it is
// past the interval. Only the tick's own concurrency lock may skip a pass.
func TestAutoCloseClosesWhileAJobIsInFlight(t *testing.T) {
	payload := autoClosePayload(
		sweepWindow(900, true, []string{"s1"}),
		sweepWindow(910, false, []string{"s2"}),
	)
	m, now := autoCloseManager(t, autoCloseFake(sweepRows(), payload))
	m.autoActive[900] = &windowActivity{lastActiveAt: now.Add(-3 * time.Hour)}
	m.autoActive[910] = &windowActivity{lastActiveAt: now.Add(-3 * time.Hour)}
	m.beginBrowserJob("eval")
	defer m.finishBrowserJob()

	closed, err := m.AutoCloseTick(context.Background())
	if err != nil {
		t.Fatalf("AutoCloseTick: %v", err)
	}
	if len(closed) != 2 {
		t.Fatalf("closed = %+v, want both idle windows despite the job", closed)
	}
}

func TestSessionsCarriesIdleAndTheRule(t *testing.T) {
	payload := autoClosePayload(
		sweepWindow(900, true, []string{"s1"}, sweepTab(901, sweepSessionURL+"s1")),
		sweepWindow(910, false, []string{"s2"}, sweepTab(911, "https://docs.example/")),
		sweepWindow(920, false, nil, sweepTab(921, "https://search.example/")),
	)
	m, now := autoCloseManager(t, autoCloseFake(sweepRows(), payload))
	m.autoActive[910] = &windowActivity{lastActiveAt: now.Add(-42 * time.Minute), title: "Docs", tabs: 4}

	resp := m.Sessions(context.Background())
	if !resp.OK {
		t.Fatalf("Sessions error: %s", resp.Error)
	}
	if !resp.AutoClose.Enabled || resp.AutoClose.IdleSeconds != 3600 {
		t.Fatalf("auto_close = %+v", resp.AutoClose)
	}
	var idle int
	for _, row := range resp.Sessions {
		if row.WindowID == 910 {
			idle = row.IdleSeconds
		}
	}
	if idle != 2520 {
		t.Fatalf("idle_seconds for window 910 = %d, want 2520", idle)
	}
}

func TestAutoCloseHandlerValidatesAndSaves(t *testing.T) {
	m := newTestManager(t, &fakeClient{})
	m.autoNow = time.Now

	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/browser/auto-close", strings.NewReader(body))
		rec := httptest.NewRecorder()
		m.handleAutoClose(rec, req)
		return rec
	}

	if rec := put(`{"enabled":true,"idle_seconds":120}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("short interval status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if rec := put(`{"enabled":true,"idle_seconds":3600}`); rec.Code != http.StatusOK {
		t.Fatalf("save status = %d: %s", rec.Code, rec.Body.String())
	}
	if !m.AutoCloseStatus().Enabled {
		t.Fatal("rule is not enabled after PUT")
	}
	if rec := put(`{"enabled":true,"idle_seconds":90000}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("long interval status = %d, want 400", rec.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/browser/auto-close", nil)
	getRec := httptest.NewRecorder()
	m.handleAutoClose(getRec, getReq)
	if getRec.Code != http.StatusOK || !strings.Contains(getRec.Body.String(), `"idle_seconds":3600`) {
		t.Fatalf("GET body = %s", getRec.Body.String())
	}
}
