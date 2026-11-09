package loop

// The async agent loop phase 1 behavior tests (card t_f3f953d0): a
// yield-class call ends the turn without parking, the settled result
// re-enters as a synthetic input message, and every harness-less or
// degraded path keeps today's loop-back semantics. Real loop, fake
// provider - the agent_loop_test.go patterns.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// yieldEcho is the reference yield-class tool: registration declares
// the class; the run itself is plain.
func yieldEcho() core.Tool {
	return core.Tool{
		Description: "Echo later",
		Yield:       true,
		Run: func(_ context.Context, args map[string]any) (string, error) {
			msg, _ := args["msg"].(string)
			return "async:" + msg, nil
		},
	}
}

// loopbackEcho is the reference loop-back tool.
func loopbackEcho() core.Tool {
	return core.Tool{
		Description: "Echo now",
		Run: func(_ context.Context, args map[string]any) (string, error) {
			msg, _ := args["msg"].(string)
			return msg, nil
		},
	}
}

// agentEnd sums up the assertion-relevant shapes of a finished Run.
type agentEnd struct {
	turns   int
	finish  string
	pending int
}

// runLoop runs one Run to completion and collects the event count plus
// the AgentEnd. The events channel is buffered large enough that the
// loop never blocks on an unread event.
func runLoop(t *testing.T, ctx context.Context, opts agent.LoopOptions) (int, agentEnd) {
	t.Helper()
	events := make(chan core.Event, 256)
	opts.Events = events
	end := agentEnd{}
	go func() {
		defer close(events)
		Loop{}.Run(ctx, opts)
	}()
	n := 0
	for e := range events {
		n++
		if endV, ok := e.(core.AgentEnd); ok {
			end.turns, end.finish = endV.Turns, endV.FinishReason
		}
		if turnEnd, ok := e.(core.TurnEnd); ok {
			end.pending = turnEnd.PendingCalls
		}
	}
	return n, end
}

// wakeRecorder is the mutex-guarded wake sink the tests inject - the
// dispatched executor goroutines race the readers.
type wakeRecorder struct {
	mu   sync.Mutex
	list []core.YieldResult
}

func (w *wakeRecorder) add(_ string, res core.YieldResult) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = append(w.list, res)
}

func (w *wakeRecorder) lenSync() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.list)
}

func (w *wakeRecorder) snapshot() []core.YieldResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]core.YieldResult(nil), w.list...)
}

// waitForWakes blocks until the recorder holds n wakes or the deadline
// lapses (the dispatched executors land asynchronously).
func waitForWakes(t *testing.T, rec *wakeRecorder, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && rec.lenSync() < n {
		time.Sleep(5 * time.Millisecond)
	}
	if rec.lenSync() != n {
		t.Fatalf("wakes = %d, want %d", rec.lenSync(), n)
	}
}

// memSink collects the messages a run persisted through its TurnSink.
type memSink struct {
	mu   sync.Mutex
	msgs []core.Message
}

func (s *memSink) AppendTurnMessage(_ context.Context, msg core.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, msg)
	return nil
}

func yieldOpts(provider agent.Provider, tools map[string]core.Tool, ledger agent.YieldLedger, wake func(string, core.YieldResult)) agent.LoopOptions {
	return agent.LoopOptions{
		Provider:     provider,
		Tools:        tools,
		YieldLedger:  ledger,
		YieldWake:    wake,
		YieldTimeout: 2 * time.Second,
	}
}

// TestLoopYieldCallEndsTurn: one yield-class call ends the turn and the
// run - the second script never plays, no conversation goroutine parks
// (provider calls stay at one, FinishReason is yielded, the wake
// carries the settled result, the ledger settled before injecting).
func TestLoopYieldCallEndsTurn(t *testing.T) {
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"msg": "hi"}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "never"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	ledger := agent.NewMemYieldLedger()
	rec := &wakeRecorder{}
	opts := yieldOpts(provider,
		map[string]core.Tool{"echo": yieldEcho()},
		ledger,
		rec.add)
	opts.Messages = []core.Message{core.NewUser("go")}

	_, end := runLoop(t, agent.WithSessionID(context.Background(), "s1"), opts)

	if end.finish != "yielded" {
		t.Errorf("finish = %q, want yielded", end.finish)
	}
	if end.turns != 1 {
		t.Errorf("turns = %d, want 1 (the turn ended on the yield call)", end.turns)
	}
	if end.pending != 1 {
		t.Errorf("TurnEnd pending = %d, want 1", end.pending)
	}
	if provider.Calls() != 1 {
		t.Errorf("provider calls = %d, want 1 - the run ended on the yield call", provider.Calls())
	}
	waitForWakes(t, rec, 1)
	if res := rec.snapshot()[0]; res.ToolCallID != "c1" || res.Content != "async:hi" || res.IsError || res.Timeout {
		t.Errorf("wake = %+v", res)
	}
	if left := ledger.All(); len(left) != 0 {
		t.Errorf("ledger still holds %d entries after settle-then-inject: %+v", len(left), left)
	}
}

