package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// directWindowsFixture is a two-window /v1/windows payload: window 11 focused
// with a content page, window 12 with only a chrome:// page.
const directWindowsFixture = `{"type":"windows","version":"1.0.24","features":["all-windows"],"windows":[` +
	`{"window_id":11,"type":"normal","focused":true,"width":1280,"height":900,"state":"normal",` +
	`"tabs":[{"tab_id":111,"index":0,"title":"Docs","url":"https://example.com/docs","active":true,"capturable":true,"role":"user"},` +
	`{"tab_id":112,"index":1,"title":"Search","url":"https://example.com/search","active":false,"capturable":true,"role":"user"}]},` +
	`{"window_id":12,"type":"normal","focused":false,"width":900,"height":700,"state":"normal",` +
	`"tabs":[{"tab_id":121,"index":0,"title":"Settings","url":"chrome://settings","active":true,"capturable":false,"role":"user"}]}]}`

// directInfoFixture answers the direct info job for window 11.
const directInfoFixture = `{"type":"info","window_id":11,"direct":true,"tabs":[` +
	`{"id":111,"index":1,"title":"Docs","url":"https://example.com/docs","active":true,"role":"user"},` +
	`{"id":112,"index":2,"title":"Search","url":"https://example.com/search","active":false,"role":"user"}],` +
	`"job_target":{"tab_id":111,"tab_index":1,"reason":"active_in_window"}}`

// directInfo12Fixture answers the direct info job for window 12 (chrome:// only).
const directInfo12Fixture = `{"type":"info","window_id":12,"direct":true,"tabs":[` +
	`{"id":121,"index":1,"title":"Settings","url":"chrome://settings","active":true,"role":"user"}]}`

// Closing the window a direct session was pinned to (by hand, in Chrome) must
// fail the session over to another FREE window — never onto a window that
// hosts another driver's /go anchor.
func TestDirectEnsureFailsOverWhenPinnedWindowClosed(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	m.recordDaemon(&daemonHealth{OK: true, Version: "1.0.27", DirectSupported: true})
	f.mu.Lock()
	f.windowsPayload = `{"type":"windows","version":"1.0.27","features":["all-windows"],"windows":[` +
		// Window 10 belongs to a live session track: it has a /go anchor.
		`{"window_id":10,"type":"normal","focused":true,"width":1280,"height":900,"state":"normal",` +
		`"tabs":[{"tab_id":101,"index":0,"title":"go","url":"http://127.0.0.1:43761/go?session=sess-live","active":true,"capturable":true,"role":"session_page"}]},` +
		// Window 12 is free: one capturable data: placeholder tab.
		`{"window_id":12,"type":"normal","focused":false,"width":900,"height":700,"state":"normal",` +
		`"tabs":[{"tab_id":122,"index":0,"title":"ai-critic","url":"` + directNewWindowURL + `","active":true,"capturable":true,"role":"user"}]}]}`
	f.mu.Unlock()

	// The pinned window 11 no longer exists: dead, disconnected state.
	m.mu.Lock()
	m.sessionID = sessionlessSessionID(11)
	m.connected = false
	m.windowID = 0
	m.mu.Unlock()

	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got := m.sessionIDValue(); got != sessionlessSessionID(12) {
		t.Fatalf("failed over to %q, want window 12 (window 10 hosts a live session)", got)
	}
	if m.windowIDValue() != 12 {
		t.Fatalf("window = %d, want 12", m.windowIDValue())
	}
	if !m.Snapshot().Session.Connected {
		t.Fatal("expected connected after failover")
	}
}

