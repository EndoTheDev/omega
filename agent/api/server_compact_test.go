package api

// server_compact_test.go: /compact is a real fold, never a placebo. The capsule
// declares the intent; this surface owns the session, so it runs the fold and answers
// what the fold actually did. The session persists - the transcript keeps every row
// and only the context boundary moves.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// foldCompactor is the manual fold face: it answers a fixed fold and records what it
// was handed.
type foldCompactor struct {
	out   []core.Message
	err   error
	calls int
	got   []core.Message
}

func (f *foldCompactor) FoldNow(_ context.Context, messages []core.Message) ([]core.Message, error) {
	f.calls++
	f.got = messages
	return f.out, f.err
}

// mountCompactor offers the fold face and registers /compact's intent handler on the
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

// TestCompactFoldsTheSession is the card's law: a manual fold moves the boundary to
// the row the compactor kept verbatim, writes its summary, keeps every transcript row,
// and answers with the real numbers - never a bare "done".
func TestCompactFoldsTheSession(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	seedSession(t, s, st, "s-1", "one", "two", "three")
	c := &foldCompactor{out: []core.Message{
		core.NewSystem(core.ContextFoldPrefix + "the middle of it" + core.ContextFoldSuffix),
		core.NewUser("three"),
	}}
	mountCompactor(t, s, c)

	code, got, body := compactCall(t, s, "s-1")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", code, body)
	}
	if c.calls != 1 || len(c.got) != 3 {
		t.Fatalf("fold was handed %d rows in %d calls, want all 3 in one", len(c.got), c.calls)
	}

	view, err := st.GetActiveView(ctx, "s-1")
	if err != nil {
		t.Fatalf("active view: %v", err)
	}
	wantStart := core.MsgID(view.Path[2].ID)
	if want := "folded 2 messages into a summary - the context now starts at message " + wantStart + ", and the last 1 stay verbatim"; got.Text != want {
		t.Fatalf("answer = %q, want %q", got.Text, want)
	}
	if len(got.Actions) != 0 {
		t.Fatalf("actions = %+v, want the intent consumed by the host", got.Actions)
	}

	row, err := st.GetSession(ctx, "s-1")
	if err != nil {
		t.Fatalf("session row: %v", err)
	}
	if row.ContextStart != wantStart {
		t.Fatalf("context_start = %q, want the kept row %q", row.ContextStart, wantStart)
	}
	if row.ContextSummary != "the middle of it" {
		t.Fatalf("context_summary = %q, want the compactor's summary", row.ContextSummary)
	}
	// The session PERSISTS: a fold is not /new.
	msgs, err := st.GetMessages(ctx, "s-1")
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("transcript = %d rows, want all 3 kept", len(msgs))
	}
}

// TestCompactRefusesAFoldWithNothingToSay pins the honest no-op: a boundary that drops
// the middle and keeps nothing in its place is worse than no fold.
func TestCompactRefusesAFoldWithNothingToSay(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	seedSession(t, s, st, "s-2", "one", "two")
	c := &foldCompactor{}
	mountCompactor(t, s, c)

	code, got, body := compactCall(t, s, "s-2")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", code, body)
	}
	if want := "nothing folded: the compactor had nothing to summarize"; !strings.HasPrefix(got.Text, want) {
		t.Fatalf("answer = %q, want it to open %q", got.Text, want)
	}
	row, err := st.GetSession(ctx, "s-2")
	if err != nil {
		t.Fatalf("session row: %v", err)
	}
	if row.ContextStart != "" || row.ContextSummary != "" {
		t.Fatalf("a refused fold wrote a boundary (start=%q summary=%q)", row.ContextStart, row.ContextSummary)
	}
}

// TestCompactNeedsASession pins the two cases with nowhere to write.
func TestCompactNeedsASession(t *testing.T) {
	cases := []struct {
		name      string
		sessionID string
		want      string
	}{
		{"no session bound", "", "nothing folded: no session is bound to this chat yet"},
		{"ephemeral keeps no history", "eph-live", "nothing folded: ephemeral sessions keep no history"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestServerWithStore(t)
			c := &foldCompactor{out: []core.Message{core.NewSystem(core.ContextFoldPrefix + "s" + core.ContextFoldSuffix)}}
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

// TestCompactWithoutACompactor pins the unmounted module: the fold says so instead of
// silently moving the boundary.
func TestCompactWithoutACompactor(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	seedSession(t, s, st, "s-3", "one", "two", "three")
	mountCompactor(t, s, nil)

	code, got, body := compactCall(t, s, "s-3")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", code, body)
	}
	if want := "nothing folded: the compaction module is not mounted"; got.Text != want {
		t.Fatalf("answer = %q, want %q", got.Text, want)
	}
	row, err := st.GetSession(ctx, "s-3")
	if err != nil {
		t.Fatalf("session row: %v", err)
	}
	if row.ContextStart != "" {
		t.Fatalf("a fold with no compactor wrote a boundary (%q)", row.ContextStart)
	}
}

// TestCompactSurfacesTheCompactorError pins the failure lane: a dead upstream is a
// named failure on the wire, and no boundary is written.
func TestCompactSurfacesTheCompactorError(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	seedSession(t, s, st, "s-4", "one", "two", "three")
	mountCompactor(t, s, &foldCompactor{err: errors.New("compactor: upstream is down")})

	code, _, body := compactCall(t, s, "s-4")
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (%s)", code, body)
	}
	if !strings.Contains(body, "command failed: compactor: upstream is down") {
		t.Fatalf("body = %q, want the upstream's own failure", body)
	}
	row, err := st.GetSession(ctx, "s-4")
	if err != nil {
		t.Fatalf("session row: %v", err)
	}
	if row.ContextStart != "" {
		t.Fatalf("a failed fold wrote a boundary (%q)", row.ContextStart)
	}
}

// TestCompactLeavesOtherActionsAlone pins the filter: only the fold intent is consumed,
// so a command that also carries a set_model keeps it on the wire.
func TestCompactLeavesOtherActionsAlone(t *testing.T) {
	s, st := newTestServerWithStore(t)
	seedSession(t, s, st, "s-5", "one", "two", "three")
	offerServices(t, s, map[string]any{"compactor": &foldCompactor{out: []core.Message{
		core.NewSystem(core.ContextFoldPrefix + "sum" + core.ContextFoldSuffix),
		core.NewUser("three"),
	}}})
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
