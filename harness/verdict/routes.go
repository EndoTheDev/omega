// The verdict cards' REST face, riding the verdict capsule (shell-ring
// shrink wave 3, card t_b75df168 - moved whole from
// agent/api/server_verdicts.go, package unchanged, URLs unchanged).
//
// GET /verdicts (cross-session pending listing, the bell's count) +
// /sessions/{id}/verdicts (one session's listing, and the answering
// POST - card t_2ecf9c51's answer surface, the message guidance of
// card t_63204dfd). The card itself renders live from the existing
// tool_call / tool_result SSE frames (the verdict tool's args carry
// title/options/timeout; the result carries the verdict JSON) - these
// endpoints are the ANSWER path only. They hold the Hub the mount
// offered under "verdicts", and a disabled capsule answers 404 at the
// module mux - the routes mount and die with their capsule, so the
// chat bell empties with the feature instead of polling a dead seam.
package verdict

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/EndoTheDev/omega/core"
)

// verdictCardWire is one pending card on the wire. ExpiresAt is a Unix
// millisecond epoch so the client countdowns against the server clock
// without a timezone dance.
type verdictCardWire struct {
	ID        string                `json:"id"`
	SessionID string                `json:"session_id"`
	Title     string                `json:"title"`
	Body      string                `json:"body,omitempty"`
	Options   []core.QuestionOption `json:"options,omitempty"`
	Multi     bool                  `json:"multi,omitempty"`
	AllowText bool                  `json:"allow_text,omitempty"`
	TimeoutS  int                   `json:"timeout_s,omitempty"`
	ExpiresAt int64                 `json:"expires_at"`
}

// verdictAnswerRequest is the POST /sessions/{id}/verdicts body. One of
// option_indexes (multi), option_index, option (label), text, or skip.
// card_id is optional: when absent the session's oldest pending card
// is answered (one run per session, so the common case is exactly one).
// Message is the follow-up guidance field (card t_63204dfd): when set
// it rides the answer as the resolution's free text (an option pick +
// message = deny with directions; message alone = a typed answer where
// the card allows text). Capped at maxVerdictMessage runes; empty =
// the plain answer, no guidance.
type verdictAnswerRequest struct {
	CardID        string `json:"card_id,omitempty"`
	OptionIndexes []int  `json:"option_indexes,omitempty"`
	OptionIndex   int    `json:"option_index,omitempty"`
	Option        string `json:"option,omitempty"`
	Text          string `json:"text,omitempty"`
	Skip          bool   `json:"skip,omitempty"`
	Message       string `json:"message,omitempty"`
}

// maxVerdictMessage caps the resolution message (card t_63204dfd).
const maxVerdictMessage = 2000

// Routes is the capsule's REST face over the hub (p.hub at mount).
type Routes struct {
	hub *Hub
}

// newRoutes builds the REST face on the mount's hub handle.
func newRoutes(hub *Hub) *Routes { return &Routes{hub: hub} }

// writeJSON serves the JSON responses.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleVerdicts serves GET /verdicts and GET+POST
// /sessions/{id}/verdicts.
func (rt *Routes) handleVerdicts(w http.ResponseWriter, r *http.Request) {
	// All verdict endpoints live under /verdicts (cross-session listing)
	// and /sessions/{id}/verdicts (per-session listing + answering).
	id := r.PathValue("id")
	if r.URL.Path == "/verdicts" && id != "" {
		// unreachable: /verdicts has no {id}; guard anyway
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		rt.serveVerdictsList(w, r, id)
	case http.MethodPost:
		if id == "" {
			http.Error(w, "answering requires a session id", http.StatusBadRequest)
			return
		}
		rt.serveVerdictAnswer(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// serveVerdictsList answers GET /verdicts (all sessions) and GET
// /sessions/{id}/verdicts (one session): the pending cards, oldest
// first, that the bell counts and the webui re-renders after a reload.
func (rt *Routes) serveVerdictsList(w http.ResponseWriter, r *http.Request, sessionID string) {
	cards := rt.hub.Pending(sessionID)
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
func (rt *Routes) serveVerdictAnswer(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req verdictAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	// The follow-up guidance (card t_63204dfd): message wins over text
	// (the newer field), and one size cap covers both spellings.
	text := req.Text
	if req.Message != "" {
		text = req.Message
	}
	if utf8.RuneCountInString(text) > maxVerdictMessage {
		http.Error(w, fmt.Sprintf("message over the %d character cap", maxVerdictMessage), http.StatusBadRequest)
		return
	}
	v := core.QuestionAnswer{
		OptionIndexes: req.OptionIndexes,
		OptionIndex:   req.OptionIndex,
		Option:        req.Option,
		Text:          text,
		Skipped:       req.Skip,
	}
	answered, err := rt.hub.Answer(sessionID, req.CardID, v)
	if err != nil {
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
	// the hub resolves the real card id (the no-card_id POST answers the
	// session's oldest pending card) and fills the picked option's
	// label - echo what actually landed
	out := map[string]any{
		"card_id": answered.CardID,
		"verdict": answered.Kind(),
	}
	if answered.OptionIndex > 0 {
		out["option"] = answered.Option
		out["option_index"] = answered.OptionIndex
	}
	if len(answered.OptionIndexes) > 0 {
		out["option_indexes"] = answered.OptionIndexes
		out["options"] = answered.Options
	}
	if answered.Text != "" {
		out["text"] = answered.Text
	}
	if answered.Skipped {
		out["skipped"] = true
	}
	writeJSON(w, http.StatusOK, out)
}

// toVerdictWire maps one runtime card to its wire shape.
func toVerdictWire(c core.QuestionPrompt) verdictCardWire {
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
		Multi:     c.Multi,
		AllowText: c.AllowText,
		TimeoutS:  c.TimeoutS,
		ExpiresAt: exp,
	}
}

// isAlreadySettled reports whether an Answer error is a lost-race
// (the card was answered, skipped, or expired before this attempt).
func isAlreadySettled(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "already answered") ||
		strings.Contains(msg, "already skipped") ||
		strings.Contains(msg, "already expired")
}

// isUnknownCard reports whether an Answer error is "no such card".
func isUnknownCard(err error) bool {
	return strings.Contains(err.Error(), "no pending verdict card")
}