// With every remaining window claimed by a session track, failover takes one
// over. A page that refuses forever is worse than a taken-over window: the rail
// has nothing to click and Retry repeats the same refusal.
func TestDirectEnsureTakesOverClaimedWindow(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	m.recordDaemon(&daemonHealth{OK: true, Version: "1.0.27", DirectSupported: true})
	f.mu.Lock()
	f.windowsPayload = `{"type":"windows","version":"1.0.27","features":["all-windows"],"windows":[` +
		`{"window_id":10,"type":"normal","focused":true,"width":1280,"height":900,"state":"normal",` +
		`"tabs":[{"tab_id":101,"index":0,"title":"go","url":"http://127.0.0.1:43761/go?session=sess-live","active":true,"capturable":true,"role":"session_page"}]}]}`
	f.mu.Unlock()
	fakeDirectWindowInfo(f, 10, 101)
	m.mu.Lock()
	m.sessionID = sessionlessSessionID(11)
	m.connected = false
	m.windowID = 0
	m.mu.Unlock()

	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure error = %v, want a takeover of window 10", err)
	}
	if got := m.windowIDValue(); got != 10 {
		t.Fatalf("window = %d, want 10 (the only remaining window)", got)
	}
	if got := m.sessionIDValue(); got != sessionlessSessionID(10) {
		t.Fatalf("session id = %q, want the taken-over window", got)
	}
	if !m.Snapshot().Session.Connected {
		t.Fatal("expected connected after takeover")
	}
}

// Taking over a session window must prefer one already showing a real page: the
// only other candidate carries ai-critic's own placeholder, and mirroring that
// yields a blank frame that looks exactly like the failure being repaired.
func TestDirectEnsurePrefersTakeoverWithContent(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	m.recordDaemon(&daemonHealth{OK: true, Version: "1.0.27", DirectSupported: true})
	f.mu.Lock()
	f.windowsPayload = `{"type":"windows","version":"1.0.27","features":["all-windows"],"windows":[` +
		`{"window_id":30,"type":"normal","focused":false,"width":1280,"height":900,"state":"normal",` +
		`"tabs":[{"tab_id":301,"index":0,"title":"go","url":"http://127.0.0.1:43761/go?session=s1","active":false,"capturable":true,"role":"session_page"},` +
		`{"tab_id":302,"index":1,"title":"ai-critic","url":"` + directNewWindowURL + `","active":true,"capturable":true,"role":"user"}]},` +
		`{"window_id":31,"type":"normal","focused":false,"width":1280,"height":900,"state":"normal",` +
		`"tabs":[{"tab_id":311,"index":0,"title":"go","url":"http://127.0.0.1:43761/go?session=s2","active":false,"capturable":true,"role":"session_page"},` +
		`{"tab_id":312,"index":1,"title":"Orchestrator","url":"http://127.0.0.1:8422/orchestrator","active":true,"capturable":true,"role":"user"}]}]}`
	f.mu.Unlock()
	fakeDirectWindowInfo(f, 31, 312)
	m.mu.Lock()
	m.sessionID = sessionlessSessionID(11)
	m.connected = false
	m.windowID = 0
	m.mu.Unlock()

	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got := m.windowIDValue(); got != 31 {
		t.Fatalf("window = %d, want 31 (the one with a real page)", got)
	}
}

