package http_channel

import (
	"encoding/json"
	"net/http"

	"github.com/EndoTheDev/omega/core"
)

// Config-knob surface for the webui settings pane. Read/write of the
// host's config goes through the Settings seam supplied by the host:
// this file knows nothing about config.yaml. A host without the seam
// (nil settings) answers 501 so the pane can fall back to its
// client-side settings only.

// handleSettings dispatches GET (read the schema + values) and PUT
// (apply a values map, re-serve the refreshed schema).
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if s.settings == nil {
			http.Error(w, "settings not configured", http.StatusNotImplemented)
			return
		}
		payload, err := s.settings.Schema()
		if err != nil {
			http.Error(w, "load settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeSettingsJSON(w, filterKnobSurface(payload))
	case http.MethodPut:
		if s.settings == nil {
			http.Error(w, "settings not configured", http.StatusNotImplemented)
			return
		}
		var body struct {
			Values map[string]any `json:"values"`
		}
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&body); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(body.Values) == 0 {
			http.Error(w, "invalid request: values is empty", http.StatusBadRequest)
			return
		}
		payload, err := s.settings.Save(body.Values)
		if err != nil {
			// both user-input validation failures and write failures
			// surface as a bad request with the host's message; the
			// pane shows it inline either way
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeSettingsJSON(w, payload)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// writeSettingsJSON emits the schema payload with no-store caching:
// a settings pane that reads a cached GET after a save shows stale
// values, and the values can change from outside (config.yaml edits).
func writeSettingsJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(payload)
}

// filterKnobSurface drops knobs this serving surface cannot use. The
// webui is the http channel's surface: knobs carrying surface "tui" are
// config for an app this pane isn't, and an exposed setting must be a
// usable one (Endo, 2026-10-18). Groups left empty by the filter drop
// out too. The seam is typed (core.SettingsSchema), so this walks the
// structs directly - knobs copy out of the range, the input is never
// mutated. Wire shape is unchanged: surface carries omitempty, so
// clearing it keeps the JSON exactly what the old map round-trip served.
func filterKnobSurface(payload *core.SettingsSchema) *core.SettingsSchema {
	if payload == nil {
		return nil
	}
	kept := make([]core.SettingsGroup, 0, len(payload.Groups))
	for _, g := range payload.Groups {
		knobs := make([]core.SettingsKnob, 0, len(g.Knobs))
		for _, k := range g.Knobs {
			if k.Surface == "tui" {
				continue
			}
			k.Surface = "" // the wire stays clean for the pane
			knobs = append(knobs, k)
		}
		if len(knobs) == 0 {
			continue
		}
		g.Knobs = knobs
		kept = append(kept, g)
	}
	return &core.SettingsSchema{Groups: kept}
}