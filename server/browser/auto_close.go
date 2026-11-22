package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// Auto-close idle windows
//
// The rail lets a person close windows by hand, but the CPU a forgotten window
// costs is paid while nobody is looking at the rail at all. This file owns one
// daemon-side rule: windows nobody has used for IdleSeconds are closed, at most
// once per tick, and every close lands in a last-10 record that survives a page
// reload (but not a daemon restart).
//
// Activity is observed, not stored in Chrome: every "windows" payload marks the
// focused window as active, every mirror frame or input marks the window being
// watched, and any job routed to the current window does too. A window seen for
// the first time starts with a full interval, so a daemon restart never closes
// the browser before anyone has had a chance to use it.

const (
	autoCloseDefaultIdle = time.Hour
	autoCloseMinIdle     = 5 * time.Minute
	autoCloseMaxIdle     = 24 * time.Hour
	autoCloseRecordLimit = 10
	autoCloseTickEvery   = 30 * time.Second
)

var errAutoCloseIdleRange = fmt.Errorf(
	"idle_seconds must be between %d and %d",
	int(autoCloseMinIdle.Seconds()),
	int(autoCloseMaxIdle.Seconds()),
)

// AutoCloseConfig is the persisted rule (browser-config.json). Enabled is
// explicit because arming a destructive timer is never a default.
type AutoCloseConfig struct {
	Enabled     bool `json:"enabled"`
	IdleSeconds int  `json:"idle_seconds,omitempty"`
}

func defaultAutoCloseConfig() AutoCloseConfig {
	return AutoCloseConfig{IdleSeconds: int(autoCloseDefaultIdle.Seconds())}
}

// normalized repairs a config that was written by hand or by an older build.
// A zero interval means "the default", not "close immediately".
func (c AutoCloseConfig) normalized() AutoCloseConfig {
	if c.IdleSeconds <= 0 {
		c.IdleSeconds = int(autoCloseDefaultIdle.Seconds())
	}
	if c.IdleSeconds < int(autoCloseMinIdle.Seconds()) {
		c.IdleSeconds = int(autoCloseMinIdle.Seconds())
	}
	if c.IdleSeconds > int(autoCloseMaxIdle.Seconds()) {
		c.IdleSeconds = int(autoCloseMaxIdle.Seconds())
	}
	return c
}

// AutoCloseRecord is one closed window. It is small on purpose: the record is
// the only trace left after the window (and its session page) is gone.
type AutoCloseRecord struct {
	ID          int64  `json:"id"`
	WindowID    int64  `json:"window_id"`
	SessionID   string `json:"session_id,omitempty"`
	Title       string `json:"title,omitempty"`
	Tabs        int    `json:"tabs"`
	IdleSeconds int    `json:"idle_seconds"`
	ClosedAt    string `json:"closed_at"`
}

// AutoCloseStatus is the whole client-facing state: the rule, whether this
// extension can execute it, and the last ten closes.
type AutoCloseStatus struct {
	Enabled     bool              `json:"enabled"`
	IdleSeconds int               `json:"idle_seconds"`
	Supported   bool              `json:"supported"`
	Records     []AutoCloseRecord `json:"records"`
}

type windowActivity struct {
	lastActiveAt time.Time
	title        string
	sessionID    string
	tabs         int
}

type autoCloseCandidate struct {
	win  *windowSnapshot
	idle time.Duration
}

