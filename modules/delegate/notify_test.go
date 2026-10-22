package delegate

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// recordingNotifier captures the finished-things emissions (card
// t_5d9032d3).
type recordingNotifier struct {
	got []agent.Notification
}

func (r *recordingNotifier) Emit(n agent.Notification) (agent.Notification, error) {
	n.ID = int64(len(r.got) + 1)
	n.CreatedAt = "2026-10-22T04:00:00Z"
	r.got = append(r.got, n)
	return n, nil
}
func (r *recordingNotifier) List(ctx context.Context, unreadOnly bool, limit, offset int) ([]agent.Notification, int, error) {
	return nil, 0, nil
}
func (r *recordingNotifier) MarkRead(ctx context.Context, id int64) error { return nil }
func (r *recordingNotifier) MarkAllRead(ctx context.Context) error        { return nil }
func (r *recordingNotifier) Subscribe() (<-chan agent.Notification, func()) {
	ch := make(chan agent.Notification, 4)
	return ch, func() {}
}

// registerTask seeds a tracked task (what runDelegateTask does before
// the runner goroutine starts).
func registerTask(d *Delegate, id string) *delegateTask {
	task := &delegateTask{id: id}
	d.mu.Lock()
	d.tasks[id] = task
	d.mu.Unlock()
	return task
}

// card t_5d9032d3: a dispatched run's finish emits one chat record -
// ok on a clean exit, failed on error, metadata-only title.
func TestFinishEmitsNotification(t *testing.T) {
	d := NewDelegate()
	n := &recordingNotifier{}
	d.notifier = n
	registerTask(d, "task-7")

	d.finish("task-7", []byte("subagent output"), nil)
	if len(n.got) != 1 {
		t.Fatalf("clean finish emitted %d records, want 1", len(n.got))
	}
	if n.got[0].Kind != "chat" || n.got[0].SourceID != "task-7" || n.got[0].Verdict != "ok" {
		t.Fatalf("clean record wrong: %+v", n.got[0])
	}
	if n.got[0].Title != "chat task-7 finished" {
		t.Fatalf("title leaked run content: %q", n.got[0].Title)
	}

	d.finish("task-8", []byte("boom"), context.DeadlineExceeded)
	if len(n.got) != 2 || n.got[1].Verdict != "failed" {
		t.Fatalf("failed finish emitted %+v", n.got)
	}
}

func TestFinishWithoutNotifierIsHarmless(t *testing.T) {
	d := NewDelegate()
	registerTask(d, "task-9")
	d.finish("task-9", []byte("out"), nil) // no panic, no emission
	select {
	case msg := <-d.InjectedChannel():
		if msg.text == "" {
			t.Fatalf("empty injection without the notifier")
		}
	default:
		t.Fatalf("injection lost without the notifier")
	}
}