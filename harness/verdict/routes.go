// The verdict cards' REST face, riding the verdict capsule.
package verdict

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/EndoTheDev/omega/core"
)

// verdictCardWire is one pending card on the wire.
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

// verdictAnswerRequest is the POST /sessions/{id}/verdicts body. card_id is optional:
// when absent the session's oldest pending card is answered (one run per session, so
// the common case is exactly one).
type verdictAnswerRequest struct {
	CardID        string `json:"card_id,omitempty"`
	OptionIndexes []int  `json:"option_indexes,omitempty"`
	OptionIndex   int    `json:"option_index,omitempty"`
	Option        string `json:"option,omitempty"`
	Text          string `json:"text,omitempty"`
	Skip          bool   `json:"skip,omitempty"`
	Message       string `json:"message,omitempty"`
}

const maxVerdictMessage = 2000

type Routes struct {
	hub *Hub
}

func newRoutes(hub *Hub) *Routes { return &Routes{hub: hub} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleVerdicts serves GET /verdicts and GET+POST /sessions/{id}/verdicts.
func (rt *Routes) handleVerdicts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if r.URL.Path == "/verdicts" && id != "" {
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

func (rt *Routes) serveVerdictsList(w http.ResponseWriter, r *http.Request, sessionID string) {
	cards := rt.hub.Pending(sessionID)
	out := make([]verdictCardWire, 0, len(cards))
	for _, c := range cards {
		out = append(out, toVerdictWire(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": out, "count": len(out)})
}

// serveVerdictAnswer answers POST /sessions/{id}/verdicts. First answer wins; a second
// attempt from either surface returns 409 with the reason (the both-frontend
// single-answer race).
func (rt *Routes) serveVerdictAnswer(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req verdictAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
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
		// settled cards (answered/skipped/expired) race to 409 with the reason.
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

func isAlreadySettled(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "already answered") ||
		strings.Contains(msg, "already skipped") ||
		strings.Contains(msg, "already expired")
}

func isUnknownCard(err error) bool {
	return strings.Contains(err.Error(), "no pending verdict card")
}
