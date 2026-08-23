package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
)

func collect(t *testing.T, events <-chan Event) []Event {
	t.Helper()
	var out []Event
	for e := range events {
		out = append(out, e)
	}
	return out
}

func eventTypes(events []Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		switch e.(type) {
		case AgentStart:
			out = append(out, "agent_start")
		case TurnStart:
			out = append(out, "turn_start")
		case TurnEnd:
			out = append(out, "turn_end")
		case AgentEnd:
			out = append(out, "agent_end")
		case StreamEvent:
			out = append(out, "stream")
		}
	}
	return out
}

func lastAgentEnd(events []Event) AgentEnd {
	var end AgentEnd
	for _, e := range events {
		if v, ok := e.(AgentEnd); ok {
			end = v
		}
	}
	return end
}

func TestRunSingleTurn(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	agent := NewAgent(provider, nil, 0)
	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("hi")}, nil))

	types := eventTypes(events)
	want := []string{"agent_start", "turn_start", "stream", "stream", "turn_end", "agent_end"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("event order = %v, want %v", types, want)
	}

	end := lastAgentEnd(events)
	if end.Turns != 1 || end.FinishReason != "stop" {
		t.Fatalf("AgentEnd = %+v, want 1 turn / stop", end)
	}
}

func TestRunMultiTurnToolLoop(t *testing.T) {
	provider := ai.NewFakeProviderScripts("fake",
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"text": "x"}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "done"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	tools := map[string]Tool{
		"echo": {
			Description: "echo text",
			Parameters:  map[string]any{"type": "object"},
			Run: func(_ context.Context, args map[string]any) (string, error) {
				return args["text"].(string), nil
			},
		},
	}
	agent := NewAgent(provider, tools, 0)
	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("go")}, nil))

	types := eventTypes(events)
	want := []string{"agent_start", "turn_start", "stream", "stream", "turn_end", "turn_start", "stream", "stream", "turn_end", "agent_end"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("event order = %v, want %v", types, want)
	}

	end := lastAgentEnd(events)
	if end.Turns != 2 || end.FinishReason != "stop" {
		t.Fatalf("AgentEnd = %+v, want 2 turns / stop", end)
	}
}

func TestRunMaxTurnsCap(t *testing.T) {
	provider := ai.NewFakeProviderScripts("fake",
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"text": "x"}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
	)
	tools := map[string]Tool{
		"echo": {
			Run: func(_ context.Context, _ map[string]any) (string, error) { return "x", nil },
		},
	}
	agent := NewAgent(provider, tools, 2)
	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("go")}, nil))

	end := lastAgentEnd(events)
	if end.Turns != 2 || end.FinishReason != "max_turns" {
		t.Fatalf("AgentEnd = %+v, want 2 turns / max_turns", end)
	}
}

func TestRunContextCancellation(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"text": "x"}}},
		ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
	)
	tools := map[string]Tool{
		"echo": {
			Run: func(_ context.Context, _ map[string]any) (string, error) { return "x", nil },
		},
	}
	agent := NewAgent(provider, tools, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the loop starts

	events := collect(t, agent.Run(ctx, []ai.Message{ai.NewUser("go")}, nil))

	end := lastAgentEnd(events)
	if end.FinishReason != "cancelled" || end.Error == "" {
		t.Fatalf("AgentEnd = %+v, want cancelled with error", end)
	}
}

func TestRunNoSystemPromptWithoutExtensions(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.ResponseChunk{Type: "response_chunk", Content: "ok"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	agent := NewAgent(provider, nil, 0)
	collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("hi")}, nil))

	// With no extensions loaded, no system prompt is prepended.
	// The agent sees only the user message.
	if len(provider.LastMessages) != 1 {
		t.Fatalf("messages = %d, want 1 (user only, no system prompt)", len(provider.LastMessages))
	}
	if _, ok := provider.LastMessages[0].(ai.User); !ok {
		t.Fatalf("first message = %#v, want user", provider.LastMessages[0])
	}
}

func TestRunUnknownTool(t *testing.T) {
	provider := ai.NewFakeProviderScripts("fake",
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "nope", Arguments: map[string]any{}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "ok"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	agent := NewAgent(provider, nil, 0)
	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("go")}, nil))

	end := lastAgentEnd(events)
	if end.Turns != 2 || end.FinishReason != "stop" {
		t.Fatalf("AgentEnd = %+v, want 2 turns / stop (unknown tool handled as error result)", end)
	}
}

