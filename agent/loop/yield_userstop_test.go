package loop

// The user-stop tests.

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// waitForCond polls until cond holds - bounded to catch a HANG, never to time.
func waitForCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestUserStopKillsDispatchedCallAndDropsItsResult is the card's pinned sequence.
// The stop must terminate the call (the shell-process stand-in observes ctx.Done) and
// its settled result must never reach the wake - zero wake launches, ledger dry.
func TestUserStopKillsDispatchedCallAndDropsItsResult(t *testing.T) {
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "long", Name: "long", Arguments: map[string]any{}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// The wake turn that must never play.
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "resurrected"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	ledger := agent.NewMemYieldLedger()
	rec := &wakeRecorder{}
	stop := core.NewRunStop()
	var started, terminated atomic.Bool
	opts := yieldOpts(provider, map[string]core.Tool{"long": {
		Description: "a long-running call that stands in for a shell",
		Yield:       true,
		Run: func(ctx context.Context, _ map[string]any) (string, error) {
			started.Store(true)
			<-ctx.Done()
			terminated.Store(true)
			return "", ctx.Err()
		},
	}}, ledger, rec.add)
	opts.Stop = stop
	opts.Messages = []core.Message{core.NewUser("go")}
	runCtx := core.WithRunStop(agent.WithSessionID(context.Background(), "s1"), stop)

	_, end := runLoop(t, runCtx, opts)
	if end.finish != "yielded" {
		t.Fatalf("run ended %q, want yielded - the call did not dispatch", end.finish)
	}
	waitForCond(t, "the dispatched call to start", started.Load)
	if left := ledger.All(); len(left) != 1 {
		t.Fatalf("pending calls = %d, want 1 while the call runs", len(left))
	}

	stop.Stop()

	waitForCond(t, "the dispatched call to terminate", terminated.Load)
	waitForCond(t, "the ledger to drain", func() bool { return len(ledger.All()) == 0 })
	// The settled result must not have re-entered.
	if n := rec.lenSync(); n != 0 {
		t.Fatalf("wakes = %d, want 0 - a stopped turn's result re-entered the loop: %+v", n, rec.snapshot())
	}
}

// TestUserStopKillsConvertedCallAndDropsItsResult is the same promise.
func TestUserStopKillsConvertedCallAndDropsItsResult(t *testing.T) {
	shrinkBudget(t, 20*time.Millisecond)
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "slow", Name: "slow", Arguments: map[string]any{"msg": "hi"}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "resurrected"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	ledger := agent.NewMemYieldLedger()
	rec := &wakeRecorder{}
	stop := core.NewRunStop()
	var started, terminated atomic.Bool
	opts := yieldOpts(provider, map[string]core.Tool{"slow": {
		Description: "runs past the inline budget, ctx-aware",
		Run: func(ctx context.Context, _ map[string]any) (string, error) {
			started.Store(true)
			<-ctx.Done()
			terminated.Store(true)
			return "", ctx.Err()
		},
	}}, ledger, rec.add)
	opts.Stop = stop
	opts.Messages = []core.Message{core.NewUser("go")}
	runCtx := core.WithRunStop(agent.WithSessionID(context.Background(), "s1"), stop)

	_, end := runLoop(t, runCtx, opts)
	if end.pending != 1 {
		t.Fatalf("TurnEnd pending = %d, want 1 - the call never converted", end.pending)
	}
	waitForCond(t, "the converted call to start", started.Load)

	stop.Stop()

	waitForCond(t, "the converted call to terminate", terminated.Load)
	waitForCond(t, "the ledger to drain", func() bool { return len(ledger.All()) == 0 })
	if n := rec.lenSync(); n != 0 {
		t.Fatalf("wakes = %d, want 0 - a stopped converted call re-entered the loop: %+v", n, rec.snapshot())
	}
}

// TestUserStopCancelCauseIsTheSentinel: the cancellation the latch forwards names.
func TestUserStopCancelCauseIsTheSentinel(t *testing.T) {
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "long", Name: "long", Arguments: map[string]any{}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
	)
	ledger := agent.NewMemYieldLedger()
	rec := &wakeRecorder{}
	stop := core.NewRunStop()
	seen := make(chan error, 1)
	opts := yieldOpts(provider, map[string]core.Tool{"long": {
		Description: "reports the cancellation cause",
		Yield:       true,
		Run: func(ctx context.Context, _ map[string]any) (string, error) {
			<-ctx.Done()
			seen <- context.Cause(ctx)
			return "", ctx.Err()
		},
	}}, ledger, rec.add)
	opts.Stop = stop
	opts.Messages = []core.Message{core.NewUser("go")}
	runCtx := core.WithRunStop(agent.WithSessionID(context.Background(), "s1"), stop)

	runLoop(t, runCtx, opts)
	stop.Stop()

	select {
	case cause := <-seen:
		if cause == nil || !strings.Contains(cause.Error(), core.ErrUserStopped.Error()) {
			t.Fatalf("cancellation cause = %v, want %v", cause, core.ErrUserStopped)
		}
	case <-time.After(testWaitTimeout):
		t.Fatal("the dispatched call never reported its cancellation")
	}
	if n := rec.lenSync(); n != 0 {
		t.Fatalf("wakes = %d, want 0", n)
	}
}