// observeWindows folds one extension listing into the activity ledger. A window
// is active when Chrome says it is focused; a window seen for the first time is
// active now. Windows that disappeared are dropped together with their history.
func (m *Manager) observeWindows(payload *windowsPayload) {
	if payload == nil {
		return
	}
	now := m.now()
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	if m.autoActive == nil {
		m.autoActive = make(map[int64]*windowActivity)
	}
	m.autoSupported = payload.supportsAllWindows() && payload.supportsWindowClose()
	seen := make(map[int64]bool, len(payload.Windows))
	for i := range payload.Windows {
		win := &payload.Windows[i]
		if win.WindowID == 0 || !drivableWindow(win) {
			continue
		}
		seen[win.WindowID] = true
		activity := m.autoActive[win.WindowID]
		if activity == nil {
			activity = &windowActivity{lastActiveAt: now}
			m.autoActive[win.WindowID] = activity
		}
		if win.Focused {
			activity.lastActiveAt = now
		}
		activity.title, _ = windowLabel(win)
		activity.tabs = len(win.Tabs)
		if len(win.SessionIDs) > 0 {
			activity.sessionID = win.SessionIDs[0]
		}
	}
	for id := range m.autoActive {
		if !seen[id] {
			delete(m.autoActive, id)
		}
	}
}

// touchWindow records a person or agent using one window right now.
func (m *Manager) touchWindow(windowID int64) {
	if windowID == 0 {
		return
	}
	now := m.now()
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	if m.autoActive == nil {
		m.autoActive = make(map[int64]*windowActivity)
	}
	activity := m.autoActive[windowID]
	if activity == nil {
		activity = &windowActivity{}
		m.autoActive[windowID] = activity
	}
	activity.lastActiveAt = now
}

// touchCurrentWindow marks the window the page is driving as active. Called
// from frame and input paths: watching a mirror is using that window.
func (m *Manager) touchCurrentWindow() {
	m.touchWindow(m.windowIDValue())
}

// beginBrowserJob/finishBrowserJob track jobs so a tick never closes a window
// while the daemon is in the middle of using it. Passive capture and polling
// are excluded on purpose: the mirror takes a screenshot several times a
// second, and counting it would keep the rule from ever running.
func (m *Manager) beginBrowserJob(typ string) bool {
	switch typ {
	case "screenshot", "info":
		return false
	}
	m.autoMu.Lock()
	m.autoJobs++
	m.autoMu.Unlock()
	return true
}

func (m *Manager) finishBrowserJob() {
	m.autoMu.Lock()
	if m.autoJobs > 0 {
		m.autoJobs--
	}
	m.autoMu.Unlock()
}

func (m *Manager) jobsInFlight() int {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	return m.autoJobs
}

func (m *Manager) now() time.Time {
	if m.autoNow != nil {
		return m.autoNow()
	}
	return time.Now()
}

// idleSecondsFor is what the rail's per-window chip shows. Unknown windows read
// as zero ("just active"), which is the safe answer for a destructive rule.
func (m *Manager) idleSecondsFor(windowID int64, now time.Time) int {
	if windowID == 0 {
		return 0
	}
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	activity := m.autoActive[windowID]
	if activity == nil || activity.lastActiveAt.IsZero() {
		return 0
	}
	idle := now.Sub(activity.lastActiveAt)
	if idle < 0 {
		return 0
	}
	return int(idle.Seconds())
}

// autoCloseStatus snapshots the rule for the API and the sessions poll.
func (m *Manager) autoCloseStatus() AutoCloseStatus {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	records := make([]AutoCloseRecord, len(m.autoRecords))
	copy(records, m.autoRecords)
	if records == nil {
		records = []AutoCloseRecord{}
	}
	return AutoCloseStatus{
		Enabled:     m.autoClose.Enabled,
		IdleSeconds: m.autoClose.IdleSeconds,
		Supported:   m.autoSupported,
		Records:     records,
	}
}

// SetAutoClose validates and persists one change. idleSeconds <= 0 keeps the
// current interval, so a pure on/off toggle never rewrites it.
func (m *Manager) SetAutoClose(enabled bool, idleSeconds int) (AutoCloseStatus, error) {
	m.autoMu.Lock()
	current := m.autoClose
	m.autoMu.Unlock()
	if idleSeconds <= 0 {
		idleSeconds = current.IdleSeconds
	}
	if idleSeconds == 0 {
		idleSeconds = int(autoCloseDefaultIdle.Seconds())
	}
	if idleSeconds < int(autoCloseMinIdle.Seconds()) || idleSeconds > int(autoCloseMaxIdle.Seconds()) {
		return AutoCloseStatus{}, errAutoCloseIdleRange
	}
	cfg := AutoCloseConfig{Enabled: enabled, IdleSeconds: idleSeconds}.normalized()

	persisted := loadConfig()
	persisted.AutoClose = &cfg
	if err := saveConfig(persisted); err != nil {
		return AutoCloseStatus{}, err
	}
	m.autoMu.Lock()
	m.autoClose = cfg
	m.autoMu.Unlock()
	return m.autoCloseStatus(), nil
}

