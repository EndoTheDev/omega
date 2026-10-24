package http_channel

// Variant surface (t_17365247): GET /sessions/{id}/variants serves the
// branch-point metadata; POST/PATCH /sessions/{id}/variants flips the
// active variant server-side (persisted via a variant_select
// bookkeeping entry in the store - the active path survives reload
// with no schema change and no payload rewrite). The webui card's fork
// button rides the same endpoint: forking at a historical message =
// selecting the message's variant point; the next send continues from
// the selected branch.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// variantSelectRequest is the select body: message_id is EITHER a
// branch point (a parent message carrying sibling runs - select, when
// present, is that sibling's 0-based index) OR any conversation
// message (then the selection resolves to the message's own run; a
// present select index is ignored - the message identifies the run).
type variantSelectRequest struct {
	MessageID string `json:"message_id"`
	Select    *int   `json:"select,omitempty"`
}

// handleSessionVariants serves the session's variant block and the
// select action. Same gates as the other session endpoints: 501
// without a store, 404 for an unknown session, 400 on malformed or
// unknown targets.
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
	// Selecting the already-active variant writes nothing - the entry
	// would only re-record what derivation already resolves to.
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
	// The select changed the panel's active path. The canvas snapshot is
	// push-driven (Snapshot serves the last pushed panel), and a select
	// is not a run - no delta would ever carry the swap. Re-push the
	// focused panel so the webui's post-select snapshot refetch repaints
	// the NEW active path (journey 30: the flip stuck on the old row).
	if c := s.chat; c != nil && c.srv != nil {
		if focusID, running := c.focus(); focusID == id {
			c.push(c.buildPanel(id, running))
		}
	}
	s.writeVariants(w, http.StatusOK, updated)
}

// writeVariants serves the variants block. Sessions without any >=2
// branch point carry an EMPTY list (not absent) on the select surface -
// the select endpoint is only wired for sessions with a store, so the
// client can rely on the key existing there.
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
		// No select index on a branch point: keep the current pin.
		return body.MessageID, view.ActiveHeads[body.MessageID], nil
	}
	head, ok := view.HeadByMessage[body.MessageID]
	if !ok {
		return "", "", fmt.Errorf("unknown message %q (not part of this session's tree)", body.MessageID)
	}
	return view.BranchByHead[head], head, nil
}
