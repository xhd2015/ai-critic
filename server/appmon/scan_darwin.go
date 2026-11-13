//go:build darwin

package appmon

import (
	"path/filepath"
	"sort"
	"strings"
)

// darwinAppDirs are the roots scanned for .app bundles. /System/Applications is
// included but every app below it is flagged System so the UI can hide the
// Apple-shipped inventory behind a toggle.
func darwinAppDirs(env fsEnv) []string {
	dirs := []string{}
	for _, dir := range []string{"/Applications", "/System/Applications"} {
		if fileExists(env, dir) {
			dirs = append(dirs, dir)
		}
	}
	if home, err := env.HomeDir(); err == nil && home != "" {
		if dir := filepath.Join(home, "Applications"); fileExists(env, dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// scanPlatform lists installed .app bundles and their advertised metadata.
func scanPlatform(env fsEnv) ([]App, []string, string, error) {
	dirs := darwinAppDirs(env)
	seen := map[string]bool{}
	apps := []App{}
	for _, dir := range dirs {
		entries, err := env.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".app") || strings.HasPrefix(name, ".") {
				continue
			}
			// The raw path is the join key against `ps` command lines, so it must
			// not be symlink-resolved.
			path := filepath.Join(dir, name)
			if seen[path] {
				continue
			}
			info, err := env.Stat(path)
			if err != nil || !info.IsDir() {
				continue
			}
			seen[path] = true
			apps = append(apps, darwinApp(env, path))
		}
	}
	return apps, dirs, "", nil
}

// darwinApp builds one inventory entry from a bundle's Info.plist.
func darwinApp(env fsEnv, bundlePath string) App {
	plist := readInfoPlist(env, bundlePath)
	name := firstNonEmpty(plist["CFBundleDisplayName"], plist["CFBundleName"])
	if name == "" {
		base := filepath.Base(bundlePath)
		name = strings.TrimSuffix(base, ".app")
	}
	version := firstNonEmpty(plist["CFBundleShortVersionString"], plist["CFBundleVersion"])
	iconPath := darwinIconPath(env, bundlePath, plist["CFBundleIconFile"])
	return App{
		Name:     name,
		Version:  version,
		BundleID: plist["CFBundleIdentifier"],
		Path:     bundlePath,
		System:   strings.HasPrefix(bundlePath, "/System/"),
		HasIcon:  iconPath != "",
	}
}

// readInfoPlist parses Contents/Info.plist; missing or unparsable files yield
// an empty map so the bundle still shows up under its folder name.
func readInfoPlist(env fsEnv, bundlePath string) map[string]string {
	data, err := env.ReadFile(filepath.Join(bundlePath, "Contents", "Info.plist"))
	if err != nil {
		return map[string]string{}
	}
	keys, err := parsePlistKeys(data)
	if err != nil {
		return map[string]string{}
	}
	return keys
}

// darwinIconPath resolves the .icns file for a bundle. CFBundleIconFile wins;
// otherwise the first *.icns (usually AppIcon.icns) in Contents/Resources.
func darwinIconPath(env fsEnv, bundlePath, iconFile string) string {
	resources := filepath.Join(bundlePath, "Contents", "Resources")
	if iconFile != "" {
		candidate := iconFile
		if !strings.HasSuffix(strings.ToLower(candidate), ".icns") {
			candidate += ".icns"
		}
		if path := filepath.Join(resources, candidate); fileExists(env, path) {
			return path
		}
	}
	entries, err := env.ReadDir(resources)
	if err != nil {
		return ""
	}
	names := []string{}
	for _, entry := range entries {
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".icns") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.EqualFold(name, "AppIcon.icns") {
			return filepath.Join(resources, name)
		}
	}
	return filepath.Join(resources, names[0])
}

// iconFor reads the bundle icon and returns the embedded PNG chunk verbatim.
func iconFor(appPath string, env fsEnv) ([]byte, error) {
	iconPath := darwinIconPath(env, appPath, readInfoPlist(env, appPath)["CFBundleIconFile"])
	if iconPath == "" {
		return nil, ErrNoIcon
	}
	data, err := env.ReadFile(iconPath)
	if err != nil {
		return nil, err
	}
	png := pngFromICNS(data)
	if len(png) == 0 {
		return nil, ErrNoIcon
	}
	return png, nil
}
