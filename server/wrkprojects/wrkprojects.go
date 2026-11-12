// Package wrkprojects serves the host-owned /api/wrk project endpoints used by
// the macOS menubar.
//
// The upstream wrk package (wrkcli/wrkserver) gathers full git state for every
// registered project on every GET {base}/projects: with ~90 registered projects
// that is a few hundred git forks per request (branch, commit subject, status
// --porcelain for the main checkout plus one per linked worktree). Polling that
// endpoint every 30s from the menubar keeps a steady stream of git children
// alive. This package splits the contract instead:
//
//	GET {base}/projects         → registry listing only, zero git forks
//	GET {base}/projects/status  → full git state for one project (?path=…)
//
// Everything else under {base}/ (worktree create, ops, …) is delegated to the
// upstream mux unchanged.
package wrkprojects

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/xhd2015/wrk/wrkcli/wrkserver"
)

const (
	// ProjectsPath lists registered projects (cheap: no git calls).
	ProjectsPath = "/api/wrk/projects"
	// StatusPath returns the git state of one registered project.
	StatusPath = "/api/wrk/projects/status"
)

// Options configures a Handler.
type Options struct {
	// WrkHome is the wrk storage root (projects.json). Empty resolves like the
	// CLI (WRK_HOME or ~/.wrk).
	WrkHome string
}

// Handler serves the cheap project list and the on-demand project status.
type Handler struct {
	WrkHome string
}

// New constructs a Handler from Options.
func New(opts Options) *Handler {
	return &Handler{WrkHome: strings.TrimSpace(opts.WrkHome)}
}

// Register mounts the cheap list and the on-demand status endpoint on mux, and
// delegates every other path under base to fallback (the upstream wrkserver
// mux). Go 1.22+ ServeMux prefers the exact leaf patterns over the base
// subtree, so the upstream {base}/projects handler is never reached.
func Register(mux *http.ServeMux, base string, fallback http.Handler) {
	RegisterHandler(mux, base, fallback, &Handler{})
}

// RegisterHandler is Register with an injected Handler (tests).
func RegisterHandler(mux *http.ServeMux, base string, fallback http.Handler, h *Handler) {
	if h == nil {
		h = &Handler{}
	}
	base = strings.TrimRight(base, "/")
	mux.HandleFunc(base+"/projects", h.handleList)
	mux.HandleFunc(base+"/projects/status", h.handleStatus)
	if fallback != nil {
		mux.Handle(base+"/", fallback)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, wrkserver.ErrorBody{Error: msg})
}
