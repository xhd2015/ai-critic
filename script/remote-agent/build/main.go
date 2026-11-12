// Command build compiles the remote-agent CLI (./cmd/remote-agent).
//
// On macOS the resulting binary is code signed. That is about more than
// Gatekeeper: macOS attaches privacy grants (Local Network, Screen Recording,
// Accessibility, ...) to the code identity of a binary, and an ad-hoc/unsigned
// identity is derived from the binary hash. Rebuilding therefore silently
// revokes every grant the previous build had earned -- which is exactly why a
// third-party Go binary looks "blocked" for local network access while Apple's
// own /usr/bin/curl works. Signing with a stable --identifier (and a real
// identity when one is available) is what makes such a grant survive rebuilds.
//
// See script/macos-app/bundle.sh for the same reasoning applied to the app
// bundle.
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
	// defaultOutput matches the /tmp convention used by script/server/build.
	defaultOutput = "/tmp/remote-agent"
	// defaultIdentifier keeps the code identity stable across rebuilds so
	// macOS privacy grants are not keyed to the binary hash.
	defaultIdentifier = "com.xhd2015.remote-agent"
	// mainPackage is the remote-agent entrypoint, relative to the repo root.
	mainPackage = "./cmd/remote-agent"
)

var help = `
Usage: go run ./script/remote-agent/build [options]

Builds the remote-agent CLI from ./cmd/remote-agent.

On macOS the binary is code signed with a stable identifier so macOS privacy
grants attach to the identity instead of the binary hash (an unsigned/ad-hoc
signature would revoke them on every rebuild).

Options:
  -o, --output PATH     Output binary path (default: /tmp/remote-agent)
      --no-sign         Skip code signing (macOS only; signing is on by default)
      --identity NAME   Signing identity (default: $CODESIGN_IDENTITY, else the
                        first codesigning identity in the keychain, else ad-hoc "-")
      --identifier ID   Signature identifier (default: com.xhd2015.remote-agent)
  -h, --help            Show this help message

Examples:
  go run ./script/remote-agent/build
  go run ./script/remote-agent/build -o /tmp/ra
  CODESIGN_IDENTITY="Developer ID Application: ..." go run ./script/remote-agent/build
  go run ./script/remote-agent/build --no-sign
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

// Handle builds remote-agent and signs it on macOS.
func Handle(args []string) error {
	var output string
	var noSign bool
	var identity string
	var identifier string
	_, err := flags.
		String("-o,--output", &output).
		Bool("--no-sign", &noSign).
		String("--identity", &identity).
		String("--identifier", &identifier).
		Help("-h,--help", help).
		Parse(args)
	if err != nil {
		return err
	}

	if output == "" {
		output = defaultOutput
	}
	if identifier == "" {
		identifier = defaultIdentifier
	}
	if abs, err := filepath.Abs(output); err == nil {
		output = abs
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	fmt.Printf("==> Building %s -> %s\n", mainPackage, output)
	build := exec.Command("go", "build", "-o", output, mainPackage)
	build.Dir = root
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("go build %s failed: %v", mainPackage, err)
	}

	if runtime.GOOS != "darwin" {
		if !noSign {
			fmt.Printf("==> Skipping code signing: not macOS (GOOS=%s)\n", runtime.GOOS)
		}
		fmt.Printf("Built %s\n", output)
		return nil
	}
	if noSign {
		fmt.Println("==> Skipping code signing (--no-sign)")
		fmt.Printf("Built %s\n", output)
		return nil
	}

	resolved, err := resolveIdentity(identity)
	if err != nil {
		return err
	}
	fmt.Printf("==> Signing with identity %q (identifier %s)\n", resolved, identifier)
	if err := sign(output, resolved, identifier); err != nil {
		return err
	}
	if summary, err := signatureSummary(output); err == nil {
		for _, line := range summary {
			fmt.Println("    " + line)
		}
	}
	fmt.Printf("Built %s\n", output)
	return nil
}

// repoRoot resolves the repository root from this source file's path, which is
// stable under `go run` (the compiled binary lives in a temp dir, so os.Args[0]
// is not usable).
func repoRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot resolve script location")
	}
	// <root>/script/remote-agent/build/main.go -> <root>
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("cannot locate repo root above %s: %v", thisFile, err)
	}
	return root, nil
}

// resolveIdentity picks the signing identity: explicit flag, then
// $CODESIGN_IDENTITY, then the keychain's first codesigning identity, then
// ad-hoc.
func resolveIdentity(fromFlag string) (string, error) {
	if s := strings.TrimSpace(fromFlag); s != "" {
		return s, nil
	}
	if s := strings.TrimSpace(os.Getenv("CODESIGN_IDENTITY")); s != "" {
		return s, nil
	}
	if detected := detectIdentity(); detected != "" {
		return detected, nil
	}
	fmt.Println("    no codesigning identity found in the keychain; falling back to ad-hoc")
	return "-", nil
}

// detectIdentity returns the keychain's first codesigning identity name, or ""
// when there is none. Output looks like:
//
//  1. 5754F6... "Marcus"
func detectIdentity() string {
	out, err := exec.Command("security", "find-identity", "-v", "-p", "codesigning").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		first := strings.Index(line, "\"")
		if first < 0 {
			continue
		}
		last := strings.LastIndex(line, "\"")
		if last <= first {
			continue
		}
		return line[first+1 : last]
	}
	return ""
}

// sign applies a signature to path.
func sign(path, identity, identifier string) error {
	args := []string{
		"--force",
		"--sign", identity,
		"--identifier", identifier,
		// Match script/macos-app/bundle.sh: no network timestamp.
		"--timestamp=none",
		path,
	}
	out, err := exec.Command("codesign", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign failed: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// signatureSummary returns the Identifier/Authority lines of codesign -dv.
// codesign writes its details to stderr, so capture both streams.
func signatureSummary(path string) ([]string, error) {
	out, err := exec.Command("codesign", "-dv", "--verbose=2", path).CombinedOutput()
	if err != nil {
		return nil, err
	}
	var keep []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Identifier=") || strings.HasPrefix(line, "Authority=") {
			keep = append(keep, line)
		}
	}
	return keep, nil
}
