package api

// The user-stop tests (card t_caa67135). Endo cancelled a streaming turn
// in the webui and got the opposite of a hard stop: the yield calls the
// cancelled run had left running settled afterwards and each settlement
// launched a whole new wake turn (messages 1598-1607 in session
// e3c90f70ff2fff4235d9db87fe88495d, right after the wake note at 1597),
// while the go-test shell behind them kept working. These pin the three
// promises at the serving shell: a stopped home launches no wake, a
// stopped home's wake run leaves no "send a message to continue" note,
// and the pending table is abandoned rather than left to re-enter.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// newStopTestServer wires the real loop over a silent scripted provider
// and an in-memory store - the wake tests' shape.
func newStopTestServer(t *testing.T) (*Server, *store.Store, *httptest.Server) {
	t.Helper()
	prov := newWakeScripted("step", [][]core.ProviderStreamEvent{finalStep("never")})
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := NewServer(a, nil, st, nil, nil, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, st, ts
}

// TestUserStopAbandonsPendingAndDropsWakes pins the wake half of the
// card without a run in flight: the stop endpoint latches the session,
// settles every pending call (nothing left for the never-fold law or the
// placeholders to read), empties the queued results, and refuses the
// settlement that arrives after the cancel.
func TestUserStopAbandonsPendingAndDropsWakes(t *testing.T) {
	s, st, ts := newStopTestServer(t)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	// The session holds a live run slot (the same shape the endpoint
	// finds) and one pending call, registered the way the loop
	// registers a dispatch.
	run, ok := s.runs.start("s1")
	if !ok {
		t.Fatal("hub start")
	}
	s.runs.registerCancel(run, func() {})
	if err := s.yieldLedger.Register(agent.PendingCall{
		SessionID: "s1", CallID: "call_71lhf7os", ToolName: "shell.run",
		Arguments: `{"command":"go vet ./..."}`, DispatchedAt: core.NowISO(),
	}); err != nil {
		t.Fatalf("register pending: %v", err)
	}
	// A result that settled BEFORE the stop and is still queued behind
	// the live slot: the cancel must drop it, not launch it.
	s.wakes.enqueue("s1", core.YieldResult{
		ToolCallID: "call_earlier", ToolName: "shell.run", Content: "ok", Timestamp: core.NowISO(),
	})

	resp, err := http.Post(ts.URL+"/sessions/s1/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("stop request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stop status = %d, want 200", resp.StatusCode)
	}

	if !s.runs.userStopped("s1") {
		t.Fatal("the stop did not latch the session")
	}
	if left := s.yieldLedger.All(); len(left) != 0 {
		t.Fatalf("pending calls left after the stop: %+v", left)
	}
	s.wakes.mu.Lock()
	queued := len(s.wakes.byID["s1"])
	s.wakes.mu.Unlock()
	if queued != 0 {
		t.Fatalf("queued wake results left after the stop: %d", queued)
	}

	// The killed call settles late (its result arrives after the cancel),
	// and a drain runs: neither may launch a wake turn.
	s.wakeHome("s1", core.YieldResult{
		ToolCallID: "call_71lhf7os", ToolName: "shell.run",
		Content: "FAIL github.com/EndoTheDev/omega/agent", IsError: true, Timestamp: core.NowISO(),
	})
	s.drainWakes("s1")
	if n := s.wakeLaunchCount("s1"); n != 0 {
		t.Fatalf("wake launches after the stop = %d, want 0 - the cancelled turn re-entered the loop", n)
	}
	// The synthetic slot is still the session's only run: no wake run
	// took it (or waited to take it).
	if cur, live := s.runs.get("s1"); !live || cur != run {
		t.Fatal("a wake run took the stopped session's slot")
	}

	// Nothing settled into the transcript either: the result the user
	// killed is not a message in a conversation that ended.
	msgs, err := st.GetMessages(context.Background(), "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	for _, m := range msgs {
		if _, ok := m.(core.YieldResult); ok {
			t.Fatalf("the stopped turn's result persisted as a row: %s", sessionRowDump(msgs))
		}
	}
	s.runs.finish(run)
}

// TestUserStopLeavesNoContinueNote: the wake note exists to explain a
// wake turn nobody answered. A run the user just killed was answered -
// by the stop - so the note is a lie ("send a message to continue" on a
// conversation the human ended). Both note paths (the all-empty
// completion and the stopped end) stay silent on a stopped home, and the
// same sinks still write their note once the human speaks again.
func TestUserStopLeavesNoContinueNote(t *testing.T) {
	s, st, ts := newStopTestServer(t)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	run, ok := s.runs.start("s1")
	if !ok {
		t.Fatal("hub start")
	}
	s.runs.registerCancel(run, func() {})
	if _, cancellable := s.stopRun("s1"); !cancellable {
		t.Fatal("stopRun did not stop the live run")
	}

	ctx := context.Background()
	stopped := &wakeNoteSink{s: s, id: "s1"}
	if err := stopped.AppendTurnMessage(ctx, core.NewAssistant("")); err != nil {
		t.Fatalf("append: %v", err)
	}
	stopped.noteStoppedEnd(ctx, "error")
	msgs, err := st.GetMessages(ctx, "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	for _, m := range msgs {
		if u, ok := m.(core.User); ok && contains(u.Content, "send a message to continue") {
			t.Fatalf("a stopped run left a continue note: %q", u.Content)
		}
	}

	// Control: the identical sink on a re-armed home still notes, so the
	// silence above is the gate and not a broken note path.
	s.runs.rearm("s1")
	if err := (&wakeNoteSink{s: s, id: "s1"}).AppendTurnMessage(ctx, core.NewAssistant("")); err != nil {
		t.Fatalf("append after rearm: %v", err)
	}
	msgs, err = st.GetMessages(ctx, "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	noted := false
	for _, m := range msgs {
		if u, ok := m.(core.User); ok && contains(u.Content, "send a message to continue") {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("a re-armed home lost its wake note: %s", sessionRowDump(msgs))
	}
	s.runs.finish(run)
}

// TestCancelMidTurnDropsSettledResultAndKillsTheCall is the card's
// acceptance sequence end to end through the real pipe: a turn dispatches
// a long call (the shell stand-in), a later turn is live when the user
// presses stop, and the stop must end BOTH - the live turn and the call
// left running behind it. Zero wake launches, the call terminated, the
// pending table dry, no continue note on the transcript.
func TestCancelMidTurnDropsSettledResultAndKillsTheCall(t *testing.T) {
	// Two scripts, one per run (wakeScripted hands out one script per
	// Stream call - a shared receive channel would let the runs race each
	// other's batches). Run 1 dispatches the long call; run 2 parks in a
	// tool so the session has a LIVE turn when the user presses stop.
	prov := newWakeScripted("step", [][]core.ProviderStreamEvent{
		{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "call_probe", Name: "probe", Arguments: map[string]any{}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "call_hang", Name: "hang", Arguments: map[string]any{}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
	})
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	var probeStarted, probeTerminated bool
	var hangStarted, hangTerminated bool
	var mu sync.Mutex
	tools := map[string]core.Tool{
		"probe": {
			Description: "a dispatched call that stands in for a long shell",
			Yield:       true,
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				mu.Lock()
				probeStarted = true
				mu.Unlock()
				<-ctx.Done()
				mu.Lock()
				probeTerminated = true
				mu.Unlock()
				return "", ctx.Err()
			},
		},
		"hang": {
			Description: "keeps the next turn live",
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				mu.Lock()
				hangStarted = true
				mu.Unlock()
				<-ctx.Done()
				mu.Lock()
				hangTerminated = true
				mu.Unlock()
				return "", ctx.Err()
			},
		},
	}
	s := NewServer(a, tools, st, nil, nil, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	// Run 1: dispatch the long call. The turn ends yielded, the run's
	// slot frees, and the call keeps running out of band.
	resp, cancel, sc := postChatStream(t, ts, "s1")
	readFrames(t, sc, "agent_end", 1)
	detachClient(cancel, resp)
	waitFor(t, "the long call dispatched and running", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return probeStarted
	})
	waitFor(t, "run 1 released the slot", func() bool { return !s.runs.active("s1") })

	// Run 2: a live turn the user can actually cancel, parked in a tool.
	resp2, cancel2, _ := postChatStream(t, ts, "s1")
	waitFor(t, "the live turn parked in its tool", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hangStarted
	})

	// The user presses stop - the reported intent: stop generation, stop
	// the in-flight processes, end the turn.
	stopResp, err := http.Post(ts.URL+"/sessions/s1/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("stop request: %v", err)
	}
	stopResp.Body.Close()
	if stopResp.StatusCode != http.StatusOK {
		t.Fatalf("stop status = %d, want 200", stopResp.StatusCode)
	}
	detachClient(cancel2, resp2)

	// The running call dies with the turn, and the live run unwinds.
	waitFor(t, "the dispatched call terminated by the stop", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return probeTerminated
	})
	waitFor(t, "the live turn ended", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hangTerminated
	})
	waitFor(t, "the run slot released", func() bool { return !s.runs.active("s1") })

	// ZERO wake launches: the settled (or settling) call never re-enters.
	if n := s.wakeLaunchCount("s1"); n != 0 {
		t.Fatalf("wake launches = %d, want 0 - the cancelled turn re-triggered the loop", n)
	}
	if left := s.yieldLedger.All(); len(left) != 0 {
		t.Fatalf("pending calls left after the stop: %+v", left)
	}
	msgs, err := st.GetMessages(context.Background(), "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	for _, m := range msgs {
		if u, ok := m.(core.User); ok && contains(u.Content, "send a message to continue") {
			t.Fatalf("the cancelled turn left a continue note: %q", u.Content)
		}
		if y, ok := m.(core.YieldResult); ok && y.ToolCallID == "call_probe" {
			t.Fatalf("the killed call's result persisted as a wake input row: %s", sessionRowDump(msgs))
		}
	}
}
