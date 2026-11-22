//go:build darwin

package browser

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// clipboardInfoExpr prints the advertised classes and their sizes.
const clipboardInfoExpr = `clipboard info`

// readSystemClipboard enumerates the Mac clipboard and reads the representations
// the peek needs. It reads every candidate in one osascript call so a peek does
// not spawn a process per flavor.
func readSystemClipboard(ctx context.Context) ([]clipboardFlavor, map[string]string, error) {
	info, err := runOsascript(ctx, clipboardInfoExpr)
	if err != nil {
		return nil, nil, err
	}
	flavors := parseClipboardInfo(info)
	if len(flavors) == 0 {
		return nil, nil, nil
	}

	values := map[string]string{}
	// File list: the clipboard stores it as a plain string of file:// URLs.
	if flavorPresent(flavors, "furl") {
		if out, err := runOsascript(ctx, `get the clipboard as string`); err == nil {
			if text := strings.TrimPrefix(strings.TrimSpace(out), "text:"); strings.Contains(text, "file://") {
				values["furl"] = text
			}
		}
	}
	// Plain text: pbpaste is cheaper than AppleScript and never re-encodes.
	if flavorPresent(flavors, "utf8") || flavorPresent(flavors, "string") || flavorPresent(flavors, "unicode") {
		if out, err := exec.CommandContext(ctx, "/usr/bin/pbpaste").Output(); err == nil && len(out) > 0 {
			values["utf8"] = string(out)
		}
	}
	// Rich text: HTML is decoded from the hex blob; RTF the same way.
	// Image headers: only when an image flavor exists, and only the picked one,
	// so a peek reads at most one picture.
	if mime, flavor, size := pickImageFlavor(flavors); mime != "" && size <= maxClipboardBytes {
		if raw, err := readSystemClipboardFlavor(ctx, flavor); err == nil {
			values["image_raw"] = string(raw.Data)
		}
	}
	if flavorPresent(flavors, "html") {
		values["html"] = readAppleData(ctx, "HTML", 4)
	}
	if flavorPresent(flavors, "rtf") {
		if rtf := readAppleData(ctx, "RTF ", 4); rtf != "" {
			if html, err := rtfToHTML(ctx, rtf); err == nil {
				values["html"] = html
			} else {
				values["rtf"] = rtf
			}
		}
	}
	return flavors, values, nil
}

// readAppleData pulls one «data CLASS<hex>» payload and decodes it. Empty string
// on any failure: a missing flavor is not an error, the next one is tried.
func readAppleData(ctx context.Context, class string, classLen int) string {
	out, err := runOsascript(ctx, fmt.Sprintf(`get the clipboard as «class %s»`, class))
	if err != nil {
		return ""
	}
	raw, err := decodeAppleHex(out, classLen)
	if err != nil {
		return ""
	}
	return string(raw)
}

// rtfToHTML converts RTF to HTML with textutil, which ships with macOS. RTF alone
// cannot become a ClipboardItem flavor, so the conversion happens server-side.
func rtfToHTML(ctx context.Context, rtf string) (string, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/textutil", "-stdin", "-convert", "html", "-stdout")
	cmd.Stdin = strings.NewReader(rtf)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// readSystemClipboardFlavor returns one representation's raw bytes.
func readSystemClipboardFlavor(ctx context.Context, flavor string) (ClipboardRaw, error) {
	switch flavor {
	case "text", "utf8", "string":
		out, err := exec.CommandContext(ctx, "/usr/bin/pbpaste").Output()
		if err != nil {
			return ClipboardRaw{}, fmt.Errorf("read clipboard text: %w", err)
		}
		return ClipboardRaw{Flavor: "text", MIME: "text/plain;charset=utf-8", Data: out}, nil
	case "html":
		html := readAppleData(ctx, "HTML", 4)
		if html == "" {
			// Fall back to the plain text so a "copy as HTML" never yields nothing.
			out, err := exec.CommandContext(ctx, "/usr/bin/pbpaste").Output()
			if err != nil {
				return ClipboardRaw{}, fmt.Errorf("read clipboard html: %w", err)
			}
			return ClipboardRaw{Flavor: "html", MIME: "text/html", Data: []byte(htmlEscapeText(string(out)))}, nil
		}
		return ClipboardRaw{Flavor: "html", MIME: "text/html", Data: []byte(html)}, nil
	case "rtf":
		rtf := readAppleData(ctx, "RTF ", 4)
		if rtf == "" {
			return ClipboardRaw{}, fmt.Errorf("clipboard holds no RTF")
		}
		html, err := rtfToHTML(ctx, rtf)
		if err != nil {
			return ClipboardRaw{}, fmt.Errorf("convert rtf: %w", err)
		}
		return ClipboardRaw{Flavor: "html", MIME: "text/html", Data: []byte(html)}, nil
	case "png", "jpeg", "jpg", "tiff", "gif":
		class, mime := imageClassFor(flavor)
		out, err := runOsascript(ctx, fmt.Sprintf(`get the clipboard as «class %s»`, class))
		if err != nil {
			return ClipboardRaw{}, fmt.Errorf("read clipboard image: %w", err)
		}
		raw, err := decodeAppleHex(out, len(class))
		if err != nil {
			return ClipboardRaw{}, err
		}
		return ClipboardRaw{Flavor: flavor, MIME: mime, Data: raw}, nil
	}
	return ClipboardRaw{}, fmt.Errorf("unsupported clipboard flavor %q", flavor)
}

// imageClassFor maps a flavor to the AppleScript class and MIME type.
func imageClassFor(flavor string) (class, mime string) {
	switch flavor {
	case "jpeg", "jpg":
		return "JPEG", "image/jpeg"
	case "tiff":
		return "TIFF", "image/tiff"
	case "gif":
		return "GIFf", "image/gif"
	default:
		return "PNGf", "image/png"
	}
}

// htmlEscapeText makes a plain-text fallback safe to hand to a browser as HTML.
func htmlEscapeText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return "<pre>" + s + "</pre>"
}

// runOsascript runs one AppleScript expression over stdin, which avoids quoting
// the class names into a shell string.
func runOsascript(ctx context.Context, expr string) (string, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-")
	cmd.Stdin = strings.NewReader(expr)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("osascript: %w", err)
	}
	return string(out), nil
}