func TestRunExtensionToolInLoop(t *testing.T) {
	provider := ai.NewFakeProviderScripts("fake",
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "echo_tool", Arguments: map[string]any{"text": "hello"}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "done"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	mgr := &StdioManager{}
	if err := mgr.Load(mockExtensionDir(t), ""); err != nil {
		t.Fatalf("load extension: %v", err)
	}
	defer mgr.Close()

	agent := NewAgent(provider, nil, 0)
	agent.SetExtensions(mgr)

	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("go")}, nil))

	// Extension tool result must reach the provider as a ToolResult message.
	var found bool
	for _, m := range provider.LastMessages {
		if tr, ok := m.(ai.ToolResult); ok && tr.ToolCallID == "c1" && strings.Contains(tr.Content, "echo: hello") && !tr.IsError {
			found = true
		}
	}
	if !found {
		t.Fatalf("extension tool result not fed back: %#v", provider.LastMessages)
	}

	end := lastAgentEnd(events)
	if end.Turns != 2 || end.FinishReason != "stop" {
		t.Fatalf("AgentEnd = %+v, want 2 turns / stop", end)
	}
}

func TestRunExtensionToolWinsNoConflictWithBuiltIn(t *testing.T) {
	provider := ai.NewFakeProviderScripts("fake",
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "echo_tool", Arguments: map[string]any{"text": "x"}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "done"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	// Built-in "echo_tool" should shadow the extension "echo_tool".
	builtIn := map[string]Tool{
		"echo_tool": {
			Run: func(_ context.Context, args map[string]any) (string, error) {
				return "built-in", nil
			},
		},
	}

	mgr := &StdioManager{}
	if err := mgr.Load(mockExtensionDir(t), ""); err != nil {
		t.Fatalf("load extension: %v", err)
	}
	defer mgr.Close()

	agent := NewAgent(provider, builtIn, 0)
	agent.SetExtensions(mgr)

	collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("go")}, nil))

	var found bool
	for _, m := range provider.LastMessages {
		if tr, ok := m.(ai.ToolResult); ok && tr.ToolCallID == "c1" && tr.Content == "built-in" {
			found = true
		}
	}
	if !found {
		t.Fatalf("built-in tool should win on name conflict: %#v", provider.LastMessages)
	}
}

func TestRunExtensionEventsDispatched(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)

	mgr := &StdioManager{}
	if err := mgr.Load(mockExtensionDir(t), ""); err != nil {
		t.Fatalf("load extension: %v", err)
	}
	defer mgr.Close()

	agent := NewAgent(provider, nil, 0)
	agent.SetExtensions(mgr)

	collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("hi")}, nil))
}

func TestRunSetExtensionsNilFallsBackToNoop(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	agent := NewAgent(provider, nil, 0)
	agent.SetExtensions(nil)

	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("hi")}, nil))
	if len(events) == 0 {
		t.Fatal("expected events with nil extension manager")
	}
}

func TestRunToolExecutionLifecycle(t *testing.T) {
	provider := ai.NewFakeProviderScripts("fake",
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"text": "x"}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "done"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	tools := map[string]Tool{
		"echo": {
			Run: func(_ context.Context, args map[string]any) (string, error) {
				return args["text"].(string), nil
			},
		},
	}
	agent := NewAgent(provider, tools, 0)
	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("go")}, nil))

	// The tool result must be appended to the history fed to the second turn.
	// LastMessages holds the messages from the most recent (second) Stream call.
	var found bool
	for _, m := range provider.LastMessages {
		if tr, ok := m.(ai.ToolResult); ok && tr.ToolCallID == "c1" && tr.Content == "x" && !tr.IsError {
			found = true
		}
	}
	if !found {
		t.Fatalf("tool result not appended to history: %#v", provider.LastMessages)
	}

	end := lastAgentEnd(events)
	if end.Turns != 2 || end.FinishReason != "stop" {
		t.Fatalf("AgentEnd = %+v, want 2 turns / stop", end)
	}
}

func TestRunToolErrorHandling(t *testing.T) {
	provider := ai.NewFakeProviderScripts("fake",
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "boom", Arguments: map[string]any{}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "ok"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	tools := map[string]Tool{
		"boom": {
			Run: func(_ context.Context, _ map[string]any) (string, error) {
				return "", errors.New("kaboom")
			},
		},
	}
	agent := NewAgent(provider, tools, 0)
	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("go")}, nil))

	// The error result must be fed back to the model as a tool result.
	var found bool
	for _, m := range provider.LastMessages {
		if tr, ok := m.(ai.ToolResult); ok && tr.ToolCallID == "c1" && tr.Content == "kaboom" && tr.IsError {
			found = true
		}
	}
	if !found {
		t.Fatalf("error result not fed back to model: %#v", provider.LastMessages)
	}

	end := lastAgentEnd(events)
	if end.Turns != 2 || end.FinishReason != "stop" {
		t.Fatalf("AgentEnd = %+v, want 2 turns / stop", end)
	}
}

