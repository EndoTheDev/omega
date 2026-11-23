package loop

// The steer boundary law: everything entering a live turn - a steer the human typed
// mid-turn, a result that settled mid-turn - lands AFTER the current tool batch, in the
// SAME turn. The loop's own tests pin the wire shape; the http channel's tests pin the
// queue that feeds it.

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// steerFeedOf hands out queued deliveries batch by batch, and records how many calls the
// loop made (the boundary count).
type steerFeedOf struct {
	mu     sync.Mutex
	batches [][]agent.SteerDelivery
	calls  int
}

func (f *steerFeedOf) feed() []agent.SteerDelivery {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.batches) == 0 {
		return nil
	}
	batch := f.batches[0]
	f.batches = f.batches[1:]
	return batch
}

func (f *steerFeedOf) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// runLoopEvents runs one Loop to completion and returns every event it emitted.
func runLoopEvents(t *testing.T, ctx context.Context, opts agent.LoopOptions) []core.Event {
	t.Helper()
	events := make(chan core.Event, 256)
	opts.Events = events
	var out []core.Event
	done := make(chan struct{})
	go func() {
		defer close(events)
		defer close(done)
		Loop{}.Run(ctx, opts)
	}()
	for e := range events {
		out = append(out, e)
	}
	<-done
	return out
}

// indexOfMessage returns the position of the first message matching pred, or -1.
func indexOfMessage(messages []core.Message, pred func(core.Message) bool) int {
	for i, m := range messages {
		if pred(m) {
			return i
		}
	}
	return -1
}

// TestSteerLandsAfterTheToolBatch: a steer queued WHILE a tool runs reaches the model in
// the same turn, after that batch's results - never mid-stream, never a second run.
func TestSteerLandsAfterTheToolBatch(t *testing.T) {
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "slow", Arguments: map[string]any{}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "adjusted"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	feed := &steerFeedOf{}
	tool := core.Tool{
		Description: "slow",
		Run: func(_ context.Context, _ map[string]any) (string, error) {
			feed.mu.Lock()
			feed.batches = append(feed.batches, []agent.SteerDelivery{{
				ID:      "q1",
				Message: core.NewUser("actually, take the left fork"),
			}})
			feed.mu.Unlock()
			return "done", nil
		},
	}
	sink := &memSink{}
	opts := agent.LoopOptions{
		Provider: provider,
		Tools:    map[string]core.Tool{"slow": tool},
		Messages: []core.Message{core.NewUser("go")},
		Steer:    feed.feed,
	}
	ctx := agent.WithTurnSink(context.Background(), sink)
	events := runLoopEvents(t, ctx, opts)

	if provider.Calls() != 2 {
		t.Fatalf("provider calls = %d, want 2 (the steer adjusted the SAME run)", provider.Calls())
	}
	msgs := provider.LastMessages
	steerAt := indexOfMessage(msgs, func(m core.Message) bool {
		u, ok := m.(core.User)
		return ok && strings.Contains(u.Content, "left fork")
	})
	resultAt := indexOfMessage(msgs, func(m core.Message) bool {
		tr, ok := m.(core.ToolResult)
		return ok && tr.ToolCallID == "c1"
	})
	if steerAt < 0 {
		t.Fatalf("the steer never reached the model: %+v", msgs)
	}
	if resultAt < 0 {
		t.Fatalf("the tool result never reached the model: %+v", msgs)
	}
	if steerAt < resultAt {
		t.Errorf("steer landed at %d, before the tool batch's result at %d - the boundary law broke", steerAt, resultAt)
	}

	endings := 0
	delivered := 0
	for _, e := range events {
		switch ev := e.(type) {
		case core.AgentEnd:
			endings++
			if ev.FinishReason != "stop" {
				t.Errorf("agent_end finish = %q, want stop", ev.FinishReason)
			}
		case core.SteerDeliveredEvent:
			delivered++
			if ev.ID != "q1" {
				t.Errorf("delivered id = %q, want q1", ev.ID)
			}
		}
	}
	if endings != 1 {
		t.Errorf("agent_end count = %d, want 1 (one run)", endings)
	}
	if delivered != 1 {
		t.Errorf("steer_delivered count = %d, want 1", delivered)
	}
	if !sinkHas(sink, "left fork") {
		t.Error("the delivered steer never persisted - history would lose the human's words")
	}
}

