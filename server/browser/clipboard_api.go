package browser

import (
	"context"
	"net/http"
	"time"
)

// handleClipboardPeek reports what the Mac clipboard holds. It is a GET because
// it changes nothing, but it is never cached: the clipboard moves constantly and
// a stale preview would be worse than none.
func (m *Manager) handleClipboardPeek(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if m.isReadOnlyValue() {
		writeError(w, http.StatusConflict, errReadOnly.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	peek, err := m.PeekClipboard(ctx)
	if err != nil {
		// An unreadable clipboard is a state, not a failure to hide: the panel
		// shows the reason and disables its buttons.
		writeJSON(w, http.StatusOK, ClipboardPeek{Kind: ClipboardEmpty, Note: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, peek)
}

// handleClipboardRaw serves one representation's bytes so the phone can write
// them to its own clipboard, or save them.
func (m *Manager) handleClipboardRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if m.isReadOnlyValue() {
		writeError(w, http.StatusConflict, errReadOnly.Error())
		return
	}
	flavor := r.URL.Query().Get("flavor")
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	raw, err := m.ClipboardFlavorBytes(ctx, flavor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(raw.Data) > maxClipboardBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "clipboard payload is too large to copy")
		return
	}
	w.Header().Set("Content-Type", raw.MIME)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Clipboard-Flavor", raw.Flavor)
	_, _ = w.Write(raw.Data)
}