func TestRunConcurrentPromptRejection(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.ResponseChunk{Type: "response_chunk", Content: "first"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	agent := NewAgent(provider, nil, 0)

	first := agent.Run(context.Background(), []ai.Message{ai.NewUser("go")}, nil)
	if first == nil {
		t.Fatal("first Run() returned nil, want a live channel")
	}

	// Second Run() while the first is active must be rejected.
	if second := agent.Run(context.Background(), []ai.Message{ai.NewUser("again")}, nil); second != nil {
		t.Fatal("second Run() returned a channel, want nil (rejected)")
	}

	collect(t, first)
}

func TestRunErrorBodyPassthrough(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "upstream exploded"},
	)
	agent := NewAgent(provider, nil, 0)
	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("hi")}, nil))

	end := lastAgentEnd(events)
	if end.FinishReason != "error" || end.Error != "upstream exploded" {
		t.Fatalf("AgentEnd = %+v, want error / upstream exploded", end)
	}
}

// compactionConfig returns a config with a small budget so tests can
// trigger compaction with short histories.
func compactionConfig() *CompactionConfig {
	return &CompactionConfig{
		Enabled:       true,
		Threshold:     0.5,
		ContextWindow: 100, // budget = 50 tokens
		KeepFirst:     1,
		KeepLast:      1,
	}
}

// bigHistory returns 20 user messages of 20 chars each: 400 chars = 100
// tokens, well over the 50-token budget.
func bigHistory() []ai.Message {
	history := make([]ai.Message, 0, 20)
	for i := 0; i < 20; i++ {
		history = append(history, ai.NewUser("message number "+fmt.Sprint(i)))
	}
	return history
}

func hasCompactedSystem(history []ai.Message) bool {
	for _, m := range history {
		if s, ok := m.(ai.System); ok && strings.Contains(s.Content, "[compacted:") {
			return true
		}
	}
	return false
}

