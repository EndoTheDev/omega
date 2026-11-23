package store

// jsonl_crash_test.go: the crash-mid-append contract. A process killed between the
// payload and its newline leaves a torn final line; readers drop it, boot recovery cuts
// it, and the session keeps every complete line exactly as written.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// tornAppend writes a fragment with no trailing newline, exactly what an interrupted
// single write leaves behind.
func tornAppend(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open for torn append: %v", err)
	}
	if _, err := f.WriteString(`{"v":1,"kind":"message","session":"torn",`); err != nil {
		t.Fatalf("torn append: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close torn append: %v", err)
	}
}

func TestTornLastLineIsToleratedAndDropped(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "torn.db")
	ctx := context.Background()
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.CreateSession(ctx, "torn", "", "torn"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AppendMessage(ctx, "torn", core.NewUser("first complete line")); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := s.AppendMessage(ctx, "torn", core.NewAssistant("second complete line")); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	path, err := s.sessionPath("torn")
	if err != nil {
		t.Fatalf("session path: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	tornAppend(t, path)

	// A reader tolerates the fragment: the fold ends at the last complete line and says
	// so, instead of failing the whole session.
	folded, err := foldFile(path)
	if err != nil {
		t.Fatalf("fold with a torn tail: %v", err)
	}
	if !folded.torn {
		t.Fatal("fold did not report the torn tail")
	}
	if len(folded.messages) != 2 {
		t.Fatalf("folded %d messages, want the 2 complete lines", len(folded.messages))
	}

	// Boot recovery drops the fragment, so the next append lands on a line boundary.
	s, err = Open(dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("recovery left a torn tail on disk: %q", tail(string(data), 60))
	}
	after, err := foldFile(path)
	if err != nil {
		t.Fatalf("fold after recovery: %v", err)
	}
	if after.torn {
		t.Fatal("the file still folds with a torn tail after recovery")
	}
	if len(after.messages) != 2 {
		t.Fatalf("recovery kept %d messages, want 2", len(after.messages))
	}
	// The session still works: the next line is a normal append.
	if err := s.AppendMessage(ctx, "torn", core.NewUser("after the crash")); err != nil {
		t.Fatalf("append after recovery: %v", err)
	}
	msgs, err := s.GetMessages(ctx, "torn")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
}

// TestCrashBetweenFileAndIndexRepairsOnBoot: the file append committed, the index write
// never ran. The next open re-indexes the grown bytes and the message is searchable.
func TestCrashBetweenFileAndIndexRepairsOnBoot(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "crash.db")
	ctx := context.Background()
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.CreateSession(ctx, "crash", "", "crash"); err != nil {
		t.Fatalf("create: %v", err)
	}
	path, err := s.sessionPath("crash")
	if err != nil {
		t.Fatalf("session path: %v", err)
	}
	// The line is in the file; the derived index is untouched (the "crash window").
	if _, err := appendLines(path, fileLine{
		V: jsonlVersion, Kind: kindMessage, Session: "crash", ID: 1,
		Role: "user", Payload: []byte(`{"content":"survives the crash window","timestamp":"2026-11-23T00:00:00Z"}`),
		At:   "2026-11-23T00:00:00Z",
	}); err != nil {
		t.Fatalf("append line: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM messages`); err != nil {
		t.Fatalf("drop indexed messages: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s, err = Open(dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	msgs, err := s.GetMessages(ctx, "crash")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want the one line the file holds", len(msgs))
	}
	results, err := s.SearchMessages(ctx, "survives")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].SessionID != "crash" {
		t.Fatalf("boot catch-up did not re-index the grown line: %+v", results)
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
