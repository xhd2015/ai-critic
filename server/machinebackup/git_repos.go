package machinebackup

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xhd2015/dot-pkgs/go-pkgs/git/reposnapshot"
	"github.com/xhd2015/dot-pkgs/go-pkgs/git/scan_repo"
)

const (
	metaGitReposName = "git-repo-worktrees.json"
	gitReposVersion  = "1.0"

	// gitReposScanCacheDir is the durable scan cache directory under HOME.
	gitReposScanCacheDir = "git-repo-scan"
	// gitReposCacheNamespace nests the scan cache under an ai-critic owned
	// directory. scan_repo's default cache root is $HOME/.cache/git-repo-scan
	// and its store is not keyed by the scanned root, so any other caller that
	// scans while HOME is this user (tests, CLI helpers) would otherwise write
	// its repos into the same index and the warm serve path - which accepts
	// entries solely by prefix - would silently report a truncated repo set.
	gitReposCacheNamespace = "ai-critic"
)

// gitReposScanCacheRoot returns the durable scan cache root for one HOME.
func gitReposScanCacheRoot(home string) string {
	return filepath.Join(home, ".cache", gitReposCacheNamespace, gitReposScanCacheDir)
}

// backupLogf emits one observable backup line. A keep-alive managed server
// appends its stderr to ai-critic-server.log, so the standard logger is the
// channel that makes scan phases visible there instead of silent.
func backupLogf(format string, args ...any) {
	log.Printf("[machinebackup] "+format, args...)
}

// backupLogWriter funnels scan_repo debug output (the "scan: ..." phase lines
// emitted when Debug is set) into the same log stream.
type backupLogWriter struct{}

func (backupLogWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			log.Print("[machinebackup] " + line)
		}
	}
	return len(p), nil
}

// ScanGitRepos discovers git repositories under HOME.
// When opts.SkipGitDirsScan is true, returns (nil, false, nil).
func ScanGitRepos(home string, opts GitScanOptions) (*GitRepoWorktreesSnapshot, bool, error) {
	if opts.SkipGitDirsScan {
		return nil, true, nil
	}

	// Reuse the durable scan cache so repeated backups skip re-walking HOME.
	// The cache is scoped per HOME: the store's universe key is the constant
	// "home", so one shared store would serve entries recorded for another HOME.
	cacheRoot := gitReposScanCacheRoot(home)
	// The warm serve path does not apply MaxDepth, so only an unbounded scan may
	// be served from the cache; a depth-limited scan keeps walking cold.
	useCache := opts.GitDirsScanMaxDepth <= 0

	scanOpts := scan_repo.Options{
		Roots:         []string{home},
		MaxDepth:      opts.GitDirsScanMaxDepth,
		ListWorktrees: true,
		// ListRemotes: required for origin_url in meta + dry-run ORIGIN column.
		ListRemotes: true,
		// Debug: emit cache mode (warm/cold + reason), serve/refresh counts and
		// per-root durations so a slow backup can be attributed from the log.
		Debug:  true,
		Stderr: backupLogWriter{},
	}
	if useCache {
		scanOpts.CacheRoot = cacheRoot
	} else {
		scanOpts.NoCache = true
	}

	start := time.Now()
	backupLogf("git scan: start home=%s max_depth=%d cache_root=%s cache=%v",
		home, opts.GitDirsScanMaxDepth, cacheRoot, useCache)

	ctx := context.Background()
	result, err := scan_repo.Scan(ctx, scanOpts)
	if err != nil {
		backupLogf("git scan: failed home=%s elapsed=%s err=%v",
			home, time.Since(start).Round(time.Millisecond), err)
		return nil, false, fmt.Errorf("scan git repos under HOME: %w", err)
	}
	scanElapsed := time.Since(start)
	backupLogf("git scan: discovered home=%s repos=%d root_errors=%d elapsed=%s",
		home, len(result.Repos), len(result.RootErrors), scanElapsed.Round(time.Millisecond))

	rel := func(abs string) string {
		return mustRelPathFromHome(home, abs)
	}
	buildStart := time.Now()
	snap := reposnapshot.Build(result, rel)
	buildElapsed := time.Since(buildStart)
	backupLogf("git snapshot: built home=%s nodes=%d worktrees=%d node_errors=%d scan=%s enrich=%s total=%s",
		home, len(snap.Nodes), countSnapshotWorktrees(snap.Nodes), countSnapshotErrors(snap.Nodes),
		scanElapsed.Round(time.Millisecond), buildElapsed.Round(time.Millisecond),
		time.Since(start).Round(time.Millisecond))

	return gitReposSnapshotFromBuild(snap, originURLByRelPath(result, rel)), false, nil
}

func countSnapshotWorktrees(nodes []reposnapshot.Node) int {
	total := 0
	for _, node := range nodes {
		total += len(node.Worktrees)
	}
	return total
}

func countSnapshotErrors(nodes []reposnapshot.Node) int {
	total := 0
	for _, node := range nodes {
		if node.Error != "" {
			total++
		}
	}
	return total
}

func emptyGitReposSnapshot() *GitRepoWorktreesSnapshot {
	return &GitRepoWorktreesSnapshot{
		Version:    gitReposVersion,
		CapturedAt: time.Now().UTC(),
		Repos:      []GitRepoEntry{},
	}
}