// Preferring a window with a real page must not mean taking one away from a live
// session: a window whose registered session is dead is free in practice.
func TestDirectEnsurePrefersDeadSessionOverLiveOne(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	m.recordDaemon(&daemonHealth{OK: true, Version: "1.0.27", DirectSupported: true})
	f.mu.Lock()
	f.windowsPayload = `{"type":"windows","version":"1.0.27","features":["all-windows"],"windows":[` +
		// 40 is live and shows a real page — the tempting one.
		`{"window_id":40,"type":"normal","focused":false,"width":1280,"height":900,"state":"normal","session_ids":["sess-live"],` +
		`"tabs":[{"tab_id":401,"index":0,"title":"go","url":"http://127.0.0.1:43761/go?session=sess-live","active":false,"capturable":true,"role":"session_page"},` +
		`{"tab_id":402,"index":1,"title":"Orchestrator","url":"http://127.0.0.1:8422/orchestrator","active":true,"capturable":true,"role":"user"}]},` +
		// 41 belongs to a dead session and shows a real page too.
		`{"window_id":41,"type":"normal","focused":false,"width":1280,"height":900,"state":"normal","session_ids":["sess-dead"],` +
		`"tabs":[{"tab_id":411,"index":0,"title":"go","url":"http://127.0.0.1:43761/go?session=sess-dead","active":false,"capturable":true,"role":"session_page"},` +
		`{"tab_id":412,"index":1,"title":"Terminal","url":"http://127.0.0.1:8422/terminal","active":true,"capturable":true,"role":"user"}]}]}`
	f.rows = []sessionRow{
		{SessionID: "sess-live", Extension: extensionInfo{Connected: true, Version: "1.0.30"}, SessionPageCount: 1},
		{SessionID: "sess-dead", Extension: extensionInfo{Connected: false, Version: "1.0.30"}, SessionPageCount: 1},
	}
	f.mu.Unlock()
	fakeDirectWindowInfo(f, 41, 412)
	m.mu.Lock()
	m.sessionID = sessionlessSessionID(11)
	m.connected = false
	m.windowID = 0
	m.mu.Unlock()

	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got := m.windowIDValue(); got != 41 {
		t.Fatalf("window = %d, want 41 (its session is dead; 40 is live)", got)
	}
}

// A daemon that answers but has no windows left is recoverable on an explicit
// action: reopen one instead of surfacing a permanent error.
func TestDirectEnsureReopensWhenNoWindowsLeft(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	m.recordDaemon(&daemonHealth{OK: true, Version: "1.0.27", DirectSupported: true})
	f.mu.Lock()
	f.windowsPayload = `{"type":"windows","version":"1.0.27","features":["all-windows"],"windows":[]}`
	f.mu.Unlock()
	opened := 0
	m.windowOpener = func(ctx context.Context, url string) error {
		opened++
		f.mu.Lock()
		f.windowsPayload = `{"type":"windows","version":"1.0.27","features":["all-windows"],"windows":[` +
			`{"window_id":21,"type":"normal","width":1280,"height":900,"state":"normal",` +
			`"tabs":[{"tab_id":201,"index":0,"title":"ai-critic","url":"data:text/html,ok","active":true,"capturable":true}]}]}`
		f.mu.Unlock()
		return nil
	}
	fakeDirectWindowInfo(f, 21, 201)
	m.mu.Lock()
	m.sessionID = sessionlessSessionID(11)
	m.connected = false
	m.mu.Unlock()

	if err := m.ensureLocked(context.Background(), true); err != nil {
		t.Fatalf("ensureLocked(allowOpen) error = %v", err)
	}
	if opened != 1 {
		t.Fatalf("windowOpener calls = %d, want 1", opened)
	}
	if got := m.windowIDValue(); got != 21 {
		t.Fatalf("window = %d, want the reopened window 21", got)
	}
}

// A passive poll must not launch Chrome: it reports the recoverable state so the
// page can offer a start action instead of popping a window unasked.
func TestDirectEnsureNoWindowsWithoutAllowOpen(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	m.recordDaemon(&daemonHealth{OK: true, Version: "1.0.27", DirectSupported: true})
	f.mu.Lock()
	f.windowsPayload = `{"type":"windows","version":"1.0.27","features":["all-windows"],"windows":[]}`
	f.mu.Unlock()
	m.mu.Lock()
	m.sessionID = sessionlessSessionID(11)
	m.connected = false
	m.mu.Unlock()
	opened := 0
	m.windowOpener = func(ctx context.Context, url string) error {
		opened++
		return nil
	}

	err := m.Ensure(context.Background())
	if !errors.Is(err, errNoChromeWindows) {
		t.Fatalf("Ensure error = %v, want errNoChromeWindows", err)
	}
	if opened != 0 {
		t.Fatalf("windowOpener calls = %d, want 0 for a passive poll", opened)
	}
}

