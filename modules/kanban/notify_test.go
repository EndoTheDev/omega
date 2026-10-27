package kanban

import (
	"context"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// fakeNotifier records Emit calls (core.NotificationsProvider).
type fakeNotifier struct {
	mu  sync.Mutex
	got []core.Notification
}

func (f *fakeNotifier) Emit(n core.Notification) (core.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n.ID = int64(len(f.got) + 1)
	n.CreatedAt = "2026-10-22T04:00:00Z"
	f.got = append(f.got, n)
	return n, nil
}
func (f *fakeNotifier) List(ctx context.Context, unreadOnly bool, limit, offset int) ([]core.Notification, int, error) {
	return nil, 0, nil
}
func (f *fakeNotifier) MarkRead(ctx context.Context, id int64) error { return nil }
func (f *fakeNotifier) MarkAllRead(ctx context.Context) error        { return nil }
func (f *fakeNotifier) Subscribe() (<-chan core.Notification, func()) {
	ch := make(chan core.Notification, 4)
	return ch, func() {}
}

// card t_5d9032d3: a task that ENTERS done emits one finished-things
// record; moving through every other status path emits none.
func TestUpdateTaskEmitsOnEnterDone(t *testing.T) {
	s := testStore(t)
	n := &fakeNotifier{}
	s.SetNotifier(n)
	ctx := context.Background()

	in, err := s.CreateTask(ctx, "", KanbanTask{Title: "wire the drawer"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// a non-done move emits nothing
	if _, err := s.UpdateTask(ctx, "", in.ID, KanbanPatch{Status: "running"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	n.mu.Lock()
	count := len(n.got)
	n.mu.Unlock()
	if count != 0 {
		t.Fatalf("a running move emitted %d records", count)
	}

	// entering done emits exactly one, metadata only
	if _, err := s.UpdateTask(ctx, "", in.ID, KanbanPatch{Status: StatusDone}); err != nil {
		t.Fatalf("update to done: %v", err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.got) != 1 {
		t.Fatalf("entering done emitted %d records, want 1", len(n.got))
	}
	rec := n.got[0]
	if rec.Kind != "task" || rec.SourceID != in.ID || rec.Verdict != "ok" {
		t.Fatalf("record wrong: %+v", rec)
	}
	if rec.Title != "task "+in.ID+" done" {
		t.Fatalf("title carries more than metadata: %q", rec.Title)
	}

	// a second done move (already settled, completed_at untouched)
	// emits nothing - one transition, one record
	if _, err := s.UpdateTask(ctx, "", in.ID, KanbanPatch{Status: StatusDone}); err != nil {
		t.Fatalf("re-close: %v", err)
	}
	count = len(n.got)
	if count != 1 {
		t.Fatalf("re-closing emitted again: %d records", count)
	}
}