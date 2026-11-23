package api

// server_compact_test.go: /compact is a real compact, never a placebo. The capsule
// declares the intent; this surface owns the session, so it runs the compact and answers
// what the compact actually did. The session persists - the transcript keeps every row and
// the context is now derived from the block the compact appended.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// compactCompactor is the compact face: it answers a fixed summary and records what it was
// handed.
type compactCompactor struct {
	answer agent.CompactAnswer
	err    error
	calls  int
	got    []core.Message
}

func (f *compactCompactor) PlanCompact(context.Context, agent.CompactRequest) agent.CompactPlan {
	return agent.CompactPlan{Fire: true, Keep: f.answer.Keep}
}

func (f *compactCompactor) CompactNow(_ context.Context, req agent.CompactRequest) (agent.CompactAnswer, error) {
	f.calls++
	f.got = req.Wire
	return f.answer, f.err
}

// mountCompactor offers the compact face and registers /compact's intent handler on the
// server's own seams (after offerServices, which rewires them from a fresh Context).
func mountCompactor(t *testing.T, s *Server, c any) {
	t.Helper()
	offers := map[string]any{}
	if c != nil {
		offers["compactor"] = c
	}
	offerServices(t, s, offers)
	s.seams.mu.Lock()
	s.seams.commands = []core.ExtensionCommand{{Name: "/compact", Description: "summarize conversation history"}}
	s.seams.commandHandler = func(context.Context, string, string) (core.CommandResult, error) {
		return core.CommandResult{Actions: []core.CmdAction{{Type: compactAction}}}, nil
	}
	s.seams.mu.Unlock()
}

// compactCall dispatches /compact and decodes the answer (the body text on a failure).
func compactCall(t *testing.T, s *Server, sessionID string) (int, commandDispatchResponse, string) {
	t.Helper()
	body := `{"name":"/compact"}`
	if sessionID != "" {
		body = `{"name":"/compact","session_id":"` + sessionID + `"}`
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/commands/dispatch", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		return rec.Code, commandDispatchResponse{}, rec.Body.String()
	}
	var got commandDispatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body)
	}
	return rec.Code, got, ""
}

// seedSession writes n user rows into a fresh session.
func seedSession(t *testing.T, s *Server, st interface {
	CreateSession(ctx context.Context, id, parentID, label string) error
	AppendMessage(ctx context.Context, sessionID string, msg core.Message) error
}, id string, rows ...string) {
	t.Helper()
	ctx := context.Background()
	if err := st.CreateSession(ctx, id, "", "work"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, row := range rows {
		if err := st.AppendMessage(ctx, id, core.NewUser(row)); err != nil {
			t.Fatalf("append %q: %v", row, err)
		}
	}
}

// TestCompactMovesTheSession is the card's law: a manual compact appends the carrier pair,
// records the block, keeps every transcript row, and answers with the real numbers - never a
// bare "done".
func TestCompactMovesTheSession(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	seedSession(t, s, st, "s-1", "one", "two", "three")
	c := &compactCompactor{answer: agent.CompactAnswer{Summary: "the middle of it", Keep: 1}}
	mountCompactor(t, s, c)

	code, got, body := compactCall(t, s, "s-1")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", code, body)
	}
	if c.calls != 1 || len(c.got) != 3 {
		t.Fatalf("compact was handed %d rows in %d calls, want all 3 in one", len(c.got), c.calls)
	}

	blocks, err := st.ContextBlocks(ctx, "s-1")
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want the one compact", len(blocks))
	}
	b := blocks[0]
	if b.Tier != 1 || b.From != 2 || b.To != 3 || b.Keep != 3 || b.Call == 0 || b.Carrier == 0 {
		t.Fatalf("block = %+v, want tier 1 spanning rows 2-3 with a kept tail at 3", b)
	}
	want := fmt.Sprintf("compacted 1 messages into a summary carried by compress-%d - the last 1 stays verbatim", b.Call)
	if got.Text != want {
		t.Fatalf("answer = %q, want %q", got.Text, want)
	}
	if len(got.Actions) != 0 {
		t.Fatalf("actions = %+v, want the intent consumed by the host", got.Actions)
	}

	// The context now reads as head + carrier + kept tail; the transcript keeps every row.
	msgs, err := agent.SessionContext(ctx, st, "s-1")
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("context = %d messages, want 4 (head + carrier pair + kept tail)", len(msgs))
	}
	if !agent.IsCarrierResult(msgs[2]) || agent.MessageText(msgs[2]) != "the middle of it" {
		t.Fatalf("msgs[2] = %q, want the summary carrier", agent.MessageText(msgs[2]))
	}
	transcript, err := st.GetMessages(ctx, "s-1")
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(transcript) != 5 {
		t.Fatalf("transcript = %d rows, want all 3 plus the carrier pair", len(transcript))
	}
}

