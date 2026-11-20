// usage: go run ./script/github/release [--dry-run] [--skip-prebuild] [--include-windows]
//
// Release ai-critic-server binaries to GitHub Releases.
// Pattern matches github.com/xhd2015/browser-agent/script/github/release.
//
// Live requirements:
//   - clean git worktree
//   - HEAD is a v* tag (git describe --tags HEAD)
//   - .upload-credentials.json {token, owner, repo}
//     A linked worktree uses the main checkout's file when this checkout has none.
//   - node/npm for the frontend bundle (embedded via //go:embed), unless
//     --skip-prebuild is used with an already-fresh ai-critic-react/dist.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xhd2015/ai-critic/script/lib"
	githublib "github.com/xhd2015/kool/pkgs/github"
	"github.com/xhd2015/kool/pkgs/release"
	"github.com/xhd2015/less-gen/flags"
	"github.com/xhd2015/xgo/support/cmd"
)

const help = `
Usage: go run ./script/github/release [options]

Release ai-critic fat multi-platform binaries to GitHub Releases.

Asset names (binaries):
  ai-critic-server-{tag}-{os}-{arch}
  e.g. ai-critic-server-v0.0.75-linux-amd64

  Install with:
    curl -fsSL https://raw.githubusercontent.com/xhd2015/ai-critic/master/install.sh | bash

Targets:
  darwin/amd64  darwin/arm64  linux/amd64  linux/arm64
  windows/amd64 windows/arm64   only with --include-windows; the server does not
  cross-compile for Windows yet (unix-only syscalls in run/daemon, server/*, and
  dot-pkgs/go-pkgs), so the build fails fast with the offending packages.

Options:
  --dry-run          print plan without building or uploading
  --skip-prebuild    skip npm run build; use the existing ai-critic-react/dist
  --include-windows  also build/upload windows/amd64 and windows/arm64
  -h, --help         show this help

Credentials (live only):
  .upload-credentials.json
    {"token":"ghp_...","owner":"xhd2015","repo":"ai-critic"}
  A linked worktree uses the main checkout's file when this checkout has none.
`

// spec is one release target.
type spec struct {
	OS   string
	Arch string
}

// defaultSpecs are the targets every release ships.
var defaultSpecs = []spec{
	{OS: "darwin", Arch: "amd64"},
	{OS: "darwin", Arch: "arm64"},
	{OS: "linux", Arch: "amd64"},
	{OS: "linux", Arch: "arm64"},
}

// windowsSpecs are opt-in: the server does not build for GOOS=windows yet.
var windowsSpecs = []spec{
	{OS: "windows", Arch: "amd64"},
	{OS: "windows", Arch: "arm64"},
}

