package appmon

import "time"

// cacheTTL bounds how often the installed-app inventory is rebuilt. Directory
// mtimes are still checked on every request, so newly installed apps show up
// immediately; only content changes inside an existing bundle wait out the TTL.
const cacheTTL = 5 * time.Minute

// App is one installed application, independent of whether it is running.
type App struct {
	// Name is the display name (CFBundleDisplayName / CFBundleName / .app basename).
	Name string `json:"name"`
	// Version is CFBundleShortVersionString when the bundle provides one.
	Version string `json:"version,omitempty"`
	// BundleID is CFBundleIdentifier; empty for Linux/Windows entries.
	BundleID string `json:"bundle_id,omitempty"`
	// Path is the absolute bundle (macOS) or .desktop/registry (other) path and
	// doubles as the join key against live processes.
	Path string `json:"path"`
	// System marks apps shipped with the OS. The UI hides them by default.
	System bool `json:"system,omitempty"`
	// Exec is the launcher basename used to attribute processes on platforms
	// without a bundle path (Linux .desktop Exec=, Windows DisplayIcon).
	Exec string `json:"exec,omitempty"`
	// HasIcon reports whether the icon endpoint returns bytes; false means the
	// UI draws a letter tile and never issues an icon request.
	HasIcon bool `json:"has_icon"`
}

// Inventory is the full installed-app picture for this host.
type Inventory struct {
	ScannedAt   string   `json:"scanned_at"`
	Platform    string   `json:"platform"`
	Dirs        []string `json:"dirs"`
	Apps        []App    `json:"apps"`
	Unsupported bool     `json:"unsupported,omitempty"`
	Note        string   `json:"note,omitempty"`
}
