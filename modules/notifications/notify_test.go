package notifications

import (
	"context"
	"fmt"
	"sync"
	"time"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestEmitFillsAndValidates(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	out, err := s.Emit(core.Notification{Kind: "task", SourceID: "t_abc", Title: "task t_abc done"})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if out.ID == 0 {
		t.Fatalf("emit did not fill id: %+v", out)
	}
	if out.Verdict != "ok" || out.CreatedAt == "" || out.Read {
		t.Fatalf("emit defaults wrong: %+v", out)
	}

	// verdict "" normalizes, failed survives, garbage kind and verdict reject
	if _, err := s.Emit(core.Notification{Kind: "cron", SourceID: "nightly", Verdict: "failed"}); err != nil {
		t.Fatalf("emit failed verdict: %v", err)
	}
	if _, err := s.Emit(core.Notification{Kind: "email", SourceID: "x"}); err == nil {
		t.Fatalf("bogus kind accepted")
	}
	if _, err := s.Emit(core.Notification{Kind: "task", SourceID: "x", Verdict: "maybe"}); err == nil {
		t.Fatalf("bogus verdict accepted")
	}
	if _, err := s.Emit(core.Notification{Kind: "task", SourceID: "  "}); err == nil {
		t.Fatalf("empty source_id accepted")
	}

	notes, unread, err := s.List(ctx, false, 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != 2 || unread != 2 {
		t.Fatalf("list = %d notes, %d unread, want 2/2", len(notes), unread)
	}
	if notes[0].Kind != "cron" || notes[0].Verdict != "failed" {
		t.Fatalf("newest-first violated: %+v", notes[0])
	}
}

func TestListUnreadFilterAndCount(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for i := range 3 {
		if _, err := s.Emit(core.Notification{Kind: "chat", SourceID: fmt.Sprintf("task-%d", i)}); err != nil {
			t.Fatalf("emit: %v", err)
		}
	}
	if err := s.MarkRead(ctx, 2); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	notes, unread, err := s.List(ctx, true, 0, 0)
	if err != nil {
		t.Fatalf("list unread: %v", err)
	}
	if len(notes) != 2 || unread != 2 {
		t.Fatalf("unread list = %d rows, count %d, want 2/2", len(notes), unread)
	}
	for _, n := range notes {
		if n.Read {
			t.Fatalf("unread filter leaked a read row: %+v", n)
		}
	}
	// the full list still carries everything; count stays a whole-set number
	notes, unread, _ = s.List(ctx, false, 0, 0)
	if len(notes) != 3 || unread != 2 {
		t.Fatalf("full list = %d rows, count %d, want 3/2", len(notes), unread)
	}

	// pagination: limit+offset page through newest first
	notes, _, _ = s.List(ctx, false, 1, 1)
	if len(notes) != 1 || notes[0].ID != 2 {
		t.Fatalf("page limit=1 offset=1 = %+v, want id 2", notes)
	}
}

func TestMarkReadAndAll(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	s.Emit(core.Notification{Kind: "cron", SourceID: "j"})
	s.Emit(core.Notification{Kind: "cron", SourceID: "k"})

	if err := s.MarkRead(ctx, 42); err == nil {
		t.Fatalf("mark read on a ghost id must error")
	}
	if err := s.MarkAllRead(ctx); err != nil {
		t.Fatalf("mark all: %v", err)
	}
	_, unread, _ := s.List(ctx, false, 0, 0)
	if unread != 0 {
		t.Fatalf("unread after mark all = %d, want 0", unread)
	}
}

func TestRetentionKeeps200(t *testing.T) {
	s := testStore(t)
	for i := range retention + 5 {
		if _, err := s.Emit(core.Notification{Kind: "task", SourceID: fmt.Sprintf("t_%03d", i)}); err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}
	notes, unread, err := s.List(context.Background(), false, retention, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != retention || unread != retention {
		t.Fatalf("retention: %d rows / %d unread, want %d", len(notes), unread, retention)
	}
	// the OLDEST pruned, the newest kept: id 0 is gone, id 204 lives
	if notes[0].SourceID != fmt.Sprintf("t_%03d", retention+4) {
		t.Fatalf("newest pruned: %+v", notes[0])
	}
	for _, n := range notes {
		if n.SourceID == "t_000" {
			t.Fatalf("oldest record survived retention")
		}
	}
}

func TestSubscribeBroadcastAndCancel(t *testing.T) {
	s := testStore(t)
	ch, cancel := s.Subscribe()
	defer cancel()

	go func() { _, _ = s.Emit(core.Notification{Kind: "chat", SourceID: "task-1"}) }()

	select {
	case n := <-ch:
		if n.SourceID != "task-1" || n.Kind != "chat" {
			t.Fatalf("broadcast carried the wrong record: %+v", n)
		}
	case <-time.After(2*time.Second):
		t.Fatalf("no broadcast within 2s")
	}

	// cancel is idempotent and releases the subscription; a later emit
	// must not reach the closed channel
	cancel()
	cancel()
	if _, err := s.Emit(core.Notification{Kind: "chat", SourceID: "task-2"}); err != nil {
		t.Fatalf("emit after cancel: %v", err)
	}
}

func TestSubscriberOverflowDropsNotBlocks(t *testing.T) {
	s := testStore(t)
	ch, cancel := s.Subscribe()
	defer cancel()

	// flood past the buffer: emission must never block on a slow reader
	for i := range subscriberBuf * 4 {
		s.Emit(core.Notification{Kind: "task", SourceID: fmt.Sprintf("t_%d", i)})
	}
	// the reader drains what survived; the store never wedged
	deadline := time.After(time.Second)
	for range 3 {
		select {
		case <-ch:
		case <-deadline:
			return // whatever still sits in the buffer stays; delivery is best-effort
		}
	}
}

func TestEmitConcurrent(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := range 10 {
				if _, err := s.Emit(core.Notification{Kind: "cron", SourceID: fmt.Sprintf("job-%d-%d", i, j)}); err != nil {
					t.Errorf("emit: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()
	notes, unread, _ := s.List(context.Background(), false, 100, 0)
	if len(notes) != 80 || unread != 80 {
		t.Fatalf("after 80 concurrent emits = %d rows / %d unread", len(notes), unread)
	}
}

// TestMigrationIdempotent re-opens a file-backed store: the schema
// creation is IF NOT EXISTS everywhere, an existing db keeps its rows.
func TestMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/data/notifications.db"
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("open 1: %v", err)
	}
	if _, err := s1.Emit(core.Notification{Kind: "task", SourceID: "t_one"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	s1.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("open 2: %v", err)
	}
	defer s2.Close()
	notes, _, err := s2.List(context.Background(), false, 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != 1 || notes[0].SourceID != "t_one" {
		t.Fatalf("reopen lost the row: %+v", notes)
	}
}