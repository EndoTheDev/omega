// Package verdict implements verdict cards v1 (card t_2ecf9c51): the
// agent issues a question mid-turn, the turn HARD-PAUSES inside the
// request_verdict tool, and the user answers by click (webui) or type
// (TUI). Whichever surface answers first wins - the second answerer is
// rejected with a reason, never double-applied. Advisory in v1: the
// verdict is recorded as session messages; the agent then acts through
// its normal tools. Nothing on the card executes automatically.
//
// Seam: verdicts (exclusive), tools (additive), store (read-only
// persistence of the card + verdict records).
package verdict

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// DefaultTimeout is the per-card default deadline (design Q5): twenty
// minutes. A tool call's timeout_s argument overrides it per card.
const DefaultTimeout = 20 * time.Minute

// Hub is the in-memory registry of verdict cards. Cards live in issued
// order; the transcript persistence (ai.VerdictCard / ai.Verdict rows,
// via the store) is the durable audit trail - the hub itself survives
// only for the process. A restart with still-pending cards leaves the
// transcript's card unreconciled (no tool result) - the next run is not
// blocked by it; a v2 card can reconcile against the store.
//
// ponytail: terminal cards stay in memory forever. Each is a few
// hundred bytes and issuance is human-paced; the upgrade path is a
// prune pass when the slice exceeds a few hundred entries.
type Hub struct {
	store  agent.StoreProvider
	logger agent.LoggerProvider
	// deadline is the default per-card timeout when a call passes none.
	deadline time.Duration

	mu     sync.Mutex
	cards  []*cardState
	closed bool
}

// cardState is one card's identity plus its answer race: exactly one
// resolution channel send wins (Answer or the expiry timer), and any
// later Answer sees the settled state and returns the reason.
type cardState struct {
	prompt   agent.VerdictPrompt
	answer   chan agent.Verdict
	settled  bool // answered, skipped, or expired
	expired  bool
	skipped  bool
	option   string
	optionIx int
	text     string
}

// NewHub builds a hub. store is optional (ephemeral sessions and
// store-less hosts skip persistence); logger optional.
func NewHub(store agent.StoreProvider, logger agent.LoggerProvider) *Hub {
	return &Hub{store: store, logger: logger, deadline: DefaultTimeout}
}

// Close releases every blocked Ask (as a cancelled verdict error) - the
// host calls it on shutdown so a mid-verdict tool call does not hang.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, c := range h.cards {
		if !c.settled {
			c.settled = true
			close(c.answer) // waiter sees zero Verdict + closed
		}
	}
}

// Ask implements agent.VerdictsProvider: registers the card, persists
// the ai.VerdictCard record, arms the expiry timer, and blocks until
// an answer, the expiry, or ctx cancellation. A hub already closed (or
// context gone before registration) errors immediately.
func (h *Hub) Ask(ctx context.Context, card agent.VerdictPrompt) (agent.Verdict, error) {
	if card.ID == "" {
		id, err := agent.NewSessionID()
		if err != nil {
			return agent.Verdict{}, fmt.Errorf("mint card id: %w", err)
		}
		card.ID = id
	}
	timeout := h.deadline
	if card.TimeoutS > 0 {
		timeout = time.Duration(card.TimeoutS) * time.Second
	}
	card.ExpiresAt = time.Now().Add(timeout)

	state := &cardState{prompt: card, answer: make(chan agent.Verdict, 1)}
	h.mu.Lock()
	if h.closed || len(state.answer) > 0 {
		h.mu.Unlock()
		return agent.Verdict{}, fmt.Errorf("verdicts hub closed")
	}
	h.cards = append(h.cards, state)
	h.mu.Unlock()

	// Persist the card BEFORE the pause completes the audit trail even
	// if the process dies mid-question. Best effort: persistence must
	// never block or fail the question itself.
	h.persist(ai.NewVerdictCard(card.ID, card.Title, card.Body, card.Options, card.FreeText, card.TimeoutS))

	timer := time.AfterFunc(timeout, func() { h.expire(card.ID) })
	defer timer.Stop()

	select {
	case v, ok := <-state.answer:
		if !ok {
			return agent.Verdict{}, fmt.Errorf("verdict cancelled: the hub closed mid-question")
		}
		return v, nil
	case <-ctx.Done():
		// Run stop / shutdown: settle the card so a later Answer is
		// rejected cleanly instead of delivering into a dead tool call.
		h.mu.Lock()
		state.settled = true
		h.mu.Unlock()
		return agent.Verdict{}, ctx.Err()
	}
}

// expire fires the card's safe answer (deny / no-pick): the waiter gets
// Verdict{Expired: true}, the transcript records the expiry event.
// A card that was answered first is untouched (the timer lost the race).
func (h *Hub) expire(cardID string) {
	h.mu.Lock()
	c := h.byIDLocked(cardID)
	if c == nil || c.settled {
		h.mu.Unlock()
		return
	}
	c.settled = true
	c.expired = true
	record := ai.NewVerdict(cardID, 0, "", "", false, true)
	c.answer <- agent.Verdict{CardID: cardID, Expired: true}
	h.mu.Unlock()

	if h.logger != nil {
		h.logger.Printf("verdict card %s expired with no answer", cardID)
	}
	h.persist(record)
}