// fakeDirectWindowInfo teaches the fake to answer direct info/window jobs for a
// window the test adopts, so a takeover can resolve tabs like a real window. The
// stock fixture only knows windows 11 and 12 and fails every other window.
func fakeDirectWindowInfo(f *fakeClient, id, tabID int64) {
	f.mu.Lock()
	prevHandle := f.directHandle
	f.directHandle = func(call fakeCall) (json.RawMessage, bool) {
		if call.Session == sessionlessSessionID(id) {
			switch call.Type {
			case "info":
				return json.RawMessage(fmt.Sprintf(
					`{"type":"info","window_id":%d,"direct":true,"tabs":[{"id":%d,"index":0,"title":"ai-critic","url":"data:text/html,ok","active":true,"role":"user"}]}`,
					id, tabID)), true
			case "window":
				return json.RawMessage(fmt.Sprintf(`{"window_id":%d,"window":{"left":0,"top":0,"width":1280,"height":1000}}`, id)), true
			case "eval":
				return json.RawMessage(`{"value":"{\"w\":1280,\"h\":900,\"dpr\":2}"}`), true
			}
		}
		if prevHandle != nil {
			return prevHandle(call)
		}
		return nil, false
	}
	f.directErrFor = func(call fakeCall) error {
		if call.Session == sessionlessSessionID(id) {
			return nil
		}
		return errWindowGone
	}
	f.mu.Unlock()
}

func directFake() *fakeClient {
	return &fakeClient{
		direct:         true,
		windowsPayload: directWindowsFixture,
		info:           directInfoFixture,
		window:         `{"window_id":11,"window":{"left":0,"top":0,"width":1280,"height":1000}}`,
		vpW:            1280,
		vpH:            900,
		// No live session exists by default: the direct rail is what connects.
		sessionFn: func(id string) (*sessionRow, error) {
			return &sessionRow{SessionID: id, Extension: extensionInfo{Connected: false}}, nil
		},
		// Info answers per window; unknown windows fail like the extension would.
		directHandle: func(call fakeCall) (json.RawMessage, bool) {
			switch call.Type {
			case "info":
				switch call.Session {
				case sessionlessSessionID(11):
					return json.RawMessage(directInfoFixture), true
				case sessionlessSessionID(12):
					return json.RawMessage(directInfo12Fixture), true
				}
			case "window":
				if op, _ := call.Params["op"].(string); op == "get" && call.Session == sessionlessSessionID(11) {
					return json.RawMessage(`{"window_id":11,"window":{"left":0,"top":0,"width":1280,"height":1000}}`), true
				}
				return json.RawMessage(`{}`), true
			case "resize":
				return json.RawMessage(`{}`), true
			}
			return nil, false
		},
		directErrFor: func(call fakeCall) error {
			if call.Type == "info" && call.Session != sessionlessSessionID(11) && call.Session != sessionlessSessionID(12) {
				return errWindowGone
			}
			return nil
		},
	}
}

func TestDirectEnsureDrivesFocusedWindowWithoutSession(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	st := m.Snapshot()
	if !st.Session.Connected {
		t.Fatal("expected connected after direct ensure")
	}
	if st.Session.WindowID != 11 {
		t.Fatalf("window = %d, want 11 (focused)", st.Session.WindowID)
	}
	if st.Session.ID != sessionlessSessionID(11) {
		t.Fatalf("session id = %q, want the sessionless window id", st.Session.ID)
	}
	if st.Session.ContentTabID != 111 {
		t.Fatalf("content tab = %d, want 111 (active capturable)", st.Session.ContentTabID)
	}
	if st.Session.URL != "https://example.com/docs" {
		t.Fatalf("url = %q", st.Session.URL)
	}
	// No session may be created on the daemon.
	if len(f.created) != 0 {
		t.Fatalf("createSession called for %v in direct mode", f.created)
	}
}

