//go:build windows

package appmon

import (
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// winUninstallRoots are the registry locations that hold "Programs and
// Features" entries. The WOW6432Node copy covers 32-bit installers, and HKCU
// covers per-user installs.
var winUninstallRoots = []struct {
	hive registry.Key
	path string
}{
	{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
	{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
}

// scanPlatform lists installed programs from the uninstall registry keys, which
// is what Windows' own "Installed apps" page is built from.
func scanPlatform(env fsEnv) ([]App, []string, string, error) {
	apps := []App{}
	seen := map[string]bool{}
	dirs := []string{}
	for _, root := range winUninstallRoots {
		key, err := registry.OpenKey(root.hive, root.path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		dirs = append(dirs, `registry:`+root.path)
		names, err := key.ReadSubKeyNames(-1)
		if err != nil {
			_ = key.Close()
			continue
		}
		for _, name := range names {
			sub, err := registry.OpenKey(key, name, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			app, ok := windowsApp(sub, root.path, name)
			_ = sub.Close()
			if !ok || seen[strings.ToLower(app.Name)] {
				continue
			}
			seen[strings.ToLower(app.Name)] = true
			apps = append(apps, app)
		}
		_ = key.Close()
	}
	sort.Slice(apps, func(i, j int) bool { return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name) })
	return apps, dirs, "windows-icons-pending", nil
}

func windowsApp(key registry.Key, rootPath, name string) (App, bool) {
	displayName, _, err := key.GetStringValue("DisplayName")
	if err != nil || strings.TrimSpace(displayName) == "" {
		return App{}, false
	}
	// Patches and updates point at a parent product and are not apps.
	if parent, _, _ := key.GetStringValue("ParentKeyName"); parent != "" {
		return App{}, false
	}
	if component, _, _ := key.GetIntegerValue("SystemComponent"); component == 1 {
		return App{}, false
	}
	version, _, _ := key.GetStringValue("DisplayVersion")
	icon, _, _ := key.GetStringValue("DisplayIcon")
	execPath := strings.Trim(strings.SplitN(icon, ",", 2)[0], `"`)

	// Path is synthetic: it identifies the registry entry, which is unique.
	return App{
		Name:     strings.TrimSpace(displayName),
		Version:  strings.TrimSpace(version),
		BundleID: name,
		Path:     `registry:` + rootPath + `\` + name,
		HasIcon:  false, // icon extraction from .exe needs win32; letter tiles for now
		Exec:     windowsExecBase(execPath),
	}, true
}

func windowsExecBase(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(strings.ReplaceAll(path, `\`, `/`))
	base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	return base
}

func iconFor(appPath string, env fsEnv) ([]byte, error) {
	return nil, ErrNoIcon
}
