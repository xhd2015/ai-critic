//go:build linux

package appmon

import (
	"path/filepath"
	"sort"
	"strings"
)

// linuxDesktopDirs are the freedesktop application directories. Flatpak and
// snap publish .desktop entries into their own export roots.
func linuxDesktopDirs(env fsEnv) []string {
	dirs := []string{}
	for _, dir := range []string{
		"/usr/share/applications",
		"/usr/local/share/applications",
		"/var/lib/flatpak/exports/share/applications",
		"/var/lib/snapd/desktop/applications",
	} {
		if fileExists(env, dir) {
			dirs = append(dirs, dir)
		}
	}
	if home, err := env.HomeDir(); err == nil && home != "" {
		for _, rel := range []string{
			filepath.Join(".local", "share", "applications"),
			filepath.Join(".local", "share", "flatpak", "exports", "share", "applications"),
		} {
			if dir := filepath.Join(home, rel); fileExists(env, dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs
}

// scanPlatform lists freedesktop .desktop entries.
func scanPlatform(env fsEnv) ([]App, []string, string, error) {
	dirs := linuxDesktopDirs(env)
	apps := []App{}
	for _, dir := range dirs {
		entries, err := env.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".desktop") || strings.HasPrefix(name, ".") {
				continue
			}
			path := filepath.Join(dir, name)
			app, ok := linuxDesktopApp(env, path)
			if !ok {
				continue
			}
			apps = append(apps, app)
		}
	}
	return apps, dirs, "", nil
}

func linuxDesktopApp(env fsEnv, path string) (App, bool) {
	data, err := env.ReadFile(path)
	if err != nil {
		return App{}, false
	}
	fields := parseDesktopEntry(string(data))
	if !strings.EqualFold(fields["Type"], "Application") {
		return App{}, false
	}
	if isTrue(fields["NoDisplay"]) || isTrue(fields["Hidden"]) {
		return App{}, false
	}
	name := fields["Name"]
	if name == "" {
		return App{}, false
	}
	iconPath := linuxIconPath(env, fields["Icon"])
	return App{
		Name:     name,
		BundleID: strings.TrimSuffix(filepath.Base(path), ".desktop"),
		Path:     path,
		System:   strings.HasPrefix(path, "/usr/") || strings.HasPrefix(path, "/var/"),
		HasIcon:  iconPath != "" && strings.HasSuffix(strings.ToLower(iconPath), ".png"),
		Exec:     execBase(fields["Exec"]),
	}, true
}

func linuxIconPath(env fsEnv, icon string) string {
	if icon == "" {
		return ""
	}
	if strings.HasPrefix(icon, "/") {
		if fileExists(env, icon) {
			return icon
		}
		return ""
	}
	home, _ := env.HomeDir()
	roots := []string{"/usr/share/icons", "/usr/local/share/icons"}
	if home != "" {
		roots = append(roots, filepath.Join(home, ".local", "share", "icons"), filepath.Join(home, ".icons"))
	}
	sizes := []string{"256x256", "128x128", "64x64", "48x48", "scalable", "32x32"}
	for _, root := range roots {
		themes, err := env.ReadDir(root)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(themes))
		for _, theme := range themes {
			if theme.IsDir() {
				names = append(names, theme.Name())
			}
		}
		sort.Strings(names)
		for _, theme := range names {
			for _, size := range sizes {
				for _, ext := range []string{".png", ".svg"} {
					candidate := filepath.Join(root, theme, size, "apps", icon+ext)
					if fileExists(env, candidate) {
						return candidate
					}
				}
			}
		}
	}
	for _, dir := range []string{"/usr/share/pixmaps", "/usr/local/share/pixmaps"} {
		for _, ext := range []string{".png", ".svg", ".xpm"} {
			candidate := filepath.Join(dir, icon+ext)
			if fileExists(env, candidate) {
				return candidate
			}
		}
	}
	return ""
}

// iconFor returns the PNG bytes of a .desktop entry's icon.
func iconFor(appPath string, env fsEnv) ([]byte, error) {
	data, err := env.ReadFile(appPath)
	if err != nil {
		return nil, err
	}
	iconPath := linuxIconPath(env, parseDesktopEntry(string(data))["Icon"])
	if iconPath == "" {
		return nil, ErrNoIcon
	}
	raw, err := env.ReadFile(iconPath)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(string(raw), "\x89PNG") {
		return nil, ErrNoIcon
	}
	return raw, nil
}
