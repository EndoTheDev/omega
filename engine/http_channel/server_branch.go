package http_channel

import (
	"fmt"
	"net/http"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// Session-fork + export surface (t_6bf696e4): the webui parity batch for
// the TUI's /branch, /label and /export commands.

// handleSessionBranch serves POST /sessions/{id}/branch (/branch parity):
// creates a child session of id through the SAME machinery the TUI's
// command drives (store.CreateSession with the parent link - the store
// rejects a missing parent), then appends ONE summary system row into
// the child so the lineage marker lives in the store (the TUI paints the
// equivalent "[branched from ...]" affordance client-side; the webui row
// badge reads parent_id). Read-time semantics ride the store's merged
// view: the child's active path is the whole ancestor chain, so the
// transcript restore and the context assembly both inherit the parent's
// history as a prefix. 501 without a store, 404 on an unknown parent,
// 201 with the child session (the shape POST /sessions already returns).
func (s *Server) handleSessionBranch(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "session store not configured", http.StatusNotImplemented)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	parent, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		http.Error(w, "session not found: "+err.Error(), http.StatusNotFound)
		return
	}
	// New child id - the same fallback the POST /sessions flow uses
	// (crypto/rand never fails on supported platforms; a timestamp keeps
	// the branch alive if it ever does).
	childID, err := agent.NewSessionID()
	if err != nil {
		childID = fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	if err := s.store.CreateSession(r.Context(), childID, id, ""); err != nil {
		http.Error(w, "branch session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Summary system row: excluded from counts and the message tree, so
	// only the transcript-side lineage marker and this record exist.
	summary := "branched from " + id
	if parent.Label != "" {
		summary = "branched from " + parent.Label + " (" + id + ")"
	}
	if err := s.store.AppendMessage(r.Context(), childID, core.NewSystem(summary)); err != nil {
		http.Error(w, "summary message: "+err.Error(), http.StatusInternalServerError)
		return
	}
	sess, err := s.store.GetSession(r.Context(), childID)
	if err != nil {
		http.Error(w, "get session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

// exportLine is one JSONL line of GET /sessions/{id}/export - the exact
// shape the TUI's /export command writes{"role": "<role>",
// "content": "<text>"} per message.
type exportLine struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// handleSessionExport serves GET /sessions/{id}/export (/export parity):
// the session's ACTIVE PATH as JSONL - one {"role","content"} line per
// conversation message, the same shape HandleExportCommand writes to
// disk for the TUI. For a branched session the active path is the whole
// lineage (store-side ancestors), so the exported file matches what both
// the webui and the TUI render. Served as an attachment (Content-
// Disposition download named <session id>.jsonl); 501 without a store,
// 404 for an unknown session, 500 on store faults.
func (s *Server) handleSessionExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "session store not configured", http.StatusNotImplemented)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	sess, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		http.Error(w, "session not found: "+err.Error(), http.StatusNotFound)
		return
	}
	messages, err := s.store.GetActiveMessages(r.Context(), id)
	if err != nil {
		http.Error(w, "get messages: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/jsonl")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.jsonl"`, sess.ID))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		// headers only - the download probe (the webui anchor, curl -I)
		// reads Content-Disposition without the body; net/http would
		// discard a written body for HEAD anyway
		return
	}
	for _, msg := range messages {
		line := exportLine{Role: core.MessageRole(msg), Content: agent.MessageText(msg)}
		if _, err := fmt.Fprintf(w, "{\"role\":%q,\"content\":%q}\n", line.Role, line.Content); err != nil {
			return
		}
	}
}
