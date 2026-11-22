package browser

import (
	"os"
	"testing"
)

func TestParseClipboardInfoPairsNamesWithSizes(t *testing.T) {
	// The real shape from `clipboard info`, including the unquoted labels.
	got := parseClipboardInfo("«class utf8», 15, «class ut16», 32, string, 15, Unicode text, 30")
	if len(got) != 4 {
		t.Fatalf("flavors = %+v, want 4", got)
	}
	if got[0].Name != "utf8" || got[0].Bytes != 15 {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Name != "ut16" || got[1].Bytes != 32 {
		t.Fatalf("second = %+v", got[1])
	}
	if got[3].Name != "unicode text" || got[3].Bytes != 30 {
		t.Fatalf("fourth = %+v", got[3])
	}
}

func TestParseClipboardInfoHandlesAnImageListing(t *testing.T) {
	got := parseClipboardInfo("«class PNGf», 93763, «class AVIF», 30787, GIF picture, 36552, «class jp2 », 108043")
	if !flavorPresent(got, "png") {
		t.Fatalf("png missing: %+v", got)
	}
	if !flavorPresent(got, "gif") {
		t.Fatalf("gif missing: %+v", got)
	}
	// A zero-length clipboard still lists its types; that is not an empty list.
	if len(got) != 4 {
		t.Fatalf("flavors = %+v, want 4", got)
	}
}

func TestParseClipboardInfoEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "\n"} {
		if got := parseClipboardInfo(in); len(got) != 0 {
			t.Fatalf("%q -> %+v, want none", in, got)
		}
	}
}

func TestDecodeAppleHexStripsTheClassCode(t *testing.T) {
	// «data HTML<hex of "<h1>hi</h1>">»
	raw, err := decodeAppleHex("«data HTML3C68313E68693C2F68313E»", 4)
	if err != nil {
		t.Fatalf("decodeAppleHex: %v", err)
	}
	if string(raw) != "<h1>hi</h1>" {
		t.Fatalf("decoded = %q", string(raw))
	}
}

func TestDecodeAppleHexRejectsGarbage(t *testing.T) {
	if _, err := decodeAppleHex("«data HTMLzz»", 4); err == nil {
		t.Fatal("non-hex payload must fail")
	}
	if _, err := decodeAppleHex("", 4); err == nil {
		t.Fatal("empty payload must fail")
	}
}

func TestBuildPeekPrefersText(t *testing.T) {
	flavors := parseClipboardInfo("«class utf8», 17, «class ut16», 34")
	peek := buildClipboardPeek(flavors, map[string]string{"utf8": "line one\nline two"})
	if peek.Kind != ClipboardText {
		t.Fatalf("kind = %s", peek.Kind)
	}
	if peek.Text != "line one\nline two" {
		t.Fatalf("text = %q", peek.Text)
	}
	if peek.CopyFlavor != "text" {
		t.Fatalf("copy flavor = %q", peek.CopyFlavor)
	}
}

func TestBuildPeekPrefersHTMLForRichText(t *testing.T) {
	flavors := parseClipboardInfo("«class HTML», 88, «class utf8», 40")
	peek := buildClipboardPeek(flavors, map[string]string{
		"html": "<h1 style=\"color:#c00\">Plan</h1><p>Ship <b>it</b>.</p>",
		"utf8": "Plan\nShip it.",
	})
	if peek.Kind != ClipboardRich {
		t.Fatalf("kind = %s", peek.Kind)
	}
	if peek.CopyFlavor != "html" {
		t.Fatalf("copy flavor = %q, want html", peek.CopyFlavor)
	}
	// The plain half comes from the clipboard's own text flavor, not from
	// stripping tags, so the fallback keeps the author's line breaks.
	if peek.Text != "Plan\nShip it." {
		t.Fatalf("text = %q", peek.Text)
	}
}

func TestBuildPeekDerivesPlainTextWhenNoTextFlavor(t *testing.T) {
	flavors := parseClipboardInfo("«class HTML», 60")
	peek := buildClipboardPeek(flavors, map[string]string{
		"html": "<h1>Plan</h1><p>Ship &amp; <b>copy</b>.</p>",
	})
	if peek.Kind != ClipboardRich {
		t.Fatalf("kind = %s", peek.Kind)
	}
	if peek.Text != "PlanShip & copy." {
		t.Fatalf("derived text = %q", peek.Text)
	}
}

func TestBuildPeekTreatsFilesAsAPathList(t *testing.T) {
	flavors := parseClipboardInfo("«class furl», 120, «class utf8», 120")
	peek := buildClipboardPeek(flavors, map[string]string{
		"furl": "file:///Users/x/report.pdf\nfile:///Users/x/notes.md\n",
	})
	if peek.Kind != ClipboardFiles {
		t.Fatalf("kind = %s", peek.Kind)
	}
	if len(peek.Files) != 2 {
		t.Fatalf("files = %+v", peek.Files)
	}
	if peek.Files[0].Name != "report.pdf" {
		t.Fatalf("first = %+v", peek.Files[0])
	}
	if peek.CopyFlavor != "text" {
		t.Fatalf("copy flavor = %q, want text", peek.CopyFlavor)
	}
	if peek.Text != "/Users/x/report.pdf\n/Users/x/notes.md" {
		t.Fatalf("paths = %q", peek.Text)
	}
}