func TestDirectJobRoutedByWindowID(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := m.jobIn(context.Background(), sessionlessSessionID(11), 111, "screenshot", nil, 5*time.Second); err != nil {
		t.Fatalf("jobIn: %v", err)
	}
	found := false
	for _, c := range f.directCalls {
		if c.Direct && c.Type == "screenshot" && c.Session == sessionlessSessionID(11) {
			found = true
		}
	}
	if !found {
		t.Fatalf("screenshot did not ride the direct rail: %+v", f.directCalls)
	}
}

func TestDirectSessionlessSwitchDrivesNamedWindow(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	// Request window 12 (chrome:// only): the direct ensure picks it, finds no
	// capturable tab, and creates the placeholder content tab.
	err := m.SwitchSession(context.Background(), sessionlessSessionID(12), false)
	if err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}
	st := m.Snapshot()
	if st.Session.WindowID != 12 {
		t.Fatalf("window = %d, want 12", st.Session.WindowID)
	}
	created := false
	for _, c := range f.directCalls {
		if c.Type == "create_tab" {
			created = true
			if c.Params["window_id"] != int64(12) && c.Params["window_id"] != float64(12) {
				t.Fatalf("create_tab window_id = %v, want 12", c.Params["window_id"])
			}
		}
	}
	if !created {
		t.Fatal("window 12 has no capturable tab; expected a placeholder create_tab")
	}
}

func TestDirectFallsBackToSessionWhenWindowsFail(t *testing.T) {
	// healthyFake answers the session track (window 900 with a /go page); the
	// direct rail is advertised but every direct call fails.
	f := healthyFake()
	f.direct = true
	f.windowsPayload = directWindowsFixture
	f.directFail = context.DeadlineExceeded
	f.rows = []sessionRow{{SessionID: "sess-x", Extension: extensionInfo{Connected: true}, SessionPageCount: 1}}
	m := newTestManager(t, f)
	m.mu.Lock()
	m.sessionID = "sess-x"
	m.mu.Unlock()
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure should fall back to the session track: %v", err)
	}
	st := m.Snapshot()
	if st.Session.ID != "sess-x" || st.Session.WindowID != 900 {
		t.Fatalf("fallback state = %+v", st.Session)
	}
	if len(f.directCalls) != 0 {
		t.Fatalf("direct jobs should not fire after fallback: %+v", f.directCalls)
	}
}

func TestDirectDisabledKeepsSessionTrack(t *testing.T) {
	f := healthyFake()
	m := newTestManager(t, f)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	st := m.Snapshot()
	if st.Session.WindowID != 900 {
		t.Fatalf("session track window = %d, want 900", st.Session.WindowID)
	}
}

func TestDirectLiveSessionNotStolen(t *testing.T) {
	f := directFake()
	f.sessionFn = func(id string) (*sessionRow, error) {
		return &sessionRow{SessionID: id, Extension: extensionInfo{Connected: true}, SessionPageCount: 1}, nil
	}
	m := newTestManager(t, f)
	m.mu.Lock()
	m.sessionID = "sess-live"
	m.mu.Unlock()
	// ensureDirectWindow refuses; the session chain drives the live session.
	if err := m.ensureDirectWindow(context.Background(), false); err == nil {
		t.Fatal("expected direct ensure to refuse a live session")
	}
}

func TestDirectSessionlessWindowGone(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	m.recordDaemon(&daemonHealth{OK: true, Version: "1.0.27", DirectSupported: true})
	m.mu.Lock()
	m.sessionID = sessionlessSessionID(33)
	m.mu.Unlock()
	// The pinned window is gone; ensure fails over to a free window (11 here —
	// focused, content tabs) instead of staying dead on the closed id.
	err := m.ensureDirectWindow(context.Background(), false)
	if err != nil {
		t.Fatalf("err = %v, want failover to a free window", err)
	}
	if got := m.sessionIDValueLocked(); got != sessionlessSessionID(11) {
		t.Fatalf("session id = %q, want the free window 11", got)
	}
}

