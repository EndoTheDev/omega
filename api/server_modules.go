package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// The module-toggle surface (v0.12 E2). A host without the seam (nil modules) answers
// 501 so the pane can hide the list.

// shellModules never appear in the list.
var shellModules = map[string]bool{
	"http_channel": true,
}

// alwaysOnModules is the always-on tier. Those three are what the process cannot serve
// without, so the toggle endpoint refuses them AT THE SEAM - the refusal is the guard,
// and the UI's non-toggleable always-on row only mirrors it. (The frontend reads the
// trio off the introspection's toggleable flag; it never carries a second copy of this
// list.)
var alwaysOnModules = map[string]bool{
	"agent-loop":    true,
	"provider":      true,
	"observability": true,
}

// handleModules serves GET.
func (s *Server) handleModules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.ModuleToggles() == nil {
		http.Error(w, "module toggles not configured", http.StatusNotImplemented)
		return
	}
	all := s.ModuleToggles().Modules()
	mods := make([]core.ModuleStatus, 0, len(all))
	for _, m := range all {
		if shellModules[m.Name] {
			continue // tier law: the shell never toggles
		}
		mods = append(mods, m)
	}
	payload := &modulesPayload{Modules: mods, Refusals: refusalsTail(s.ModuleToggles())}
	writeSettingsJSON(w, payload)
}

// modulesPayload is the GET /modules wire shape.
type modulesPayload struct {
	Modules  []core.ModuleStatus `json:"modules"`
	Refusals []refusalRow        `json:"refusals,omitempty"`
}

// refusalRow is one ledger entry on the wire.
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

// handleModuleToggle applies PUT /modules/{name}.
func (s *Server) handleModuleToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.ModuleToggles() == nil {
		http.Error(w, "module toggles not configured", http.StatusNotImplemented)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing module name", http.StatusBadRequest)
		return
	}
	if shellModules[name] {
		http.Error(w, "module "+name+" is shell - always on, never toggled", http.StatusBadRequest)
		return
	}
	// the load-bearing pair is locked here, not in the UI.
	if alwaysOnModules[name] {
		http.Error(w, "module "+name+" is always on", http.StatusBadRequest)
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
	if err := s.ModuleToggles().Toggle(r.Context(), name, *body.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	all := s.ModuleToggles().Modules()
	mods := make([]core.ModuleStatus, 0, len(all))
	for _, m := range all {
		if shellModules[m.Name] {
			continue
		}
		mods = append(mods, m)
	}
	writeSettingsJSON(w, &modulesPayload{Modules: mods, Refusals: refusalsTail(s.ModuleToggles())})
}