// TestCompactCarrierIsTheOnlyThingAppended pins the two-carrier law at the store: the rows
// that existed before the compact are untouched, byte for byte, and the carrier rides the
// tail.
func TestCompactCarrierIsTheOnlyThingAppended(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	seedSession(t, s, st, "s-tail", "one", "two", "three", "four")
	before, err := st.GetActiveView(ctx, "s-tail")
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	prefixBefore := agent.StoredMessages(before.Path)

	mountCompactor(t, s, &compactCompactor{answer: agent.CompactAnswer{Summary: "a summary", Keep: 2}})
	if code, _, body := compactCall(t, s, "s-tail"); code != http.StatusOK {
		t.Fatalf("status = %d (%s)", code, body)
	}
	after, err := st.GetActiveView(ctx, "s-tail")
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if len(after.Path) != len(before.Path)+2 {
		t.Fatalf("rows = %d, want the original %d plus the carrier pair", len(after.Path), len(before.Path))
	}
	for i := range prefixBefore {
		got := after.Path[i].Message
		wantRole, wantPayload, _ := core.EncodeMessage(prefixBefore[i])
		gotRole, gotPayload, _ := core.EncodeMessage(got)
		if wantRole != gotRole || string(wantPayload) != string(gotPayload) {
			t.Fatalf("row %d changed bytes: %s %s -> %s %s", i, wantRole, wantPayload, gotRole, gotPayload)
		}
	}
	// And the wire the provider sees shares that same prefix.
	wire, err := agent.SessionContext(ctx, st, "s-tail")
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if len(wire) == 0 || !sameEncoded(wire[0], prefixBefore[0]) {
		t.Fatalf("wire head changed: %#v", wire[0])
	}
}

func sameEncoded(a, b core.Message) bool {
	ra, pa, errA := core.EncodeMessage(a)
	rb, pb, errB := core.EncodeMessage(b)
	return errA == nil && errB == nil && ra == rb && string(pa) == string(pb)
}

// TestCompactRefusesACompactWithNothingToSay pins the honest no-op: a block that drops the
// middle and keeps nothing in its place is worse than no compact.
func TestCompactRefusesACompactWithNothingToSay(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	seedSession(t, s, st, "s-2", "one", "two")
	mountCompactor(t, s, &compactCompactor{})

	code, got, body := compactCall(t, s, "s-2")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", code, body)
	}
	if want := "nothing compacted: the compactor had nothing to summarize"; !strings.HasPrefix(got.Text, want) {
		t.Fatalf("answer = %q, want it to open %q", got.Text, want)
	}
	blocks, err := st.ContextBlocks(ctx, "s-2")
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("a refused compact recorded %d blocks", len(blocks))
	}
}

