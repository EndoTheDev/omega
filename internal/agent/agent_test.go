package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega-dev/internal/ai"
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

func TestRunPrependsSystemPrompt(t *testing.T) {
	provider := ai.NewFakeProvider("fake",
		ai.ResponseChunk{Type: "response_chunk", Content: "ok"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	agent := NewAgent(provider, nil, 0)
	agent.SetSystemPrompt("you are a coding agent")
	collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("hi")}, nil))

	if len(provider.LastMessages) != 2 {
		t.Fatalf("messages = %d, want 2 (system + user)", len(provider.LastMessages))
	}
	sys, ok := provider.LastMessages[0].(ai.System)
	if !ok || sys.Content != "you are a coding agent" {
		t.Fatalf("first message = %#v, want system prompt", provider.LastMessages[0])
	}
	if _, ok := provider.LastMessages[1].(ai.User); !ok {
		t.Fatalf("second message = %#v, want user", provider.LastMessages[1])
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
