package wrkprojects

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xhd2015/wrk/wrkcli/storage"
	"github.com/xhd2015/wrk/wrkcli/wrkserver"
)

// newWrkHome writes a projects.json with the given registered main repos.
func newWrkHome(t *testing.T, paths ...string) string {
	t.Helper()
	home := t.TempDir()
	pf := storage.ProjectsFile{Version: 1}
	for _, p := range paths {
		pf.Projects = append(pf.Projects, storage.Project{
			Path:    p,
			AddedAt: "2026-01-01T00:00:00Z",
			Source:  storage.SourceManual,
		})
	}
	if err := storage.SaveProjects(home, pf); err != nil {
		t.Fatalf("save projects.json: %v", err)
	}
	return home
}

// serve mounts h on a bare mux (no fallback) and runs one request.
func serve(t *testing.T, h *Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	RegisterHandler(mux, "/api/wrk", nil, h)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

// statusTarget builds {StatusPath}?path=<escaped>.
func statusTarget(path string) string {
	return StatusPath + "?" + url.Values{"path": []string{path}}.Encode()
}

// installGitSpy puts a fake `git` first in PATH that records every invocation.
// A test that asserts the marker stays absent proves the code path ran no git.
func installGitSpy(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "git-calls.log")
	script := "#!/bin/sh\necho \"$@\" >> " + marker + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func decodeList(t *testing.T, rec *httptest.ResponseRecorder) ListResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode list: %v (body %s)", err, rec.Body.String())
	}
	return got
}

