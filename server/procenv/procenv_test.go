package procenv

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xhd2015/dot-pkgs/go-pkgs/shell/lookpath"
)

// fakeToolBin creates a directory holding executable stubs for every managed
// CLI so enrichWithLookupPaths never needs a login-shell probe.
func fakeToolBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range DefaultManagedCLIs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("write stub %s: %v", name, err)
		}
	}
	return dir
}

func TestNormalizeExtraPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "blank", in: "   ", want: ""},
		{name: "single", in: "/opt/nvm/bin", want: "/opt/nvm/bin"},
		{name: "several", in: "/opt/a:/opt/b", want: "/opt/a:/opt/b"},
		{name: "duplicates", in: "/opt/a:/opt/a:/opt/b", want: "/opt/a:/opt/b"},
		{name: "cleans trailing slash", in: "/opt/a/:/opt/b//", want: "/opt/a:/opt/b"},
		{name: "trims spaces", in: " /opt/a : /opt/b ", want: "/opt/a:/opt/b"},
		{name: "expands tilde", in: "~/bin", want: filepath.Join(home, "bin")},
		{name: "expands tilde with rest", in: "~/.local/bin:/opt/b", want: filepath.Join(home, ".local/bin") + ":/opt/b"},
		{name: "keeps bare tilde", in: "~", want: home},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeExtraPath(tc.in)
			if err != nil {
				t.Fatalf("NormalizeExtraPath(%q) error = %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeExtraPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeExtraPathRejects(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantSub string
	}{
		{name: "empty entry", in: "/opt/a::/opt/b", wantSub: "empty entry"},
		{name: "leading separator", in: ":/opt/a", wantSub: "empty entry"},
		{name: "variable", in: "$HOME/bin", wantSub: "does not expand variables"},
		{name: "command substitution", in: "`pwd`/bin", wantSub: "does not expand variables"},
		{name: "multi line", in: "/opt/a\n/opt/b", wantSub: "single line"},
		{name: "root", in: "/", wantSub: "invalid extra path directory"},
		{name: "relative", in: "opt/bin", wantSub: "must be absolute"},
		{name: "dot entries", in: ".:/opt/a", wantSub: "must be absolute"},
		{name: "relative after cleaning", in: "/opt/a:./b", wantSub: "must be absolute"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeExtraPath(tc.in)
			if err == nil {
				t.Fatalf("NormalizeExtraPath(%q) = nil error, want error", tc.in)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("NormalizeExtraPath(%q) error = %v, want containing %q", tc.in, err, tc.wantSub)
			}
		})
	}
}

// testBaseEnv pins PATH to a stub bin holding the managed CLIs so nothing in
// this test file shells out.
func testBaseEnv(t *testing.T) string {
	t.Helper()
	bin := fakeToolBin(t)
	t.Setenv("PATH", bin)
	t.Setenv("PROCENV_TEST_MARKER", "process")
	return bin
}

func envValue(env []string, key string) string { return lookupEnv(env, key) }

func TestBuildManagedEnvPrependsExtraPath(t *testing.T) {
	bin := testBaseEnv(t)
	extra := filepath.Join(t.TempDir(), "nvm", "bin")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	env, err := BuildManagedEnvWithOptions(Options{ExtraPath: extra})
	if err != nil {
		t.Fatalf("BuildManagedEnvWithOptions() error = %v", err)
	}
	path := envValue(env, "PATH")
	if !strings.HasPrefix(path, extra+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q, want prefix %q", path, extra)
	}
	if !strings.Contains(path, bin) {
		t.Fatalf("PATH = %q, want original dir %q preserved", path, bin)
	}
	if got := envValue(env, "PROCENV_TEST_MARKER"); got != "process" {
		t.Fatalf("PROCENV_TEST_MARKER = %q, want process env preserved", got)
	}
}

func TestPrependPathDirsDedupesAgainstExistingPath(t *testing.T) {
	env := []string{"PATH=/opt/a:/opt/b", "HOME=/home/me"}

	got := prependPathDirs(env, []string{"/opt/b", "/opt/c", "/opt/c"})
	path := envValue(got, "PATH")
	want := "/opt/c" + string(os.PathListSeparator) + "/opt/a" + string(os.PathListSeparator) + "/opt/b"
	if path != want {
		t.Fatalf("PATH = %q, want %q", path, want)
	}
	if got := envValue(got, "HOME"); got != "/home/me" {
		t.Fatalf("HOME = %q, want untouched", got)
	}
}

func TestPrependPathDirsAddsPathWhenMissing(t *testing.T) {
	got := prependPathDirs([]string{"HOME=/home/me"}, []string{"/opt/c"})
	if path := envValue(got, "PATH"); path != "/opt/c" {
		t.Fatalf("PATH = %q, want /opt/c", path)
	}
}

func TestBuildManagedEnvExtraPathOverridesExtraEnvPath(t *testing.T) {
	bin := testBaseEnv(t)
	base := filepath.Join(t.TempDir(), "base", "bin")
	extra := filepath.Join(t.TempDir(), "extra", "bin")
	for _, dir := range []string{base, extra} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	env, err := BuildManagedEnvWithOptions(Options{
		Extra:     map[string]string{"PATH": base + string(os.PathListSeparator) + bin},
		ExtraPath: extra,
	})
	if err != nil {
		t.Fatalf("BuildManagedEnvWithOptions() error = %v", err)
	}
	path := envValue(env, "PATH")
	want := extra + string(os.PathListSeparator) + base + string(os.PathListSeparator) + bin
	if path != want {
		t.Fatalf("PATH = %q, want %q", path, want)
	}
}

