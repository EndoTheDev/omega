package api

// The connection switcher's backend: GET/PUT /instances, a SERVE's own list. The list's
// home is config.yaml (`desktop.instances`); the host's Settings implementation owns the
// write (one atomic writer, one validation pass), this file owns the wire shape. A desktop
// window never calls this door: its own list is answered by the app (desktop_windows.go),
// because the page it is showing may have been served by an older build.

import (
	"encoding/json"
	"net/http"

	"github.com/EndoTheDev/omega/core"
)

// instanceLister is the channel's own face on the host's saved-instance store. It rides
// the settings seam's implementation instead of widening core.Settings: a host without
// it answers 501, the same nil-safe floor the settings face keeps.
type instanceLister interface {
	Instances() ([]core.SavedInstance, error)
	SaveInstances([]core.SavedInstance) error
}

// handleInstances reads the list (GET) or replaces it whole (PUT).
func (s *Server) handleInstances(w http.ResponseWriter, r *http.Request) {
	lister, ok := s.settings.(instanceLister)
	if !ok {
		http.Error(w, "instances not configured", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := lister.Instances()
		if err != nil {
			http.Error(w, "load instances: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeInstancesJSON(w, list)
	case http.MethodPut:
		var body struct {
			Instances []core.SavedInstance `json:"instances"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		// The validator is core's: a hand edit of config.yaml passes the same one.
		list, err := core.NormalizeInstances(body.Instances)
		if err != nil {
			http.Error(w, "instances: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := lister.SaveInstances(list); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeInstancesJSON(w, list)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// writeInstancesJSON answers the {"instances":[...]} envelope - never a bare array - and
// an empty list, never null.
func writeInstancesJSON(w http.ResponseWriter, list []core.SavedInstance) {
	if list == nil {
		list = []core.SavedInstance{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Instances []core.SavedInstance `json:"instances"`
	}{Instances: list})
}
