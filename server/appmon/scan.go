package appmon

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrUnsupported is returned on platforms without an app inventory.
var ErrUnsupported = errors.New("app inventory is not supported on this platform")

// ErrNoIcon means the app exists but exposes no PNG icon; the UI draws a tile.
var ErrNoIcon = errors.New("no icon")

const noteUnsupported = "unsupported"

// fsEnv is the filesystem surface the scanners use, injectable in tests.
type fsEnv struct {
	ReadDir  func(dir string) ([]os.DirEntry, error)
	ReadFile func(name string) ([]byte, error)
	Stat     func(name string) (os.FileInfo, error)
	HomeDir  func() (string, error)
	GOOS     string
}

func defaultEnv() fsEnv {
	return fsEnv{
		ReadDir:  os.ReadDir,
		ReadFile: os.ReadFile,
		Stat:     os.Stat,
		HomeDir:  os.UserHomeDir,
		GOOS:     runtime.GOOS,
	}
}

// manager owns the cached inventory plus the lazily decoded icons.
type manager struct {
	env fsEnv
	now func() time.Time

	mu      sync.Mutex
	cached  *Inventory
	cacheAt time.Time
	dirs    []string
	stamp   string
	// icons caches decoded PNG bytes keyed by app path; cleared on every rescan.
	icons    map[string][]byte
	iconMiss map[string]bool
}

var defaultManager = newManager(defaultEnv(), time.Now)

func newManager(env fsEnv, now func() time.Time) *manager {
	return &manager{env: env, now: now, icons: map[string][]byte{}, iconMiss: map[string]bool{}}
}

// Get returns the installed-app inventory, rebuilding it when the directory
// fingerprint changes or the TTL expires.
func Get() (*Inventory, error) {
	return defaultManager.get(false)
}

// Refresh rebuilds the inventory unconditionally.
func Refresh() (*Inventory, error) {
	return defaultManager.get(true)
}

func (m *manager) get(force bool) (*Inventory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !force && m.cached != nil && m.now().Sub(m.cacheAt) < cacheTTL && m.stamp == m.fingerprint() {
		return m.cached, nil
	}
	inv, err := m.scan()
	if err != nil {
		if m.cached != nil {
			return m.cached, nil
		}
		return nil, err
	}
	m.cached = inv
	m.cacheAt = m.now()
	m.dirs = inv.Dirs
	m.stamp = m.fingerprint()
	m.icons = map[string][]byte{}
	m.iconMiss = map[string]bool{}
	return inv, nil
}

// fingerprint tracks the mtime of every scanned directory, so installing or
// removing an app is visible without waiting for the TTL to expire.
func (m *manager) fingerprint() string {
	var b strings.Builder
	for _, dir := range m.dirs {
		info, err := m.env.Stat(dir)
		if err != nil {
			b.WriteString(dir)
			b.WriteString("|-\n")
			continue
		}
		b.WriteString(dir)
		b.WriteString("|")
		b.WriteString(info.ModTime().UTC().Format(time.RFC3339Nano))
		b.WriteString("\n")
	}
	return b.String()
}

func (m *manager) scan() (*Inventory, error) {
	apps, dirs, note, err := scanPlatform(m.env)
	if err != nil {
		return nil, err
	}
	sort.Slice(apps, func(i, j int) bool {
		li, lj := strings.ToLower(apps[i].Name), strings.ToLower(apps[j].Name)
		if li == lj {
			return apps[i].Path < apps[j].Path
		}
		return li < lj
	})
	inv := &Inventory{
		ScannedAt: m.now().UTC().Format(time.RFC3339),
		Platform:  m.env.GOOS,
		Dirs:      dirs,
		Apps:      apps,
		Note:      note,
	}
	if note == noteUnsupported {
		inv.Unsupported = true
	}
	return inv, nil
}

// icon returns PNG bytes for one inventory path. Only paths that are part of
// the cached inventory are served, so the endpoint cannot read arbitrary files.
func (m *manager) icon(path string) ([]byte, bool) {
	inv, err := m.get(false)
	if err != nil || inv == nil || path == "" {
		return nil, false
	}
	allowed := false
	for _, app := range inv.Apps {
		if app.Path == path {
			allowed = app.HasIcon
			break
		}
	}
	if !allowed {
		return nil, false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if data, ok := m.icons[path]; ok {
		return data, true
	}
	if m.iconMiss[path] {
		return nil, false
	}
	data, err := iconFor(path, m.env)
	if err != nil || len(data) == 0 {
		m.iconMiss[path] = true
		return nil, false
	}
	m.icons[path] = data
	return data, true
}

// etag is a strong validator tied to the icon bytes.
func etag(data []byte) string {
	sum := sha1.Sum(data)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// serveIcon writes PNG bytes with a long-lived cache header.
func serveIcon(w http.ResponseWriter, r *http.Request, data []byte) {
	tag := etag(data)
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	w.Header().Set("ETag", tag)
	if match := r.Header.Get("If-None-Match"); match != "" && match == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(data)
}

// fileExists reports whether path exists as a regular file or directory.
func fileExists(env fsEnv, path string) bool {
	if path == "" {
		return false
	}
	_, err := env.Stat(path)
	return err == nil
}

// joinPath joins path segments for the scanned platform.
func joinPath(parts ...string) string {
	return filepath.Join(parts...)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
