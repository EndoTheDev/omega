package store

// store_write_guard_test.go: the write side's own guards, now that the session FILES - not
// foreign keys - are the truth. Two laws: a message never lands in a session that does not
// exist, and a delete takes the file with it. Both are exercised on a file-backed store,
// where a real connection pool sits behind the index.

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

func TestWritesAreGuardedAndDeleteTakesTheFile(t *testing.T) {
	dsn := t.TempDir() + "/guards.db"
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open file-backed store: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := s.CreateSession(ctx, "guard-1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// 16 concurrent appends: the file append and the index fold share one store lock, and
	// the pooled index connection must keep up.
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.AppendMessage(ctx, "guard-1", core.NewUser(fmt.Sprintf("wave %d", i)))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent append: %v", err)
		}
	}

	// A session that does not exist is refused: no file, no row, no silent orphan.
	if err := s.AppendMessage(ctx, "guard-gone", core.NewUser("orphan")); err == nil {
		t.Fatal("append under a nonexistent session succeeded")
	}
	if s.sessionFileExists("guard-gone") {
		t.Fatal("the refused append created a session file")
	}
	// Lineage is refused the same way: a child cannot name a parent that is not there.
	if err := s.CreateSession(ctx, "guard-child", "guard-gone", ""); err == nil {
		t.Fatal("a session with a missing fork parent was created")
	}

	msgs, err := s.GetMessages(ctx, "guard-1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 16 {
		t.Fatalf("messages before delete = %d, want 16", len(msgs))
	}

	// DeleteSession takes the file: the transcript cannot come back on the next boot.
	if err := s.DeleteSession(ctx, "guard-1"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if s.sessionFileExists("guard-1") {
		t.Fatal("the session file survived DeleteSession")
	}
	msgs, err = s.GetMessages(ctx, "guard-1")
	if err != nil {
		t.Fatalf("get messages after delete: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("messages after DeleteSession = %d, want 0", len(msgs))
	}
}
