package api

// Variant surface: GET /sessions/{id}/variants serves the branch-point metadata.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// variantSelectRequest is the select body.
type variantSelectRequest struct {
	MessageID string `json:"message_id"`
	Select    *int   `json:"select,omitempty"`
}

// handleSessionVariants serves the session's variant block and the select action.
func (s *Server) handleSessionVariants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodPatch {
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
	view, err := s.store.GetActiveView(r.Context(), id)
	if err != nil {
		http.Error(w, "session not found: "+err.Error(), http.StatusNotFound)
		return
	}
	if r.Method == http.MethodGet {
		s.writeVariants(w, http.StatusOK, view)
		return
	}
	var body variantSelectRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.MessageID) == "" {
		http.Error(w, "missing message_id", http.StatusBadRequest)
		return
	}
	branch, child, err := variantTarget(view, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if view.ActiveHeads[branch] == child {
		s.writeVariants(w, http.StatusOK, view)
		return
	}
	if err := s.store.AppendMessage(r.Context(), id, core.NewVariantSelect(branch, child)); err != nil {
		http.Error(w, "persist selection: "+err.Error(), http.StatusInternalServerError)
		return
	}
	updated, err := s.store.GetActiveView(r.Context(), id)
	if err != nil {
		http.Error(w, "get session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if c := s.chat; c != nil && c.srv.Load() != nil {
		if focusID, running := c.focus(); focusID == id {
			c.push(c.buildPanel(id, running))
		}
	}
	s.writeVariants(w, http.StatusOK, updated)
}

// writeVariants serves the variants block.
func (s *Server) writeVariants(w http.ResponseWriter, status int, view core.ActiveView) {
	writeJSON(w, status, map[string]any{"variants": view.Branches})
}

// variantTarget resolves the select body against the view. Branch-point
// ids (message ids carrying sibling runs) select the given index;
// anything else must name a conversation message whose run is selected.
func variantTarget(view core.ActiveView, body variantSelectRequest) (branch, child string, err error) {
	if heads, ok := view.SiblingHeads[body.MessageID]; ok {
		idx := -1
		if body.Select != nil {
			idx = *body.Select
			if idx < 0 || idx >= len(heads) {
				return "", "", fmt.Errorf("select index %d out of range (0..%d)", idx, len(heads)-1)
			}
			return body.MessageID, heads[idx], nil
		}
		return body.MessageID, view.ActiveHeads[body.MessageID], nil
	}
	head, ok := view.HeadByMessage[body.MessageID]
	if !ok {
		return "", "", fmt.Errorf("unknown message %q (not part of this session's tree)", body.MessageID)
	}
	return view.BranchByHead[head], head, nil
}
