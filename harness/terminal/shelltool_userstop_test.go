package terminal

// The user-stop test (card t_caa67135). The shell registry's whole
// reason to exist is detachment: the child outlives the turn, the run
// cap, and even omega itself. The ONE exception is the human pressing
// stop - a hard stop ends the work, not just the stream - and this pins
// both halves: the stop kills the child, an ordinary interruption
// (the run cap) still leaves it running.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// liveShell returns the id of the first running shell, "" when none.
func liveShell(reg *Registry) string {
	for _, e := range reg.List() {
		if !e.Exited {
			return e.ID
		}
	}
	return ""
}

// TestRunShellKillsChildOnUserStop: a call cancelled by the user's stop
// latch terminates the shell it spawned (the reported session's go-test
// shell survived the cancel and kept feeding results), while the same
// call cut by a plain cancellation keeps today's detachment contract.
func TestRunShellKillsChildOnUserStop(t *testing.T) {
	skipOnWindows(t)
	reg := testRegistry(t)
	p := NewShellProvider(nil, reg)

	stop := core.NewRunStop()
	ctx, cancel := context.WithCancelCause(core.WithRunStop(context.Background(), stop))
	defer cancel(nil)
	type call struct {
		out string
		err error
	}
	done := make(chan call, 1)
	go func() {
		out, err := p.RunShell(ctx, map[string]any{"command": "echo started; sleep 120"})
		done <- call{out: out, err: err}
	}()

	id := ""
	waitFor(t, testWaitTimeout, "the shell to spawn", func() bool {
		id = liveShell(reg)
		return id != ""
	})

	// The human presses stop: the latch latches first, then the run ctx
	// is cancelled - the hub's order (a tool reads the intent off the
	// latch the instant its ctx drops).
	stop.Stop()
	cancel(core.ErrUserStopped)

	select {
	case got := <-done:
		if got.err == nil {
			t.Fatalf("RunShell returned no error for a stopped call (out %q)", got.out)
		}
		if !strings.Contains(got.err.Error(), "the user ended the turn") {
			t.Fatalf("error = %q, want it to name the user's stop", got.err)
		}
		if !strings.Contains(got.out, "started") {
			t.Errorf("output %q lost the shell's bytes", got.out)
		}
	case <-time.After(testWaitTimeout):
		t.Fatal("RunShell never returned after the stop")
	}

	// Terminated for real, by the registry's own verdict.
	waitFor(t, testWaitTimeout, "the shell to exit after the stop", func() bool {
		e, ok := reg.Find(id)
		return ok && e.Exited
	})

	// Control: a plain cancellation (the run cap, a converted call's
	// yield deadline) is NOT a user stop - the child stays detached and
	// addressable, which is the whole registry contract.
	plain, cancelPlain := context.WithCancel(context.Background())
	done2 := make(chan call, 1)
	go func() {
		out, err := p.RunShell(plain, map[string]any{"command": "echo started; sleep 120"})
		done2 <- call{out: out, err: err}
	}()
	id2 := ""
	waitFor(t, testWaitTimeout, "the control shell to spawn", func() bool {
		id2 = liveShell(reg)
		return id2 != ""
	})
	cancelPlain()
	select {
	case got := <-done2:
		if got.err == nil || !strings.Contains(got.err.Error(), "is still running") {
			t.Fatalf("control error = %v, want the detached-shell message", got.err)
		}
	case <-time.After(testWaitTimeout):
		t.Fatal("the control RunShell never returned after the plain cancel")
	}
	e2, ok := reg.Find(id2)
	if !ok || e2.Exited {
		t.Fatalf("a plain cancel killed the shell - detachment is the contract (entry %+v)", e2)
	}
	if msg, err := reg.Kill(id2); err != nil {
		t.Fatalf("cleanup kill: %v (%s)", err, msg)
	}
}
