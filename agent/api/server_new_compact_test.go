package api

// server_new_compact_test.go: /new on a conversation the MACHINERY owns. The rule is one
// line long - "/new in an agent chat compacts, never forks the relationship" - and it
// has two halves: the wire answers the compact (fork vs compacted), and the
// provider-facing history starts at the compact's boundary with the summary heading it.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

// TestSessionNewForksTheHumansOwn pins the half that must NOT change:
// the human's own conversation forks on /new, exactly as before - no
// boundary is written, the shell opens a fresh tab.
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
	row, err := st.GetSession(ctx, "mine")
	if err != nil {
		t.Fatalf("row: %v", err)
	}
	if row.ContextStart != "" || row.ContextSummary != "" {
		t.Fatalf("a fork wrote a compact (start=%q summary=%q) - /new must not touch the human's own context",
			row.ContextStart, row.ContextSummary)
	}
}

// TestSessionNewCompactsMachineryOwned pins the other half.
func TestSessionNewCompactsMachineryOwned(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateCanonicalChat(ctx, "scout-canonical", "scout"); err != nil {
		t.Fatalf("create canonical chat: %v", err)
	}
	for _, m := range []core.Message{core.NewUser("patrol the queue"), core.NewAssistant("queue is empty")} {
		if err := st.AppendMessage(ctx, "scout-canonical", m); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

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
	if row.ContextStart == "" {
		t.Fatal("no context boundary written - the compact did nothing")
	}

	view, err := st.GetActiveView(ctx, "scout-canonical")
	if err != nil {
		t.Fatalf("active view: %v", err)
	}
	if len(view.Path) != 2 {
		t.Fatalf("active path = %d rows, want 2 (the transcript is never pruned)", len(view.Path))
	}
	if want := core.MsgID(view.Path[len(view.Path)-1].ID); row.ContextStart != want {
		t.Fatalf("context_start = %q, want the tail %q", row.ContextStart, want)
	}
	compacted := compactPath(view.Path, row.ContextStart)
	if len(compacted) != 1 {
		t.Fatalf("compacted path = %d rows, want 1 (the boundary onward)", len(compacted))
	}
	if _, ok := compacted[0].Message.(core.Assistant); !ok {
		t.Fatalf("compacted path starts at %T, want the tail assistant row", compacted[0].Message)
	}

	if code, body := newCompactCall(t, s, "scout-canonical"); code != http.StatusOK || body["kind"] != "compacted" {
		t.Fatalf("second compact = %d %v, want 200 compacted", code, body)
	}
}

// TestSessionNewUnknownIs404 pins the miss: an id no store owns is a 404, never.
func TestSessionNewUnknownIs404(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	if code, _ := newCompactCall(t, s, "ghost"); code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
}

// TestCompactSummaryPrefix pins what heads a compacted context.
func TestCompactSummaryPrefix(t *testing.T) {
	if got := compactSummaryPrefix(""); got != nil {
		t.Fatalf("empty summary = %v, want no message", got)
	}
	got := compactSummaryPrefix("the queue was empty at 10:00")
	if len(got) != 1 {
		t.Fatalf("summary head = %d messages, want 1", len(got))
	}
	sys, ok := got[0].(core.System)
	if !ok {
		t.Fatalf("summary head is %T, want a system message", got[0])
	}
	if !strings.HasPrefix(sys.Content, core.ContextCompactPrefix) ||
		!strings.HasSuffix(sys.Content, core.ContextCompactSuffix) ||
		!strings.Contains(sys.Content, "the queue was empty at 10:00") {
		t.Fatalf("summary head = %q, want it wrapped in the compact markers", sys.Content)
	}
}

// TestCompactPathMissingBoundary pins the healing rule. A conversation heals to a full
// context, never to an empty one.
func TestCompactPathMissingBoundary(t *testing.T) {
	view := []core.StoredMessage{
		{ID: 1, Message: core.NewUser("a")},
		{ID: 2, Message: core.NewAssistant("b")},
	}
	if got := compactPath(view, ""); len(got) != 2 {
		t.Fatalf("no boundary = %d rows, want the whole path", len(got))
	}
	if got := compactPath(view, core.MsgID(99)); len(got) != 2 {
		t.Fatalf("unknown boundary = %d rows, want the whole path", len(got))
	}
	if got := compactPath(view, core.MsgID(2)); len(got) != 1 {
		t.Fatalf("boundary = %d rows, want the tail", len(got))
	}
}
