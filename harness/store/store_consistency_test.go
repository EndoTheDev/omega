package store

// store_consistency_test.go: the append/catch-up boundary. Three properties the store
// cannot lose, each pinned by the failure it prevents:
//
//	the index path never cuts a session file (a search during a run must not delete a line
//	    the append already acknowledged);
//	message ids come from the files, not the index (a failed apply must not hand out an id
//	    a file already holds);
//	delete follows the files' lineage (a child whose derived row never landed must not
//	    survive its parent as an orphan).
//
// The concurrency test is deliberately modest: it is a witness that the two paths run at
// the same time without losing a line, not a stress harness.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

func TestCatchUpNeverCutsASessionFile(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "nocut.db")
	ctx := context.Background()
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.CreateSession(ctx, "nocut", "", "nocut"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AppendMessage(ctx, "nocut", core.NewUser("first complete line")); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := s.AppendMessage(ctx, "nocut", core.NewUser("second complete line")); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	path, err := s.sessionPath("nocut")
	if err != nil {
		t.Fatalf("session path: %v", err)
	}
	// A tail that appears while the store is live belongs to an append in flight, not to a
	// crash: the store has no way to tell them apart from the bytes alone, so the catch-up a
	// search triggers must leave the file exactly as it found it.
	tornAppend(t, path)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if _, err := s.SearchMessages(ctx, "complete"); err != nil {
		t.Fatalf("search: %v", err)
	}
	if _, err := s.SearchSessions(ctx, "nocut"); err != nil {
		t.Fatalf("session search: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("catch-up cut the file: %d -> %d bytes - the index path must never write a session file",
			before.Size(), after.Size())
	}
	msgs, err := s.GetMessages(ctx, "nocut")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want the 2 complete lines", len(msgs))
	}
}

