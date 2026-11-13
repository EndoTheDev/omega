package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// Config-knob surface for the webui settings pane. Read/write of the
// host's config goes through the Settings seam supplied by the host:
// this file knows nothing about config.yaml. A host without the seam
// (nil settings) answers 501 so the pane can fall back to its
// client-side settings only.

// handleSettings dispatches GET (read the schema + values, or one knob
// by ?key=) and PUT (apply a values map, re-serve the refreshed schema).
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
		// One-knob read (card t_013b4643): ?key=<dotted path> answers
		// the knob object exactly as it rides inside its group, so a
		// script client reads a single value without the group walk.
		// An unknown key is a 404 naming it - answering the full
		// payload would make a typo look like a successful read.
		if key := r.URL.Query().Get("key"); key != "" {
			knob, ok := payload.Knob(key)
			if !ok {
				http.Error(w, "unknown setting: "+key, http.StatusNotFound)
				return
			}
			writeSettingsJSON(w, knob)
			return
		}
		writeSettingsJSON(w, payload)
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

// handleSettingsRaw is the whole-document door beside the knob door
// (card t_a82d39c8): GET answers the config file as YAML text for the
// pane's export and edit-as-YAML surfaces, PUT validates and persists
// a submitted document through the host's own write path. It carries
// no schema: the payload IS the file, so the pane can show what a text
// editor would.
func (s *Server) handleSettingsRaw(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		http.Error(w, "settings not configured", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodGet:
		text, err := s.settings.Raw()
		if err != nil {
			http.Error(w, "load config: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(text))
	case http.MethodPut:
		body, err := io.ReadAll(io.LimitReader(r.Body, maxRawConfigBytes))
		if err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(bytes.TrimSpace(body)) == 0 {
			http.Error(w, "invalid request: body is empty", http.StatusBadRequest)
			return
		}
		payload, err := s.settings.SaveRaw(string(body))
		if err != nil {
			// a document that will not parse or validate is the
			// caller's text, not a server fault: 400 with the
			// host's own sentence, exactly like the knob save
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeSettingsJSON(w, payload)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// maxRawConfigBytes caps the raw-config PUT body. A config.yaml is a
// few KB (the shipped example is under 8 KB); the cap exists because
// the route is unauthenticated loopback and read into memory.
const maxRawConfigBytes = 1 << 20

// handleSettingsReset restores the config defaults through the
// host's Settings seam (card t_a82d39c8). POST because it is a
// non-idempotent-looking write with no body: the defaults belong to
// the host, so the client never assembles them.
func (s *Server) handleSettingsReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.settings == nil {
		http.Error(w, "settings not configured", http.StatusNotImplemented)
		return
	}
	payload, err := s.settings.Reset()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeSettingsJSON(w, payload)
}

// writeSettingsJSON emits the schema payload with no-store caching:
// a settings pane that reads a cached GET after a save shows stale
// values, and the values can change from outside (config.yaml edits).
func writeSettingsJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(payload)
}
