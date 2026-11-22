package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// clipboardReadExpr reads the selection, falling back to the whole focused
// field. navigator.clipboard.readText() is deliberately not used: it needs a
// permission prompt the phone cannot answer.
const clipboardReadExpr = `(() => {
  const el = document.activeElement;
  const isField = el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA');
  if (isField && typeof el.selectionStart === 'number' && el.selectionStart !== el.selectionEnd) {
    return el.value.slice(el.selectionStart, el.selectionEnd);
  }
  const sel = String(window.getSelection ? window.getSelection() : '');
  if (sel) return sel;
  if (isField && el.value) return el.value;
  return '';
})()`

// copySelection reports what was copied so the page can toast the size, and
// returns "" when there is nothing to copy.
func (m *Manager) copySelection(ctx context.Context, tab int64) (string, error) {
	data, err := m.job(ctx, tab, "cdp", map[string]any{
		"method": "Runtime.evaluate",
		"params": map[string]any{"expression": clipboardReadExpr, "returnByValue": true},
	}, 15*time.Second)
	if err != nil {
		return "", err
	}
	return evalStringValue(data)
}

// pasteText inserts text into the focused field. The text arrives with the
// request: the server cannot read the phone's clipboard.
func (m *Manager) pasteText(ctx context.Context, tab int64, text string) error {
	if text == "" {
		return fmt.Errorf("nothing to paste: the client sent no text")
	}
	_, err := m.job(ctx, tab, "cdp", map[string]any{
		"method": "Input.insertText",
		"params": map[string]any{"text": text},
	}, 15*time.Second)
	return err
}

// openContextMenu right-clicks at the given page point so the page's own menu
// appears. A viewport point is required: Chrome anchors the menu to the event.
func (m *Manager) openContextMenu(ctx context.Context, tab int64, x, y float64) error {
	vp, err := m.viewportSize(ctx)
	if err != nil {
		return err
	}
	params := map[string]any{
		"x":      round2(x * vp.Width),
		"y":      round2(y * vp.Height),
		"button": "right",
	}
	if err := m.dispatchMouse(ctx, tab, "mousePressed", ButtonRight, 1, params); err != nil {
		return err
	}
	return m.dispatchMouse(ctx, tab, "mouseReleased", ButtonRight, 0, params)
}

// closeDrivenTab closes the tab the mirror is driving. The viewer asked for this
// tab to go, so the tab is removed through the same polyfilled Target.closeTarget
// the sweep uses; the manager then re-points at whatever tab Chrome activates.
func (m *Manager) closeDrivenTab(ctx context.Context) error {
	tab := m.contentTab()
	if tab == 0 {
		return errNoPageTab
	}
	params := map[string]any{"targetId": fmt.Sprintf("%d", tab)}
	if _, err := m.job(ctx, tab, "cdp", map[string]any{
		"method": "Target.closeTarget",
		"params": params,
	}, 20*time.Second); err != nil {
		return fmt.Errorf("close tab %d: %w", tab, err)
	}
	m.mu.Lock()
	m.contentTabID = 0
	m.mu.Unlock()
	m.hub.kick()
	return nil
}

// evalStringValue unwraps the string a Runtime.evaluate call returned.
func evalStringValue(data json.RawMessage) (string, error) {
	var out struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("decode eval: %w", err)
	}
	return out.Value, nil
}

// shortcutModifiers builds the CDP modifier mask from the client's key names, so
// a shortcut button can say "meta" and the server owns the wire numbers.
func shortcutModifiers(mods []string) int {
	mask := 0
	for _, name := range mods {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "alt", "option":
			mask |= ModAlt
		case "ctrl", "control":
			mask |= ModCtrl
		case "meta", "cmd", "command":
			mask |= ModMeta
		case "shift":
			mask |= ModShift
		}
	}
	return mask
}
