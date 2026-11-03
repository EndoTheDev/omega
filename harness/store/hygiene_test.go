package store

// hygiene_test.go: the pass's behaviour proofs (card t_329b9312). The
// summarizer writes to the row AND the FTS index; prune deletes ONLY
// past-retention sessions (pinned exempt); vacuum leaves a zero-byte
// WAL on disk; a legacy two-column index rebuilds at open.

import (
	"github.com/EndoTheDev/omega/agent"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/llm"
)

// fakeSummary answers every consult with one fixed paragraph.
func fakeSummary(text string) *agent.FakeProvider {
	return agent.NewFakeProvider("test-model",
		core.ResponseChunk{Type: "response.chunk", Content: text},
		core.StreamEnd{Type: "stream.end", FinishReason: "stop"})
}

// newFileStore opens a store on a real file (the WAL proofs need one).
func newFileStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/hygiene.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ageSession backdates a session's updated_at (migrate() never backfills
// the summary, and the pass reads updated_at, so the queue shape is
// driven by this stamp).
func ageSession(t *testing.T, s *Store, id string, daysAgo int) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`,
		timeAgo(daysAgo), id); err != nil {
		t.Fatalf("backdate %s: %v", id, err)
	}
}

// timeAgo renders an RFC3339 stamp `days` in the past.
func timeAgo(days int) string {
	return time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
}

func TestSummarizerWritesRowAndFTS(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", "the fibonacci task"); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "s1", core.NewUser("compute the first ten fibonacci numbers")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "s1", core.Assistant{Content: "1, 1, 2, 3, 5, 8, 13, 21, 34, 55"}); err != nil {
		t.Fatal(err)
	}
	ageSession(t, s, "s1", 2) // two days idle: past the 24h floor
	rep, err := s.RunSummarizer(ctx, fakeSummary("The user asked for fibonacci numbers and got 1..55."), "", 24)
	if err != nil {
		t.Fatalf("RunSummarizer: %v", err)
	}
	if rep.Summarized != 1 || rep.Pending != 0 {
		t.Fatalf("summarized %d pending %d, want 1/0 (skipped: %v)", rep.Summarized, rep.Pending, rep.Skipped)
	}

	// The row carries it.
	sess, err := s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sess.Summary, "fibonacci") {
		t.Errorf("row summary: %q", sess.Summary)
	}
	// A summary is not activity: updated_at stayed backdated.
	if sess.UpdatedAt != timeAgo(2) {
		t.Errorf("updated_at %q moved by the summary write", sess.UpdatedAt)
	}

	// ...and the FTS index answers for the SUMMARY text (content never
	// contained the word paragraph-unique below).
	res, err := s.SearchSessions(ctx, "asked for fibonacci")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].SessionID != "s1" || !res[0].Summary {
		t.Fatalf("summary search: %+v", res)
	}

	// A young unsammarized session stays pending; a nil provider skips
	// loudly.
	if err := s.CreateSession(ctx, "s2", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "s2", core.NewUser("hi")); err != nil {
		t.Fatal(err)
	}
	rep2, err := s.RunSummarizer(ctx, nil, "", 24)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Summarized != 0 || rep2.Pending != 0 || len(rep2.Skipped) == 0 {
		t.Errorf("nil provider pass: %+v (a skip must be reported)", rep2)
	}

	// An empty session never queues: nothing to summarize.
	if err := s.CreateSession(ctx, "s3", "", ""); err != nil {
		t.Fatal(err)
	}
	ageSession(t, s, "s3", 5)
	rep3, err := s.RunSummarizer(ctx, fakeSummary("anything"), "", 24)
	if err != nil {
		t.Fatal(err)
	}
	if rep3.Summarized != 0 || rep3.Pending != 0 {
		t.Errorf("empty session summarized: %+v", rep3)
	}
}

func TestSummarizerRespectsAgeFloorAndCap(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()
	for i := 0; i < 7; i++ {
		id := "old-" + string(rune('a'+i))
		if err := s.CreateSession(ctx, id, "", ""); err != nil {
			t.Fatal(err)
		}
		if err := s.AppendMessage(ctx, id, core.NewUser("hello")); err != nil {
			t.Fatal(err)
		}
		ageSession(t, s, id, 3) // one idle day MORE than the floor
	}
	if err := s.CreateSession(ctx, "young", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "young", core.NewUser("young hello")); err != nil {
		t.Fatal(err)
	}
	prov := fakeSummary("a summary")
	rep, err := s.RunSummarizer(ctx, prov, "", 48)
	if err != nil {
		t.Fatal(err)
	}
	// The cap bounds one pass at 5; everything else is the next run's
	// backlog. The young session never enters the queue.
	if rep.Summarized != maxSummariesPerPass || rep.Pending != 2 {
		t.Fatalf("summarized %d pending %d, want 5/2 (skipped: %v)", rep.Summarized, rep.Pending, rep.Skipped)
	}
	if prov.Calls() < maxSummariesPerPass {
		t.Fatalf("consults %d < summarized %d", prov.Calls(), rep.Summarized)
	}
}

func TestPruneDeletesOnlyPastRetention(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "old", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "old", core.NewUser("old")); err != nil {
		t.Fatal(err)
	}
	ageSession(t, s, "old", 100) // past any 30d window
	if err := s.CreateSession(ctx, "pinned-old", "", ""); err != nil {
		t.Fatal(err)
	}
	ageSession(t, s, "pinned-old", 100)
	if err := s.SetSessionPinned(ctx, "pinned-old", true); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, "young", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "young", core.NewUser("young")); err != nil {
		t.Fatal(err)
	}
	ageSession(t, s, "young", 3)

	ids, err := s.PruneSessions(ctx, 30)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(ids) != 1 || ids[0] != "old" {
		t.Fatalf("pruned %v, want [old]", ids)
	}
	for id, want := range map[string]bool{"old": false, "pinned-old": true, "young": true} {
		if _, err := s.GetSession(ctx, id); want && err != nil {
			t.Errorf("%s pruned, should survive", id)
		} else if !want && err == nil {
			t.Errorf("%s survived, should be pruned", id)
		}
	}

	// 0 = never prune: no deletion, no error.
	ids, err = s.PruneSessions(ctx, 0)
	if err != nil || len(ids) != 0 {
		t.Fatalf("retention 0 pruned %v err %v", ids, err)
	}
}

func TestVacuumShrinksWALFileOnDisk(t *testing.T) {
	dsn := t.TempDir() + "/vac.db"
	s, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateSession(ctx, "big", "", ""); err != nil {
		t.Fatal(err)
	}
	// Grow the WAL: a batch of large tool results without a checkpoint
	// between them.
	big := strings.Repeat("x", 1<<20) // 1 MB
	for i := 0; i < 8; i++ {
		if err := s.AppendMessage(ctx, "big", core.NewUser(big)); err != nil {
			t.Fatal(err)
		}
	}
	if _, wal := s.DBFileSizes(); wal == 0 {
		t.Fatal("WAL file empty after bulk writes - the test premise is dead")
	}
	if err := s.RunVacuum(ctx); err != nil {
		t.Fatalf("RunVacuum: %v", err)
	}
	if _, wal := s.DBFileSizes(); wal != 0 {
		t.Fatalf("WAL file %d bytes after TRUNCATE checkpoint, want 0", wal)
	}
}

func TestLegacyTwoColumnIndexRebuildsAtOpen(t *testing.T) {
	dsn := t.TempDir() + "/legacy.db"
	// Build the PRE-summary shape by hand: a full-length sessions table
	// without the summary column and a two-column stand-alone fts table.
	legacy, err := sql.Open("sqlite", dsn+"?_pragma=foreign_keys(1)&_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(fmt.Sprintf(`
