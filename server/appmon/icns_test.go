package appmon

import (
	"encoding/binary"
	"testing"
)

func TestPNGFromICNSPrefersModernChunk(t *testing.T) {
	png := pngFromICNS(readFixture(t, "app.icns"))
	if len(png) == 0 {
		t.Fatal("no png extracted")
	}
	want := readFixture(t, "icon11.png")
	if string(png) != string(want) {
		t.Fatalf("extracted %d bytes, want %d bytes", len(png), len(want))
	}
}

func TestPNGFromICNSSkipsLegacyOnly(t *testing.T) {
	legacy := buildICNS(map[string][]byte{"ic04": {0, 0, 0, 1, 0, 0}})
	if png := pngFromICNS(legacy); png != nil {
		t.Fatalf("legacy-only icns returned %d bytes", len(png))
	}
}

func TestPNGFromICNSRejectsMalformed(t *testing.T) {
	good := readFixture(t, "app.icns")
	cases := map[string][]byte{
		"empty":     nil,
		"bad magic": append([]byte("icnz"), good[4:]...),
		"truncated": good[:20],
		// A chunk whose length points past the container must not slice out.
		"overflow": func() []byte {
			bad := append([]byte(nil), good...)
			binary.BigEndian.PutUint32(bad[8+4:8+8], 0xFFFFFF)
			return bad
		}(),
	}
	for name, data := range cases {
		if png := pngFromICNS(data); png != nil {
			t.Errorf("%s: expected no png, got %d bytes", name, len(png))
		}
	}
}

func buildICNS(chunks map[string][]byte) []byte {
	order := []string{"ic04", "ic07", "ic11", "ic12"}
	body := []byte{}
	for _, kind := range order {
		payload, ok := chunks[kind]
		if !ok {
			continue
		}
		header := make([]byte, 8)
		copy(header, kind)
		binary.BigEndian.PutUint32(header[4:], uint32(8+len(payload)))
		body = append(body, header...)
		body = append(body, payload...)
	}
	out := make([]byte, 8)
	copy(out, "icns")
	binary.BigEndian.PutUint32(out[4:], uint32(8+len(body)))
	return append(out, body...)
}
