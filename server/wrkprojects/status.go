package wrkprojects

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/xhd2015/dot-pkgs/go-pkgs/git/cmd"
	"github.com/xhd2015/dot-pkgs/go-pkgs/git/worktree"
	"github.com/xhd2015/wrk/wrkcli"
	"github.com/xhd2015/wrk/wrkcli/storage"
	"github.com/xhd2015/wrk/wrkcli/wrkserver"
)

// handleStatus serves GET {base}/projects/status?path=<project path>.
//
// The response is exactly one wrkserver.ProjectStatus, so existing clients keep
// decoding the same shape; the cost (branch, commit, status --porcelain for the
// main checkout and per linked worktree) is paid only for the project the user
// actually looks at.
func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	wrkHome, err := wrkcli.ResolveWrkHome(h.wrkHome())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	paths, err := storage.ListProjects(wrkHome)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	target, ok := matchProject(paths, raw)
	if !ok {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	status := BuildStatus(r.Context(), target)
	writeJSON(w, http.StatusOK, status)
}

// matchProject resolves raw (possibly a symlinked or non-normalized spelling of
// a registered project, or a linked worktree inside one) to a registered path.
func matchProject(paths []string, raw string) (string, bool) {
	want := storage.NormalizePath(raw)
	for _, p := range paths {
		if p == want {
			return p, true
		}
	}
	// A linked worktree path resolves to its registered main checkout.
	if inside := storage.NormalizePath(raw); inside != "" {
		if main, err := worktree.ResolveMainRepo(inside); err == nil {
			main = storage.NormalizePath(main)
			for _, p := range paths {
				if p == main {
					return p, true
				}
			}
		}
	}
	return "", false
}

// BuildStatus mirrors wrkcli/wrkserver's per-project gathering, with context
// cancellation so a closed HTTP request stops the git work.
func BuildStatus(ctx context.Context, mainPath string) wrkserver.ProjectStatus {
	mainPath = storage.NormalizePath(mainPath)
	ps := wrkserver.ProjectStatus{
		Path:      mainPath,
		Name:      filepath.Base(mainPath),
		Worktrees: []wrkserver.WorktreeStatus{},
	}

	if _, err := os.Stat(mainPath); err != nil {
		if os.IsNotExist(err) {
			ps.Error = "path does not exist"
		} else {
			ps.Error = err.Error()
		}
		return ps
	}
	if !worktree.IsInsideWorkTree(mainPath) {
		ps.Error = fmt.Sprintf("%s is not a git repository", mainPath)
		return ps
	}
	// Prefer the resolved main repo (registry may point at a checkout).
	if top, err := worktree.ShowToplevel(mainPath); err == nil {
		if main, err := worktree.ResolveMainRepo(top); err == nil {
			mainPath = storage.NormalizePath(main)
			ps.Path = mainPath
			ps.Name = filepath.Base(mainPath)
		}
	}

	branch, err := worktree.ReadBranchCtx(ctx, mainPath)
	if err != nil {
		ps.Error = err.Error()
		return ps
	}
	ps.Branch = branch

	short, subject, err := commitShortSubject(ctx, mainPath)
	if err != nil {
		ps.Error = err.Error()
		return ps
	}
	ps.Commit = short
	ps.Subject = subject

	linked, err := worktree.ListLinked(mainPath)
	if err != nil {
		ps.Error = err.Error()
		return ps
	}

	clean, err := mainIsClean(ctx, mainPath, linked)
	if err != nil {
		ps.Error = err.Error()
		return ps
	}
	ps.Clean = clean

	for _, entry := range linked {
		wt := wrkserver.WorktreeStatus{
			Path:   storage.NormalizePath(entry.Path),
			Name:   filepath.Base(entry.Path),
			Branch: entry.Branch,
		}
		if worktree.IsDead(entry.Path) {
			wt.Error = "path does not exist"
			ps.Worktrees = append(ps.Worktrees, wt)
			continue
		}
		if err := ctx.Err(); err != nil {
			wt.Error = err.Error()
			ps.Worktrees = append(ps.Worktrees, wt)
			continue
		}
		isClean, err := worktree.IsCleanWrk(entry.Path)
		if err != nil {
			wt.Error = err.Error()
		} else {
			wt.Clean = isClean
		}
		ps.Worktrees = append(ps.Worktrees, wt)
	}
	return ps
}

func commitShortSubject(ctx context.Context, repoPath string) (short, subject string, err error) {
	out, err := cmd.Run(ctx, repoPath, "log", "-1", "--pretty=format:%h %s")
	if err != nil {
		return "", "", err
	}
	out = strings.TrimSpace(out)
	if i := strings.IndexByte(out, ' '); i >= 0 {
		return out[:i], out[i+1:], nil
	}
	return out, "", nil
}

// mainIsClean ignores untracked entries that are nested linked worktrees, which
// is what wrk's own listing does.
func mainIsClean(ctx context.Context, mainPath string, linked []worktree.Entry) (bool, error) {
	out, err := cmd.Run(ctx, mainPath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	skip := skipUntrackedRelPaths(mainPath, linked)
	if len(skip) == 0 {
		return worktree.IsCleanWrk(mainPath)
	}
	var filtered strings.Builder
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "??") {
			path := strings.TrimSuffix(strings.TrimSpace(line[2:]), "/")
			if _, ok := skip[path]; ok {
				continue
			}
		}
		if filtered.Len() > 0 {
			filtered.WriteByte('\n')
		}
		filtered.WriteString(line)
	}
	return strings.TrimSpace(filtered.String()) == "", nil
}

func skipUntrackedRelPaths(mainRepo string, linked []worktree.Entry) map[string]struct{} {
	skip := make(map[string]struct{}, len(linked))
	for _, entry := range linked {
		if worktree.IsDead(entry.Path) {
			continue
		}
		rel, err := filepath.Rel(mainRepo, entry.Path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "..") {
			continue
		}
		skip[rel] = struct{}{}
	}
	return skip
}
