package browser

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// ClipboardKind names what the Mac clipboard holds, after picking the richest
// representation the reader can hand back.
type ClipboardKind string

const (
	ClipboardEmpty ClipboardKind = "empty"
	ClipboardText  ClipboardKind = "text"
	ClipboardRich  ClipboardKind = "rich"
	ClipboardPicture ClipboardKind = "image"
	ClipboardFiles ClipboardKind = "files"
)

// maxClipboardPreview bounds the text handed to the panel. The full bytes are
// fetched only when somebody asks for them: a 2 MB HTML copy would otherwise
// stall the keyboard on every peek.
const maxClipboardPreview = 4000

// maxClipboardBytes refuses absurd payloads outright. A clipboard holding more
// than this is almost certainly not something a phone should pull.
const maxClipboardBytes = 24 << 20

// ClipboardPeek is the panel's view of the Mac clipboard. It is a snapshot, not
// a handle: nothing is cached after the response.
type ClipboardPeek struct {
	Kind ClipboardKind `json:"kind"`
	// Flavors lists every representation the clipboard advertised, in macOS
	// spelling, so the panel can show what is actually there.
	Flavors []string `json:"flavors,omitempty"`
	// Bytes is the size of the representation picked for copying.
	Bytes int `json:"bytes,omitempty"`
	// Text is the plain-text preview (truncated for text, the whole string for
	// short values).
	Text string `json:"text,omitempty"`
	// HTML is the rich preview, already decoded and sanitised of nothing but
	// size; the panel renders it in a sandboxed box.
	HTML string `json:"html,omitempty"`
	// Truncated says Text was cut, so the panel can offer the full value.
	Truncated bool `json:"truncated,omitempty"`
	// Image describes the picked image representation.
	Image *ClipboardPictureInfo `json:"image,omitempty"`
	// Files lists file:// paths when the clipboard holds files.
	Files []ClipboardFile `json:"files,omitempty"`
	// CopyFlavor names the flavor the client should request to write to the
	// phone clipboard. Empty when the clipboard is empty.
	CopyFlavor string `json:"copy_flavor,omitempty"`
	// Note explains a degradation the client cannot otherwise know about.
	Note string `json:"note,omitempty"`
}

// ClipboardPictureInfo describes an image representation.
type ClipboardPictureInfo struct {
	MIME   string `json:"mime"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Bytes  int    `json:"bytes"`
}

// ClipboardFile is one file:// entry from the clipboard.
type ClipboardFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size,omitempty"`
}

// ClipboardRaw is one representation's bytes, ready to be written to the phone.
type ClipboardRaw struct {
	Flavor string `json:"flavor"`
	MIME   string `json:"mime"`
	Data   []byte `json:"-"`
}

// clipboardFlavor is one advertised representation.
type clipboardFlavor struct {
	// Name is the macOS class name (utf8, HTML, PNGf …) as printed by
	// `clipboard info`, lowercased for lookup.
	Name string
	// Bytes is the advertised size; 0 when the line carried no size.
	Bytes int
}

// parseClipboardInfo reads `clipboard info` output into flavors. The line looks
// like: «class utf8», 15, «class ut16», 32, string, 15, Unicode text, 30
// i.e. alternating name/size pairs, with plain-English names left unquoted.
func parseClipboardInfo(out string) []clipboardFlavor {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	// Normalise «class X» / «data X…» down to the bare name so both spellings
	// compare equal later.
	replaced := out
	for _, prefix := range []string{"«class ", "«data ", "«"} {
		replaced = strings.ReplaceAll(replaced, prefix, "")
	}
	replaced = strings.ReplaceAll(replaced, "»", "")

	parts := strings.Split(replaced, ",")
	flavors := make([]clipboardFlavor, 0, len(parts)/2)
	for i := 0; i < len(parts); i++ {
		name := strings.TrimSpace(parts[i])
		if name == "" {
			continue
		}
		// A name carrying spaces is a human label ("Unicode text"); keep it, the
		// caller matches on the canonical short names it cares about.
		size := 0
		if i+1 < len(parts) {
			next := strings.TrimSpace(parts[i+1])
			if next != "" && isAllDigits(next) {
				size = atoiSafe(next)
				i++
			}
		}
		flavors = append(flavors, clipboardFlavor{Name: strings.ToLower(name), Bytes: size})
	}
	return flavors
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
		if n > maxClipboardBytes*4 {
			return 0
		}
	}
	return n
}

// flavorPresent reports whether any advertised flavor satisfies want. Matching is
// by substring on the lowercased name so «class PNGf» and «class png "public.png"»
// both hit "png".
func flavorPresent(flavors []clipboardFlavor, want string) bool {
	for _, f := range flavors {
		if strings.Contains(f.Name, want) {
			return true
		}
	}
	return false
}

