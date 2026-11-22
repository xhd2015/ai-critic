package browser

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// historyFixture answers Page.getNavigationHistory with the given entries and
// current index, so the step logic can be tested without Chrome.
func historyFixture(t *testing.T, f *fakeClient, index int, urls ...string) {
	t.Helper()
	entries := make([]map[string]any, 0, len(urls))
	for i, u := range urls {
		entries = append(entries, map[string]any{"id": int64(100 + i), "url": u, "title": u})
	}
	raw, err := json.Marshal(map[string]any{"currentIndex": index, "entries": entries})
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	info := `{"type":"info","window_id":900,"tabs":[{"id":901,"index":0,"title":"Page","url":"https://a.example/","active":true,"role":"user"}]}`
	f.mu.Lock()
	f.handle = func(call fakeCall) (json.RawMessage, bool) {
		switch call.Type {
		case "info":
			return json.RawMessage(info), true
		case "cdp":
			switch call.Params["method"] {
			case "Page.getNavigationHistory":
				return raw, true
			case "Page.navigateToHistoryEntry":
				return json.RawMessage(`{}`), true
			}
			return json.RawMessage(`{}`), true
		}
		return nil, false
	}
	f.mu.Unlock()
}

func historyManager(t *testing.T, f *fakeClient) *Manager {
	t.Helper()
	m := newTestManager(t, f)
	m.mu.Lock()
	m.sessionID = "s1"
	m.connected = true
	m.windowID = 900
	m.contentTabID = 901
	m.mu.Unlock()
	return m
}

func TestStepHistoryMovesToTheNeighbouringEntry(t *testing.T) {
	f := &fakeClient{}
	historyFixture(t, f, 1, "https://a.example/", "https://b.example/", "https://c.example/")
	m := historyManager(t, f)

	if err := m.StepHistory(context.Background(), false); err != nil {
		t.Fatalf("StepHistory(back): %v", err)
	}
	calls := f.all("cdp")
	var moved map[string]any
	for _, call := range calls {
		if call.Params["method"] == "Page.navigateToHistoryEntry" {
			moved, _ = call.Params["params"].(map[string]any)
		}
	}
	if moved == nil {
		t.Fatalf("no navigateToHistoryEntry call: %+v", calls)
	}
	// Index 1 -> 0, whose entry id is 100.
	if got := moved["entryId"]; got != float64(100) && got != int64(100) && got != 100 {
		t.Fatalf("entryId = %v, want 100", got)
	}
}

func TestStepHistoryForwardWalksToTheNextEntry(t *testing.T) {
	f := &fakeClient{}
	historyFixture(t, f, 0, "https://a.example/", "https://b.example/")
	m := historyManager(t, f)

	if err := m.StepHistory(context.Background(), true); err != nil {
		t.Fatalf("StepHistory(forward): %v", err)
	}
	for _, call := range f.all("cdp") {
		if call.Params["method"] == "Page.navigateToHistoryEntry" {
			params, _ := call.Params["params"].(map[string]any)
			if got := params["entryId"]; got != float64(101) && got != int64(101) && got != 101 {
				t.Fatalf("entryId = %v, want 101", got)
			}
			return
		}
	}
	t.Fatal("no navigateToHistoryEntry call")
}

func TestStepHistoryRefusesAtTheEnds(t *testing.T) {
	f := &fakeClient{}
	historyFixture(t, f, 0, "https://a.example/", "https://b.example/")
	m := historyManager(t, f)
	if err := m.StepHistory(context.Background(), false); !errors.Is(err, errHistoryEnd) {
		t.Fatalf("back at the first entry = %v, want errHistoryEnd", err)
	}

	f2 := &fakeClient{}
	historyFixture(t, f2, 1, "https://a.example/", "https://b.example/")
	m2 := historyManager(t, f2)
	if err := m2.StepHistory(context.Background(), true); !errors.Is(err, errHistoryEnd) {
		t.Fatalf("forward at the last entry = %v, want errHistoryEnd", err)
	}
}