// TestSteerAtRunEndContinuesTheRun: a steer arriving while the LAST response streamed
// (no tool batch to re-enter on) keeps the run alive - the human's words never wait for a
// second run slot.
func TestSteerAtRunEndContinuesTheRun(t *testing.T) {
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "one"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "two"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	feed := &steerFeedOf{batches: [][]agent.SteerDelivery{
		nil, // the turn's opening boundary: nothing queued yet
		{{ID: "q9", Message: core.NewUser("and one more thing")}},
	}}
	sink := &memSink{}
	opts := agent.LoopOptions{
		Provider: provider,
		Messages: []core.Message{core.NewUser("go")},
		Steer:    feed.feed,
	}
	events := runLoopEvents(t, agent.WithTurnSink(context.Background(), sink), opts)

	if provider.Calls() != 2 {
		t.Fatalf("provider calls = %d, want 2 (the run continued with the steer)", provider.Calls())
	}
	last := provider.LastMessages
	if at := indexOfMessage(last, func(m core.Message) bool {
		u, ok := m.(core.User)
		return ok && strings.Contains(u.Content, "one more thing")
	}); at < 0 {
		t.Fatalf("the end-boundary steer never reached the model: %+v", last)
	}
	endings, turns := 0, 0
	for _, e := range events {
		switch ev := e.(type) {
		case core.AgentEnd:
			endings++
			turns = ev.Turns
		}
	}
	if endings != 1 {
		t.Errorf("agent_end count = %d, want 1 (one run, not a relaunch)", endings)
	}
	if turns != 2 {
		t.Errorf("turns = %d, want 2 (the steer opened a second turn in the same run)", turns)
	}
}

// TestWakeResultTakesItsPlaceholder: a settled call's result delivered mid-turn REPLACES
// the still-pending sibling placeholder - appending it would leave one call answered twice
// on the wire.
func TestWakeResultTakesItsPlaceholder(t *testing.T) {
	provider := agent.NewFakeProviderScripts("test", []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response_chunk", Content: "used it"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	})
	ledger := agent.NewMemYieldLedger()
	if err := ledger.Register(agent.PendingCall{SessionID: "s1", CallID: "c1", ToolName: "echo", DispatchedAt: core.NowISO()}); err != nil {
		t.Fatalf("register pending call: %v", err)
	}
	assistant := core.NewAssistant("")
	assistant.ToolCalls = []core.ToolCall{{ID: "c1", Name: "echo"}}
	feed := &steerFeedOf{batches: [][]agent.SteerDelivery{
		{{ID: "w1", Message: core.NewYieldResult("async:ok", "c1", false, false)}},
	}}
	sink := &memSink{}
	opts := agent.LoopOptions{
		Provider:    provider,
		YieldLedger: ledger,
		Messages:    []core.Message{core.NewUser("go"), assistant},
		Steer:       feed.feed,
	}
	ctx := agent.WithSessionID(agent.WithTurnSink(context.Background(), sink), "s1")
	runLoopEvents(t, ctx, opts)

	msgs := provider.LastMessages
	answers := 0
	for _, m := range msgs {
		switch v := m.(type) {
		case core.YieldResult:
			if v.ToolCallID != "c1" {
				continue
			}
			answers++
			if v.Content != "async:ok" {
				t.Errorf("delivered result = %+v, want the settled content", v)
			}
		case core.ToolResult:
			if v.ToolCallID != "c1" {
				continue
			}
			answers++
			t.Errorf("placeholder survived beside the result: %+v", v)
		}
	}
	if answers != 1 {
		t.Fatalf("answers for c1 = %d, want exactly 1 (the result took the placeholder's slot)", answers)
	}
}

// sinkHas reports whether the sink persisted a message carrying the text.
func sinkHas(s *memSink, text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.msgs {
		if u, ok := m.(core.User); ok && strings.Contains(u.Content, text) {
			return true
		}
	}
	return false
}
