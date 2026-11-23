package api

import (
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// sessionContextInfo is one GET /sessions/{id}/context answer: the two numbers the
// model-switch dialog decides on - the live context estimate of the session's ACTIVE
// PATH wire, and the context window of the model a pick would switch TO. The estimate
// is agent.EstimateTokens over agent.DeriveWire, the same pair the compaction machinery
// budgets with, so the dialog can never quote a number the compactor disagrees with.
type sessionContextInfo struct {
	Tokens        int    `json:"tokens"`
	ContextWindow int    `json:"context_window"`
	Model         string `json:"model,omitempty"`
}

// contextWindowFor resolves one model slot to its provider's context window, probing once
// per slot per process (the same cache /models reads). A provider that cannot report a
// window answers 0 here; callers apply the DefaultContextWindow floor.
func (s *Server) contextWindowFor(slot string) int {
	slot = strings.TrimSpace(slot)
	if slot == "" || s.agent == nil {
		return 0
	}
	s.ctxMu.Lock()
	if window, cached := s.ctxWindow[slot]; cached {
		s.ctxMu.Unlock()
		return window
	}
	s.ctxMu.Unlock()
	prov := s.agent.Provider()
	window := 0
	if prov != nil {
		p, model := agent.ResolveModelSlot(prov, slot)
		if model != "" {
			p = p.WithModel(model)
		}
		if info, err := p.ModelInfo(); err == nil {
			window = info.ContextWindow
		}
	}
	s.ctxMu.Lock()
	s.ctxWindow[slot] = window
	s.ctxMu.Unlock()
	return window
}

// handleSessionContext serves GET /sessions/{id}/context?model=<slot>. The model query
// is the PICK's target slot; absent, the session's own pin (then the daemon's model)
// answers. A switch to a window smaller than tokens is the dialog's trigger - the
// arithmetic lives in the client (modelSwitch.ts) so the rule is pinned by unit tests.
func (s *Server) handleSessionContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	slot := strings.TrimSpace(r.URL.Query().Get("model"))
	var wire []core.Message
	if isEphemeralID(id) {
		wire = s.eph.history(id)
		if wire == nil {
			http.Error(w, "ephemeral session not found: expired or restarted", http.StatusNotFound)
			return
		}
	} else {
		if s.store == nil {
			http.Error(w, "session store not configured", http.StatusNotImplemented)
			return
		}
		row, err := s.store.GetSession(r.Context(), id)
		if err != nil {
			http.Error(w, "session not found: "+err.Error(), http.StatusNotFound)
			return
		}
		if slot == "" {
			slot = row.Model
		}
		view, err := s.store.GetActiveView(r.Context(), id)
		if err != nil {
			http.Error(w, "load history: "+err.Error(), http.StatusInternalServerError)
			return
		}
		blocks, err := s.contextBlocks(r.Context(), id)
		if err != nil {
			http.Error(w, "load context blocks: "+err.Error(), http.StatusInternalServerError)
			return
		}
		wire = agent.DeriveWire(view.Path, blocks, row)
	}
	if slot == "" && s.agent != nil {
		slot = s.agent.ModelName()
	}
	window := s.contextWindowFor(slot)
	if window <= 0 {
		window = agent.DefaultContextWindow
	}
	writeJSON(w, http.StatusOK, sessionContextInfo{
		Tokens:        agent.EstimateTokens(wire),
		ContextWindow: window,
		Model:         slot,
	})
}
