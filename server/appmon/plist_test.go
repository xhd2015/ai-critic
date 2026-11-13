package appmon

import (
	"encoding/base64"
	"testing"
)

// readFixture returns an embedded fixture by file name so tests keep working
// from any working directory and without binary files in the repository.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	switch name {
	case "info.xml.plist":
		return []byte(fixtureXMLPlist)
	case "info.binary.plist":
		return decodeFixture(t, name, fixtureBinaryPlistBase64)
	case "app.icns":
		return decodeFixture(t, name, fixtureICNSBase64)
	case "icon11.png":
		return decodeFixture(t, name, fixtureIconPNGBase64)
	}
	t.Fatalf("unknown fixture %q", name)
	return nil
}

func decodeFixture(t *testing.T, name, encoded string) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return data
}

func checkFixtureKeys(t *testing.T, keys map[string]string) {
	t.Helper()
	want := map[string]string{
		"CFBundleName":               "Fixture",
		"CFBundleDisplayName":        "Fixture App",
		"CFBundleIdentifier":         "com.example.fixture",
		"CFBundleShortVersionString": "2.5.1",
		"CFBundleVersion":            "251",
		"LSMinimumSystemVersion":     "13.0",
		"NSHighResolutionCapable":    "1",
	}
	for key, value := range want {
		if keys[key] != value {
			t.Errorf("%s=%q want %q", key, keys[key], value)
		}
	}
	// Nested containers are skipped, so their keys must not leak upward.
	if keys["Items"] != "" {
		t.Errorf("nested array leaked into top level: %q", keys["Items"])
	}
}

func TestParsePlistKeysXML(t *testing.T) {
	keys, err := parsePlistKeys(readFixture(t, "info.xml.plist"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	checkFixtureKeys(t, keys)
	// The nested dict repeats CFBundleName; the first occurrence must win.
	if keys["CFBundleName"] != "Fixture" {
		t.Fatalf("nested dict overwrote top level: %q", keys["CFBundleName"])
	}
}

func TestParsePlistKeysBinary(t *testing.T) {
	keys, err := parsePlistKeys(readFixture(t, "info.binary.plist"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	checkFixtureKeys(t, keys)
}

func TestParsePlistKeysRejectsGarbage(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("not a plist"), []byte("bplist00"), []byte("<plist><dict>")} {
		if _, err := parsePlistKeys(data); err == nil {
			t.Errorf("expected error for %q", string(data))
		}
	}
}

func TestParsePlistKeysBinaryTruncated(t *testing.T) {
	full := readFixture(t, "info.binary.plist")
	for _, cut := range []int{8, 20, len(full) / 2, len(full) - 4} {
		if _, err := parsePlistKeys(full[:cut]); err == nil {
			t.Errorf("expected error for truncated binary plist (%d bytes)", cut)
		}
	}
}