// decodeAppleHex turns the `«data HTML3C68…»` form into raw bytes. Everything
// after the four-character class code and before the closing » is hex.
func decodeAppleHex(out string, classLen int) ([]byte, error) {
	s := strings.TrimSpace(out)
	if s == "" {
		return nil, fmt.Errorf("empty clipboard data")
	}
	s = strings.TrimPrefix(s, "«data ")
	s = strings.TrimSuffix(s, "»")
	if len(s) <= classLen {
		return nil, fmt.Errorf("clipboard data too short: %q", s[:min(len(s), 40)])
	}
	payload := s[classLen:]
	// Apple pads nothing; an odd length means the payload is not pure hex.
	if len(payload)%2 != 0 {
		payload = payload[:len(payload)-1]
	}
	raw, err := hex.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decode clipboard hex: %w", err)
	}
	return raw, nil
}

// validUTF8OrDrop keeps a truncated preview from ending mid-rune.
func validUTF8OrDrop(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	for len(s) > 0 {
		s = s[:len(s)-1]
		if utf8.ValidString(s) {
			return s
		}
	}
	return ""
}

// trimPreview cuts text to the preview budget and reports whether it cut.
func trimPreview(s string) (string, bool) {
	if len(s) <= maxClipboardPreview {
		return s, false
	}
	return validUTF8OrDrop(s[:maxClipboardPreview]), true
}

// htmlToPlainText is the last-resort plain preview for rich content when the
// clipboard carries no text flavor. It is deliberately crude: enough for a
// preview and for pasting into a plain field, never a formatter.
func htmlToPlainText(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	out := b.String()
	out = strings.ReplaceAll(out, "&amp;", "&")
	out = strings.ReplaceAll(out, "&lt;", "<")
	out = strings.ReplaceAll(out, "&gt;", ">")
	out = strings.ReplaceAll(out, "&quot;", `"`)
	out = strings.ReplaceAll(out, "&#39;", "'")
	out = strings.ReplaceAll(out, "&nbsp;", " ")
	return strings.TrimSpace(out)
}

// buildClipboardPeek picks the richest representation from what the system
// reader found. It is pure so the tests can drive every combination without a
// real clipboard.
func buildClipboardPeek(flavors []clipboardFlavor, values map[string]string) ClipboardPeek {
	peek := ClipboardPeek{Flavors: flavorNames(flavors)}
	if len(flavors) == 0 {
		peek.Kind = ClipboardEmpty
		return peek
	}

	// Files first: a Finder copy also carries the paths as text, and the file
	// list is the more useful framing.
	if raw, ok := values["furl"]; ok && strings.TrimSpace(raw) != "" {
		if files := parseFileList(raw); len(files) > 0 {
			peek.Kind = ClipboardFiles
			peek.Files = files
			peek.Text = strings.Join(filePaths(files), "\n")
			peek.CopyFlavor = "text"
			peek.Note = "A phone clipboard has no file type: copying gives the paths."
			return peek
		}
	}

	// Rich text: prefer HTML, fall back to RTF.
	if html, ok := values["html"]; ok && strings.TrimSpace(html) != "" {
		trimmed, cut := trimPreview(html)
		peek.Kind = ClipboardRich
		peek.HTML = trimmed
		peek.Truncated = cut
		peek.Bytes = len(html)
		peek.CopyFlavor = "html"
		text := ""
		if t, ok := values["utf8"]; ok && t != "" {
			text = t
		} else if t, ok := values["rtf"]; ok && t != "" {
			text = htmlToPlainText(t)
		} else {
			text = htmlToPlainText(html)
		}
		peek.Text, peek.Truncated = trimPreview(text)
		return peek
	}
	if rtf, ok := values["rtf"]; ok && strings.TrimSpace(rtf) != "" {
		_, cut := trimPreview(rtf)
		peek.Kind = ClipboardRich
		peek.Truncated = cut
		peek.Bytes = len(rtf)
		// RTF is converted to HTML on demand by the raw endpoint.
		peek.CopyFlavor = "rtf"
		peek.Note = "Rich text arrived as RTF; it is converted to HTML when copied."
		peek.Text, _ = trimPreview(htmlToPlainText(rtf))
		return peek
	}

	// Images before text: a screenshot usually also advertises a text flavor
	// (a filename or an empty string), and the picture is what was copied.
	if mime, flavor, size := pickImageFlavor(flavors); mime != "" {
		peek.Kind = ClipboardPicture
		peek.CopyFlavor = flavor
		peek.Bytes = size
		peek.Image = &ClipboardPictureInfo{MIME: mime, Bytes: size}
		if w, h, ok := imageSizeFromRaw(values["image_raw"], mime); ok {
			peek.Image.Width, peek.Image.Height = w, h
		}
		peek.Note = "If the phone refuses an image clipboard item, long-press the picture to save it."
		return peek
	}

	if text, ok := values["utf8"]; ok {
		trimmed, cut := trimPreview(text)
		peek.Kind = ClipboardText
		peek.Text = trimmed
		peek.Truncated = cut
		peek.Bytes = len(text)
		peek.CopyFlavor = "text"
		return peek
	}
	if text, ok := values["string"]; ok {
		trimmed, cut := trimPreview(text)
		peek.Kind = ClipboardText
		peek.Text = trimmed
		peek.Truncated = cut
		peek.Bytes = len(text)
		peek.CopyFlavor = "text"
		return peek
	}

	// Nothing readable: macOS keeps the type list after the content is cleared, so
	// an empty clipboard still advertises utf8. Say "empty" rather than showing a
	// blank text preview with dead buttons.
	peek.Kind = ClipboardEmpty
	peek.Note = "The Mac clipboard is empty."
	return peek
}

