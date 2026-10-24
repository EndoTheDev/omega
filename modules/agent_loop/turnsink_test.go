package agent_loop

import (
	"context"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// recordingSink captures the loop's turn-copy writes in order - the
// persistence-shape assertions run against this, not a real store.
type recordingSink struct {
	msgs []core.Message
}

func (s *recordingSink) AppendTurnMessage(ctx context.Context, msg core.Message) error {
	s.msgs = append(s.msgs, msg)
	return nil
}

// TestLoopPersistsTurnCopiesOnce pins the seam law (t_471f6d4a): one
// run input write, then per turn exactly assistant(A + thinking +
// calls, timing attached) -> tool result, never duplicated, never out
// of order.
func TestLoopPersistsTurnCopiesOnce(t *testing.T) {
	sink := &recordingSink{}
	ctx := agent.WithTurnSink(context.Background(), sink)
	ctx = agent.WithPersistInput(ctx, []core.Message{core.NewUser("hi")})

	tool := core.Tool{
		Description: "test no-op",
		Run:         func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
	}
	script1 := []core.ProviderStreamEvent{
		core.ThinkingChunk{Type: "thinking_chunk", Content: "t1"},
		core.ResponseChunk{Type: "response_chunk", Content: "one"},
		core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "nop"}},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
	script2 := []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response_chunk", Content: "two"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
	prov := ai.NewFakeProviderScripts("test", script1, script2).WithDelay(5 * time.Millisecond)

	events := make(chan core.Event, 64)
	opts := core.LoopOptions{
		Provider: prov,
		Messages: []core.Message{core.NewUser("hi")},
		Tools:    map[string]core.Tool{"nop": tool},
		Events:   events,
	}
	go func() {
		_ = Loop{}.Run(ctx, opts)
		close(events)
	}()
	for range events {
	}

	if len(sink.msgs) != 4 {
		t.Fatalf("want user + assistant(A) + tool result + assistant(B), got %d writes", len(sink.msgs))
	}
	if _, ok := sink.msgs[0].(core.User); !ok {
		t.Fatalf("first write not the run input: %T", sink.msgs[0])
	}
	a, ok := sink.msgs[1].(core.Assistant)
	if !ok {
		t.Fatalf("second write not the assistant record: %T", sink.msgs[1])
	}
	if a.Content != "one" || len(a.ToolCalls) != 1 {
		t.Fatalf("assistant record wrong: content %q, calls %d", a.Content, len(a.ToolCalls))
	}
	if a.Thinking == nil || *a.Thinking != "t1" {
		t.Fatalf("assistant record lost thinking: %v", a.Thinking)
	}
	if a.Timing == nil {
		t.Fatalf("assistant record carries no timing")
	}
	res, ok := sink.msgs[2].(core.ToolResult)
	if !ok {
		t.Fatalf("third write not the tool result: %T", sink.msgs[2])
	}
	if res.Timing == nil {
		t.Fatalf("tool result carries no timing")
	}
	b, ok := sink.msgs[3].(core.Assistant)
	if !ok || b.Content != "two" {
		t.Fatalf("fourth write not assistant(B): %T", sink.msgs[3])
	}
}

// pinStore satisfies the store seam while recording appends. Only
// AppendMessage is called by the store sink under test.
type pinStore struct {
	core.StoreProvider
	msgs []core.Message
}

func (s *pinStore) AppendMessage(ctx context.Context, sessionID string, msg core.Message) error {
	s.msgs = append(s.msgs, msg)
	return nil
}

// TestStoreSinkHeadPinning pins t_17365247 at the sink: a pinned run
// parents its FIRST conversation-carrying assistant to the branch
// point explicitly; everything after chains off the derived leaf. The
// all-empty assistant stays unwritten (nothing the provider produced).
func TestStoreSinkHeadPinning(t *testing.T) {
	st := &pinStore{}
	sink := agent.NewStoreTurnSink(st, "sess-1", "user-7")
	if sink == nil {
		t.Fatal("store sink nil")
	}
	empty := core.NewAssistant("")
	if err := sink.AppendTurnMessage(context.Background(), empty); err != nil {
		t.Fatalf("empty assistant: %v", err)
	}
	head := core.NewAssistant("head")
	if err := sink.AppendTurnMessage(context.Background(), head); err != nil {
		t.Fatalf("head: %v", err)
	}
	next := core.NewAssistant("next")
	if err := sink.AppendTurnMessage(context.Background(), next); err != nil {
		t.Fatalf("next: %v", err)
	}
	if len(st.msgs) != 2 {
		t.Fatalf("empty assistant must not write, want 2 rows, got %d", len(st.msgs))
	}
	if core.ParentIDOf(st.msgs[0]) != "user-7" {
		t.Fatalf("head not pinned: %q", core.ParentIDOf(st.msgs[0]))
	}
	if core.ParentIDOf(st.msgs[1]) != "" {
		t.Fatalf("second assistant must derive, got parent %q", core.ParentIDOf(st.msgs[1]))
	}
}
