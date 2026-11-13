//go:build darwin

package appmon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mappingEnv presents a temp directory tree under the absolute paths the darwin
// scanner is hard-coded to look at.
type mappingEnv struct {
	m    map[string]string
	home string
}

func (e *mappingEnv) resolve(path string) string {
	for virt, real := range e.m {
		if path == virt {
			return real
		}
		if strings.HasPrefix(path, virt+"/") {
			return real + path[len(virt):]
		}
	}
	return path
}

func (e *mappingEnv) env() fsEnv {
	return fsEnv{
		ReadDir:  func(dir string) ([]os.DirEntry, error) { return os.ReadDir(e.resolve(dir)) },
		ReadFile: func(name string) ([]byte, error) { return os.ReadFile(e.resolve(name)) },
		Stat:     func(name string) (os.FileInfo, error) { return os.Stat(e.resolve(name)) },
		HomeDir:  func() (string, error) { return e.home, nil },
		GOOS:     "darwin",
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// plistFor builds an Info.plist with just the keys the scanner reads.
func plistFor(displayName, name, version, iconFile string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>`)
	write := func(key, value string) {
		if value == "" {
			return
		}
		b.WriteString("<key>" + key + "</key><string>" + value + "</string>")
	}
	write("CFBundleDisplayName", displayName)
	write("CFBundleName", name)
	write("CFBundleShortVersionString", version)
	write("CFBundleIconFile", iconFile)
	b.WriteString("</dict></plist>")
	return []byte(b.String())
}

const testHome = "/Users/tester"

// fixtureEnv builds three app roots: /Applications, /System/Applications and
// the user's ~/Applications, each with a distinct bundle.
func fixtureEnv(t *testing.T) *mappingEnv {
	t.Helper()
	root := t.TempDir()
	apps := filepath.Join(root, "Applications")
	system := filepath.Join(root, "SystemApplications")
	user := filepath.Join(root, "home", "Applications")
	for _, dir := range []string{apps, system, user} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	icns := readFixture(t, "app.icns")

	// Explicit CFBundleIconFile.
	mustWrite(t, filepath.Join(apps, "Named.app", "Contents", "Info.plist"),
		plistFor("Named App", "Named", "1.2.3", "Icon"))
	mustWrite(t, filepath.Join(apps, "Named.app", "Contents", "Resources", "Icon.icns"), icns)

	// No CFBundleIconFile: the scanner must fall back to Resources/*.icns.
	mustWrite(t, filepath.Join(apps, "Fallback.app", "Contents", "Info.plist"),
		plistFor("", "Fallback", "4.5", ""))
	mustWrite(t, filepath.Join(apps, "Fallback.app", "Contents", "Resources", "Whatever.icns"), icns)

	// Binary Info.plist, no icon resources at all.
	mustWrite(t, filepath.Join(apps, "Binary.app", "Contents", "Info.plist"), readFixture(t, "info.binary.plist"))
	// Bare bundle: no Info.plist → name from folder, no icon.
	if err := os.MkdirAll(filepath.Join(apps, "Plain.app", "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Only *.app entries are apps.
	mustWrite(t, filepath.Join(apps, "notes.txt"), []byte("ignored"))

	// System bundle is flagged, and its display name is distinct.
	mustWrite(t, filepath.Join(system, "Systemy.app", "Contents", "Info.plist"),
		plistFor("Systemy App", "Systemy", "1.0", ""))
	// Per-user bundle.
	mustWrite(t, filepath.Join(user, "UserApp.app", "Contents", "Info.plist"),
		plistFor("User App", "UserApp", "9.9", ""))

	return &mappingEnv{
		m: map[string]string{
			"/Applications":            apps,
			"/System/Applications":     system,
			testHome + "/Applications": user,
		},
		home: testHome,
	}
}

func TestDarwinScanInventory(t *testing.T) {
	fe := fixtureEnv(t)
	env := fe.env()
	apps, dirs, note, err := scanPlatform(env)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if note != "" {
		t.Fatalf("unexpected note %q", note)
	}
	if len(dirs) != 3 {
		t.Fatalf("dirs=%v", dirs)
	}

	byName := map[string]App{}
	for _, app := range apps {
		byName[app.Name] = app
	}
	if len(apps) != 6 {
		t.Fatalf("apps=%d %+v", len(apps), byName)
	}

	named, ok := byName["Named App"]
	if !ok {
		t.Fatalf("display name not preferred: %+v", byName)
	}
	if named.Version != "1.2.3" || !named.HasIcon || named.System {
		t.Errorf("named=%+v", named)
	}
	if named.Path != "/Applications/Named.app" {
		t.Errorf("path=%q", named.Path)
	}
	if named.BundleID != "" {
		t.Errorf("bundle id should be empty in fixture: %q", named.BundleID)
	}

	fallback, ok := byName["Fallback"]
	if !ok {
		t.Fatalf("missing fallback bundle: %+v", byName)
	}
	if !fallback.HasIcon {
		t.Errorf("fallback icns not detected: %+v", fallback)
	}

	binaryApp, ok := byName["Fixture App"]
	if !ok {
		t.Fatalf("binary plist bundle missing: %+v", byName)
	}
	if binaryApp.Version != "2.5.1" || binaryApp.BundleID != "com.example.fixture" {
		t.Errorf("binary=%+v", binaryApp)
	}
	if binaryApp.HasIcon {
		t.Errorf("binary bundle has no resources but reports an icon: %+v", binaryApp)
	}

	plain, ok := byName["Plain"]
	if !ok {
		t.Fatalf("bare bundle missing: %+v", byName)
	}
	if plain.HasIcon {
		t.Errorf("bare bundle reported an icon: %+v", plain)
	}
	if plain.Version != "" {
		t.Errorf("bare bundle version=%q", plain.Version)
	}

	systemy, ok := byName["Systemy App"]
	if !ok {
		t.Fatalf("system bundle missing: %+v", byName)
	}
	if !systemy.System {
		t.Errorf("system bundle not flagged: %+v", systemy)
	}
	if userApp, ok := byName["User App"]; !ok || userApp.System {
		t.Errorf("user app=%+v present=%v", userApp, ok)
	}
}

func TestDarwinIconForReturnsPNG(t *testing.T) {
	fe := fixtureEnv(t)
	env := fe.env()
	apps, _, _, err := scanPlatform(env)
	if err != nil {
		t.Fatal(err)
	}
	var withIcon App
	for _, app := range apps {
		if app.HasIcon {
			withIcon = app
			break
		}
	}
	if withIcon.Path == "" {
		t.Fatal("no app with icon in fixture")
	}
	data, err := iconFor(withIcon.Path, env)
	if err != nil {
		t.Fatalf("iconFor: %v", err)
	}
	if want := readFixture(t, "icon11.png"); string(data) != string(want) {
		t.Fatalf("icon bytes differ: %d vs %d", len(data), len(want))
	}

	// A bundle without resources must report ErrNoIcon, not empty bytes.
	data, err = iconFor("/Applications/Plain.app", env)
	if err != ErrNoIcon {
		t.Fatalf("err=%v data=%d", err, len(data))
	}
}

func TestManagerCachesUntilDirectoryChanges(t *testing.T) {
	fe := fixtureEnv(t)
	env := fe.env()
	now := time.Unix(0, 0)
	m := newManager(env, func() time.Time { return now })

	first, err := m.get(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Apps) != 6 {
		t.Fatalf("apps=%d", len(first.Apps))
	}
	// Second call inside the TTL must not rescan.
	second, err := m.get(false)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatal("expected the cached inventory pointer")
	}

	// A new bundle changes the directory mtime, so the cache is invalidated.
	mustWrite(t, filepath.Join(fe.m["/Applications"], "New.app", "Contents", "Info.plist"),
		plistFor("New App", "New", "1.0", ""))
	future := now.Add(time.Minute)
	for i := 0; i < 2; i++ {
		if err := os.Chtimes(fe.m["/Applications"], future, future); err != nil {
			t.Fatal(err)
		}
	}
	third, err := m.get(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Apps) != 7 {
		t.Fatalf("directory change did not invalidate cache: apps=%d", len(third.Apps))
	}
}