func main() {
	if err := handle(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func handle(args []string) error {
	var dryRun bool
	var skipPrebuild bool
	var includeWindows bool
	rest, err := flags.
		Bool("--dry-run", &dryRun).
		Bool("--skip-prebuild", &skipPrebuild).
		Bool("--include-windows", &includeWindows).
		Help("-h,--help", help).
		Parse(args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return fmt.Errorf("unrecognized extra args: %s", strings.Join(rest, " "))
	}

	root, err := moduleRoot()
	if err != nil {
		return err
	}

	specs := append([]spec(nil), defaultSpecs...)
	if includeWindows {
		specs = append(specs, windowsSpecs...)
	}

	tag, tagErr := release.GetTag()
	tagSource := ""
	if tagErr != nil {
		if !dryRun {
			return tagErr
		}
		fmt.Fprintf(os.Stderr, "[dry-run] warning: %v\n", tagErr)
		next, prev, nextErr := suggestNextTag(root)
		if nextErr == nil && next != "" {
			tag = next
			tagSource = fmt.Sprintf(" (suggested: next after %s)", prev)
		} else {
			tag = "(unknown)"
		}
	}

	creds, err := loadUploadCredentials()
	if err != nil {
		if !dryRun {
			return err
		}
		fmt.Fprintf(os.Stderr, "[dry-run] warning: %v\n", err)
		creds = &release.Credentials{Owner: "xhd2015", Repo: "ai-critic"}
	}

	if dryRun {
		fmt.Printf("[dry-run] tag: %s%s\n", tag, tagSource)
		if skipPrebuild {
			fmt.Println("[dry-run] would skip npm run build (--skip-prebuild); reuse ai-critic-react/dist")
		} else {
			fmt.Println("[dry-run] would prebuild: npm run build (ai-critic-react/dist, embedded with //go:embed)")
		}
		for _, s := range specs {
			fmt.Printf("[dry-run] would build: %s\n", assetName(tag, s))
		}
		if !includeWindows {
			fmt.Println("[dry-run] skipped: windows/amd64 windows/arm64 (pass --include-windows; the server does not build for Windows yet)")
		}
		fmt.Printf("[dry-run] would upload to %s/%s release (creates if 404)\n", creds.Owner, creds.Repo)
		return nil
	}

	if !skipPrebuild {
		if err := prebuild(root); err != nil {
			return err
		}
	}

	files, err := buildAll(root, tag, specs)
	if err != nil {
		return err
	}

	client := githublib.NewReleaseClient(creds.Token, creds.Owner, creds.Repo)
	rel, err := getOrCreateRelease(client, tag, releaseNotes(root, tag))
	if err != nil {
		return fmt.Errorf("failed to get or create release for tag %s: %v", tag, err)
	}

	for _, file := range files {
		if err := client.UploadReleaseAsset(rel.ID, file); err != nil {
			return fmt.Errorf("failed to upload %s: %v", file, err)
		}
		fmt.Printf("Uploaded %s\n", filepath.Base(file))
	}

	fmt.Printf("Release %s: https://github.com/%s/%s/releases/tag/%s\n",
		tag, creds.Owner, creds.Repo, tag)
	return nil
}

// assetName is the released artifact basename; install.sh uses the same formula.
func assetName(tag string, s spec) string {
	return fmt.Sprintf("%s-%s-%s-%s", lib.BinaryName, tag, s.OS, s.Arch)
}

// plannedAssetNames lists every artifact for tag + specs.
func plannedAssetNames(tag string, specs []spec) []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, assetName(tag, s))
	}
	return out
}

// moduleRoot walks up from cwd to the ai-critic module root.
func moduleRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(data), "module github.com/xhd2015/ai-critic") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("ai-critic module root not found from %s", wd)
		}
		dir = parent
	}
}

// prebuild rebuilds the embedded frontend so every binary ships the same dist.
func prebuild(root string) error {
	reactDir := filepath.Join(root, "ai-critic-react")
	if _, err := os.Stat(filepath.Join(reactDir, "node_modules")); err != nil {
		fmt.Println("==> prebuild: node_modules missing, npm install")
		if err := cmd.Debug().Dir(reactDir).Run("npm", "install"); err != nil {
			return fmt.Errorf("npm install: %w", err)
		}
	}
	fmt.Println("==> prebuild: npm run build (ai-critic-react/dist)")
	if err := cmd.Debug().Dir(reactDir).Run("npm", "run", "build"); err != nil {
		return fmt.Errorf("frontend build: %w\n  hint: need node + npm in ai-critic-react/; or re-run with --skip-prebuild if dist is already fresh", err)
	}
	return nil
}

// buildAll cross-compiles every spec into root and returns the asset paths.
func buildAll(root, tag string, specs []spec) ([]string, error) {
	files := make([]string, 0, len(specs))
	for _, s := range specs {
		name := assetName(tag, s)
		out := filepath.Join(root, name)
		fmt.Printf("==> build: %s\n", name)
		if err := lib.BuildServer(lib.BuildServerOptions{
			Output: out,
			GOOS:   s.OS,
			GOARCH: s.Arch,
		}); err != nil {
			if s.OS == "windows" {
				return nil, fmt.Errorf("build %s/%s: %w\n  hint: the server is unix-only today (run/daemon, server/*, dot-pkgs/go-pkgs). Drop --include-windows to ship darwin+linux", s.OS, s.Arch, err)
			}
			return nil, fmt.Errorf("build %s/%s: %w", s.OS, s.Arch, err)
		}
		st, err := os.Stat(out)
		if err != nil || st.Size() == 0 {
			return nil, fmt.Errorf("built binary missing or empty: %s", out)
		}
		files = append(files, out)
	}
	return files, nil
}

