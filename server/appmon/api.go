package appmon

import (
	"encoding/json"
	"net/http"
)

// RegisterAPI mounts the installed-app inventory endpoints. Icons are served
// separately so the inventory JSON stays small enough to poll cheaply.
func RegisterAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/server/apps", handleApps)
	mux.HandleFunc("/api/server/apps/icon", handleIcon)
}

func handleApps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Query().Get("refresh") == "1" {
		if _, err := Refresh(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	inv, err := GetView()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(inv)
}

func handleIcon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, ok := defaultManager.icon(r.URL.Query().Get("path"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	serveIcon(w, r, data)
}