func TestMessageIDsComeFromTheTruth(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "ids.db")
	ctx := context.Background()
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.CreateSession(ctx, "ids", "", "ids"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AppendMessage(ctx, "ids", core.NewUser("first line")); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	path, err := s.sessionPath("ids")
	if err != nil {
		t.Fatalf("session path: %v", err)
	}
	// The crash window, from the id allocator's side: the line is in the file, the derived
	// row never landed, and the watermark never moved. The next append must still see the
	// ids the file holds.
	if _, err := s.db.Exec(`DELETE FROM messages`); err != nil {
		t.Fatalf("drop derived messages: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM session_files`); err != nil {
		t.Fatalf("drop watermark: %v", err)
	}
	if err := s.AppendMessage(ctx, "ids", core.NewAssistant("second line")); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	f, err := foldFile(path)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if len(f.messages) != 2 {
		t.Fatalf("file holds %d message lines, want 2", len(f.messages))
	}
	if f.messages[0].ID == f.messages[1].ID {
		t.Fatalf("the append reused id %d that the file already holds", f.messages[0].ID)
	}
	// Both lines reach the index on the next catch-up, which is the whole point of an id
	// that was never reused.
	for _, want := range []string{"first", "second"} {
		results, serr := s.SearchMessages(ctx, want)
		if serr != nil {
			t.Fatalf("search %q: %v", want, serr)
		}
		if len(results) != 1 || results[0].SessionID != "ids" {
			t.Fatalf("search %q = %+v, want the session's line", want, results)
		}
	}
}

func TestConcurrentAppendAndSearchLoseNoLine(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "race.db")
	ctx := context.Background()
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.CreateSession(ctx, "race", "", "race"); err != nil {
		t.Fatalf("create: %v", err)
	}
	const lines = 24
	pad := strings.Repeat("x", 2048)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, serr := s.SearchMessages(ctx, "payload"); serr != nil {
				t.Errorf("concurrent search: %v", serr)
				return
			}
		}
	}()
	for i := 0; i < lines; i++ {
		msg := core.NewUser(fmt.Sprintf("payload %d %s", i, pad))
		if err := s.AppendMessage(ctx, "race", msg); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("append %d: %v", i, err)
		}
	}
	// Let the searcher overlap the tail of the run too, then stop it.
	time.Sleep(20 * time.Millisecond)
	close(stop)
	wg.Wait()

	path, err := s.sessionPath("race")
	if err != nil {
		t.Fatalf("session path: %v", err)
	}
	f, err := foldFile(path)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if f.torn {
		t.Fatal("the run left a torn tail behind")
	}
	if len(f.messages) != lines {
		t.Fatalf("file holds %d message lines, want %d - a line was lost", len(f.messages), lines)
	}
	seen := make(map[int64]bool, len(f.messages))
	for _, m := range f.messages {
		if seen[m.ID] {
			t.Fatalf("message id %d appears twice", m.ID)
		}
		seen[m.ID] = true
	}
	// The index agrees with the file once the run is quiet.
	if _, err := s.SearchMessages(ctx, "payload"); err != nil {
		t.Fatalf("final search: %v", err)
	}
	var indexed int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = 'race'`).Scan(&indexed); err != nil {
		t.Fatalf("count: %v", err)
	}
	if indexed != lines {
		t.Fatalf("index holds %d message rows, want %d", indexed, lines)
	}
}

func TestRenameKeepsPayloadBytesVerbatim(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "verbatim.db")
	ctx := context.Background()
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.CreateSession(ctx, "raw", "", "raw"); err != nil {
		t.Fatalf("create: %v", err)
	}
	path, err := s.sessionPath("raw")
	if err != nil {
		t.Fatalf("session path: %v", err)
	}
	// A stored payload is the codec's bytes, whatever they contain - including the raw
	// markup characters an encoder that escapes would rewrite. Rename must not be the one
	// writer that rewrites them.
	body := "<b>a & b</b> > c"
	if _, _, err := appendLines(path, fileLine{
		V: jsonlVersion, Kind: kindMessage, Session: "raw", ID: 1, Role: "user",
		Payload: []byte(`{"content":"` + body + `","timestamp":"2026-11-23T00:00:00Z"}`),
		At:      "2026-11-23T00:00:00Z",
	}); err != nil {
		t.Fatalf("append raw payload: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := RenameSession(dsn, "raw", "raw-renamed"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(sessionsDirFor(dsn), "raw-renamed.jsonl"))
	if err != nil {
		t.Fatalf("read renamed file: %v", err)
	}
	if !strings.Contains(string(data), body) {
		t.Fatalf("the rename rewrote the payload bytes:\n%s", data)
	}
	if strings.Contains(string(data), `\u003c`) || strings.Contains(string(data), `\u0026`) {
		t.Fatalf("the rename HTML-escaped the payload:\n%s", data)
	}
	s, err = Open(dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	msgs, err := s.GetMessages(ctx, "raw-renamed")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("read-back returned %d messages, want 1", len(msgs))
	}
	u, ok := msgs[0].(core.User)
	if !ok || u.Content != body {
		t.Fatalf("read-back lost the payload: %+v", msgs[0])
	}
}

func TestDeleteFollowsTheFilesLineage(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "orphan.db")
	ctx := context.Background()
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.CreateSession(ctx, "parent", "", "parent"); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if err := s.CreateSession(ctx, "child", "parent", "child"); err != nil {
		t.Fatalf("create child: %v", err)
	}
	// The child's derived row never landed. Its file still names the parent, so the delete
	// has to walk the truth - an index walk would leave the child behind to resurrect.
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE id = 'child'`); err != nil {
		t.Fatalf("drop child row: %v", err)
	}
	if err := s.DeleteSession(ctx, "parent"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, id := range []string{"parent", "child"} {
		path, perr := s.sessionPath(id)
		if perr != nil {
			t.Fatalf("session path %s: %v", id, perr)
		}
		if _, serr := os.Stat(path); !os.IsNotExist(serr) {
			t.Fatalf("session file %q survived its parent's delete", id)
		}
	}
	if _, err := s.SearchMessages(ctx, "child"); err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	var rows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id IN ('parent','child')`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 0 {
		t.Fatalf("derived rows survived the delete: %d", rows)
	}
}
