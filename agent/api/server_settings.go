package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/EndoTheDev/omega/core"
)

// Config-knob surface for the webui settings pane. A host without the seam (nil
// settings) answers 501 so the pane can fall back to its client-side settings only.

// handleSettings dispatches GET (read the schema + values, or one knob by ?key=).
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
		// One-knob read: ?key=<dotted path> answers the knob object exactly as it rides.
		if key := r.URL.Query().Get("key"); key != "" {
			knob, ok := payload.Knob(key)
			if !ok {
				http.Error(w, "unknown setting: "+key, http.StatusNotFound)
				return
			}
			writeSettingsJSON(w, knob)
			return
		}
		// The SCOPES slice: the settings pane's scope refresh needs the per-agent scopes.
		if r.URL.Query().Get("scopes") == "1" {
			writeSettingsJSON(w, &core.SettingsSchema{
				Groups: []core.SettingsGroup{},
				Scopes: payload.Scopes,
			})
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
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeSettingsJSON(w, payload)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSettingsRaw is the whole-document door beside the knob door.
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
		// The cap REJECTS, it never truncates: a YAML document cut at a top-level key
		// boundary still parses, so a truncated body can pass Validate and be persisted as a
		// silently PARTIAL config (a JSON body cannot - truncation always breaks the parse,
		// which is why the house truncating reads are safe). Read one byte past the cap to
		// tell "exactly at the cap" from "over it".
		body, err := io.ReadAll(io.LimitReader(r.Body, maxRawConfigBytes+1))
		if err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(body) > maxRawConfigBytes {
			http.Error(w, fmt.Sprintf("config document exceeds %d bytes", maxRawConfigBytes), http.StatusRequestEntityTooLarge)
			return
		}
		if len(bytes.TrimSpace(body)) == 0 {
			http.Error(w, "invalid request: body is empty", http.StatusBadRequest)
			return
		}
		payload, err := s.settings.SaveRaw(string(body))
		if err != nil {
			// a document that will not parse or validate is the caller's text, not a server
			// fault: 400 with the host's own sentence, exactly like the knob save
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeSettingsJSON(w, payload)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// maxRawConfigBytes caps the raw-config PUT body.
const maxRawConfigBytes = 1 << 20

// handleSettingsReset restores the config defaults through the host's Settings seam.
// POST because it is a non-idempotent-looking write with no body: the defaults belong
// to the host, so the client never assembles them.
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

// writeSettingsJSON emits the schema payload with no-store caching.
func writeSettingsJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(payload)
}