func errorsIs(err, want error) bool {
	return err != nil && strings.Contains(err.Error(), want.Error())
}

// A /go anchor for a sess-window-<id> can outlive its window (dragged or
// reopened inside another driver's window). The daemon still reports that
// anchor as connected, but the rail must not list a sessionless id whose
// window is gone: switching to it fails with "window not found".
func TestDirectSessionsHideGhostSessionlessAnchors(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	m.recordDaemon(&daemonHealth{OK: true, Version: "1.0.27", DirectSupported: true})
	// The daemon registry still carries a "connected" anchor for the closed
	// window 061 (its /go page lives on in another driver's window).
	f.mu.Lock()
	sessionFn := f.sessionFn
	f.sessionFn = func(id string) (*sessionRow, error) {
		if id == sessionlessSessionID(61) {
			return &sessionRow{
				SessionID:        sessionlessSessionID(61),
				Phase:            "extension_connected",
				Extension:        extensionInfo{Connected: true, Version: "1.0.27"},
				SessionPageCount: 1,
			}, nil
		}
		return sessionFn(id)
	}
	f.mu.Unlock()

	resp := m.Sessions(context.Background())
	if !resp.OK {
		t.Fatalf("Sessions: %s", resp.Error)
	}
	for _, si := range resp.Sessions {
		if si.ID == sessionlessSessionID(61) {
			t.Fatalf("ghost anchor row listed: %+v", si)
		}
	}
	// Real windows from the enumeration stay listed.
	found := false
	for _, si := range resp.Sessions {
		if si.ID == sessionlessSessionID(11) {
			found = true
		}
	}
	if !found {
		t.Fatalf("window 11 row missing: %+v", resp.Sessions)
	}
}

func TestDirectSessionsListSynthesizesWindowRows(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	resp := m.Sessions(context.Background())
	if !resp.OK {
		t.Fatalf("Sessions: %s", resp.Error)
	}
	byID := map[string]SessionInfo{}
	for _, s := range resp.Sessions {
		byID[s.ID] = s
	}
	w11, ok := byID[sessionlessSessionID(11)]
	if !ok {
		t.Fatalf("missing synthesized row for window 11: %+v", resp.Sessions)
	}
	// The current window is driven by the direct rail, so its row presents as
	// a controlled session (not sessionless — that would re-offer Attach).
	if w11.Sessionless || !w11.Attached || !w11.Attachable || w11.WindowID != 11 {
		t.Fatalf("window row flags = %+v", w11)
	}
	if len(w11.Tabs) != 2 {
		t.Fatalf("window row tabs = %+v", w11.Tabs)
	}
	w12, ok := byID[sessionlessSessionID(12)]
	if !ok {
		t.Fatalf("missing synthesized row for window 12: %+v", resp.Sessions)
	}
	if w12.Sessionless == false || w12.WindowID != 12 {
		t.Fatalf("window 12 row = %+v", w12)
	}
	// Current row seeded from cache presents as a controlled session.
	if cur, ok := byID[sessionlessSessionID(11)]; !ok || !cur.Current || cur.Sessionless {
		t.Fatalf("current direct row missing: %+v", resp.Sessions)
	}
}

