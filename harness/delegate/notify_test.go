package delegate

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// recordingNotifier captures the finished-things emissions (card
// t_5d9032d3).
type recordingNotifier struct {
	got []core.Notice
}

func (r *recordingNotifier) Emit(n core.Notice) (core.Notice, error) {
	n.ID = int64(len(r.got) + 1)
	n.CreatedAt = "2026-10-22T04:00:00Z"
	r.got = append(r.got, n)
	return n, nil
}
func (r *recordingNotifier) List(ctx context.Context, unreadOnly bool, limit, offset int) ([]core.Notice, int, error) {
	return nil, 0, nil
}
func (r *recordingNotifier) MarkRead(ctx context.Context, id int64) error { return nil }
func (r *recordingNotifier) MarkAllRead(ctx context.Context) error        { return nil }
func (r *recordingNotifier) Subscribe() (<-chan core.Notice, func()) {
	ch := make(chan core.Notice, 4)
	return ch, func() {}
}

// card t_5d9032d3: a dispatched run's finish emits one chat record -
// ok on a clean exit, failed on error, metadata-only title. The
// emission rides the blocking wait now (card t_0805d438): the tool run
// itself completes the task, then emits.
func TestRunEmitsNotification(t *testing.T) {
	bin := writeStub(t, "stub-notify-omega", "done output", 0)
	t.Setenv("OMEGA_BIN", bin)

	d := NewDelegate()
	n := &recordingNotifier{}
	d.notifier = n

	if _, err := d.runDelegateTask(context.Background(), map[string]any{"prompt": "clean"}); err != nil {
		t.Fatalf("runDelegateTask: %v", err)
	}
	if len(n.got) != 1 {
		t.Fatalf("clean finish emitted %d records, want 1", len(n.got))
	}
	if n.got[0].Kind != "chat" || n.got[0].SourceID != "task-1" || n.got[0].Verdict != "ok" {
		t.Fatalf("clean record wrong: %+v", n.got[0])
	}
	if n.got[0].Title != "chat task-1 finished" {
		t.Fatalf("title leaked run content: %q", n.got[0].Title)
	}

	bin = writeStub(t, "stub-notify-fail-omega", "boom", 2)
	t.Setenv("OMEGA_BIN", bin)
	d2 := NewDelegate()
	d2.notifier = n
	if _, err := d2.runDelegateTask(context.Background(), map[string]any{"prompt": "failing"}); err != nil {
		t.Fatalf("runDelegateTask (failing): %v", err)
	}
	if len(n.got) != 2 || n.got[1].Verdict != "failed" || n.got[1].SourceID != "task-1" {
		t.Fatalf("failed finish emitted %+v", n.got)
	}
}

func TestRunWithoutNotifierIsHarmless(t *testing.T) {
	bin := writeStub(t, "stub-noinfo-omega", "quiet", 0)
	t.Setenv("OMEGA_BIN", bin)

	d := NewDelegate() // no notifier: the emission path is nil-safe
	out, err := d.runDelegateTask(context.Background(), map[string]any{"prompt": "no notify"})
	if err != nil {
		t.Fatalf("runDelegateTask: %v", err)
	}
	if out == "" {
		t.Fatal("empty tool result without the notifier")
	}
}
