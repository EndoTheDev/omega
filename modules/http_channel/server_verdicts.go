package http_channel

import (
	"encoding/json"
	"net/http"

	"github.com/EndoTheDev/omega/agent"
)

// Verdict cards v1 (card t_2ecf9c51): the answer surface. The card
// itself renders live from the existing tool_call / tool_result SSE
// frames (request_verdict's args carry title/options/timeout; the result
// carries the verdict JSON) - these endpoints are the ANSWER path and
// the pending listing the bell polls. The verdicts seam is optional:
// a nil Server.Verdicts degrades to 501, mirroring the board and
// notifications seams.

// verdictCardWire is one pending card on the wire. ExpiresAt is a Unix
// millisecond epoch so the client countdowns against the server clock
// without a timezone dance.
type verdictCardWire struct {
	ID        string   `json:"id"`
	SessionID string   `json:"session_id"`
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Options   []string `json:"options,omitempty"`
	FreeText  bool     `json:"free_text,omitempty"`
	TimeoutS  int      `json:"timeout_s,omitempty"`
	ExpiresAt int64    `json:"expires_at"`
}

// verdictAnswerRequest is the POST /sessions/{id}/verdicts body. One of
// option_index, option (label), text, or skip. card_id is optional: when
// absent the session's oldest pending card is answered (one run per
// session, so the common case is exactly one).
type verdictAnswerRequest struct {
	CardID      string `json:"card_id,omitempty"`
	OptionIndex int    `json:"option_index,omitempty"`
	Option      string `json:"option,omitempty"`
	Text        string `json:"text,omitempty"`
	Skip        bool   `json:"skip,omitempty"`
}

func (s *Server) handleVerdicts(w http.ResponseWriter, r *http.Request) {
	// All verdict endpoints live under /verdicts (cross-session listing)
	// and /sessions/{id}/verdicts (per-session listing + answering).
	id := r.PathValue("id")
	if r.URL.Path == "/verdicts" && id != "" {
		// unreachable: /verdicts has no {id}; guard anyway
		http.NotFound(w, r)
		return
	}
	if s.Verdicts == nil {
		http.Error(w, "verdicts seam not mounted", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.serveVerdictsList(w, r, id)
	case http.MethodPost:
		if id == "" {
			http.Error(w, "answering requires a session id", http.StatusBadRequest)
			return
		}
		s.serveVerdictAnswer(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// serveVerdictsList answers GET /verdicts (all sessions) and GET
// /sessions/{id}/verdicts (one session): the pending cards, oldest
// first, that the bell counts and the webui re-renders after a reload.
func (s *Server) serveVerdictsList(w http.ResponseWriter, r *http.Request, sessionID string) {
	cards := s.Verdicts.Pending(sessionID)
	out := make([]verdictCardWire, 0, len(cards))
	for _, c := range cards {
		out = append(out, toVerdictWire(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": out, "count": len(out)})
}

// serveVerdictAnswer answers POST /sessions/{id}/verdicts: records the
// user's verdict on one pending card. First answer wins; a second
// attempt from either surface returns 409 with the reason (the
// both-frontend single-answer race). An unknown or expired card is 404
// when there is no pending card to disambiguate against, 409 when the
// card exists but is settled.
func (s *Server) serveVerdictAnswer(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req verdictAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	v := agent.Verdict{
		OptionIndex: req.OptionIndex,
		Option:      req.Option,
		Text:        req.Text,
		Skipped:     req.Skip,
	}
	if err := s.Verdicts.Answer(sessionID, req.CardID, v); err != nil {
		// settled cards (answered/skipped/expired) race to 409 with the
		// reason; unknown / no-pending-card is 404. The message names the
		// reason so the losing surface can show it instead of retrying.
		switch {
		case isAlreadySettled(err):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		case isUnknownCard(err):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	v.CardID = req.CardID
	writeJSON(w, http.StatusOK, map[string]any{
		"card_id": req.CardID,
		"verdict": v.Kind(),
		"option":  v.Option,
		"option_index": v.OptionIndex,
		"text":    v.Text,
		"skipped": v.Skipped,
	})
}

// toVerdictWire maps one runtime card to its wire shape.
func toVerdictWire(c agent.VerdictPrompt) verdictCardWire {
	exp := int64(0)
	if !c.ExpiresAt.IsZero() {
		exp = c.ExpiresAt.UnixMilli()
	}
	return verdictCardWire{
		ID:        c.ID,
		SessionID: c.SessionID,
		Title:     c.Title,
		Body:      c.Body,
		Options:   c.Options,
		FreeText:  c.FreeText,
		TimeoutS:  c.TimeoutS,
		ExpiresAt: exp,
	}
}

// isAlreadySettled reports whether an Answer error is a lost-race
// (the card was answered, skipped, or expired before this attempt).
func isAlreadySettled(err error) bool {
	msg := err.Error()
	for _, phrase := range []string{"already answered", "already skipped", "already expired"} {
		if contains(msg, phrase) {
			return true
		}
	}
	return false
}

// isUnknownCard reports whether an Answer error is "no such card".
func isUnknownCard(err error) bool {
	return contains(err.Error(), "no pending verdict card")
}

// contains is a tiny strings.Contains local to avoid pulling strings
// into this file's import set for two call sites.
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}