// A window that already carries a live /go session page is listed by its
// session row; the windows enumeration must not synthesize a second
// sess-window-<id> row for the same window.
func TestDirectSessionsSkipsWindowWithLiveSession(t *testing.T) {
	f := directFake()
	// Window 11 carries live session "sess-live"; window 12 is unclaimed.
	f.windowsPayload = `{"type":"windows","version":"1.0.24","features":["all-windows"],"windows":[` +
		`{"window_id":11,"type":"normal","focused":true,"width":1280,"height":900,"state":"normal",` +
		`"session_ids":["sess-live"],` +
		`"tabs":[{"tab_id":111,"index":0,"title":"Docs","url":"https://example.com/docs","active":true,"capturable":true,"role":"user"},` +
		`{"tab_id":110,"index":1,"title":"sess-live - Browser Agent","url":"` + "http://127.0.0.1:43761/go?session=sess-live" + `","active":false,"capturable":false,"role":"session"}]},` +
		`{"window_id":12,"type":"normal","focused":false,"width":900,"height":700,"state":"normal",` +
		`"tabs":[{"tab_id":121,"index":0,"title":"Settings","url":"chrome://settings","active":true,"capturable":false,"role":"user"}]}]}`
	f.rows = []sessionRow{
		{SessionID: "sess-live", Phase: "extension_connected", Extension: extensionInfo{Connected: true}, SessionPageCount: 1},
	}
	m := newTestManager(t, f)
	// Sessions() gates the direct rail on the health-reported capability;
	// record it the way Ensure would, without running the full ensure chain.
	m.recordDaemon(&daemonHealth{OK: true, Version: "1.0.24", DirectSupported: true})
	resp := m.Sessions(context.Background())
	if !resp.OK {
		t.Fatalf("Sessions: %s", resp.Error)
	}
	ids := map[string]int{}
	for _, s := range resp.Sessions {
		ids[s.ID]++
	}
	if n := ids["sess-live"]; n != 1 {
		t.Fatalf("live session rows = %d (%+v)", n, resp.Sessions)
	}
	if _, ok := ids[sessionlessSessionID(11)]; ok {
		t.Fatalf("window 11 has a live session; got a phantom sess-window row: %+v", resp.Sessions)
	}
	if n := ids[sessionlessSessionID(12)]; n != 1 {
		t.Fatalf("unclaimed window 12 rows = %d (%+v)", n, resp.Sessions)
	}
}

// A registered session whose page is gone does not claim its window: the row
// must stay adoptable, or the window would vanish from the rail entirely.
func TestDirectSessionsKeepsWindowWithDeadSessionAdoptable(t *testing.T) {
	f := directFake()
	f.windowsPayload = `{"type":"windows","version":"1.0.24","features":["all-windows"],"windows":[` +
		`{"window_id":11,"type":"normal","focused":true,"width":1280,"height":900,"state":"normal",` +
		`"session_ids":["sess-dead"],` +
		`"tabs":[{"tab_id":111,"index":0,"title":"Docs","url":"https://example.com/docs","active":true,"capturable":true,"role":"user"}]}]}`
	// Registered but disconnected: not live.
	f.rows = []sessionRow{
		{SessionID: "sess-dead", Extension: extensionInfo{Connected: false}, SessionPageCount: 0},
	}
	m := newTestManager(t, f)
	// Sessions() gates the direct rail on the health-reported capability;
	// record it the way Ensure would, without running the full ensure chain.
	m.recordDaemon(&daemonHealth{OK: true, Version: "1.0.24", DirectSupported: true})
	resp := m.Sessions(context.Background())
	if !resp.OK {
		t.Fatalf("Sessions: %s", resp.Error)
	}
	found := false
	for _, s := range resp.Sessions {
		if s.ID == sessionlessSessionID(11) {
			found = true
			if !s.Sessionless || !s.Adoptable {
				t.Fatalf("orphan window row = %+v", s)
			}
		}
		if s.ID == "sess-dead" {
			t.Fatalf("dead session must not be listed: %+v", resp.Sessions)
		}
	}
	if !found {
		t.Fatalf("orphan window 11 missing from rows: %+v", resp.Sessions)
	}
}

func TestDirectPollTabsMarksDisconnectedWhenWindowGone(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	// The window disappears: direct info job starts failing.
	f.mu.Lock()
	f.directFail = errWindowGone
	f.mu.Unlock()
	m.mu.Lock()
	m.tabsAt = time.Now().Add(-tabInfoTTL - time.Second)
	m.mu.Unlock()
	m.pollTabs(context.Background())
	st := m.Snapshot()
	if st.Session.Connected {
		t.Fatal("expected disconnected after the direct window vanished")
	}
}

