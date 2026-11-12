package machinebackup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatGitReposSummaryLines(t *testing.T) {
	snap := &GitRepoWorktreesSnapshot{
		CapturedAt: mustParseRFC3339(t, "2026-07-07T12:00:00Z"),
		Repos: []GitRepoEntry{{
			Path: "main", Branch: "main", CommitSHA: "abc1234", CommitMsg: "fix", Status: "clean",
			Worktrees: []GitWorktreeEntry{{
				Path: "wt", Branch: "feature", CommitSHA: "def5678", CommitMsg: "wip", Status: "dirty (1 modified)",
			}},
		}},
	}
	lines := formatGitReposSummaryLines(snap, false)
	text := strings.Join(lines, "\n")
	for _, want := range []string{
		"GIT REPOS(.backup/git-repo-worktrees.json):",
		"captured_at: 2026-07-07T12:00:00Z  (1 repo, 1 worktree)",
		"KIND",
		"PATH",
		"BRANCH",
		"SHA",
		"STATUS",
		"ORIGIN",
		"MESSAGE",
		"repo      main",
		"main          abc1234   clean",
		"fix",
		"worktree  wt",
		"feature       def5678   dirty (1 modified)",
		"wip",
		"(none)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}

	if got := strings.Join(formatGitReposSummaryLines(nil, true), "\n"); got != "  GIT REPOS(.backup/git-repo-worktrees.json): (skipped)" {
		t.Fatalf("skipped summary = %q", got)
	}
	if got := strings.Join(formatGitReposSummaryLines(emptyGitReposSnapshot(), false), "\n"); got != "  GIT REPOS(.backup/git-repo-worktrees.json): (none)" {
		t.Fatalf("none summary = %q", got)
	}
}

func TestFormatGitReposSummaryLinesErrorOnlyRow(t *testing.T) {
	snap := &GitRepoWorktreesSnapshot{
		CapturedAt: mustParseRFC3339(t, "2026-07-07T12:00:00Z"),
		Repos: []GitRepoEntry{{
			Path: ".wrk-test/empty", Error: "no commits (HEAD unborn)",
		}},
	}
	text := strings.Join(formatGitReposSummaryLines(snap, false), "\n")
	if !strings.Contains(text, "repo      .wrk-test/empty") {
		t.Fatalf("missing error row path:\n%s", text)
	}
	if !strings.Contains(text, "error: no commits (HEAD unborn)") {
		t.Fatalf("missing error status:\n%s", text)
	}
}

func TestBuildPlanGitReposIntegration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	home := t.TempDir()
	mainDir := filepath.Join(home, ".wrk-test", "main")
	if err := os.MkdirAll(mainDir, 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, mainDir, "init")
	gitRun(t, mainDir, "config", "user.email", "test@example.com")
	gitRun(t, mainDir, "config", "user.name", "Test User")
	gitRun(t, mainDir, "branch", "-M", "main")
	if err := os.WriteFile(filepath.Join(mainDir, "README.md"), []byte("fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, mainDir, "add", "README.md")
	gitRun(t, mainDir, "commit", "-m", "backup git fixture")
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("export FAKE=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := BuildPlan(home, nil, nil, GitScanOptions{})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.GitRepos == nil || len(plan.GitRepos.Repos) == 0 {
		t.Fatalf("GitRepos empty: %+v", plan.GitRepos)
	}
	if plan.GitRepos.Repos[0].Path != ".wrk-test/main" {
		t.Fatalf("repo path = %q", plan.GitRepos.Repos[0].Path)
	}
	lines := formatBackupDryRunSummary(plan, DryRunSummaryOptions{})
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "GIT REPOS(.backup/git-repo-worktrees.json):") || !strings.Contains(text, ".wrk-test/main") {
		t.Fatalf("summary missing git repos:\n%s", text)
	}
}

func TestScanGitReposReusesHomeScopedCache(t *testing.T) {
	home, want := newGitScanFixture(t)

	first, skipped, err := ScanGitRepos(home, GitScanOptions{})
	if err != nil {
		t.Fatalf("first ScanGitRepos: %v", err)
	}
	if skipped {
		t.Fatal("first scan reported skipped")
	}
	assertSingleRepo(t, first, want)

	indexPath := filepath.Join(gitReposScanCacheRoot(home), "home", "repos.json")
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("expected durable scan cache at %s: %v", indexPath, err)
	}

	// Second backup reuses the warm index; it must still describe the same repo.
	second, _, err := ScanGitRepos(home, GitScanOptions{})
	if err != nil {
		t.Fatalf("second ScanGitRepos: %v", err)
	}
	assertSingleRepo(t, second, want)
	if second.CapturedAt.Before(first.CapturedAt) {
		t.Fatalf("captured_at went backwards: %s < %s", second.CapturedAt, first.CapturedAt)
	}
}

func TestScanGitReposDepthLimitedBypassesCache(t *testing.T) {
	home, _ := newGitScanFixture(t)

	// A depth-limited scan cannot be served warm (the warm path ignores
	// MaxDepth), so it must stay cold and leave no cache behind.
	if _, _, err := ScanGitRepos(home, GitScanOptions{GitDirsScanMaxDepth: 1}); err != nil {
		t.Fatalf("ScanGitRepos: %v", err)
	}
	cachePath := gitReposScanCacheRoot(home)
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("depth-limited scan wrote scan cache at %s (err=%v)", cachePath, err)
	}
}

func newGitScanFixture(t *testing.T) (home, repoPath string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	home = t.TempDir()
	repoPath = filepath.Join(home, ".wrk-test", "main")
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoPath, "init")
	gitRun(t, repoPath, "config", "user.email", "test@example.com")
	gitRun(t, repoPath, "config", "user.name", "Test User")
	gitRun(t, repoPath, "branch", "-M", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoPath, "add", "README.md")
	gitRun(t, repoPath, "commit", "-m", "backup git fixture")
	return home, ".wrk-test/main"
}

func assertSingleRepo(t *testing.T, snap *GitRepoWorktreesSnapshot, wantPath string) {
	t.Helper()
	if snap == nil {
		t.Fatal("snapshot is nil")
	}
	if len(snap.Repos) != 1 {
		t.Fatalf("repos = %+v, want exactly 1", snap.Repos)
	}
	if snap.Repos[0].Path != wantPath {
		t.Fatalf("repo path = %q, want %q", snap.Repos[0].Path, wantPath)
	}
}

func mustParseRFC3339(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func TestRelPathFromHome(t *testing.T) {
	home := "/home/user"
	got, err := relPathFromHome(home, "/home/user/.wrk-test/main")
	if err != nil {
		t.Fatal(err)
	}
	if got != ".wrk-test/main" {
		t.Fatalf("rel = %q, want .wrk-test/main", got)
	}
}

func TestFoldRelPathFirstSegmentCase(t *testing.T) {
	home := t.TempDir()
	projectsDir := filepath.Join(home, "Projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatal(err)
	}
	demoDir := filepath.Join(home, "projects", "demo")
	if err := os.MkdirAll(demoDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(demoDir, "README.md"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := relPathFromHome(home, filepath.Join(home, "Projects", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "projects/demo" {
		t.Fatalf("rel = %q, want projects/demo", got)
	}
}