CREATE TABLE sessions (
	id         TEXT PRIMARY KEY,
	parent_id  TEXT REFERENCES sessions(id) ON DELETE CASCADE,
	label      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	pinned INTEGER NOT NULL DEFAULT 0,
	archived INTEGER NOT NULL DEFAULT 0,
	title_state INTEGER NOT NULL DEFAULT 0,
	last_read_at TEXT NOT NULL DEFAULT '',
	agent TEXT NOT NULL DEFAULT 'default',
	last_run TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	workspace TEXT NOT NULL DEFAULT ''
);
CREATE TABLE messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	role       TEXT NOT NULL,
	payload    TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE VIRTUAL TABLE messages_fts USING fts5(
	session_id UNINDEXED, payload, content='messages', content_rowid='id'
);
CREATE VIRTUAL TABLE sessions_fts USING fts5(session_id UNINDEXED, label);
CREATE TABLE image_captions (image_id TEXT PRIMARY KEY, caption TEXT, created_at TEXT);
INSERT INTO sessions (id, label, created_at, updated_at)
VALUES ('s-legacy', 'legacy label', '%s', '%s');`, timeAgo(30), timeAgo(3)))
	if err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	legacy.Close()

	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	defer s.Close()
	// The summary column landed, the index answers the new arity.
	if _, err := s.db.Exec(`UPDATE sessions SET summary = 'legacy note about syncthing setup' WHERE id = 's-legacy'`); err != nil {
		t.Fatalf("write summary to migrated row: %v", err)
	}
	res, err := s.SearchSessions(context.Background(), "syncthing")
	if err != nil {
		t.Fatalf("search after rebuild: %v", err)
	}
	if len(res) != 1 || res[0].SessionID != "s-legacy" || !res[0].Summary {
		t.Fatalf("summary search after migration: %+v", res)
	}
	// The label half survived the rebuild untouched (the old row's
	// label stays indexed).
	res, err = s.SearchSessions(context.Background(), "legacy label")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || !res[0].Title {
		t.Fatalf("label search after migration: %+v", res)
	}
}

func TestHygieneToolActions(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "a", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "a", core.NewUser("hello")); err != nil {
		t.Fatal(err)
	}
	ageSession(t, s, "a", 2)
	cfg := DefaultHygiene()
	tool := (&hygieneToolProvider{store: s, cfg: cfg, providerFn: func() agent.Provider {
		return fakeSummary("The user greeted the assistant; a greeting was returned.")
	}}).Tools()["sessions.hygiene"]

	out, err := tool.Run(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "hygiene: ") || !strings.Contains(out, "1 summarized") {
		t.Errorf("full-pass report: %q", out)
	}

	// An unknown action is refused, not silently run as the full pass.
	if _, err := tool.Run(ctx, map[string]any{"action": "reset"}); err == nil {
		t.Error("unknown action accepted")
	}

	// The summarize-only action answers a second run: the queue is
	// already clear, so nothing is summarized and pending stays 0.
	out, err = tool.Run(ctx, map[string]any{"action": "summarize"})
	if err != nil {
		t.Fatalf("summarize action: %v", err)
	}
	if !strings.Contains(out, "0 summarized") {
		t.Errorf("second summarize: %q", out)
	}
}

func TestInsightsCarriesFileSizes(t *testing.T) {
	s := newFileStore(t)
	ctx := context.Background()
	in, err := s.ComputeInsights(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if in.DBSizeBytes <= 0 {
		t.Errorf("insights db size %d", in.DBSizeBytes)
	}
	// A WAL that exists after the migrate writes shows up too; do not
	// pin the value (checkpoint timing), only that the db file reported.
}