func TestListProjectsRunsNoGit(t *testing.T) {
	marker := installGitSpy(t)

	repo := t.TempDir()
	missing := filepath.Join(t.TempDir(), "gone")
	wrkHome := newWrkHome(t, repo, missing)
	h := &Handler{WrkHome: wrkHome}

	rec := serve(t, h, http.MethodGet, ProjectsPath)
	got := decodeList(t, rec)

	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		calls, _ := os.ReadFile(marker)
		t.Fatalf("list ran git (%d calls):\n%s", strings.Count(string(calls), "\n"), calls)
	}

	if len(got.Projects) != 2 {
		t.Fatalf("projects = %d, want 2: %+v", len(got.Projects), got.Projects)
	}
	byName := map[string]ProjectSummary{}
	for _, p := range got.Projects {
		byName[p.Name] = p
	}
	if p, ok := byName[filepath.Base(repo)]; !ok || p.Error != "" {
		t.Fatalf("existing project = %+v (present=%v), want no error", p, ok)
	}
	if p, ok := byName["gone"]; !ok || p.Error != "path does not exist" {
		t.Fatalf("missing project = %+v (present=%v), want path does not exist", p, ok)
	}

	// The list contract must stay git-free: no branch/clean/worktrees fields.
	body := rec.Body.String()
	for _, forbidden := range []string{`"branch"`, `"clean"`, `"worktrees"`, `"commit"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("list body carries %s: %s", forbidden, body)
		}
	}
}

func TestListProjectsRejectsNonGet(t *testing.T) {
	h := &Handler{WrkHome: newWrkHome(t)}
	rec := serve(t, h, http.MethodPost, ProjectsPath)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestListProjectsEmptyIsArray(t *testing.T) {
	h := &Handler{WrkHome: newWrkHome(t)}
	rec := serve(t, h, http.MethodGet, ProjectsPath)
	if body := strings.TrimSpace(rec.Body.String()); !strings.Contains(body, `"projects":[]`) {
		t.Fatalf("body = %s, want empty array (not null)", body)
	}
}

func TestStatusReportsGitState(t *testing.T) {
	main := t.TempDir()
	runGit(t, main, "init", "-b", "main")
	writeFile(t, filepath.Join(main, "README.md"), "hello\n")
	runGit(t, main, "add", "README.md")
	runGit(t, main, "commit", "-m", "init commit")

	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, main, "worktree", "add", "-b", "feature", wt)
	writeFile(t, filepath.Join(wt, "feature.txt"), "feature\n")

	wrkHome := newWrkHome(t, main)
	h := &Handler{WrkHome: wrkHome}

	rec := serve(t, h, http.MethodGet, statusTarget(main))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got wrkserver.ProjectStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if got.Branch != "main" {
		t.Errorf("branch = %q, want main", got.Branch)
	}
	if got.Commit == "" || got.Subject != "init commit" {
		t.Errorf("commit/subject = %q/%q, want non-empty/init commit", got.Commit, got.Subject)
	}
	if !got.Clean {
		t.Errorf("clean = false, want true (untracked files inside a linked worktree are ignored)")
	}
	if len(got.Worktrees) != 1 {
		t.Fatalf("worktrees = %+v, want 1", got.Worktrees)
	}
	if w := got.Worktrees[0]; w.Name != "wt" || w.Branch != "feature" || w.IsMain {
		t.Errorf("worktree = %+v, want name=wt branch=feature is_main=false", w)
	} else if w.Path != storage.NormalizePath(wt) {
		t.Errorf("worktree path = %q, want %q", w.Path, storage.NormalizePath(wt))
	}

	// Dirty the main checkout: a modified tracked file must flip clean to false.
	writeFile(t, filepath.Join(main, "README.md"), "hello\nchanged\n")
	rec = serve(t, h, http.MethodGet, statusTarget(main))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode dirty status: %v", err)
	}
	if got.Clean {
		t.Errorf("clean = true after modifying a tracked file, want false")
	}
}

func TestStatusAcceptsLinkedWorktreePath(t *testing.T) {
	main := t.TempDir()
	runGit(t, main, "init", "-b", "main")
	writeFile(t, filepath.Join(main, "README.md"), "hello\n")
	runGit(t, main, "add", "README.md")
	runGit(t, main, "commit", "-m", "init")

	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, main, "worktree", "add", "-b", "feature", wt)

	h := &Handler{WrkHome: newWrkHome(t, main)}
	rec := serve(t, h, http.MethodGet, statusTarget(wt))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got wrkserver.ProjectStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if got.Path != storage.NormalizePath(main) {
		t.Errorf("path = %q, want main checkout %q", got.Path, storage.NormalizePath(main))
	}
}

func TestStatusUnknownProjectIs404(t *testing.T) {
	h := &Handler{WrkHome: newWrkHome(t, t.TempDir())}
	rec := serve(t, h, http.MethodGet, statusTarget(t.TempDir()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", rec.Code, rec.Body.String())
	}
	var body wrkserver.ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error != "project not found" {
		t.Fatalf("error body = %+v, %v", body, err)
	}
}

func TestStatusRequiresPath(t *testing.T) {
	h := &Handler{WrkHome: newWrkHome(t)}
	rec := serve(t, h, http.MethodGet, StatusPath)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
}

func TestStatusReportsMissingRegisteredPath(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	h := &Handler{WrkHome: newWrkHome(t, gone)}
	rec := serve(t, h, http.MethodGet, statusTarget(gone))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got wrkserver.ProjectStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if got.Error != "path does not exist" {
		t.Fatalf("error = %q, want path does not exist", got.Error)
	}
}

// TestRegisterOverridesUpstreamProjects pins the ServeMux precedence the
// dispatcher relies on: our leaf patterns win, everything else falls through to
// the upstream wrkserver mux.
func TestRegisterOverridesUpstreamProjects(t *testing.T) {
	var upstreamProjectsCalls int
	fallback := http.NewServeMux()
	fallback.HandleFunc("/api/wrk/projects", func(w http.ResponseWriter, r *http.Request) {
		upstreamProjectsCalls++
		w.WriteHeader(http.StatusTeapot)
	})
	fallback.HandleFunc("/api/wrk/worktrees", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream", "1")
		w.WriteHeader(http.StatusCreated)
	})

	mux := http.NewServeMux()
	RegisterHandler(mux, "/api/wrk", fallback, &Handler{WrkHome: newWrkHome(t)})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/wrk/projects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("projects status = %d, want 200 from our handler", rec.Code)
	}
	if upstreamProjectsCalls != 0 {
		t.Fatalf("upstream /projects handler reached %d times, want 0", upstreamProjectsCalls)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/wrk/worktrees", strings.NewReader("{}")))
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Upstream") != "1" {
		t.Fatalf("worktrees = %d %q, want delegated 201", rec.Code, rec.Header().Get("X-Upstream"))
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/wrk/projects/status", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status route = %d, want 400 (path required)", rec.Code)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