func TestDirectReadOnlyStillStreams(t *testing.T) {
	// Old-daemon readOnly path must be untouched: a direct-capable daemon also
	// carries tab polyfills, but a fake claiming both must stream fine.
	f := directFake()
	m := newTestManager(t, f)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	st := m.Snapshot()
	if st.ReadOnly {
		t.Fatal("direct-capable daemon must not be read-only")
	}
}

func TestDirectCreateTabGoesThroughJobDirect(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	created, err := m.createTabIn(context.Background(), sessionlessSessionID(11), 11, "https://example.com/new", true)
	if err != nil {
		t.Fatalf("createTabIn: %v", err)
	}
	if created == 0 {
		t.Fatal("createTabIn returned no tab")
	}
	found := false
	for _, c := range f.directCalls {
		if c.Type == "create_tab" && c.Params["url"] == "https://example.com/new" {
			found = true
			if c.Params["active"] != true {
				t.Fatalf("user tab create_tab active = %v, want true", c.Params["active"])
			}
		}
	}
	if !found {
		t.Fatalf("create_tab missing from direct calls: %+v", f.directCalls)
	}
}

// Plumbing pages open in the background: the mirror follows Chrome's active
// tab, and a placeholder or /go registration page that steals focus would drag
// the mirror off the page the user is watching.
func TestPlumbingTabsOpenInBackground(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	// Window 12 has only a chrome:// tab: the ensure chain creates the
	// placeholder there. Switch to it to force that path.
	if err := m.SwitchSession(context.Background(), sessionlessSessionID(12), false); err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}
	for _, c := range f.directCalls {
		if c.Type == "create_tab" {
			if url, _ := c.Params["url"].(string); strings.HasPrefix(url, "data:") {
				if c.Params["active"] != false {
					t.Fatalf("placeholder create_tab active = %v, want false", c.Params["active"])
				}
			}
		}
	}
}

func TestDirectAttachWindowSkipsRegistration(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	id, err := m.AttachWindow(context.Background(), 12, "")
	if err != nil {
		t.Fatalf("AttachWindow: %v", err)
	}
	if id != sessionlessSessionID(12) {
		t.Fatalf("attached id = %q", id)
	}
	if len(f.created) != 0 {
		t.Fatalf("attach registered sessions: %v", f.created)
	}
	st := m.Snapshot()
	if st.Session.WindowID != 12 || !st.Session.Connected {
		t.Fatalf("state after attach = %+v", st.Session)
	}
	// A gone window must be refused before any state change.
	if _, err := m.AttachWindow(context.Background(), 33, ""); !errorsIs(err, errWindowGone) {
		t.Fatalf("gone-window attach err = %v", err)
	}
}

func TestDirectAttachWindowWrongRequestedID(t *testing.T) {
	f := directFake()
	m := newTestManager(t, f)
	if _, err := m.AttachWindow(context.Background(), 12, "sess-other"); !errorsIs(err, errInvalidWindowSession) {
		t.Fatalf("err = %v, want errInvalidWindowSession", err)
	}
}

func TestDirectSessionlessIDRoundTrip(t *testing.T) {
	id := sessionlessSessionID(42)
	win, ok := windowIDFromSessionlessID(id)
	if !ok || win != 42 {
		t.Fatalf("round trip failed: %q → %d,%v", id, win, ok)
	}
	if _, ok := windowIDFromSessionlessID("sess-abc"); ok {
		t.Fatal("regular session ids must not parse as sessionless")
	}
	if !strings.HasPrefix(id, sessionlessIDPrefix) {
		t.Fatalf("prefix mismatch: %q", id)
	}
	raw, _ := json.Marshal(sessionlessSessionID(7))
	if !strings.Contains(string(raw), sessionlessIDPrefix) {
		t.Fatalf("json: %s", raw)
	}
}
