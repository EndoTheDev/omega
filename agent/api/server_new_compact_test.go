package api

// server_new_compact_test.go: /new on a conversation the MACHINERY owns. The rule is one
// line long - "/new in an agent chat compacts, never forks the relationship" - and it has
// two halves: the wire answers the compact (fork vs compacted), and the provider-facing
// history reads back as the compact's carrier plus a one-row foothold.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// newCompactCall POSTs /sessions/{id}/new and decodes the answer.
func newCompactCall(t *testing.T, s *Server, id string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/sessions/"+id+"/new", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

// TestSessionNewForksTheHumansOwn pins the half that must NOT change: the human's own
// conversation forks on /new, exactly as before - no block is written, the shell opens a
// fresh tab.
func TestSessionNewForksTheHumansOwn(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "mine", "", "my conversation"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.AppendMessage(ctx, "mine", core.NewUser("hello")); err != nil {
		t.Fatalf("append: %v", err)
	}

	code, body := newCompactCall(t, s, "mine")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", code, body)
	}
	if body["kind"] != "fork" {
		t.Fatalf("kind = %v, want fork - the human's own conversation forks", body["kind"])
	}
	blocks, err := st.ContextBlocks(ctx, "mine")
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("a fork recorded %d blocks - /new must not touch the human's own context", len(blocks))
	}
}

// TestSessionNewCompactsMachineryOwned pins the other half.
func TestSessionNewCompactsMachineryOwned(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateCanonicalChat(ctx, "scout-canonical", "scout"); err != nil {
		t.Fatalf("create canonical chat: %v", err)
	}
	for _, m := range []core.Message{
		core.NewUser("patrol the queue"),
		core.NewAssistant("queue is empty"),
		core.NewUser("patrol again"),
		core.NewAssistant("still empty"),
	} {
		if err := st.AppendMessage(ctx, "scout-canonical", m); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	mountCompactor(t, s, &compactCompactor{answer: agent.CompactAnswer{Summary: "the queue was empty all night", Keep: 1}})

	code, body := newCompactCall(t, s, "scout-canonical")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", code, body)
	}
	if body["kind"] != "compacted" {
		t.Fatalf("kind = %v, want compacted - an agent chat never forks", body["kind"])
	}
	if body["session"] != "scout-canonical" {
		t.Fatalf("session = %v, want the SAME id (the relationship is the point)", body["session"])
	}

	row, err := st.GetSession(ctx, "scout-canonical")
	if err != nil {
		t.Fatalf("row: %v", err)
	}
	if !row.MachineOwned {
		t.Fatal("the canonical chat is not machine-owned - it would show up in the human's session list")
	}
	blocks, err := st.ContextBlocks(ctx, "scout-canonical")
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want the compact the /new ran", len(blocks))
	}
	if blocks[0].Keep != 4 {
		t.Fatalf("block keep = %d, want the last row (4) as the foothold", blocks[0].Keep)
	}
	msgs, err := agent.SessionContext(ctx, st, "scout-canonical")
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("context = %d messages, want 4 (head + carrier pair + foothold)", len(msgs))
	}
	if !agent.IsCarrierResult(msgs[2]) || agent.MessageText(msgs[2]) != "the queue was empty all night" {
		t.Fatalf("msgs[2] = %q, want the summary carrier", agent.MessageText(msgs[2]))
	}
	if agent.MessageText(msgs[3]) != "still empty" {
		t.Fatalf("foothold = %q, want the last row verbatim", agent.MessageText(msgs[3]))
	}

	// The transcript is never pruned: the compact only moves the live context.
	transcript, err := st.GetMessages(ctx, "scout-canonical")
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	if len(transcript) != 6 {
		t.Fatalf("transcript = %d rows, want the 4 kept plus the carrier pair", len(transcript))
	}
}

// TestSessionNewUnknownIs404 pins the miss: an id no store owns is a 404, never.
func TestSessionNewUnknownIs404(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	if code, _ := newCompactCall(t, s, "ghost"); code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
}
