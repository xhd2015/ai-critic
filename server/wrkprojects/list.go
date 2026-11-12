package wrkprojects

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/xhd2015/wrk/wrkcli"
	"github.com/xhd2015/wrk/wrkcli/storage"
)

// ProjectSummary is one registry entry. It deliberately carries no git-derived
// fields (branch, commit, subject, clean, worktrees): clients ask StatusPath for
// a single project when they actually need that state, so opening a menu never
// costs one git process per registered project.
type ProjectSummary struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
}

// ListResponse is the GET ProjectsPath body.
type ListResponse struct {
	// Always an array (never null).
	Projects []ProjectSummary `json:"projects"`
}

// handleList serves GET {base}/projects. It reads projects.json and stats each
// entry; it never runs git.
func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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

	projects := make([]ProjectSummary, 0, len(paths))
	for _, path := range paths {
		item := ProjectSummary{Path: path, Name: filepath.Base(path)}
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				item.Error = "path does not exist"
			} else {
				item.Error = err.Error()
			}
		}
		projects = append(projects, item)
	}
	writeJSON(w, http.StatusOK, ListResponse{Projects: projects})
}

func (h *Handler) wrkHome() string {
	if h == nil {
		return ""
	}
	return h.WrkHome
}
