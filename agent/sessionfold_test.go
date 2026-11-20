package agent

// sessionfold_test.go: the manual fold has ONE implementation and ONE wording. These pins
// hold the shared helper both hosts call (the CLI loop and the HTTP dispatch), so neither
// surface can answer with silence or drift into a second phrasing.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// foldStore is the session shape a manual fold needs: a session row (with its boundary),
// an active path, and a recorder for the boundary write. A refusal must write NOTHING, so
// every test can prove that from writes.
type foldStore struct {
	fakeStore // the full no-op StoreProvider seam
	row       core.Session
	path      []core.StoredMessage
	start     string
	summary   string
	writes    int
}

func (s *foldStore) GetSession(context.Context, string) (core.Session, error) { return s.row, nil }
func (s *foldStore) GetActiveView(context.Context, string) (core.ActiveView, error) {
	return core.ActiveView{Path: s.path}, nil
}
func (s *foldStore) SetSessionModel(context.Context, string, string) error     { return nil }
func (s *foldStore) SetSessionProvider(context.Context, string, string) error  { return nil }
func (s *foldStore) SetSessionWorkspace(context.Context, string, string) error { return nil }
func (s *foldStore) SetSessionContextStart(_ context.Context, _ string, start, summary string) error {
	s.writes++
	s.start, s.summary = start, summary
	return nil
}

// foldFace is the capsule's manual fold face: a fixed answer, and the rows it was handed.
type foldFace struct {
	out []core.Message
	err error
	got []core.Message
}

func (f *foldFace) FoldNow(_ context.Context, msgs []core.Message) ([]core.Message, error) {
	f.got = msgs
	return f.out, f.err
}

// foldPathRows seeds n user rows with store ids 1..n.
func foldPathRows(n int) []core.StoredMessage {
	out := make([]core.StoredMessage, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, core.StoredMessage{ID: int64(i), Message: core.NewUser("row")})
	}
	return out
}

// TestFoldSessionFoldsTheHead pins the fold: the boundary sits on the first row the
// compactor kept verbatim, the summary is written with it, and the count in the line is
// the number of rows the fold removed from the live context.
func TestFoldSessionFoldsTheHead(t *testing.T) {
	st := &foldStore{path: foldPathRows(5)}
	c := &foldFace{out: []core.Message{
		core.NewSystem(core.ContextFoldPrefix + "the middle" + core.ContextFoldSuffix),
		core.NewUser("row"),
	}}
	out, err := FoldSession(context.Background(), st, c, "s-1")
	if err != nil {
		t.Fatalf("FoldSession: %v", err)
	}
	if !out.Folded || out.Start != "5" || out.Rows != 4 || out.Kept != 1 {
		t.Fatalf("outcome = %+v, want folded start=5 rows=4 kept=1", out)
	}
	if len(c.got) != 5 {
		t.Fatalf("compactor was handed %d rows, want the whole active path (5)", len(c.got))
	}
	if st.start != out.Start || st.summary != "the middle" {
		t.Fatalf("boundary = (%q, %q), want (%q, %q)", st.start, st.summary, out.Start, "the middle")
	}
	want := "folded 4 messages into a summary - the context now starts at message 5, and the last 1 stay verbatim"
	if got := CompactLine(out); got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

// TestFoldSessionRefusals pins every honest no-op: each names its own reason and writes no
// boundary - a fold that keeps nothing in the middle's place is worse than no fold.
func TestFoldSessionRefusals(t *testing.T) {
	cases := []struct {
		name string
		id   string
		st   *foldStore
		c    ManualCompactor
		want string
	}{
		{"no session", "", &foldStore{path: foldPathRows(3)}, &foldFace{}, "nothing folded: no session is bound to this chat yet"},
		{"ephemeral", "eph-live", &foldStore{path: foldPathRows(3)}, &foldFace{}, "nothing folded: ephemeral sessions keep no history"},
		{"empty conversation", "s-1", &foldStore{}, &foldFace{}, "nothing folded: this conversation has no messages yet"},
		{"no compactor", "s-1", &foldStore{path: foldPathRows(3)}, nil, "nothing folded: the compaction module is not mounted"},
		{"nothing to summarize", "s-1", &foldStore{path: foldPathRows(3)}, &foldFace{}, "nothing folded: the compactor had nothing to summarize (a short conversation, or compaction is off)"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out, err := FoldSession(context.Background(), tt.st, tt.c, tt.id)
			if err != nil {
				t.Fatalf("FoldSession: %v", err)
			}
			if out.Folded {
				t.Fatalf("outcome = %+v, want a refusal", out)
			}
			if got := CompactLine(out); got != tt.want {
				t.Fatalf("line = %q, want %q", got, tt.want)
			}
			if tt.st.writes != 0 {
				t.Fatalf("a refused fold wrote %d boundaries", tt.st.writes)
			}
		})
	}
}

// TestFoldSessionNeedsABoundarySurface pins the capability gap as a typed sentinel: the
// HTTP dispatch answers 501 on it, the CLI prints its error - neither invents a result.
func TestFoldSessionNeedsABoundarySurface(t *testing.T) {
	_, err := FoldSession(context.Background(), fakeStore{}, &foldFace{}, "s-1")
	if !errors.Is(err, ErrNoSessionContext) {
		t.Fatalf("err = %v, want ErrNoSessionContext", err)
	}
}

// TestFoldSessionSurfacesTheCompactorError pins the failure lane: a dead upstream is the
// caller's error to name, and it writes no boundary.
func TestFoldSessionSurfacesTheCompactorError(t *testing.T) {
	st := &foldStore{path: foldPathRows(3)}
	_, err := FoldSession(context.Background(), st, &foldFace{err: errors.New("compactor: upstream is down")}, "s-1")
	if err == nil || !strings.Contains(err.Error(), "upstream is down") {
		t.Fatalf("err = %v, want the upstream's own failure", err)
	}
	if st.writes != 0 {
		t.Fatalf("a failed fold wrote %d boundaries", st.writes)
	}
}

// TestFoldContextAppliesTheBoundary pins the OTHER half of the fold: a host that sends its
// own history only honours the boundary when it rebuilds the context this way.
func TestFoldContextAppliesTheBoundary(t *testing.T) {
	st := &foldStore{
		row:  core.Session{ContextStart: "3", ContextSummary: "the middle"},
		path: foldPathRows(4),
	}
	msgs, err := FoldContext(context.Background(), st, "s-1")
	if err != nil {
		t.Fatalf("FoldContext: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("context = %d rows, want the summary plus the kept tail (3)", len(msgs))
	}
	sys, ok := msgs[0].(core.System)
	if !ok || sys.Content != core.ContextFoldPrefix+"the middle"+core.ContextFoldSuffix {
		t.Fatalf("head = %#v, want the fold summary", msgs[0])
	}
	for _, m := range msgs[1:] {
		if _, ok := m.(core.User); !ok {
			t.Fatalf("tail carries %#v, want only the kept user rows", m)
		}
	}
	// No boundary, no summary: the context is the whole path.
	plain, err := FoldContext(context.Background(), &foldStore{path: foldPathRows(4)}, "s-1")
	if err != nil {
		t.Fatalf("FoldContext: %v", err)
	}
	if len(plain) != 4 {
		t.Fatalf("unfolded context = %d rows, want 4", len(plain))
	}
}