// The direct rail wraps a CDP result in the whole envelope; a session-channel
// job returns the inner result. Reading only one shape made the arrows stay
// dead on a window driven over the direct channel.
func TestHistoryPayloadAcceptsBothChannelShapes(t *testing.T) {
	inner := `{"currentIndex":1,"entries":[{"id":100,"url":"https://a.example/"},{"id":101,"url":"https://b.example/"}]}`
	enveloped := `{"method":"Page.getNavigationHistory","result":` + inner + `}`
	for name, body := range map[string]string{"inner": inner, "envelope": enveloped} {
		f := &fakeClient{handle: func(call fakeCall) (json.RawMessage, bool) {
			if call.Type == "cdp" {
				return json.RawMessage(body), true
			}
			return nil, false
		}}
		m := historyManager(t, f)
		got, err := m.historyPayload(context.Background(), 901)
		if err != nil {
			t.Fatalf("%s: historyPayload: %v", name, err)
		}
		if got.CurrentIndex != 1 || len(got.Entries) != 2 {
			t.Fatalf("%s: index=%d entries=%d, want 1 and 2", name, got.CurrentIndex, len(got.Entries))
		}
		// Index 1 of 2 is the last entry: back yes, forward no.
		status := historyStatusOf(got)
		if !status.CanBack || status.CanForward {
			t.Fatalf("%s: status = %+v, want back only", name, status)
		}
	}
}