// TestCompactNeedsASession pins the two cases with nowhere to write.
func TestCompactNeedsASession(t *testing.T) {
	cases := []struct {
		name      string
		sessionID string
		want      string
	}{
		{"no session bound", "", "nothing compacted: no session is bound to this chat yet"},
		{"ephemeral keeps no history", "eph-live", "nothing compacted: ephemeral sessions keep no history"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestServerWithStore(t)
			c := &compactCompactor{answer: agent.CompactAnswer{Summary: "s"}}
			mountCompactor(t, s, c)
			code, got, body := compactCall(t, s, tt.sessionID)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", code, body)
			}
			if got.Text != tt.want {
				t.Fatalf("answer = %q, want %q", got.Text, tt.want)
			}
			if c.calls != 0 {
				t.Fatalf("the compactor ran %d times, want never", c.calls)
			}
		})
	}
}

// TestCompactWithoutACompactor pins the unmounted module: the answer says so instead of
// silently writing a block.
func TestCompactWithoutACompactor(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	seedSession(t, s, st, "s-3", "one", "two", "three")
	mountCompactor(t, s, nil)

	code, got, body := compactCall(t, s, "s-3")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", code, body)
	}
	if want := "nothing compacted: the compaction module is not mounted"; got.Text != want {
		t.Fatalf("answer = %q, want %q", got.Text, want)
	}
	blocks, err := st.ContextBlocks(ctx, "s-3")
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("a compact with no compactor recorded %d blocks", len(blocks))
	}
}

// TestCompactSurfacesTheCompactorError pins the failure lane: a dead upstream is a named
// failure on the wire, and no block is written.
func TestCompactSurfacesTheCompactorError(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	seedSession(t, s, st, "s-4", "one", "two", "three")
	mountCompactor(t, s, &compactCompactor{err: errors.New("compactor: upstream is down")})

	code, _, body := compactCall(t, s, "s-4")
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (%s)", code, body)
	}
	if !strings.Contains(body, "command failed: compactor: upstream is down") {
		t.Fatalf("body = %q, want the upstream's own failure", body)
	}
	blocks, err := st.ContextBlocks(ctx, "s-4")
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("a failed compact recorded %d blocks", len(blocks))
	}
}

// boundarylessStore serves sessions but holds no context blocks: a store without the
// session-block surface. The capability is MISSING, so the compact answers 501 like the
// sibling handlers, never a 500 that reads as a failure of the request.
type boundarylessStore struct{ agent.StoreProvider }

// TestCompactWithoutABlockSurface pins that answer: the command cannot run on this store at
// all, so it says so with the same status its sibling handlers use.
func TestCompactWithoutABlockSurface(t *testing.T) {
	s, st := newTestServerWithStore(t)
	seedSession(t, s, st, "s-6", "one", "two", "three")
	s.store = boundarylessStore{StoreProvider: st}
	mountCompactor(t, s, &compactCompactor{answer: agent.CompactAnswer{Summary: "sum"}})

	code, _, body := compactCall(t, s, "s-6")
	if code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 (%s)", code, body)
	}
	if want := "command failed: session store has no session-context surface"; !strings.Contains(body, want) {
		t.Fatalf("body = %q, want it to name %q", body, want)
	}
}

// TestCompactLeavesOtherActionsAlone pins the filter: only the compact intent is consumed,
// so a command that also carries a set_model keeps it on the wire.
func TestCompactLeavesOtherActionsAlone(t *testing.T) {
	s, st := newTestServerWithStore(t)
	seedSession(t, s, st, "s-5", "one", "two", "three")
	offerServices(t, s, map[string]any{"compactor": &compactCompactor{answer: agent.CompactAnswer{Summary: "sum", Keep: 1}}})
	s.seams.mu.Lock()
	s.seams.commands = []core.ExtensionCommand{{Name: "/compact", Description: "summarize conversation history"}}
	s.seams.commandHandler = func(context.Context, string, string) (core.CommandResult, error) {
		return core.CommandResult{Actions: []core.CmdAction{
			{Type: "set_model", Value: "m1"},
			{Type: compactAction},
		}}, nil
	}
	s.seams.mu.Unlock()

	code, got, body := compactCall(t, s, "s-5")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", code, body)
	}
	if len(got.Actions) != 1 || got.Actions[0].Type != "set_model" {
		t.Fatalf("actions = %+v, want only the set_model left", got.Actions)
	}
}
