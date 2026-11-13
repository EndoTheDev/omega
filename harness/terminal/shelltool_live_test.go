package terminal

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// The live-stream pin's waits ride the package's testWaitTimeout
// (waits_test.go): a hang is caught without turning scheduling latency
// into a failure (the gate law: bound for hangs, never for timing
// correctness).

// liveStreamWindow is how long a live chunk may take to land after the
// call starts. The shell under test outlives it by several seconds, so
// the window separates a live tail (tens of milliseconds, the tap's own
// poll interval) from a post-exit replay (at the shell's exit, seconds
// later). It discriminates between two mechanisms; it is not a
// performance budget.
const liveStreamWindow = 2 * time.Second

// briefEchoThenSleeper emits a marker and then keeps the shell alive for
// a few seconds, so a live chunk and a finished call occupy distinct
// moments in time.
func briefEchoThenSleeper(marker string) string {
	if runtime.GOOS == "windows" {
		return "echo " + marker + " && ping -n 6 127.0.0.1 > nul"
	}
	return "echo " + marker + "; sleep 5"
}

// TestRunShellStreamsLiveBeforeItExits pins review-round-2 finding 1
// (card t_5a49790b): the pending chip's live tail must stream WHILE the
// shell runs, not replay the finished log after it exits. The tap is
// started right after the spawn, before the inline wait, so the first
// chunk lands within moments of the call; started after the wait, every
// chunk lands in one burst at the shell's exit, seconds later.
func TestRunShellStreamsLiveBeforeItExits(t *testing.T) {
	p := testShellProvider(t)

	started := time.Now()
	var mu sync.Mutex
	var chunks []core.ToolOutputChunk
	var firstChunk time.Duration
	gotFirst := false
	live := make(chan struct{}, 16)
	tap := func(c core.ToolOutputChunk) {
		mu.Lock()
		if !gotFirst {
			gotFirst = true
			firstChunk = time.Since(started)
		}
		chunks = append(chunks, c)
		mu.Unlock()
		select {
		case live <- struct{}{}:
		default:
		}
	}
	ctx := core.WithRunOutputTap(context.Background(), tap)
	ctx = core.WithToolCall(ctx, "call-live", "shell.run")

	done := make(chan struct{})
	var out string
	var runErr error
	go func() {
		out, runErr = p.RunShell(ctx, map[string]any{"command": briefEchoThenSleeper("live-marker")})
		close(done)
	}()

	select {
	case <-live:
	case <-time.After(testWaitTimeout):
		t.Fatal("no chunk reached the run's output tap while the shell was running")
	}

	mu.Lock()
	latency := firstChunk
	mu.Unlock()
	if latency >= liveStreamWindow {
		t.Fatalf("the first live chunk landed %v after the call started - at the shell's exit rather than as its output arrived: that is a post-exit replay, not a live tail", latency)
	}
	select {
	case <-done:
		t.Fatal("RunShell had already returned when the first chunk landed: the tap is not streaming a running shell")
	default:
	}

	select {
	case <-done:
	case <-time.After(testWaitTimeout):
		t.Fatal("RunShell never returned")
	}
	if runErr != nil {
		t.Fatalf("RunShell: %q err=%v", out, runErr)
	}
	if !strings.Contains(out, "live-marker") {
		t.Fatalf("the result lost the shell's output: %q", out)
	}

	mu.Lock()
	defer mu.Unlock()
	var streamed strings.Builder
	for _, c := range chunks {
		if c.CallID != "call-live" || c.Tool != "shell.run" {
			t.Fatalf("chunk not stamped with the call identity: %+v", c)
		}
		streamed.WriteString(c.Text)
	}
	if !strings.Contains(streamed.String(), "live-marker") {
		t.Fatalf("the streamed chunks never carried the shell's output: %q", streamed.String())
	}
}
