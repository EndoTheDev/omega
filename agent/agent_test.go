package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// testLoop is a minimal LoopProvider for agent tests. It mirrors the
// real loop's behavior for the cases the agent package tests exercise:
// stream provider responses, execute tool calls, feed results back.
// The real loop lives in modules/agent_loop/ — this is a test-only
// copy that avoids an import cycle.
type testLoop struct{}

func (testLoop) Run(ctx context.Context, opts core.LoopOptions) error {
	tools := opts.Tools
	if tools == nil {
		tools = map[string]Tool{}
	}
	for _, tp := range opts.ToolProviders {
		if tp == nil {
			continue
		}
		for name, t := range tp.Tools() {
			if _, exists := tools[name]; !exists {
				tools[name] = t
			}
		}
	}

	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 100
	}

	messages := opts.Messages
	start := core.AgentStart{Type: "agent_start", ModelName: ""}
	if opts.Provider != nil {
		start.ModelName = opts.Provider.ModelName()
	}
	opts.Events <- start

	turns := 0
	overflowRetries := 0
	for {
		if ctx.Err() != nil {
			opts.Events <- core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "cancelled", Error: ctx.Err().Error()}
			return nil
		}
		if turns >= maxTurns {
			opts.Events <- core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "max_turns"}
			return nil
		}

		if opts.CompactionProvider != nil {
			compacted, err := opts.CompactionProvider.Compact(ctx, messages)
			if err != nil {
				opts.Events <- core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: err.Error()}
				return nil
			}
			messages = compacted
		}

		turns++
		opts.Events <- core.TurnStart{Type: "turn_start", Turn: turns}

		var content strings.Builder
		var toolCalls []ai.ToolCall
		finishReason := "stop"
		streamErr := ""

		if opts.Provider == nil {
			opts.Events <- core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: "no provider configured"}
			return nil
		}

		for event := range opts.Provider.Stream(ctx, messages, nil) {
			switch e := event.(type) {
			case ai.ResponseChunk:
				content.WriteString(e.Content)
			case ai.ToolCallEvent:
				toolCalls = append(toolCalls, e.ToolCall)
			case ai.StreamEnd:
				finishReason = e.FinishReason
				streamErr = e.Error
			}
			opts.Events <- core.StreamEvent{Event: event}
		}

		if streamErr != "" {
			// Overflow retry: one compaction + retry.
			if isOverflowError(streamErr) && opts.CompactionProvider != nil && overflowRetries < 1 && content.Len() == 0 {
				overflowRetries++
				compacted, err := opts.CompactionProvider.Compact(ctx, messages)
				if err != nil {
					opts.Events <- core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: err.Error()}
					return nil
				}
				messages = compacted
				continue
			}
			errMsg := streamErr
			if isOverflowError(streamErr) && opts.CompactionProvider == nil {
				errMsg = "context full — start a new session (/new)"
			}
			opts.Events <- core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: errMsg}
			return nil
		}

		assistant := ai.NewAssistant(content.String())
		assistant.ToolCalls = toolCalls
		messages = append(messages, assistant)
		opts.Events <- core.AssistantMessageEvent{Type: "assistant_message", Message: assistant}

		executed := 0
		for _, call := range toolCalls {
			tool, ok := tools[call.Name]
			if !ok {
				msg := ai.NewToolResult("unknown tool: "+call.Name, call.ID, true)
				messages = append(messages, msg)
				opts.Events <- core.ToolResultEvent{Type: "tool_result", Message: msg}
				executed++
				continue
			}
			result, err := tool.Run(ctx, call.Arguments)
			var msg ai.ToolResult
			if err != nil {
				msg = ai.NewToolResult(err.Error(), call.ID, true)
			} else {
				msg = ai.NewToolResult(result, call.ID, false)
			}
			messages = append(messages, msg)
			opts.Events <- core.ToolResultEvent{Type: "tool_result", Message: msg}
			executed++
		}

		opts.Events <- core.TurnEnd{Type: "turn_end", Turn: turns, ToolCalls: executed}

		if len(toolCalls) == 0 {
			opts.Events <- core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: finishReason, Message: assistant}
			return nil
		}
	}
}

