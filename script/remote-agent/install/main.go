// Command install builds remote-agent (via script/remote-agent/build) and
// installs the binary into a directory on PATH, replacing any previous build.
//
// On macOS the build step code signs the binary, so the installed file keeps a
// stable code identity across rebuilds and macOS privacy grants (notably Local
// Network access, which third-party binaries need to reach LAN/ULA addresses)
// are not revoked every time you rebuild.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xhd2015/less-gen/flags"
)

const (
	binaryName = "remote-agent"
	// buildScript is invoked with `go run`, matching script/build/build.go.
	buildScript = "./script/remote-agent/build"
)

var help = `
Usage: go run ./script/remote-agent/install [options]

Builds remote-agent and installs it into a PATH directory, replacing any
previous build. On macOS the binary is code signed by the build step.

Options:
  -d, --dir PATH        Install directory
                        (default: $GOBIN, else <go env GOPATH>/bin)
      --no-sign         Skip code signing (macOS only; signing is on by default)
      --identity NAME   Signing identity, passed through to the build script
      --identifier ID   Signature identifier, passed through to the build script
  -h, --help            Show this help message

Examples:
  go run ./script/remote-agent/install
  go run ./script/remote-agent/install --dir ~/.local/bin
  CODESIGN_IDENTITY="Developer ID Application: ..." go run ./script/remote-agent/install
`

func main() {
	if err := Handle(os.Args[1:]); err != nil {
		if errors.Is(err, flags.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// Handle builds remote-agent and installs it into the target directory.
func Handle(args []string) error {
	var dir string
	var noSign bool
	var identity string
	var identifier string
	_, err := flags.
		String("-d,--dir", &dir).
		Bool("--no-sign", &noSign).
		String("--identity", &identity).
		String("--identifier", &identifier).
		Help("-h,--help", help).
		Parse(args)
	if err != nil {
		return err
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	if dir == "" {
		dir, err = defaultInstallDir()
		if err != nil {
			return err
		}
	}
	if abs, absErr := filepath.Abs(dir); absErr == nil {
		dir = abs
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create install directory %s: %v", dir, err)
	}

	target := filepath.Join(dir, binaryName)
	// Build next to the final path so the swap is an atomic rename on the same
	// filesystem, and so the signing step sees a stable file name.
	tmp := fmt.Sprintf("%s.tmp-%d", target, os.Getpid())

	buildArgs := []string{"run", buildScript, "-o", tmp}
	if noSign {
		buildArgs = append(buildArgs, "--no-sign")
	}
	if identity != "" {
		buildArgs = append(buildArgs, "--identity", identity)
	}
	if identifier != "" {
		buildArgs = append(buildArgs, "--identifier", identifier)
	}

	fmt.Printf("==> go %s\n", strings.Join(buildArgs, " "))
	build := exec.Command("go", buildArgs...)
	build.Dir = root
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("build failed: %v", err)
	}
	defer os.Remove(tmp) // no-op after a successful rename

	if err := os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("cannot chmod %s: %v", tmp, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("cannot install %s -> %s: %v", tmp, target, err)
	}

	info, err := os.Stat(target)
	if err == nil {
		fmt.Printf("==> Installed %s (%s, %s)\n", target, humanSize(info.Size()), runtime.GOOS+"/"+runtime.GOARCH)
	} else {
		fmt.Printf("==> Installed %s\n", target)
	}
	if !onPath(dir) {
		fmt.Printf("    warning: %s is not on PATH; add it before running %s directly\n", dir, binaryName)
	}
	return nil
}

// repoRoot resolves the repository root from this source file's path, which is
// stable under `go run`.
func repoRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot resolve script location")
	}
	// <root>/script/remote-agent/install/main.go -> <root>
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("cannot locate repo root above %s: %v", thisFile, err)
	}
	return root, nil
}

// defaultInstallDir prefers $GOBIN, then <GOPATH>/bin -- the directory `go
// install` would use, which is where remote-agent is expected to live.
func defaultInstallDir() (string, error) {
	if gobin := strings.TrimSpace(os.Getenv("GOBIN")); gobin != "" {
		return gobin, nil
	}
	out, err := exec.Command("go", "env", "GOPATH").Output()
	if err != nil {
		return "", fmt.Errorf("cannot determine GOPATH: %v", err)
	}
	gopath := strings.TrimSpace(string(out))
	if gopath == "" {
		return "", fmt.Errorf("GOPATH is empty; pass --dir")
	}
	// GOPATH may be a list; the first entry is the effective one.
	if i := strings.IndexRune(gopath, filepath.ListSeparator); i >= 0 {
		gopath = gopath[:i]
	}
	return filepath.Join(gopath, "bin"), nil
}

// onPath reports whether dir appears in $PATH.
func onPath(dir string) bool {
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == "" {
			continue
		}
		if abs, err := filepath.Abs(entry); err == nil && abs == dir {
			return true
		}
	}
	return false
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