func TestRunCompactionTriggersAtThreshold(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.ResponseChunk{Type: "response_chunk", Content: "ok"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	agent := NewAgent(provider, nil, 0)
	agent.SetCompactor(mockCompactor{Provider: provider, Config: compactionConfig()})
	collect(t, agent.Run(context.Background(), bigHistory(), nil))

	if !hasCompactedSystem(provider.LastMessages) {
		t.Fatalf("expected compacted system message in history fed to provider: %#v", provider.LastMessages)
	}
}

func TestRunCompactionNotTriggeredBelowThreshold(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.ResponseChunk{Type: "response_chunk", Content: "ok"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	agent := NewAgent(provider, nil, 0)
	agent.SetCompactor(mockCompactor{Provider: provider, Config: compactionConfig()})
	history := []ai.Message{ai.NewUser("hi"), ai.NewUser("there")} // ~10 tokens < 50
	collect(t, agent.Run(context.Background(), history, nil))

	if hasCompactedSystem(provider.LastMessages) {
		t.Fatalf("unexpected compacted system message: %#v", provider.LastMessages)
	}
}

func TestRunCompactionDisabled(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.ResponseChunk{Type: "response_chunk", Content: "ok"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	agent := NewAgent(provider, nil, 0)
	cfg := compactionConfig()
	cfg.Enabled = false
	agent.SetCompactor(mockCompactor{Provider: provider, Config: cfg})
	collect(t, agent.Run(context.Background(), bigHistory(), nil))

	if hasCompactedSystem(provider.LastMessages) {
		t.Fatalf("unexpected compacted system message with compaction disabled: %#v", provider.LastMessages)
	}
}

func TestRunCompactionErrorPropagates(t *testing.T) {
	// The first Stream call (the turn) succeeds; the second (the
	// summarize call inside compact) fails.
	provider := ai.NewFakeProviderScripts("fake",
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "ok"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
		[]ai.StreamEvent{
			ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "summarize boom"},
		},
	)
	agent := NewAgent(provider, nil, 0)
	agent.SetCompactor(mockCompactor{Provider: provider, Config: compactionConfig()})
	events := collect(t, agent.Run(context.Background(), bigHistory(), nil))

	end := lastAgentEnd(events)
	if end.FinishReason != "error" || !strings.Contains(end.Error, "summarize boom") {
		t.Fatalf("AgentEnd = %+v, want error / summarize boom", end)
	}
}

func TestRunOverflowTriggersRetry(t *testing.T) {
	// First call overflows; second call succeeds. Small history so the
	// threshold compaction does not consume the first script.
	provider := ai.NewFakeProviderScripts("fake",
		[]ai.StreamEvent{
			ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "maximum context length exceeded"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "ok"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	agent := NewAgent(provider, nil, 0)
	agent.SetCompactor(mockCompactor{Provider: provider, Config: compactionConfig()})
	history := []ai.Message{ai.NewUser("hi"), ai.NewUser("there")} // below budget
	events := collect(t, agent.Run(context.Background(), history, nil))

	if provider.Calls() != 2 {
		t.Fatalf("provider calls = %d, want 2 (overflow + retry)", provider.Calls())
	}
	end := lastAgentEnd(events)
	if end.FinishReason != "stop" {
		t.Fatalf("AgentEnd = %+v, want stop after retry", end)
	}
}

func TestRunOverflowNoRetryWhenCompactionDisabled(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "maximum context length exceeded"},
	)
	agent := NewAgent(provider, nil, 0) // no compaction set
	history := []ai.Message{ai.NewUser("hi"), ai.NewUser("there")}
	events := collect(t, agent.Run(context.Background(), history, nil))

	if provider.Calls() != 1 {
		t.Fatalf("provider calls = %d, want 1 (no retry)", provider.Calls())
	}
	end := lastAgentEnd(events)
	if end.FinishReason != "error" {
		t.Fatalf("AgentEnd = %+v, want error", end)
	}
	if end.Error != "context full — start a new session (/new)" {
		t.Fatalf("AgentEnd error = %q, want friendly overflow message", end.Error)
	}
}

func TestRunOverflowNonOverflowErrorNoRetry(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "upstream exploded"},
	)
	agent := NewAgent(provider, nil, 0)
	agent.SetCompactor(mockCompactor{Provider: provider, Config: compactionConfig()})
	history := []ai.Message{ai.NewUser("hi"), ai.NewUser("there")}
	events := collect(t, agent.Run(context.Background(), history, nil))

	if provider.Calls() != 1 {
		t.Fatalf("provider calls = %d, want 1 (non-overflow error, no retry)", provider.Calls())
	}
	end := lastAgentEnd(events)
	if end.FinishReason != "error" || end.Error != "upstream exploded" {
		t.Fatalf("AgentEnd = %+v, want error / upstream exploded", end)
	}
}

func TestRunOverflowRetryCap(t *testing.T) {
	// Two overflow scripts then success; the retry cap (1) must surface
	// the second overflow as an error.
	provider := ai.NewFakeProviderScripts("fake",
		[]ai.StreamEvent{
			ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "maximum context length exceeded"},
		},
		[]ai.StreamEvent{
			ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "maximum context length exceeded"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "ok"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	agent := NewAgent(provider, nil, 0)
	agent.SetCompactor(mockCompactor{Provider: provider, Config: compactionConfig()})
	history := []ai.Message{ai.NewUser("hi"), ai.NewUser("there")}
	events := collect(t, agent.Run(context.Background(), history, nil))

	if provider.Calls() != 2 {
		t.Fatalf("provider calls = %d, want 2 (retry cap reached)", provider.Calls())
	}
	end := lastAgentEnd(events)
	if end.FinishReason != "error" || !strings.Contains(end.Error, "context length") {
		t.Fatalf("AgentEnd = %+v, want error surfaced after retry cap", end)
	}
}

// TestRunOverflowNoRetryAfterContent verifies that an overflow error
// after response chunks were already streamed does not retry - the
// user saw the partial output and retrying would duplicate it.
func TestRunOverflowNoRetryAfterContent(t *testing.T) {
	provider := ai.NewFakeProviderScripts("fake",
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "partial "},
			ai.ResponseChunk{Type: "response_chunk", Content: "response"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "context length exceeded"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "should not appear"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	agent := NewAgent(provider, nil, 0)
	agent.SetCompactor(mockCompactor{Provider: provider, Config: compactionConfig()})
	history := []ai.Message{ai.NewUser("hi")}
	events := collect(t, agent.Run(context.Background(), history, nil))

	if provider.Calls() != 1 {
		t.Fatalf("provider calls = %d, want 1 (no retry after content emitted)", provider.Calls())
	}
	end := lastAgentEnd(events)
	if end.FinishReason != "error" {
		t.Fatalf("AgentEnd finish = %q, want error", end.FinishReason)
	}
}