// getOrCreateRelease finds the release by tag or creates it with a body.
func getOrCreateRelease(client *githublib.ReleaseClient, tag, body string) (*githublib.Release, error) {
	rel, err := client.GetReleaseByTag(tag)
	if err == nil {
		return rel, nil
	}
	if err != githublib.ErrReleaseNotFound {
		return nil, err
	}
	fmt.Printf("Release for tag %s not found, creating...\n", tag)
	return client.CreateRelease(githublib.CreateReleaseInput{
		TagName: tag,
		Body:    body,
	})
}

// releaseNotes renders the commit list since the previous tag.
func releaseNotes(root, tag string) string {
	prev, err := cmd.Dir(root).Output("git", "describe", "--tags", "--abbrev=0", tag+"^")
	if err != nil {
		return ""
	}
	prev = strings.TrimSpace(prev)
	if prev == "" {
		return ""
	}
	log, err := cmd.Dir(root).Output("git", "log", "--no-merges", "--pretty=format:- %s", prev+".."+tag)
	if err != nil {
		return ""
	}
	notes := strings.TrimSpace(log)
	if notes == "" {
		return ""
	}
	return "## Changes since " + prev + "\n\n" + notes + "\n"
}

// suggestNextTag increments the patch of the newest reachable tag and also
// returns that tag.
func suggestNextTag(root string) (next, prev string, err error) {
	out, err := cmd.Dir(root).Output("git", "describe", "--tags", "--abbrev=0")
	if err != nil {
		return "", "", err
	}
	prev = strings.TrimSpace(out)
	next, err = nextTagFrom(prev)
	return next, prev, err
}

// nextTagFrom bumps the patch component: v0.0.74 -> v0.0.75.
func nextTagFrom(tag string) (string, error) {
	if !strings.HasPrefix(tag, "v") {
		return "", fmt.Errorf("tag %q must start with 'v'", tag)
	}
	trimmed := strings.TrimPrefix(tag, "v")
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("tag %q is not vMAJOR.MINOR.PATCH", tag)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", fmt.Errorf("tag %q has a non-numeric patch: %w", tag, err)
	}
	return fmt.Sprintf("v%s.%s.%d", parts[0], parts[1], patch+1), nil
}

const uploadCredentialsFile = ".upload-credentials.json"

// loadUploadCredentials reads .upload-credentials.json from the current
// directory. A linked worktree with no file reads the main checkout's copy.
// A file that exists but cannot be used is returned as that file's error.
func loadUploadCredentials() (*release.Credentials, error) {
	if _, err := os.Stat(uploadCredentialsFile); os.IsNotExist(err) {
		if mainRoot, linked := linkedMainCheckout(); linked {
			fallback := filepath.Join(mainRoot, uploadCredentialsFile)
			if _, ferr := os.Stat(fallback); ferr == nil {
				return release.LoadCredentials(fallback)
			}
		}
	}
	return release.LoadCredentials(uploadCredentialsFile)
}

// linkedMainCheckout reports the main checkout when cwd is a linked worktree.
// git worktree list prints that checkout first.
func linkedMainCheckout() (string, bool) {
	topOut, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", false
	}
	listOut, err := exec.Command("git", "worktree", "list", "--porcelain").Output()
	if err != nil {
		return "", false
	}
	var main string
	for _, line := range strings.Split(string(listOut), "\n") {
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			main = strings.TrimSpace(rest)
			break
		}
	}
	if main == "" {
		return "", false
	}
	top := strings.TrimSpace(string(topOut))
	if samePath(top, main) {
		return main, false
	}
	return main, true
}

func samePath(a, b string) bool {
	return filepath.Clean(evalSymlink(a)) == filepath.Clean(evalSymlink(b))
}

func evalSymlink(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}
