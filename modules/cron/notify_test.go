package cron

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// fakeNotifier records Emit calls (core.NotificationsProvider).
type fakeNotifier struct {
	mu    sync.Mutex
	got   []core.Notification
	subCh chan core.Notification
}

func (f *fakeNotifier) Emit(n core.Notification) (core.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n.ID = int64(len(f.got) + 1)
	n.CreatedAt = "2026-10-22T04:00:00Z"
	f.got = append(f.got, n)
	if f.subCh != nil {
		select {
		case f.subCh <- n:
		default:
		}
	}
	return n, nil
}
func (f *fakeNotifier) List(ctx context.Context, unreadOnly bool, limit, offset int) ([]core.Notification, int, error) {
	return nil, 0, nil
}
func (f *fakeNotifier) MarkRead(ctx context.Context, id int64) error { return nil }
func (f *fakeNotifier) MarkAllRead(ctx context.Context) error        { return nil }
func (f *fakeNotifier) Subscribe() (<-chan core.Notification, func()) {
	ch := make(chan core.Notification, 16)
	f.subCh = ch
	return ch, func() {}
}

// --- fire emission (card t_5d9032d3) ---

func TestFireEmitsNotificationOkAndFailed(t *testing.T) {
	r := newTestRunner(t, t.TempDir(), Job{Name: "nightly", Schedule: "0 3 * * *", Prompt: "p"})
	n := &fakeNotifier{}
	r.notifier = n
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		return "final", nil
	}
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())

	n.mu.Lock()
	if len(n.got) != 1 {
		t.Fatalf("ok fire emitted %d records, want 1", len(n.got))
	}
	got := n.got[0]
	n.mu.Unlock()
	if got.Kind != "cron" || got.SourceID != "nightly" || got.Verdict != "ok" {
		t.Fatalf("ok record wrong: %+v", got)
	}
	if got.Title != "cron nightly finished" {
		t.Fatalf("title carries more than metadata: %q", got.Title)
	}

	// a failed run flips the verdict; job name stays the source id
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		return "", errors.New("provider dead")
	}
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.got) != 2 || n.got[1].Verdict != "failed" {
		t.Fatalf("failed fire emitted %+v", n.got)
	}
}

func TestFireWithoutNotifierNeverBlocks(t *testing.T) {
	r := newTestRunner(t, t.TempDir(), Job{Name: "solo", Schedule: "0 3 * * *", Prompt: "p"})
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		return "final", nil
	}
	// nil notifier: emission is a no-op, fire still completes
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())
}