// TestLoopYieldResultStartsNewTurn: the settled result re-enters the
// home input path as a first-class synthetic message and starts a new
// turn - persisted through the sink in its own yield_result class, and
// visible in the provider context the new turn's first call builds.
func TestLoopYieldResultStartsNewTurn(t *testing.T) {
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "woke up"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	ledger := agent.NewMemYieldLedger()
	sink := &memSink{}
	opts := yieldOpts(provider, map[string]core.Tool{"echo": yieldEcho()}, ledger, nil)
	opts.Messages = []core.Message{
		core.NewUser("go"),
		core.NewYieldResult("async:hi", "c1", false, false),
	}
	ctx := agent.WithTurnSink(agent.WithSessionID(context.Background(), "s1"), sink)
	ctx = agent.WithPersistInput(ctx, opts.Messages)

	_, end := runLoop(t, ctx, opts)

	if end.finish != "stop" {
		t.Errorf("finish = %q, want stop", end.finish)
	}
	// The synthetic message persisted in its own class.
	sink.mu.Lock()
	defer sink.mu.Unlock()
	found := false
	for _, m := range sink.msgs {
		if _, ok := m.(core.YieldResult); ok {
			role, _, err := core.EncodeMessage(m)
			if err != nil {
				t.Fatalf("encode yield_result: %v", err)
			}
			if role != "yield_result" {
				t.Errorf("persisted role = %q, want yield_result", role)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("the re-entry message never persisted")
	}
	sawTool := false
	for _, m := range provider.LastMessages {
		if y, ok := m.(core.YieldResult); ok && y.ToolCallID == "c1" {
			sawTool = true
		}
	}
	if !sawTool {
		t.Errorf("re-entry message missing from provider context: %+v", provider.LastMessages)
	}
}

// TestLoopTwoPendingWakeIndependently: two yield-class calls in one
// completion both dispatch; each settles and wakes on its own arrival
// (ruling 7 - one wake per result, never one batched).
func TestLoopTwoPendingWakeIndependently(t *testing.T) {
	provider := agent.NewFakeProvider("test",
		core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"msg": "a"}}},
		core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c2", Name: "echo", Arguments: map[string]any{"msg": "b"}}},
		core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
	)
	ledger := agent.NewMemYieldLedger()
	rec := &wakeRecorder{}
	opts := yieldOpts(provider,
		map[string]core.Tool{"echo": yieldEcho()},
		ledger,
		rec.add)
	opts.Messages = []core.Message{core.NewUser("go")}

	_, end := runLoop(t, agent.WithSessionID(context.Background(), "s1"), opts)

	if end.finish != "yielded" || end.pending != 2 {
		t.Errorf("end = %+v, want yielded with 2 pending", end)
	}
	waitForWakes(t, rec, 2)
	ids := map[string]bool{}
	for _, w := range rec.snapshot() {
		ids[w.ToolCallID] = true
	}
	if !ids["c1"] || !ids["c2"] {
		t.Errorf("wakes = %+v, want one per call", rec.snapshot())
	}
	if left := ledger.All(); len(left) != 0 {
		t.Errorf("ledger settled = %+v, want empty", left)
	}
}

// TestLoopYieldMixedWithLoopback: a completion mixing loop-back and
// yield calls runs the loop-back half inline (the turn continued for
// its result) and ends with the pending half outstanding.
func TestLoopYieldMixedWithLoopback(t *testing.T) {
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "fast", Name: "fast", Arguments: map[string]any{"msg": "now"}}},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "slow", Name: "slow", Arguments: map[string]any{"msg": "later"}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "final"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	ledger := agent.NewMemYieldLedger()
	tools := map[string]core.Tool{"fast": loopbackEcho(), "slow": yieldEcho()}
	rec := &wakeRecorder{}

	opts := yieldOpts(provider, tools, ledger, rec.add)
	opts.Messages = []core.Message{core.NewUser("go")}
	sawSecondTurn := false
	events := make(chan core.Event, 256)
	opts.Events = events
	go func() {
		defer close(events)
		Loop{}.Run(agent.WithSessionID(context.Background(), "s1"), opts)
	}()
	for e := range events {
		if v, ok := e.(core.TurnStart); ok && v.Turn == 2 {
			sawSecondTurn = true
		}
	}
	if !sawSecondTurn {
		t.Error("the mixed turn never continued for the loop-back result")
	}
	waitForWakes(t, rec, 1)
	if res := rec.snapshot()[0]; res.ToolCallID != "slow" {
		t.Errorf("wakes = %+v, want exactly the yield call", rec.snapshot())
	}
}