func TestBuildManagedEnvIgnoresInvalidExtraPath(t *testing.T) {
	bin := testBaseEnv(t)

	env, err := BuildManagedEnvWithOptions(Options{ExtraPath: "/opt/a::/opt/b"})
	if err != nil {
		t.Fatalf("BuildManagedEnvWithOptions() error = %v", err)
	}
	path := envValue(env, "PATH")
	if strings.Contains(path, "::") {
		t.Fatalf("PATH = %q, want invalid layer skipped", path)
	}
	if !strings.Contains(path, bin) {
		t.Fatalf("PATH = %q, want %q preserved", path, bin)
	}
}

// shellStub returns a RunLogin that pretends an interactive shell exported the
// given KEY=VALUE pairs, and fails when fail is non-nil.
func shellStub(pairs []string, fail error) lookpath.LoginEnvOptions {
	return lookpath.LoginEnvOptions{
		ShellBin: "bash",
		RunLogin: func(shell, command string, env []string) (string, error) {
			if fail != nil {
				return "", fail
			}
			return strings.Join(pairs, "\x00") + "\x00", nil
		},
	}
}

func TestBuildManagedEnvInheritsShellEnv(t *testing.T) {
	bin := testBaseEnv(t)
	t.Setenv("FROM_RC", "process")

	env, err := BuildManagedEnvWithOptions(Options{
		InheritShellEnv: true,
		LoginEnv:        &[]lookpath.LoginEnvOptions{shellStub([]string{"FROM_RC=shell", "ONLY_IN_SHELL=1"}, nil)}[0],
	})
	if err != nil {
		t.Fatalf("BuildManagedEnvWithOptions() error = %v", err)
	}
	if got := envValue(env, "FROM_RC"); got != "shell" {
		t.Fatalf("FROM_RC = %q, want shell env to win", got)
	}
	if got := envValue(env, "ONLY_IN_SHELL"); got != "1" {
		t.Fatalf("ONLY_IN_SHELL = %q, want inherited", got)
	}
	if got := envValue(env, "PROCENV_TEST_MARKER"); got != "process" {
		t.Fatalf("PROCENV_TEST_MARKER = %q, want process env kept", got)
	}
	if !strings.Contains(envValue(env, "PATH"), bin) {
		t.Fatalf("PATH = %q, want %q", envValue(env, "PATH"), bin)
	}
}

func TestBuildManagedEnvExtraEnvWinsOverShellEnv(t *testing.T) {
	testBaseEnv(t)

	env, err := BuildManagedEnvWithOptions(Options{
		InheritShellEnv: true,
		Extra:           map[string]string{"FROM_RC": "explicit"},
		LoginEnv:        &[]lookpath.LoginEnvOptions{shellStub([]string{"FROM_RC=shell"}, nil)}[0],
	})
	if err != nil {
		t.Fatalf("BuildManagedEnvWithOptions() error = %v", err)
	}
	if got := envValue(env, "FROM_RC"); got != "explicit" {
		t.Fatalf("FROM_RC = %q, want ExtraEnv to win", got)
	}
}

func TestBuildManagedEnvShellEnvFailureIsReported(t *testing.T) {
	testBaseEnv(t)
	probeErr := errors.New("lookpath: empty login envs from bash and zsh")

	env, err := BuildManagedEnvWithOptions(Options{
		InheritShellEnv: true,
		ExtraPath:       "/opt/extra/bin",
		LoginEnv:        &[]lookpath.LoginEnvOptions{shellStub(nil, probeErr)}[0],
	})
	if err == nil || !strings.Contains(err.Error(), probeErr.Error()) {
		t.Fatalf("error = %v, want %v", err, probeErr)
	}
	if !strings.HasPrefix(envValue(env, "PATH"), "/opt/extra/bin"+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q, want extra path still applied after probe failure", envValue(env, "PATH"))
	}
}

func TestBuildManagedEnvNotAskedDoesNotProbe(t *testing.T) {
	testBaseEnv(t)
	called := false
	env, err := BuildManagedEnvWithOptions(Options{
		LoginEnv: &lookpath.LoginEnvOptions{
			RunLogin: func(shell, command string, env []string) (string, error) {
				called = true
				return "", nil
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildManagedEnvWithOptions() error = %v", err)
	}
	if called {
		t.Fatal("login shell was probed without InheritShellEnv")
	}
	if envValue(env, "PATH") == "" {
		t.Fatal("PATH is empty")
	}
}

func TestBuildManagedEnvCompatibilityWrapperIgnoresErrors(t *testing.T) {
	bin := testBaseEnv(t)
	env := BuildManagedEnv(map[string]string{"EXTRA": "1"})
	if got := envValue(env, "EXTRA"); got != "1" {
		t.Fatalf("EXTRA = %q, want 1", got)
	}
	if !strings.Contains(envValue(env, "PATH"), bin) {
		t.Fatalf("PATH = %q, want %q", envValue(env, "PATH"), bin)
	}
}