// StartAutoCloseLoop starts the one daemon-side ticker. Safe to call repeatedly;
// the rule itself is checked on every tick, so switching it off costs nothing
// but one idle timer wake-up.
func (m *Manager) StartAutoCloseLoop(ctx context.Context) {
	m.autoLoopOnce.Do(func() {
		go m.autoCloseLoop(ctx)
	})
}

func (m *Manager) autoCloseLoop(ctx context.Context) {
	ticker := time.NewTicker(autoCloseTickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			_, _ = m.AutoCloseTick(tickCtx)
			cancel()
		}
	}
}

// AutoCloseTick runs one pass. It returns the records it appended so tests (and
// a future manual "close now" button) can assert exactly what happened.
func (m *Manager) AutoCloseTick(ctx context.Context) ([]AutoCloseRecord, error) {
	m.autoMu.Lock()
	cfg := m.autoClose
	m.autoMu.Unlock()
	if !cfg.Enabled || m.isReadOnlyValue() {
		return nil, nil
	}
	// Never interleave with an attach, a close or a sweep. Skipping one tick is
	// cheaper than making a person wait behind the timer.
	if !m.attachMu.TryLock() {
		return nil, nil
	}
	defer m.attachMu.Unlock()
	if m.jobsInFlight() > 0 {
		return nil, nil
	}

	rows, err := m.d.sessions(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := m.fetchWindowsForClose(ctx, rows)
	if err != nil {
		m.autoMu.Lock()
		m.autoSupported = false
		m.autoMu.Unlock()
		return nil, err
	}
	now := m.now()
	m.observeWindows(payload)
	if !payload.supportsAllWindows() || !payload.supportsWindowClose() {
		return nil, nil
	}

	// A session channel anchor is only needed by an extension without
	// direct-window-v1; the instance channel closes windows with no /go page.
	anchor := ""
	if !m.directSupportedValue() {
		anchor = pickWindowCloseAnchor(m.sessionIDValue(), rows, m.connectedValue())
		if anchor == "" {
			return nil, nil
		}
	}
	keep := map[int64]bool{}
	if cur := m.windowIDValue(); cur != 0 {
		keep[cur] = true
	}
	if win := payload.windowForSession(m.sessionIDValue()); win != nil {
		keep[win.WindowID] = true
	}
	// The anchor's own window carries the transport the close job travels on.
	if anchor != "" {
		if win := payload.windowForSession(anchor); win != nil {
			keep[win.WindowID] = true
		}
	}

	m.autoMu.Lock()
	active := make(map[int64]*windowActivity, len(m.autoActive))
	for id, activity := range m.autoActive {
		active[id] = activity
	}
	m.autoMu.Unlock()

	threshold := time.Duration(cfg.IdleSeconds) * time.Second
	candidates := idleCandidates(payload, active, keep, threshold, now)

	var closed []AutoCloseRecord
	for _, candidate := range candidates {
		if win := payload.windowByID(candidate.win.WindowID); win == nil || win.Focused {
			continue
		}
		if err := m.closeWindowJob(ctx, anchor, candidate.win.WindowID); err != nil {
			// A window Chrome already removed is not a failure worth stopping
			// for; anything else leaves the window for the next tick.
			continue
		}
		closed = append(closed, m.recordAutoClose(candidate, now))
	}
	return closed, nil
}

// idleCandidates is the pure part of the tick: which windows are due, oldest
// first, never the last drivable window, never a kept or focused one.
func idleCandidates(
	payload *windowsPayload,
	active map[int64]*windowActivity,
	keep map[int64]bool,
	threshold time.Duration,
	now time.Time,
) []autoCloseCandidate {
	if payload == nil {
		return nil
	}
	drivable := 0
	for i := range payload.Windows {
		if drivableWindow(&payload.Windows[i]) {
			drivable++
		}
	}
	if drivable <= 1 {
		return nil
	}
	candidates := make([]autoCloseCandidate, 0, drivable)
	for i := range payload.Windows {
		win := &payload.Windows[i]
		if !drivableWindow(win) || win.WindowID == 0 || win.Focused || keep[win.WindowID] {
			continue
		}
		activity := active[win.WindowID]
		if activity == nil || activity.lastActiveAt.IsZero() {
			continue
		}
		idle := now.Sub(activity.lastActiveAt)
		if idle < threshold {
			continue
		}
		candidates = append(candidates, autoCloseCandidate{win: win, idle: idle})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].idle != candidates[j].idle {
			return candidates[i].idle > candidates[j].idle
		}
		return candidates[i].win.WindowID < candidates[j].win.WindowID
	})
	// Closing all but the last window would quit Chrome; stop one short.
	if len(candidates) >= drivable {
		candidates = candidates[:drivable-1]
	}
	return candidates
}

