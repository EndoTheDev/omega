package delegate

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// ctx15 is the drain deadline the result-waiting tests use.
func ctx15(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ctx10 is the shorter drain deadline (the agent-stub tests).
func ctx10(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestRunAffinityIsolation pins the t_f3fb7c52 fix: results queued under
// one run's affinity key are unreachable from a drain carrying another
// key - the wrong-run steal (the title turn, a sibling /chat run) is
// structurally impossible.
func TestRunAffinityIsolation(t *testing.T) {
	d := NewDelegate()
	d.pushResult("run-a", "delegate:task-1", "result for run A")
	d.pushResult("run-b", "delegate:task-2", "result for run B")

	if got := d.Poll("run-b"); len(got) != 1 || got[0].Text != "result for run B" || got[0].Source != "delegate:task-2" {
		t.Fatalf("run-b drain got %+v, want exactly its own result", got)
	}
	if got := d.Poll("run-b"); len(got) != 0 {
		t.Fatalf("run-b drain repeated: %+v", got)
	}
	if got := d.Poll("run-c"); len(got) != 0 {
		t.Fatalf("empty-key drain got %+v, want nothing", got)
	}
	if got := d.Poll("run-a"); len(got) != 1 || got[0].Text != "result for run A" {
		t.Fatalf("run-a drain got %+v, want exactly its own result", got)
	}
}

// TestAffinityWaitCrossKeyNeverWakes pins the blocking drain: a Wait on
// one key is not released by a push for another key; the run-a push
// releases it with run-a's result.
func TestAffinityWaitCrossKeyNeverWakes(t *testing.T) {
	d := NewDelegate()
	returned := make(chan string, 1)
	go func() {
		msg, ok := d.Wait(ctx15(t), "run-a")
		if ok {
			returned <- msg.Text
		} else {
			returned <- ""
		}
	}()
	time.Sleep(100 * time.Millisecond)
	d.pushResult("run-b", "delegate:task-1", "the foreign result")
	select {
	case text := <-returned:
		t.Fatalf("Wait(run-a) released by a run-b push (text=%q) - affinity leak", text)
	case <-time.After(500 * time.Millisecond):
		// still parked on run-a, as it must be
	}
	d.pushResult("run-a", "delegate:task-2", "the run-a result")
	select {
	case text := <-returned:
		if text != "the run-a result" {
			t.Fatalf("Wait(run-a) returned %q, want %q", text, "the run-a result")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Wait(run-a) never released on its own key's push")
	}
}

// TestAffinityWaitReturnsOnPush pins the wake path: a Wait armed before
// the push returns promptly with the pushed result.
func TestAffinityWaitReturnsOnPush(t *testing.T) {
	d := NewDelegate()
	var wg sync.WaitGroup
	got := make(chan core.InjectedMessage, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		msg, ok := d.Wait(ctx15(t), "run-x")
		if ok {
			got <- msg
		}
	}()
	time.Sleep(100 * time.Millisecond)
	d.pushResult("run-x", "delegate:task-1", "the result")
	wg.Wait()
	select {
	case msg := <-got:
		if msg.Source != "delegate:task-1" || msg.Text != "the result" {
			t.Fatalf("Wait returned %+v, want the pushed result", msg)
		}
	default:
		t.Fatal("Wait did not return the pushed result")
	}
}

// TestAffinityWaitCancellation pins the ctx arm: a cancelled run's Wait
// releases with ok=false and leaves no waiter behind (a later push
// queues without a leak).
func TestAffinityWaitCancellation(t *testing.T) {
	d := NewDelegate()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		_, ok := d.Wait(ctx, "run-a")
		done <- ok
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	if ok := <-done; ok {
		t.Fatal("cancelled Wait returned ok=true")
	}
	d.qmu.Lock()
	ws := d.waiters["run-a"]
	d.qmu.Unlock()
	if len(ws) != 0 {
		t.Fatalf("cancelled Wait leaked %d waiter(s)", len(ws))
	}
	// the queue still works after the cancelled wait
	d.pushResult("run-a", "delegate:task-1", "after cancel")
	msgs := d.Poll("run-a")
	if len(msgs) != 1 || msgs[0].Text != "after cancel" {
		t.Fatalf("post-cancel Poll = %+v", msgs)
	}
}

// TestWaitKeepsQueuedSiblings pins the review fix: Wait returns ONE
// result and leaves the siblings queued for the next drain (the
// Poll-based fast paths emptied the queue and silently dropped them).
func TestWaitKeepsQueuedSiblings(t *testing.T) {
	d := NewDelegate()
	d.pushResult("run-a", "delegate:task-1", "first")
	d.pushResult("run-a", "delegate:task-2", "second")
	m, ok := d.Wait(context.Background(), "run-a")
	if !ok || m.Text != "first" {
		t.Fatalf("Wait = (%+v, %v), want the first result", m, ok)
	}
	rest := d.Poll("run-a")
	if len(rest) != 1 || rest[0].Text != "second" {
		t.Fatalf("sibling result lost after Wait: %+v", rest)
	}
}

// TestInjectedCapDropsNewest pins the global buffer cap law: the 17th
// undrained result across all keys drops (and a drained buffer frees
// room again).
func TestInjectedCapDropsNewest(t *testing.T) {
	d := NewDelegate()
	for i := 0; i < injectedCap; i++ {
		d.pushResult("run-a", "delegate:task-x", "burst")
	}
	d.pushResult("run-a", "delegate:task-over", "the overshoot")
	msgs := d.Poll("run-a")
	if len(msgs) != injectedCap {
		t.Fatalf("queued %d results, want %d (the newest dropped)", len(msgs), injectedCap)
	}
	// room freed by Poll accepts again
	d.pushResult("run-a", "delegate:task-y", "refill")
	if msgs := d.Poll("run-a"); len(msgs) != 1 || msgs[0].Text != "refill" {
		t.Fatalf("post-drain Poll = %+v", msgs)
	}
}
