package store

// FK-enforcement proof for the pooled-connection path. :memory: hides
// the pragma bug (SetMaxOpenConns(1) means exactly one connection);
// a file DSN keeps the real pool, so the test asserts what the live
// gate probe showed: the pragma is per-connection, and it must hold on
// EVERY pooled connection. The pre-fix shape (a one-shot
// `PRAGMA foreign_keys = ON` after open) let foreign keys stay off on
// every other pooled connection - orphaned message rows in, missed
// ON DELETE CASCADE on session delete.

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/ai"
)

func TestForeignKeyCascadeOnPooledConnections(t *testing.T) {
	dsn := t.TempDir() + "/fk-pool.db"
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open file-backed store: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := s.CreateSession(ctx, "fk-s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// 16 concurrent appends force the pool well past one connection
	// (the live-probe shape: 8 concurrent writers, 7 of them with
	// foreign_keys off).
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.AppendMessage(ctx, "fk-s1", ai.NewUser(fmt.Sprintf("wave %d", i)))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent append: %v", err)
		}
	}

	// Every pooled connection must enforce FK: an insert into messages
	// under a nonexistent session_id is refused.
	if err := s.AppendMessage(ctx, "fk-gone", ai.NewUser("orphan")); err == nil {
		t.Fatal("append under a nonexistent session succeeded - foreign key enforcement is OFF on the pool")
	}

	msgs, err := s.GetMessages(ctx, "fk-s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 16 {
		t.Fatalf("messages before delete = %d, want 16", len(msgs))
	}

	// DeleteSession relies on the ON DELETE CASCADE: no message rows
	// may survive it.
	if err := s.DeleteSession(ctx, "fk-s1"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	msgs, err = s.GetMessages(ctx, "fk-s1")
	if err != nil {
		t.Fatalf("get messages after delete: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("messages after DeleteSession = %d, want 0 (cascade did not fire)", len(msgs))
	}
}