func TestBuildPeekPrefersImagesOverText(t *testing.T) {
	// A screenshot also advertises a text flavor; the picture is what was copied.
	flavors := parseClipboardInfo("«class PNGf», 93763, «class jpeg», 79017, «class utf8», 4")
	peek := buildClipboardPeek(flavors, map[string]string{"utf8": "shot"})
	if peek.Kind != ClipboardPicture {
		t.Fatalf("kind = %s, want image", peek.Kind)
	}
	if peek.CopyFlavor != "png" {
		t.Fatalf("copy flavor = %q, want the lossless png", peek.CopyFlavor)
	}
	if peek.Image == nil || peek.Image.MIME != "image/png" {
		t.Fatalf("image = %+v", peek.Image)
	}
}

func TestBuildPeekFallsBackToJpegWhenNoPng(t *testing.T) {
	flavors := parseClipboardInfo("«class JPEG», 79017, «class TIFF», 4227614")
	peek := buildClipboardPeek(flavors, nil)
	if peek.CopyFlavor != "jpeg" {
		t.Fatalf("copy flavor = %q, want jpeg", peek.CopyFlavor)
	}
	if peek.Image.MIME != "image/jpeg" {
		t.Fatalf("mime = %q", peek.Image.MIME)
	}
}

func TestBuildPeekReportsEmptyWhenContentIsCleared(t *testing.T) {
	// macOS keeps the type list after the content is emptied, so flavors alone do
	// not mean there is something to copy.
	flavors := parseClipboardInfo("«class utf8», 0, string, 0")
	peek := buildClipboardPeek(flavors, nil)
	if peek.Kind != ClipboardEmpty {
		t.Fatalf("kind = %s, want empty", peek.Kind)
	}
	if peek.CopyFlavor != "" {
		t.Fatalf("copy flavor = %q, want none", peek.CopyFlavor)
	}
}

func TestBuildPeekNoFlavorsIsEmpty(t *testing.T) {
	peek := buildClipboardPeek(nil, nil)
	if peek.Kind != ClipboardEmpty {
		t.Fatalf("kind = %s", peek.Kind)
	}
}

func TestBuildPeekTruncatesLongText(t *testing.T) {
	long := make([]byte, maxClipboardPreview+500)
	for i := range long {
		long[i] = 'a'
	}
	flavors := parseClipboardInfo("«class utf8», 4500")
	peek := buildClipboardPeek(flavors, map[string]string{"utf8": string(long)})
	if !peek.Truncated {
		t.Fatal("expected Truncated")
	}
	if len(peek.Text) != maxClipboardPreview {
		t.Fatalf("preview len = %d, want %d", len(peek.Text), maxClipboardPreview)
	}
	// The reported size is the real one, so the panel can say "4000 of 4500".
	if peek.Bytes != len(long) {
		t.Fatalf("bytes = %d, want %d", peek.Bytes, len(long))
	}
}

func TestTrimPreviewDoesNotSplitARune(t *testing.T) {
	// Three-byte runes, one byte past the budget.
	text := ""
	for i := 0; i < maxClipboardPreview; i += 3 {
		text += "字"
	}
	got, cut := trimPreview(text)
	if !cut {
		t.Fatal("expected Truncated")
	}
	if !utf8Valid(got) {
		t.Fatalf("preview ends mid-rune")
	}
}

func TestImageSizeFromPNGHeader(t *testing.T) {
	// Minimal PNG signature + IHDR carrying 800x1760.
	data := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A,
		0, 0, 0, 13, 'I', 'H', 'D', 'R',
		0, 0, 3, 0x20, // width 800
		0, 0, 6, 0xE0, // height 1760
		8, 6, 0, 0, 0}
	w, h, ok := imageSizeFromRaw(string(data), "image/png")
	if !ok || w != 800 || h != 1760 {
		t.Fatalf("got %dx%d ok=%v, want 800x1760", w, h, ok)
	}
}

func TestImageSizeRejectsNonImage(t *testing.T) {
	if _, _, ok := imageSizeFromRaw("not an image", "image/png"); ok {
		t.Fatal("garbage must not report a size")
	}
	if _, _, ok := imageSizeFromRaw("", "image/png"); ok {
		t.Fatal("empty must not report a size")
	}
}

func TestClipboardFlavorBytesRefusesUnknownFlavor(t *testing.T) {
	// A stale panel must not be able to pull something the peek never named.
	if _, err := (&Manager{}).ClipboardFlavorBytes(t.Context(), "exe"); err == nil {
		t.Fatal("unknown flavor must be refused")
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			return false
		}
	}
	return true
}

// Keep the os import used on every platform build of this test file.
var _ = os.Getenv
