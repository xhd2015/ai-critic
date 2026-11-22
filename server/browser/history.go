package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HistoryStatus is the navigation position of the driven tab: where the person
// is in the tab's own history, and whether stepping either way is possible. The
// nav row renders its arrows from this instead of guessing from the URL.
type HistoryStatus struct {
	Index      int  `json:"index"`
	Count      int  `json:"count"`
	CanBack    bool `json:"can_back"`
	CanForward bool `json:"can_forward"`
}

type navigationEntry struct {
	ID    int64  `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

type navigationHistory struct {
	CurrentIndex int               `json:"currentIndex"`
	Entries      []navigationEntry `json:"entries"`
}

// History reads the driven tab's navigation list. A tab that has no history yet
// (about:blank, or a page Chrome has not committed) answers with an empty one
// rather than an error: the arrows are simply disabled.
func (m *Manager) History(ctx context.Context) HistoryStatus {
	tab := m.contentTab()
	if tab == 0 {
		return HistoryStatus{}
	}
	payload, err := m.historyPayload(ctx, tab)
	if err != nil {
		return HistoryStatus{}
	}
	return historyStatusOf(payload)
}

// historyTTL bounds how often the cached position is re-read. The nav row asks
// for status on every render, and a CDP call per render would be worse than a
// slightly stale arrow.
const historyTTL = 1500 * time.Millisecond

// refreshHistoryCache re-reads the navigation position and stores it for
// Snapshot. Callers are the tab poll and the status handler: the poll only runs
// while somebody is watching, so without the status path a page opened with no
// viewer would report a permanently empty history.
func (m *Manager) refreshHistoryCache(ctx context.Context) {
	if !m.historyDue() {
		return
	}
	tab := m.contentTab()
	if tab == 0 {
		return
	}
	payload, err := m.historyPayload(ctx, tab)
	if err != nil {
		// A tab mid-navigation can fail this; keep the previous value rather than
		// blanking the arrows.
		return
	}
	status := historyStatusOf(payload)
	m.mu.Lock()
	m.history = status
	m.historyAt = time.Now()
	m.mu.Unlock()
}

// historyDue reports whether the cached position is old enough to re-read.
func (m *Manager) historyDue() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connected == false || m.contentTabID == 0 {
		return false
	}
	return time.Since(m.historyAt) >= historyTTL
}

func historyStatusOf(h *navigationHistory) HistoryStatus {
	if h == nil || len(h.Entries) == 0 {
		return HistoryStatus{}
	}
	index := h.CurrentIndex
	if index < 0 {
		index = 0
	}
	if index >= len(h.Entries) {
		index = len(h.Entries) - 1
	}
	return HistoryStatus{
		Index:      index,
		Count:      len(h.Entries),
		CanBack:    index > 0,
		CanForward: index < len(h.Entries)-1,
	}
}

func (m *Manager) historyPayload(ctx context.Context, tab int64) (*navigationHistory, error) {
	data, err := m.job(ctx, tab, "cdp", map[string]any{
		"method": "Page.getNavigationHistory",
		"params": map[string]any{},
	}, 15*time.Second)
	if err != nil {
		return nil, err
	}
	// The direct rail answers with the whole CDP envelope
	// ({"method":…,"result":{…}}), while a session-channel job hands back the
	// inner result. Accept both: the shape depends on which channel carried the
	// job, and that is not something this function should care about.
	var envelope struct {
		Result *navigationHistory `json:"result"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Result != nil {
		return envelope.Result, nil
	}
	var out navigationHistory
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode navigation history: %w", err)
	}
	return &out, nil
}

var errHistoryEnd = fmt.Errorf("no more history in that direction")

// StepHistory moves the driven tab one entry back or forward. It refuses at the
// ends instead of silently doing nothing, so the page can explain why the arrow
// was disabled.
func (m *Manager) StepHistory(ctx context.Context, forward bool) error {
	if err := m.Ensure(ctx); err != nil {
		return err
	}
	tab := m.contentTab()
	if tab == 0 {
		return errNoPageTab
	}
	h, err := m.historyPayload(ctx, tab)
	if err != nil {
		return err
	}
	status := historyStatusOf(h)
	if forward && !status.CanForward {
		return errHistoryEnd
	}
	if !forward && !status.CanBack {
		return errHistoryEnd
	}
	target := h.CurrentIndex - 1
	if forward {
		target = h.CurrentIndex + 1
	}
	entryID := h.Entries[target].ID
	if _, err := m.job(ctx, tab, "cdp", map[string]any{
		"method": "Page.navigateToHistoryEntry",
		"params": map[string]any{"entryId": entryID},
	}, 20*time.Second); err != nil {
		return fmt.Errorf("move through history: %w", err)
	}
	// The entry's URL is what the address bar should show immediately; the tab
	// list refresh catches up on the next poll.
	m.mu.Lock()
	m.url = reportableURL(h.Entries[target].URL)
	m.history = HistoryStatus{
		Index:      target,
		Count:      len(h.Entries),
		CanBack:    target > 0,
		CanForward: target < len(h.Entries)-1,
	}
	m.historyAt = time.Now()
	m.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-time.After(250 * time.Millisecond):
	}
	if err := m.refreshViewport(ctx); err != nil {
		m.setError(err)
	}
	m.hub.kick()
	return nil
}

// handleHistory steps the driven tab one entry back or forward.
func (m *Manager) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if m.isReadOnlyValue() {
		writeError(w, http.StatusConflict, errReadOnly.Error())
		return
	}
	var body struct {
		Direction string `json:"direction"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	direction := strings.ToLower(strings.TrimSpace(body.Direction))
	if direction != "back" && direction != "forward" {
		writeError(w, http.StatusBadRequest, `direction must be "back" or "forward"`)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if err := m.StepHistory(ctx, direction == "forward"); err != nil {
		status := http.StatusInternalServerError
		if err == errHistoryEnd {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, m.Snapshot())
}