func TestHistoryStatusOfBoundsTheIndex(t *testing.T) {
	cases := []struct {
		name string
		in   *navigationHistory
		want HistoryStatus
	}{
		{"nil", nil, HistoryStatus{}},
		{"empty", &navigationHistory{CurrentIndex: 0}, HistoryStatus{}},
		{
			"first",
			&navigationHistory{CurrentIndex: 0, Entries: []navigationEntry{{ID: 1}, {ID: 2}}},
			HistoryStatus{Index: 0, Count: 2, CanBack: false, CanForward: true},
		},
		{
			"last",
			&navigationHistory{CurrentIndex: 1, Entries: []navigationEntry{{ID: 1}, {ID: 2}}},
			HistoryStatus{Index: 1, Count: 2, CanBack: true, CanForward: false},
		},
		{
			// A negative index is clamped rather than panicking on Entries[i].
			"negative",
			&navigationHistory{CurrentIndex: -3, Entries: []navigationEntry{{ID: 1}}},
			HistoryStatus{Index: 0, Count: 1},
		},
	}
	for _, tc := range cases {
		if got := historyStatusOf(tc.in); got != tc.want {
			t.Fatalf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestShortcutModifiersMapsNames(t *testing.T) {
	if got := shortcutModifiers([]string{"ctrl"}); got != ModCtrl {
		t.Fatalf("ctrl = %d", got)
	}
	if got := shortcutModifiers([]string{"meta", "shift"}); got != ModMeta|ModShift {
		t.Fatalf("meta+shift = %d", got)
	}
	if got := shortcutModifiers([]string{"cmd"}); got != ModMeta {
		t.Fatalf("cmd = %d", got)
	}
	if got := shortcutModifiers([]string{"nope", ""}); got != 0 {
		t.Fatalf("unknown = %d, want 0", got)
	}
}

func TestSendInputShortcutUsesModifierNames(t *testing.T) {
	f := &fakeClient{handle: func(call fakeCall) (json.RawMessage, bool) {
		if call.Type == "cdp" {
			return json.RawMessage(`{}`), true
		}
		return nil, false
	}}
	m := historyManager(t, f)

	err := m.SendInput(context.Background(), InputEvent{
		Kind:          InputShortcut,
		Key:           "a",
		Code:          "KeyA",
		ModifierNames: []string{"ctrl"},
	})
	if err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	for _, call := range f.all("cdp") {
		if call.Params["method"] != "Input.dispatchKeyEvent" {
			continue
		}
		params, _ := call.Params["params"].(map[string]any)
		if got := params["modifiers"]; got != ModCtrl {
			t.Fatalf("modifiers = %v, want %d", got, ModCtrl)
		}
		return
	}
	t.Fatal("no dispatchKeyEvent call")
}

func TestSendInputDeleteForwardUsesVirtualKey46(t *testing.T) {
	f := &fakeClient{handle: func(call fakeCall) (json.RawMessage, bool) {
		if call.Type == "cdp" {
			return json.RawMessage(`{}`), true
		}
		return nil, false
	}}
	m := historyManager(t, f)
	if err := m.SendInput(context.Background(), InputEvent{
		Kind: InputKey, Key: "Delete", Code: "Delete", VK: 46,
	}); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	for _, call := range f.all("cdp") {
		if call.Params["method"] != "Input.dispatchKeyEvent" {
			continue
		}
		params, _ := call.Params["params"].(map[string]any)
		if got := params["key"]; got != "Delete" {
			t.Fatalf("key = %v, want Delete", got)
		}
		if got := params["windowsVirtualKeyCode"]; got != 46 {
			t.Fatalf("vk = %v, want 46", got)
		}
		return
	}
	t.Fatal("no dispatchKeyEvent call")
}

func TestCopySelectionReturnsTheFocusedFieldText(t *testing.T) {
	f := &fakeClient{handle: func(call fakeCall) (json.RawMessage, bool) {
		if call.Type != "cdp" {
			return nil, false
		}
		if call.Params["method"] != "Runtime.evaluate" {
			return json.RawMessage(`{}`), true
		}
		return json.RawMessage(`{"value":"hello"}`), true
	}}
	m := historyManager(t, f)
	got, err := m.copySelection(context.Background(), 901)
	if err != nil {
		t.Fatalf("copySelection: %v", err)
	}
	if got != "hello" {
		t.Fatalf("copied = %q, want hello", got)
	}
}

func TestPasteTextRequiresText(t *testing.T) {
	f := &fakeClient{handle: func(call fakeCall) (json.RawMessage, bool) {
		if call.Type == "cdp" {
			return json.RawMessage(`{}`), true
		}
		return nil, false
	}}
	m := historyManager(t, f)
	if err := m.pasteText(context.Background(), 901, ""); err == nil {
		t.Fatal("empty paste must be refused, not silently do nothing")
	}
	if err := m.pasteText(context.Background(), 901, "typed"); err != nil {
		t.Fatalf("pasteText: %v", err)
	}
	for _, call := range f.all("cdp") {
		if call.Params["method"] != "Input.insertText" {
			continue
		}
		params, _ := call.Params["params"].(map[string]any)
		if params["text"] != "typed" {
			t.Fatalf("text = %v", params["text"])
		}
		return
	}
	t.Fatal("no insertText call")
}

func TestCloseDrivenTabSendsCloseTargetAndClearsTheMirror(t *testing.T) {
	f := &fakeClient{handle: func(call fakeCall) (json.RawMessage, bool) {
		if call.Type == "cdp" {
			return json.RawMessage(`{"success":true}`), true
		}
		return nil, false
	}}
	m := historyManager(t, f)
	if err := m.closeDrivenTab(context.Background()); err != nil {
		t.Fatalf("closeDrivenTab: %v", err)
	}
	sent := false
	for _, call := range f.all("cdp") {
		if call.Params["method"] == "Target.closeTarget" {
			params, _ := call.Params["params"].(map[string]any)
			if params["targetId"] != "901" {
				t.Fatalf("targetId = %v, want 901", params["targetId"])
			}
			sent = true
		}
	}
	if !sent {
		t.Fatal("no Target.closeTarget call")
	}
	// The tab is gone, so the manager must not keep driving it.
	if got := m.contentTab(); got != 0 {
		t.Fatalf("contentTab = %d after close, want 0", got)
	}
}

func TestContextMenuPressesAndReleasesTheRightButton(t *testing.T) {
	f := &fakeClient{handle: func(call fakeCall) (json.RawMessage, bool) {
		if call.Type != "cdp" {
			return nil, false
		}
		switch call.Params["method"] {
		case "Page.getLayoutMetrics", "Page.getNavigationHistory":
			return json.RawMessage(`{}`), true
		}
		return json.RawMessage(`{}`), true
	}}
	m := historyManager(t, f)
	m.mu.Lock()
	m.viewport = Viewport{Width: 1000, Height: 800}
	m.viewportAt = time.Now()
	m.mu.Unlock()

	if err := m.openContextMenu(context.Background(), 901, 0.5, 0.5); err != nil {
		t.Fatalf("openContextMenu: %v", err)
	}
	var types []string
	for _, call := range f.all("cdp") {
		if call.Params["method"] != "Input.dispatchMouseEvent" {
			continue
		}
		params, _ := call.Params["params"].(map[string]any)
		typ, _ := params["type"].(string)
		types = append(types, typ)
		if params["button"] != "right" {
			t.Fatalf("button = %v, want right", params["button"])
		}
	}
	if len(types) != 2 || types[0] != "mousePressed" || types[1] != "mouseReleased" {
		t.Fatalf("dispatched %v, want pressed then released", types)
	}
}