// newTestAgent creates an Agent with the test loop pre-wired.
func newTestAgent(provider ai.Provider, tools map[string]Tool, maxTurns int) *Agent {
	a := NewAgent(provider, tools, maxTurns)
	a.SetLoopProvider(testLoop{})
	return a
}

// isOverflowError is duplicated from the real loop (modules/agent_loop/)
// because the agent test package can't import the extension.
func isOverflowError(err string) bool {
	lower := strings.ToLower(err)
	for _, phrase := range []string{"context length", "context_length", "too long", "token limit", "maximum context"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func collect(t *testing.T, events <-chan core.Event) []core.Event {
	t.Helper()
	var out []core.Event
	for e := range events {
		out = append(out, e)
	}
	return out
}

func eventTypes(events []core.Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		switch e.(type) {
		case core.AgentStart:
			out = append(out, "agent_start")
		case core.TurnStart:
			out = append(out, "turn_start")
		case core.TurnEnd:
			out = append(out, "turn_end")
		case core.AgentEnd:
			out = append(out, "agent_end")
		case core.StreamEvent:
			out = append(out, "stream")
		}
	}
	return out
}

func lastAgentEnd(events []core.Event) core.AgentEnd {
	var end core.AgentEnd
	for _, e := range events {
		if v, ok := e.(core.AgentEnd); ok {
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
	agent := newTestAgent(provider, nil, 0)
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
	agent := newTestAgent(provider, tools, 0)
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
	agent := newTestAgent(provider, tools, 2)
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
	agent := newTestAgent(provider, tools, 0)
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
	agent := newTestAgent(provider, nil, 0)
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
	agent := newTestAgent(provider, nil, 0)
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

	// Use a ToolProvider to supply the extension tool.
	extTools := core.DefaultToolProvider{ToolsMap: map[string]Tool{
		"echo_tool": {
			Run: func(_ context.Context, args map[string]any) (string, error) {
				text, _ := args["text"].(string)
				return "echo: " + text, nil
			},
		},
	}}

	agent := newTestAgent(provider, nil, 0)
	agent.SetToolProviders([]core.ToolProvider{extTools})

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

	extTools := core.DefaultToolProvider{ToolsMap: map[string]Tool{
		"echo_tool": {
			Run: func(_ context.Context, args map[string]any) (string, error) {
				return "extension", nil
			},
		},
	}}

	agent := newTestAgent(provider, builtIn, 0)
	agent.SetToolProviders([]core.ToolProvider{extTools})

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

func TestRunEventStreamCompletes(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)

	agent := newTestAgent(provider, nil, 0)

	collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("hi")}, nil))
}

func TestRunNoExtensionsCompletes(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	agent := newTestAgent(provider, nil, 0)

	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("hi")}, nil))
	if len(events) == 0 {
		t.Fatal("expected events with no extensions")
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
	agent := newTestAgent(provider, tools, 0)
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
	agent := newTestAgent(provider, tools, 0)
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
	agent := newTestAgent(provider, nil, 0)

	first := agent.Run(context.Background(), []ai.Message{ai.NewUser("go")}, nil)
	if first == nil {
		t.Fatal("first Run() returned nil, want a live channel")
	}

	// Second Run() while the first is active must spawn too - a nil
	// here used to hang a server goroutine ranging over the channel.
	second := agent.Run(context.Background(), []ai.Message{ai.NewUser("again")}, nil)
	if second == nil {
		t.Fatal("second Run() returned nil, want a live channel")
	}

	collect(t, first)
	collect(t, second)
}

func TestRunErrorBodyPassthrough(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "upstream exploded"},
	)
	agent := newTestAgent(provider, nil, 0)
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
	agent := newTestAgent(provider, nil, 0)
	agent.SetCompactionProvider(mockCompactionProvider{Provider: provider, Config: compactionConfig()})
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
	agent := newTestAgent(provider, nil, 0)
	agent.SetCompactionProvider(mockCompactionProvider{Provider: provider, Config: compactionConfig()})
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
	agent := newTestAgent(provider, nil, 0)
	cfg := compactionConfig()
	cfg.Enabled = false
	agent.SetCompactionProvider(mockCompactionProvider{Provider: provider, Config: cfg})
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
	agent := newTestAgent(provider, nil, 0)
	agent.SetCompactionProvider(mockCompactionProvider{Provider: provider, Config: compactionConfig()})
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
	agent := newTestAgent(provider, nil, 0)
	agent.SetCompactionProvider(mockCompactionProvider{Provider: provider, Config: compactionConfig()})
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
	agent := newTestAgent(provider, nil, 0) // no compaction set
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
	agent := newTestAgent(provider, nil, 0)
	agent.SetCompactionProvider(mockCompactionProvider{Provider: provider, Config: compactionConfig()})
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
	agent := newTestAgent(provider, nil, 0)
	agent.SetCompactionProvider(mockCompactionProvider{Provider: provider, Config: compactionConfig()})
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
	agent := newTestAgent(provider, nil, 0)
	agent.SetCompactionProvider(mockCompactionProvider{Provider: provider, Config: compactionConfig()})
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

