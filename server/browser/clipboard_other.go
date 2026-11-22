//go:build !darwin

package browser

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// readSystemClipboard reports that this host has no clipboard reader. The panel
// then says so instead of showing a stale or invented value.
func readSystemClipboard(ctx context.Context) ([]clipboardFlavor, map[string]string, error) {
	return nil, nil, fmt.Errorf("the system clipboard is only readable on macOS, not %s", runtime.GOOS)
}

func readSystemClipboardFlavor(ctx context.Context, flavor string) (ClipboardRaw, error) {
	// Try the common Linux helpers before giving up, so a Linux host is not dead.
	for _, cmd := range []string{"xclip", "wl-paste"} {
		if _, err := exec.LookPath(cmd); err != nil {
			continue
		}
		var out []byte
		var err error
		if cmd == "xclip" {
			out, err = exec.CommandContext(ctx, cmd, "-selection", "clipboard", "-o").Output()
		} else {
			out, err = exec.CommandContext(ctx, cmd).Output()
		}
		if err != nil {
			continue
		}
		return ClipboardRaw{Flavor: "text", MIME: "text/plain;charset=utf-8", Data: out}, nil
	}
	return ClipboardRaw{}, fmt.Errorf("no clipboard reader is available on %s", runtime.GOOS)
}
