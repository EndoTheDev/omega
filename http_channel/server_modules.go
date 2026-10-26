package http_channel

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// The module-toggle surface (v0.12 E2). The settings pane renders the
// registry introspection and applies live toggles through it - the
// settings capsule RENDERS the data, the registry (behind the host's
// core.ModuleToggles seam) provides it. A host without the seam (nil
// modules) answers 501 so the pane can hide the list.

// engineModules never appear in the list (the tier law: engine ring,
// always on - the frontend does not render its own on/off switch, nor
// the TUI's).
var engineModules = map[string]bool{
	"http_channel": true,
	"tui":          true,
}

// handleModules serves GET: the module list (introspection, engine
// ring filtered out) plus the refusal ledger tail (Q12 - rejections
// visible as events). no-store: the state changes under the pane.
func (s *Server) handleModules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.modules == nil {
		http.Error(w, "module toggles not configured", http.StatusNotImplemented)
		return
	}
	all := s.modules.Modules()
	mods := make([]core.ModuleStatus, 0, len(all))
	for _, m := range all {
		if engineModules[m.Name] {
			continue // tier law: the engine never toggles
		}
		mods = append(mods, m)
	}
	payload := &modulesPayload{Modules: mods, Refusals: refusalsTail(s.modules)}
	writeSettingsJSON(w, payload)
}

// modulesPayload is the GET /modules wire shape.
type modulesPayload struct {
	Modules  []core.ModuleStatus `json:"modules"`
	Refusals []refusalRow        `json:"refusals,omitempty"`
}

// refusalRow is one ledger entry on the wire - the error as text (the
// error interface marshals empty and that is a Q12 lie on the wire).
type refusalRow struct {
	Module    string `json:"module"`
	Predicate string `json:"predicate"`
	Err       string `json:"err"`
	At        string `json:"at"`
}

// refusalsTail renders the last 20 ledger entries, newest last.
func refusalsTail(sink core.ModuleToggles) []refusalRow {
	entries := sink.Refusals()
	if len(entries) == 0 {
		return nil
	}
	if len(entries) > 20 {
		entries = entries[len(entries)-20:]
	}
	out := make([]refusalRow, 0, len(entries))
	for _, e := range entries {
		msg := ""
		if e.Err != nil {
			msg = e.Err.Error()
		}
		out = append(out, refusalRow{Module: e.Module, Predicate: e.Predicate, Err: msg, At: e.At.Format(time.RFC3339)})
	}
	return out
}

// handleModuleToggle applies PUT /modules/{name}: {"enabled": bool}
// through the seam's Toggle (the Q7 one code path). A rejection comes
// back 400 with the ledger text (Q11: the roll-back text the pane
// surfaces); success returns the refreshed introspection.
func (s *Server) handleModuleToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.modules == nil {
		http.Error(w, "module toggles not configured", http.StatusNotImplemented)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing module name", http.StatusBadRequest)
		return
	}
	if engineModules[name] {
		http.Error(w, "module "+name+" is engine ring - always on, never toggled", http.StatusBadRequest)
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Enabled == nil {
		http.Error(w, "invalid request: enabled is required", http.StatusBadRequest)
		return
	}
	if err := s.modules.Toggle(r.Context(), name, *body.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	all := s.modules.Modules()
	mods := make([]core.ModuleStatus, 0, len(all))
	for _, m := range all {
		if engineModules[m.Name] {
			continue
		}
		mods = append(mods, m)
	}
	writeSettingsJSON(w, &modulesPayload{Modules: mods, Refusals: refusalsTail(s.modules)})
}