// pickImageFlavor chooses the lossless option when present, else the first
// image encoding macOS offered.
func pickImageFlavor(flavors []clipboardFlavor) (mime, flavor string, size int) {
	order := []struct{ want, mime string }{
		{"png", "image/png"},
		{"jpeg", "image/jpeg"},
		{"jpg", "image/jpeg"},
		{"tiff", "image/tiff"},
		{"gif", "image/gif"},
	}
	for _, cand := range order {
		for _, f := range flavors {
			if strings.Contains(f.Name, cand.want) {
				return cand.mime, cand.want, f.Bytes
			}
		}
	}
	return "", "", 0
}

func flavorNames(flavors []clipboardFlavor) []string {
	out := make([]string, 0, len(flavors))
	seen := map[string]bool{}
	for _, f := range flavors {
		if f.Name == "" || seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		out = append(out, f.Name)
	}
	return out
}

// parseFileList splits the newline-separated file URLs a Finder copy leaves.
func parseFileList(raw string) []ClipboardFile {
	lines := strings.Split(raw, "\n")
	files := make([]ClipboardFile, 0, len(lines))
	for _, line := range lines {
		path := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "file://"))
		if path == "" {
			continue
		}
		if decoded, err := url.PathUnescape(path); err == nil {
			path = decoded
		}
		name := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 && idx+1 < len(path) {
			name = path[idx+1:]
		}
		files = append(files, ClipboardFile{Name: name, Path: path})
	}
	return files
}

func filePaths(files []ClipboardFile) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

// PeekClipboard reads the Mac clipboard once. It returns an empty peek instead of
// an error when the platform has no clipboard, so the panel can say so plainly.
func (m *Manager) PeekClipboard(ctx context.Context) (ClipboardPeek, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	flavors, values, err := readSystemClipboard(ctx)
	if err != nil {
		return ClipboardPeek{Kind: ClipboardEmpty}, err
	}
	return buildClipboardPeek(flavors, values), nil
}

// ClipboardFlavorBytes returns one representation for the phone to copy. The
// flavor names come from the peek; unknown names are refused rather than
// guessed, so a stale panel cannot pull something else.
func (m *Manager) ClipboardFlavorBytes(ctx context.Context, flavor string) (ClipboardRaw, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	flavor = strings.ToLower(strings.TrimSpace(flavor))
	switch flavor {
	case "text", "utf8", "string", "html", "rtf", "png", "jpeg", "jpg", "tiff", "gif":
	default:
		return ClipboardRaw{}, fmt.Errorf("unsupported clipboard flavor %q", flavor)
	}
	return readSystemClipboardFlavor(ctx, flavor)
}

// imageSizeFromRaw reads the pixel dimensions out of a PNG or JPEG header. The
// preview shows them so a person can tell which screenshot they are looking at
// without the full image being fetched.
func imageSizeFromRaw(raw, mime string) (int, int, bool) {
	if raw == "" {
		return 0, 0, false
	}
	data := []byte(raw)
	switch mime {
	case "image/png":
		// 8-byte signature, then IHDR: length(4) type(4) width(4) height(4).
		if len(data) < 24 || string(data[12:16]) != "IHDR" {
			return 0, 0, false
		}
		w := int(data[16])<<24 | int(data[17])<<16 | int(data[18])<<8 | int(data[19])
		h := int(data[20])<<24 | int(data[21])<<16 | int(data[22])<<8 | int(data[23])
		if w <= 0 || h <= 0 {
			return 0, 0, false
		}
		return w, h, true
	case "image/jpeg":
		// Walk the segment list to the first SOFn frame header.
		for i := 2; i+9 < len(data); {
			if data[i] != 0xFF {
				i++
				continue
			}
			marker := data[i+1]
			if marker == 0xD8 || marker == 0xD9 || (marker >= 0xD0 && marker <= 0xD7) {
				i += 2
				continue
			}
			segLen := int(data[i+2])<<8 | int(data[i+3])
			if segLen < 2 || i+2+segLen > len(data) {
				return 0, 0, false
			}
			if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
				h := int(data[i+5])<<8 | int(data[i+6])
				w := int(data[i+7])<<8 | int(data[i+8])
				if w > 0 && h > 0 {
					return w, h, true
				}
				return 0, 0, false
			}
			i += 2 + segLen
		}
	}
	return 0, 0, false
}