// recordAutoClose appends to the in-memory ring. The slice is newest-first, so
// the dialog can render it without reversing anything, and the 11th close drops
// the oldest record.
func (m *Manager) recordAutoClose(candidate autoCloseCandidate, now time.Time) AutoCloseRecord {
	win := candidate.win
	title, _ := windowLabel(win)
	rec := AutoCloseRecord{
		WindowID:    win.WindowID,
		Title:       title,
		Tabs:        len(win.Tabs),
		IdleSeconds: int(candidate.idle.Seconds()),
		ClosedAt:    now.UTC().Format(time.RFC3339),
	}
	if len(win.SessionIDs) > 0 {
		rec.SessionID = win.SessionIDs[0]
	}
	m.autoMu.Lock()
	m.autoSeq++
	rec.ID = m.autoSeq
	m.autoRecords = append([]AutoCloseRecord{rec}, m.autoRecords...)
	if len(m.autoRecords) > autoCloseRecordLimit {
		m.autoRecords = m.autoRecords[:autoCloseRecordLimit]
	}
	delete(m.autoActive, win.WindowID)
	m.autoMu.Unlock()
	return rec
}

// AutoCloseStatus exposes the rule to the page.
func (m *Manager) AutoCloseStatus() AutoCloseStatus {
	return m.autoCloseStatus()
}

// ValidateAutoCloseIdle is the API's range check, kept next to the constants so
// the handler cannot drift from them.
func ValidateAutoCloseIdle(seconds int) error {
	if seconds != 0 && (seconds < int(autoCloseMinIdle.Seconds()) || seconds > int(autoCloseMaxIdle.Seconds())) {
		return errAutoCloseIdleRange
	}
	return nil
}

// handleAutoClose reads or changes the rule. GET is what the settings card
// re-reads after a save; PUT keeps the on/off toggle and the interval in one
// request so the two can never be persisted half-applied.
func (m *Manager) handleAutoClose(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, m.autoCloseStatus())
	case http.MethodPut, http.MethodPost:
		var body struct {
			Enabled     *bool `json:"enabled"`
			IdleSeconds int   `json:"idle_seconds"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if err := dec.Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
			return
		}
		m.autoMu.Lock()
		current := m.autoClose
		m.autoMu.Unlock()
		enabled := current.Enabled
		if body.Enabled != nil {
			enabled = *body.Enabled
		}
		if err := ValidateAutoCloseIdle(body.IdleSeconds); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if enabled && m.isReadOnlyValue() {
			writeError(w, http.StatusConflict, errReadOnly.Error())
			return
		}
		status, err := m.SetAutoClose(enabled, body.IdleSeconds)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
