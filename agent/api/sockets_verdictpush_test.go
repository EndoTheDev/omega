package api

// The verdict issuance push: a card's registration is the ONE event a shell cannot.
// These tests pin the two halves on this side of the seam: the chat-topic frame itself,
// and the nil-seam boot (no verdict capsule mounted -> serve still boots, nothing
// subscribes).

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/verdict"
)

// waitPendingCard polls the hub until the card is answerable.
func waitPendingCard(t *testing.T, hub *verdict.Hub, session string) core.QuestionPrompt {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for time.Now().Before(deadline) {
		if cards := hub.Pending(session); len(cards) > 0 {
			return cards[0]
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("no pending card in %q within the wait window", session)
	return core.QuestionPrompt{}
}

// TestVerdictIssuanceRidesChatTopic: mounting the verdict hub and rewiring the server.
func TestVerdictIssuanceRidesChatTopic(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	hub := verdict.NewHub(nil, nil)
	offers := map[string]any{"verdicts": hub}
	// boot wires the push; a live module toggle rewires the same Context
	// shape - the same hub must not accumulate listeners.
	offerServices(t, s, offers)
	offerServices(t, s, offers)

	chat := s.Sockets().Attach()
	defer chat.Close()
	runsOnly := s.Sockets().Attach()
	defer runsOnly.Close()
	runsOnly.Subscribe([]string{TopicRuns})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := hub.Ask(ctx, core.QuestionPrompt{
			SessionID: "sess-gate",
			Title:     "run rm -rf build?",
			Options:   []core.QuestionOption{{Label: "yes"}, {Label: "no"}},
		})
		done <- err
	}()

	msg := recvFrame(t, chat)
	if msg.Topic != TopicChat {
		t.Fatalf("topic = %q, want %q", msg.Topic, TopicChat)
	}
	if msg.Session != "sess-gate" {
		t.Fatalf("session = %q, want the card's own session", msg.Session)
	}
	if msg.Event != verdictIssuedEvent {
		t.Fatalf("event = %q, want %q", msg.Event, verdictIssuedEvent)
	}
	var hint verdictIssuedFrame
	if err := json.Unmarshal(msg.Data, &hint); err != nil {
		t.Fatalf("decode hint payload: %v", err)
	}
	if hint.CardID == "" || hint.SessionID != "sess-gate" || hint.Title != "run rm -rf build?" {
		t.Fatalf("hint = %+v, want the registered card's id/session/title", hint)
	}
	pending := waitPendingCard(t, hub, "sess-gate")
	if pending.ID != hint.CardID {
		t.Fatalf("pending card %q != hinted card %q", pending.ID, hint.CardID)
	}

	// answering settles the Ask and pushes nothing (the tool result is the answer path's
	// own hint) - and the run-only subscriber never sees the issuance frame at all.
	if _, err := hub.Answer("sess-gate", "", core.QuestionAnswer{OptionIndex: 1}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Ask returned error: %v", err)
		}
	case <-time.After(testWaitTimeout):
		t.Fatal("Ask did not return after the answer")
	}
	expectNoFrame(t, chat)
	expectNoFrame(t, runsOnly)
}

// TestVerdictIssuancePushNilSafe: a host with no verdict capsule (or a wrong-shaped
// offer) boots clean - no subscription, no panic, and the server still serves.
func TestVerdictIssuancePushNilSafe(t *testing.T) {
	s := inspectorServer(t)
	offerServices(t, s, map[string]any{})
	if s.verdictSrc != nil {
		t.Fatalf("verdictSrc = %v, want nil (nothing to subscribe to)", s.verdictSrc)
	}

	// a wrong-shaped offer is an optional consumer's degradation, not a boot failure.
	offerServices(t, s, map[string]any{"verdicts": "not a card source"})
	if s.verdictSrc != nil {
		t.Fatalf("verdictSrc = %v, want nil for a wrong-shaped offer", s.verdictSrc)
	}

	ts := serveInspector(t, s)
	resp, err := http.Get(ts.URL + "/commands")
	if err != nil {
		t.Fatalf("GET /commands on a verdict-less server: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /commands = %d, want 200 (serve survives without the verdicts seam)", resp.StatusCode)
	}
}
