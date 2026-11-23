package agent

// sessioncompact_test.go: the manual compact has ONE implementation and ONE wording. These
// pins hold the shared helper both hosts call (the CLI loop and the HTTP dispatch), so
// neither surface can answer with silence or drift into a second phrasing.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// compactStore is the session shape a manual compact needs: a session row (with its
// boundary), an active path, and a recorder for the boundary write. A refusal must write
// NOTHING, so every test can prove that from writes.
type compactStore struct {
	fakeStore // the full no-op StoreProvider seam
	row       core.Session
	path      []core.StoredMessage
	start     string
	summary   string
	writes    int
}

func (s *compactStore) GetSession(context.Context, string) (core.Session, error) { return s.row, nil }
func (s *compactStore) GetActiveView(context.Context, string) (core.ActiveView, error) {
	return core.ActiveView{Path: s.path}, nil
}
func (s *compactStore) SetSessionModel(context.Context, string, string) error     { return nil }
func (s *compactStore) SetSessionProvider(context.Context, string, string) error  { return nil }
func (s *compactStore) SetSessionWorkspace(context.Context, string, string) error { return nil }
func (s *compactStore) SetSessionContextStart(_ context.Context, _ string, start, summary string) error {
	s.writes++
	s.start, s.summary = start, summary
	return nil
}

// compactFace is the capsule's manual compact face: a fixed answer, and the rows it was
// handed.
type compactFace struct {
	out []core.Message
	err error
	got []core.Message
}

func (f *compactFace) CompactNow(_ context.Context, msgs []core.Message) ([]core.Message, error) {
	f.got = msgs
	return f.out, f.err
}

// compactPathRows seeds n user rows with store ids 1..n.
func compactPathRows(n int) []core.StoredMessage {
	out := make([]core.StoredMessage, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, core.StoredMessage{ID: int64(i), Message: core.NewUser("row")})
	}
	return out
}

// TestCompactSessionCompactsTheHead pins the compact: the boundary sits on the first row
// the compactor kept verbatim, the summary is written with it, and the count in the line is
// the number of rows the compact removed from the live context.
func TestCompactSessionCompactsTheHead(t *testing.T) {
	st := &compactStore{path: compactPathRows(5)}
	c := &compactFace{out: []core.Message{
		core.NewSystem(core.ContextCompactPrefix + "the middle" + core.ContextCompactSuffix),
		core.NewUser("row"),
	}}
	out, err := CompactSession(context.Background(), st, c, "s-1")
	if err != nil {
		t.Fatalf("CompactSession: %v", err)
	}
	if !out.Compacted || out.Start != "5" || out.Rows != 4 || out.Kept != 1 {
		t.Fatalf("outcome = %+v, want compacted start=5 rows=4 kept=1", out)
	}
	if len(c.got) != 5 {
		t.Fatalf("compactor was handed %d rows, want the whole active path (5)", len(c.got))
	}
	if st.start != out.Start || st.summary != "the middle" {
		t.Fatalf("boundary = (%q, %q), want (%q, %q)", st.start, st.summary, out.Start, "the middle")
	}
	want := "compacted 4 messages into a summary - the context now starts at message 5, and the last 1 stay verbatim"
	if got := CompactLine(out); got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

// TestCompactSessionRefusals pins every honest no-op: each names its own reason and writes
// no boundary - a compact that keeps nothing in the middle's place is worse than no compact.
func TestCompactSessionRefusals(t *testing.T) {
	cases := []struct {
		name string
		id   string
		st   *compactStore
		c    ManualCompactor
		want string
	}{
		{"no session", "", &compactStore{path: compactPathRows(3)}, &compactFace{}, "nothing compacted: no session is bound to this chat yet"},
		{"ephemeral", "eph-live", &compactStore{path: compactPathRows(3)}, &compactFace{}, "nothing compacted: ephemeral sessions keep no history"},
		{"empty conversation", "s-1", &compactStore{}, &compactFace{}, "nothing compacted: this conversation has no messages yet"},
		{"no compactor", "s-1", &compactStore{path: compactPathRows(3)}, nil, "nothing compacted: the compaction module is not mounted"},
		{"nothing to summarize", "s-1", &compactStore{path: compactPathRows(3)}, &compactFace{}, "nothing compacted: the compactor had nothing to summarize (a short conversation, or compaction is off)"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out, err := CompactSession(context.Background(), tt.st, tt.c, tt.id)
			if err != nil {
				t.Fatalf("CompactSession: %v", err)
			}
			if out.Compacted {
				t.Fatalf("outcome = %+v, want a refusal", out)
			}
			if got := CompactLine(out); got != tt.want {
				t.Fatalf("line = %q, want %q", got, tt.want)
			}
			if tt.st.writes != 0 {
				t.Fatalf("a refused compact wrote %d boundaries", tt.st.writes)
			}
		})
	}
}

// TestCompactSessionNeedsABoundarySurface pins the capability gap as a typed sentinel: the
// HTTP dispatch answers 501 on it, the CLI prints its error - neither invents a result.
func TestCompactSessionNeedsABoundarySurface(t *testing.T) {
	_, err := CompactSession(context.Background(), fakeStore{}, &compactFace{}, "s-1")
	if !errors.Is(err, ErrNoSessionContext) {
		t.Fatalf("err = %v, want ErrNoSessionContext", err)
	}
}

// TestCompactSessionSurfacesTheCompactorError pins the failure lane: a dead upstream is the
// caller's error to name, and it writes no boundary.
func TestCompactSessionSurfacesTheCompactorError(t *testing.T) {
	st := &compactStore{path: compactPathRows(3)}
	_, err := CompactSession(context.Background(), st, &compactFace{err: errors.New("compactor: upstream is down")}, "s-1")
	if err == nil || !strings.Contains(err.Error(), "upstream is down") {
		t.Fatalf("err = %v, want the upstream's own failure", err)
	}
	if st.writes != 0 {
		t.Fatalf("a failed compact wrote %d boundaries", st.writes)
	}
}

// TestCompactContextAppliesTheBoundary pins the OTHER half of the compact: a host that sends
// its own history only honours the boundary when it rebuilds the context this way.
func TestCompactContextAppliesTheBoundary(t *testing.T) {
	st := &compactStore{
		row:  core.Session{ContextStart: "3", ContextSummary: "the middle"},
		path: compactPathRows(4),
	}
	msgs, err := CompactContext(context.Background(), st, "s-1")
	if err != nil {
		t.Fatalf("CompactContext: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("context = %d rows, want the summary plus the kept tail (3)", len(msgs))
	}
	sys, ok := msgs[0].(core.System)
	if !ok || sys.Content != core.ContextCompactPrefix+"the middle"+core.ContextCompactSuffix {
		t.Fatalf("head = %#v, want the compact summary", msgs[0])
	}
	for _, m := range msgs[1:] {
		if _, ok := m.(core.User); !ok {
			t.Fatalf("tail carries %#v, want only the kept user rows", m)
		}
	}
	// No boundary, no summary: the context is the whole path.
	plain, err := CompactContext(context.Background(), &compactStore{path: compactPathRows(4)}, "s-1")
	if err != nil {
		t.Fatalf("CompactContext: %v", err)
	}
	if len(plain) != 4 {
		t.Fatalf("uncompacted context = %d rows, want 4", len(plain))
	}
}