// modelRecorder is an ai.Provider stub whose WithModel copies share one
// recording sink. Each Stream call records the receiving copy's model
// name, so a test can assert which model served which run.
type modelRecorder struct {
	sink      *modelRecord
	modelName string
	delay     time.Duration
}

// modelRecord accumulates model names across concurrent Stream calls.
type modelRecord struct {
	mu     sync.Mutex
	models []string
}

func (r *modelRecord) add(model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models = append(r.models, model)
}

func (r *modelRecord) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.models...)
}

func (p *modelRecorder) Stream(_ context.Context, _ []ai.Message, _ []ai.ToolSchema) <-chan ai.StreamEvent {
	if p.delay > 0 {
		time.Sleep(p.delay)
	}
	p.sink.add(p.modelName)
	events := make(chan ai.StreamEvent, 2)
	events <- ai.ResponseChunk{Type: "response_chunk", Content: "ok"}
	events <- ai.StreamEnd{Type: "stream_end", FinishReason: "stop"}
	close(events)
	return events
}

func (p *modelRecorder) ModelName() string       { return p.modelName }
func (p *modelRecorder) SetThinkingLevel(string) {}
func (p *modelRecorder) WithThinkingLevel(string) ai.Provider {
	return p
}
func (p *modelRecorder) ListModels() ([]string, error)    { return nil, nil }
func (p *modelRecorder) ModelInfo() (ai.ModelInfo, error) { return ai.ModelInfo{}, nil }

func (p *modelRecorder) WithModel(model string) ai.Provider {
	cp := *p
	cp.modelName = model
	return &cp
}

// TestConcurrentRunsServeTheirOwnModel pins the concurrency contract:
// two runs started at the same time with different WithModel copies each
// stream their own model - a per-run copy, not shared mutable state.
func TestConcurrentRunsServeTheirOwnModel(t *testing.T) {
	sink := &modelRecord{}
	base := &modelRecorder{sink: sink, modelName: "base-model", delay: 20 * time.Millisecond}
	a := newTestAgent(base, nil, 2)

	ctx := context.Background()
	messages := []ai.Message{ai.NewUser("hi")}
	first := a.RunWithProvider(ctx, a.Provider().WithModel("model-a"), messages, nil)
	second := a.RunWithProvider(ctx, a.Provider().WithModel("model-b"), messages, nil)
	if first == nil || second == nil {
		t.Fatalf("concurrent runs must each get a non-nil event channel")
	}
	collect(t, first)
	collect(t, second)

	got := sink.snapshot()
	sort.Strings(got)
	want := []string{"model-a", "model-b"}
	if len(got) != 2 {
		t.Fatalf("streams served = %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("streams served = %v, want %v (each run must serve its own model)", got, want)
		}
	}
}

// TestRunNonNilWhileRunActive pins the regression: Run used to return
// nil when a run was active, and a server goroutine ranging over the nil
// channel hung forever.
func TestRunNonNilWhileRunActive(t *testing.T) {
	sink := &modelRecord{}
	p := &modelRecorder{sink: sink, modelName: "m", delay: 50 * time.Millisecond}
	a := newTestAgent(p, nil, 2)

	ctx := context.Background()
	first := a.Run(ctx, []ai.Message{ai.NewUser("one")}, nil)
	second := a.Run(ctx, []ai.Message{ai.NewUser("two")}, nil)
	if first == nil || second == nil {
		t.Fatalf("Run must always return a non-nil channel")
	}
	collect(t, first)
	collect(t, second)

	if got := len(sink.snapshot()); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
}