// TestLoopYieldWithoutHarnessRunsLoopBack: no ledger, no wake, no
// session - every degradation (each nil) keeps the tool loop-back, so
// one-shot hosts and the CLI stay exactly what they were.
func TestLoopYieldWithoutHarnessRunsLoopBack(t *testing.T) {
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"msg": "hi"}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "done"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	tools := map[string]core.Tool{"echo": yieldEcho()}
	opts := agent.LoopOptions{
		Provider: provider,
		Tools:    tools,
		Messages: []core.Message{core.NewUser("go")},
	}
	sawResult := false
	ended := ""
	events := make(chan core.Event, 256)
	opts.Events = events
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), opts)
	}()
	for e := range events {
		switch v := e.(type) {
		case core.ToolResultEvent:
			sawResult = true
			// Degraded execution runs the yield tool INLINE - its own
			// content, no synthetic wrapper.
			if v.Message.Content != "async:hi" || v.Message.IsError {
				t.Errorf("result = %+v, want the tool's inline content", v.Message)
			}
		case core.AgentEnd:
			ended = v.FinishReason
		}
	}
	if !sawResult || ended != "stop" {
		t.Errorf("loop-back degraded run: result=%v finish=%q", sawResult, ended)
	}
}

// TestLoopYieldDispatchesForEphemeralHome: the session-type parity
// pin (card t_61d3def9) - an `eph-` session id is a HOME like any
// other. The loop's dispatch gate reads the home id, not the
// backing, so an ephemeral chat's yield call ends the turn yielded
// (never the loop-back degrade) and the settled result wakes
// re-entry through whatever home input path the host owns. A long
// shell.run in an ephemeral chat yields and re-enters.
func TestLoopYieldDispatchesForEphemeralHome(t *testing.T) {
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"msg": "hi"}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "never"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	ledger := agent.NewMemYieldLedger()
	rec := &wakeRecorder{}
	opts := yieldOpts(provider,
		map[string]core.Tool{"echo": yieldEcho()},
		ledger,
		rec.add,
	)
	opts.Messages = []core.Message{core.NewUser("go")}

	_, end := runLoop(t, agent.WithSessionID(context.Background(), "eph-1a2b3c"), opts)

	if end.finish != "yielded" {
		t.Errorf("finish = %q, want yielded - an eph home must not degrade to loop-back", end.finish)
	}
	if end.pending != 1 {
		t.Errorf("TurnEnd pending = %d, want 1", end.pending)
	}
	waitForWakes(t, rec, 1)
	if res := rec.snapshot()[0]; res.ToolCallID != "c1" || res.Content != "async:hi" {
		t.Errorf("wake = %+v", res)
	}
	if left := ledger.All(); len(left) != 0 {
		t.Errorf("ledger still holds %d entries after settle-then-inject: %+v", len(left), left)
	}
}

// TestLoopPendingPlaceholders: the still-pending sibling placeholder
// (ruling 7's engine half) answers exactly the unanswered pending
// calls, once per call, and never duplicates - the answered-id scan
// covers a placeholder from an earlier turn prep, and a real
// re-entry result suppresses the placeholder for good.
func TestLoopPendingPlaceholders(t *testing.T) {
	pending := []agent.PendingCall{{SessionID: "s", CallID: "c2", ToolName: "echo"}}
	messages := []core.Message{core.NewUser("go")}
	out := appendPendingPlaceholders(messages, pending)
	if len(out) != 2 {
		t.Fatalf("placeholders = %d appended, want 1 (2 messages total)", len(out))
	}
	if tr, ok := out[1].(core.ToolResult); !ok || tr.ToolCallID != "c2" || tr.Content != pendingPlaceholderContent {
		t.Errorf("placeholder = %+v", out[1])
	}
	out2 := appendPendingPlaceholders(out, pending)
	if len(out2) != 2 {
		t.Errorf("second prep appended %d more - placeholders duplicated", len(out2)-len(out))
	}
	out3 := append(append([]core.Message(nil), messages...), core.NewYieldResult("done", "c2", false, false))
	out4 := appendPendingPlaceholders(out3, pending)
	if len(out4) != 2 {
		t.Errorf("placeholder appended beside the real result (%d messages)", len(out4))
	}
}
