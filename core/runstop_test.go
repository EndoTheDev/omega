package core

import (
	"context"
	"sync/atomic"
	"testing"
)

// TestRunStopLatchesOnceAndFiresEveryHook pins the latch contract the serving shell's
// stop path leans on: one stop, every registered cancellation forwarded exactly once, a
// hook registered AFTER the stop still fires, and a deregistered hook goes quiet.
func TestRunStopLatchesOnceAndFiresEveryHook(t *testing.T) {
	stop := NewRunStop()
	var first, second, late, unsub atomic.Int32

	offUnsub := stop.OnStop(func() { unsub.Add(1) })
	stop.OnStop(func() { first.Add(1) })
	stop.OnStop(func() { second.Add(1) })
	offUnsub() // deregistered before the stop: must stay silent

	if stop.Stopped() {
		t.Fatal("a fresh latch reports stopped")
	}
	if !stop.Stop() {
		t.Fatal("the first Stop reported no transition")
	}
	if stop.Stopped() == false {
		t.Fatal("the latch did not record the stop")
	}
	if first.Load() != 1 || second.Load() != 1 {
		t.Fatalf("hooks fired %d/%d, want 1/1", first.Load(), second.Load())
	}
	if unsub.Load() != 0 {
		t.Fatalf("a deregistered hook fired %d times", unsub.Load())
	}

	// Idempotent: a repeated stop (the endpoint's repeat 200) re-fires nothing.
	if stop.Stop() {
		t.Fatal("the second Stop reported a transition")
	}
	if first.Load() != 1 {
		t.Fatalf("the repeat stop re-fired a hook (%d)", first.Load())
	}

	// A hook registered after the fact runs immediately - the race the
	// per-call registration can lose must not lose the cancellation.
	stop.OnStop(func() { late.Add(1) })
	if late.Load() != 1 {
		t.Fatalf("a late hook fired %d times, want 1", late.Load())
	}
	select {
	case <-stop.Done():
	default:
		t.Fatal("Done did not close on the stop")
	}
}

// TestRunStopNilIsInert: hosts with no human at the wheel (cron, the CLI loop, one-shot
// runs) thread a nil latch - every method must be a no-op, never a panic.
func TestRunStopNilIsInert(t *testing.T) {
	var stop *RunStop
	if stop.Stopped() {
		t.Error("a nil latch reported stopped")
	}
	if stop.Stop() {
		t.Error("a nil latch reported a transition")
	}
	if stop.Done() != nil {
		t.Error("a nil latch handed back a live channel")
	}
	stop.OnStop(func() {}) // must not panic
	if RunStopped(context.Background()) {
		t.Error("a bare context reported a user stop")
	}
}

// TestRunStopRidesTheContext: the run ctx is how tools and the loop read the latch.
func TestRunStopRidesTheContext(t *testing.T) {
	stop := NewRunStop()
	ctx := WithRunStop(context.Background(), stop)
	if got := RunStopFrom(ctx); got != stop {
		t.Fatal("the latch did not survive the context")
	}
	if RunStopped(ctx) {
		t.Fatal("a fresh latch read as stopped through the context")
	}
	detached := context.WithoutCancel(ctx)
	if got := RunStopFrom(detached); got != stop {
		t.Fatal("WithoutCancel dropped the latch - the dispatched call cannot see a stop")
	}
	stop.Stop()
	if !RunStopped(ctx) || !RunStopped(detached) {
		t.Fatal("the stop did not read through the carried latch")
	}
	if WithRunStop(ctx, nil) != ctx {
		t.Fatal("WithRunStop(nil) wrapped instead of passing through")
	}
}