func gitReposSnapshotFromBuild(snap reposnapshot.Snapshot, originByPath map[string]string) *GitRepoWorktreesSnapshot {
	repos := make([]GitRepoEntry, 0, len(snap.Nodes))
	for _, node := range snap.Nodes {
		entry := GitRepoEntry{
			Path:      node.Path,
			Branch:    node.Checkout.Branch,
			CommitSHA: node.Checkout.CommitSHA,
			CommitMsg: node.Checkout.CommitMsg,
			Status:    node.Checkout.Status,
			OriginURL: originByPath[node.Path],
			Error:     node.Error,
		}
		for _, wt := range node.Worktrees {
			entry.Worktrees = append(entry.Worktrees, GitWorktreeEntry{
				Path:      wt.Path,
				Branch:    wt.Checkout.Branch,
				CommitSHA: wt.Checkout.CommitSHA,
				CommitMsg: wt.Checkout.CommitMsg,
				Status:    wt.Checkout.Status,
				OriginURL: originByPath[wt.Path],
				Error:     wt.Error,
			})
		}
		repos = append(repos, entry)
	}
	return &GitRepoWorktreesSnapshot{
		Version:    gitReposVersion,
		CapturedAt: time.Now().UTC(),
		Repos:      repos,
	}
}

func originURLByRelPath(result scan_repo.Result, rel func(abs string) string) map[string]string {
	out := make(map[string]string)
	for _, repo := range result.Repos {
		origin := originURLFromRemotes(repo.Remotes)
		if origin == "" {
			continue
		}
		out[rel(repo.Path)] = origin
		for _, wt := range repo.Worktrees {
			if wt.IsMain {
				continue
			}
			out[rel(wt.Path)] = origin
		}
	}
	return out
}

func originURLFromRemotes(remotes []scan_repo.Remote) string {
	for _, remote := range remotes {
		if remote.Name == "origin" && remote.URL != "" {
			return remote.URL
		}
	}
	return ""
}

func relPathFromHome(home, absPath string) (string, error) {
	rel, err := filepath.Rel(home, absPath)
	if err != nil {
		return "", err
	}
	rel = normalizeRelPath(rel)
	return foldRelPathFirstSegmentCase(home, rel), nil
}

// foldRelPathFirstSegmentCase lowercases the first path segment when it refers to
// the same path on a case-insensitive volume (e.g. Projects/demo vs projects/demo).
func foldRelPathFirstSegmentCase(home, rel string) string {
	if rel == "" || strings.HasPrefix(rel, ".") {
		return rel
	}
	parts := strings.Split(rel, "/")
	if len(parts) == 0 || parts[0] == "" {
		return rel
	}
	first := parts[0]
	if first == strings.ToLower(first) {
		return rel
	}
	lowerFirst := strings.ToLower(first[:1]) + first[1:]
	orig := filepath.Join(home, filepath.FromSlash(rel))
	alt := filepath.Join(home, filepath.FromSlash(lowerFirst))
	if len(parts) > 1 {
		alt = filepath.Join(alt, filepath.FromSlash(strings.Join(parts[1:], "/")))
	}
	if pathsSameFile(orig, alt) {
		parts[0] = lowerFirst
		return strings.Join(parts, "/")
	}
	return rel
}

func pathsSameFile(a, b string) bool {
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

func mustRelPathFromHome(home, absPath string) string {
	rel, err := relPathFromHome(home, absPath)
	if err != nil {
		return normalizeRelPath(absPath)
	}
	return rel
}

func formatGitReposSummaryLines(gitRepos *GitRepoWorktreesSnapshot, skipped bool) []string {
	if skipped {
		return []string{gitReposDryRunHeader + " (skipped)"}
	}
	if gitRepos == nil || len(gitRepos.Repos) == 0 {
		return []string{gitReposDryRunHeader + " (none)"}
	}

	repos := append([]GitRepoEntry(nil), gitRepos.Repos...)
	sort.Slice(repos, func(i, j int) bool { return repos[i].Path < repos[j].Path })

	worktreeCount := 0
	for _, repo := range repos {
		worktreeCount += len(repo.Worktrees)
	}

	lines := []string{
		gitReposDryRunHeader,
		fmt.Sprintf("    captured_at: %s  (%d repo, %d worktree)",
			formatMetaCapturedAt(gitRepos.CapturedAt), len(repos), worktreeCount),
		gitReposTableColumnHeader,
	}
	for _, repo := range repos {
		lines = append(lines, formatGitReposCheckoutRow(
			"repo", repo.Path, repo.Branch, repo.CommitSHA, repo.Status, repo.OriginURL, repo.CommitMsg, repo.Error,
		))
		for _, wt := range repo.Worktrees {
			lines = append(lines, formatGitReposCheckoutRow(
				"worktree", wt.Path, wt.Branch, wt.CommitSHA, wt.Status, wt.OriginURL, wt.CommitMsg, wt.Error,
			))
		}
	}
	return appendMetaSectionTerminator(lines)
}

func formatGitReposCheckoutRow(kind, path, branch, sha, status, originURL, commitMsg, errMsg string) string {
	const rowIndent = "    "
	if isGitCheckoutErrorOnly(branch, sha, status, errMsg) {
		return fmt.Sprintf("%s%-10s%-24s%-14s%-10s%s",
			rowIndent, kind, path, "", "", "error: "+errMsg)
	}
	origin := "(none)"
	if originURL != "" {
		origin = originURL
	}
	return fmt.Sprintf("%s%-10s%-24s%-14s%-10s%-20s%-40s%s",
		rowIndent, kind, path, branch, shortGitCommitSHA(sha), status, origin, commitMsg)
}

func isGitCheckoutErrorOnly(branch, sha, status, errMsg string) bool {
	return errMsg != "" && branch == "" && sha == "" && status == ""
}

func shortGitCommitSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}

func marshalGitReposSnapshot(snap *GitRepoWorktreesSnapshot) ([]byte, error) {
	if snap == nil {
		return nil, nil
	}
	return json.MarshalIndent(snap, "", "  ")
}