// Answer implements agent.VerdictsProvider. cardID empty means the
// session's OLDEST pending card (one run per session, so the common
// case is exactly one; ambiguous cases name the count in the error and
// leave the choice to the caller). First answer wins; every later
// attempt errors with the reason.
func (h *Hub) Answer(sessionID, cardID string, v agent.Verdict) error {
	h.mu.Lock()
	var c *cardState
	if cardID == "" {
		// oldest pending card for the session (one run per session, so
		// the common case is exactly one; ambiguous cases name the
		// count in the error and leave the choice to the caller)
		if sessionID == "" {
			h.mu.Unlock()
			return fmt.Errorf("answer is ambiguous: no session and no card given")
		}
		for _, cand := range h.cards {
			if !cand.settled && cand.prompt.SessionID == sessionID {
				c = cand
				break
			}
		}
		if c == nil {
			h.mu.Unlock()
			return fmt.Errorf("no pending verdict card in session %s", sessionID)
		}
		cardID = c.prompt.ID
	} else {
		c = h.byIDLocked(cardID)
		if c == nil {
			h.mu.Unlock()
			return fmt.Errorf("no pending verdict card %s", cardID)
		}
		// session boundary: a known card from another session cannot be
		// answered through this session's surface (the URL is the
		// boundary, v1). Empty sessionID trusts the card id (the TUI's
		// /verdict command answers without a session context).
		if sessionID != "" && c.prompt.SessionID != sessionID {
			h.mu.Unlock()
			return fmt.Errorf("verdict card %s belongs to a different session", cardID)
		}
	}
	if c.settled {
		h.mu.Unlock()
		if c.expired {
			return fmt.Errorf("verdict card %s already expired", cardID)
		}
		if c.skipped {
			return fmt.Errorf("verdict card %s already skipped", cardID)
		}
		return fmt.Errorf("verdict card %s already answered", cardID)
	}

	// Validations per card shape (design point 1: ONE card shape):
	// a pick must land on a real option; free text only when the card
	// allows it; a skip is always allowed and is recorded, never silent.
	switch {
	case v.Skipped:
		// always fine
	case v.OptionIndex > 0:
		if v.OptionIndex > len(c.prompt.Options) {
			h.mu.Unlock()
			return fmt.Errorf("option %d is out of range: the card has %d options", v.OptionIndex, len(c.prompt.Options))
		}
		v.Option = c.prompt.Options[v.OptionIndex-1]
	case v.Text != "":
		if len(c.prompt.Options) > 0 && !c.prompt.FreeText {
			h.mu.Unlock()
			return fmt.Errorf("card %s takes an option, not free text", cardID)
		}
	default:
		if len(c.prompt.Options) > 0 {
			h.mu.Unlock()
			return fmt.Errorf("card %s needs an option pick (1..%d) or a skip", cardID, len(c.prompt.Options))
		}
		h.mu.Unlock()
		return fmt.Errorf("card %s needs free text or a skip", cardID)
	}

	c.settled = true
	c.skipped = v.Skipped
	c.option = v.Option
	c.optionIx = v.OptionIndex
	c.text = v.Text
	v.CardID = cardID
	record := ai.NewVerdict(cardID, v.OptionIndex, v.Option, v.Text, v.Skipped, false)
	c.answer <- v
	h.mu.Unlock()

	h.persist(record)
	return nil
}

// Pending implements agent.VerdictsProvider: unanswered, unexpired
// cards, oldest first. Empty filter lists every session.
func (h *Hub) Pending(sessionID string) []agent.VerdictPrompt {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	out := make([]agent.VerdictPrompt, 0, len(h.cards))
	for _, c := range h.cards {
		if c.settled {
			continue
		}
		if sessionID != "" && c.prompt.SessionID != sessionID {
			continue
		}
		if now.After(c.prompt.ExpiresAt) {
			// the timer has not fired yet; the card is dead either way
			continue
		}
		out = append(out, c.prompt)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ExpiresAt.Before(out[j].ExpiresAt) })
	return out
}

// byIDLocked finds one card by id. Callers hold h.mu.
func (h *Hub) byIDLocked(cardID string) *cardState {
	for _, c := range h.cards {
		if c.prompt.ID == cardID {
			return c
		}
	}
	return nil
}

// persist appends one record to the session transcript when the hub
// has a store and knows the session. Best effort: a persistence
// failure logs and moves on - the question itself must not fail
// because the audit row could not be written.
func (h *Hub) persist(msg ai.Message) {
	if h.store == nil {
		return
	}
	cardID := cardOf(msg)
	if cardID == "" {
		return
	}
	h.mu.Lock()
	session := ""
	for _, c := range h.cards {
		if c.prompt.ID == cardID {
			session = c.prompt.SessionID
			break
		}
	}
	h.mu.Unlock()
	if session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.store.AppendMessage(ctx, session, msg); err != nil && h.logger != nil {
		h.logger.Errorf("verdict %s: persist %s record: %v", cardID, ai.MessageRole(msg), err)
	}
}

// cardOf extracts the CardID from the two record kinds persist carries.
func cardOf(msg ai.Message) string {
	switch m := msg.(type) {
	case ai.VerdictCard:
		return m.CardID
	case ai.Verdict:
		return m.CardID
	}
	return ""
}

// resultMap is the wire shape the request_verdict TOOL RESULT carries
// back to the model (the loop persists it through the normal tool
// result path). Flat and small: the agent reads it, acts through its
// normal tools, nothing executes off it.
func resultMap(v agent.Verdict) string {
	out := map[string]any{
		"card_id": v.CardID,
		"verdict": v.Kind(),
	}
	if v.OptionIndex > 0 {
		out["option_index"] = v.OptionIndex
		out["option"] = v.Option
	}
	if v.Text != "" {
		out["text"] = v.Text
	}
	if v.Skipped {
		out["skipped"] = true
	}
	if v.Expired {
		out["expired"] = true
	}
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprintf("verdict: %s", v.Kind())
	}
	return string(b)
}