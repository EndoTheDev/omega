package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega-dev/internal/ai"
)

// mockProvider is a scripted Provider for tests. Each call returns the
// next scripted stream; the final script repeats indefinitely so loops
// that must keep going (e.g. max-turns) stay alive. A script that ends
// with no tool calls terminates the loop regardless.
type mockProvider struct {
	modelName string
	scripts   [][]ai.StreamEvent
	calls     int
}

func (m *mockProvider) ModelName() string { return m.modelName }

func (m *mockProvider) Stream(_ context.Context, _ []ai.Message, _ []ai.ToolSchema) <-chan ai.StreamEvent {
	events := make(chan ai.StreamEvent)
	go func() {
		defer close(events)
		index := m.calls
		if index >= len(m.scripts) {
			index = len(m.scripts) - 1
		}
		if index >= 0 {
			for _, e := range m.scripts[index] {
				events <- e
			}
		}
		m.calls++
	}()
	return events
}

func scripted(events ...ai.StreamEvent) []ai.StreamEvent { return events }

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

func TestRunSingleTurn(t *testing.T) {
	provider := &mockProvider{
		modelName: "mock",
		scripts: [][]ai.StreamEvent{
			scripted(
				ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
				ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
			),
		},
	}
	agent := NewAgent(provider, nil, 0)
	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("hi")}, nil))

	types := eventTypes(events)
	want := []string{"agent_start", "turn_start", "stream", "stream", "turn_end", "agent_end"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("event order = %v, want %v", types, want)
	}

	var end AgentEnd
	for _, e := range events {
		if v, ok := e.(AgentEnd); ok {
			end = v
		}
	}
	if end.Turns != 1 || end.FinishReason != "stop" {
		t.Fatalf("AgentEnd = %+v, want 1 turn / stop", end)
	}
}

func TestRunMultiTurnToolLoop(t *testing.T) {
	provider := &mockProvider{
		modelName: "mock",
		scripts: [][]ai.StreamEvent{
			scripted(
				ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"text": "x"}}},
				ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
			),
			scripted(
				ai.ResponseChunk{Type: "response_chunk", Content: "done"},
				ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
			),
		},
	}
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

	var end AgentEnd
	for _, e := range events {
		if v, ok := e.(AgentEnd); ok {
			end = v
		}
	}
	if end.Turns != 2 || end.FinishReason != "stop" {
		t.Fatalf("AgentEnd = %+v, want 2 turns / stop", end)
	}
}

func TestRunMaxTurnsCap(t *testing.T) {
	provider := &mockProvider{
		modelName: "mock",
		scripts: [][]ai.StreamEvent{
			scripted(
				ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"text": "x"}}},
				ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
			),
		},
	}
	tools := map[string]Tool{
		"echo": {
			Run: func(_ context.Context, _ map[string]any) (string, error) { return "x", nil },
		},
	}
	agent := NewAgent(provider, tools, 2)
	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("go")}, nil))

	var end AgentEnd
	for _, e := range events {
		if v, ok := e.(AgentEnd); ok {
			end = v
		}
	}
	if end.Turns != 2 || end.FinishReason != "max_turns" {
		t.Fatalf("AgentEnd = %+v, want 2 turns / max_turns", end)
	}
}

func TestRunContextCancellation(t *testing.T) {
	provider := &mockProvider{
		modelName: "mock",
		scripts: [][]ai.StreamEvent{
			scripted(
				ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"text": "x"}}},
				ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
			),
		},
	}
	tools := map[string]Tool{
		"echo": {
			Run: func(_ context.Context, _ map[string]any) (string, error) { return "x", nil },
		},
	}
	agent := NewAgent(provider, tools, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the loop starts

	events := collect(t, agent.Run(ctx, []ai.Message{ai.NewUser("go")}, nil))

	var end AgentEnd
	for _, e := range events {
		if v, ok := e.(AgentEnd); ok {
			end = v
		}
	}
	if end.FinishReason != "cancelled" || end.Error == "" {
		t.Fatalf("AgentEnd = %+v, want cancelled with error", end)
	}
}

func TestRunUnknownTool(t *testing.T) {
	provider := &mockProvider{
		modelName: "mock",
		scripts: [][]ai.StreamEvent{
			scripted(
				ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "nope", Arguments: map[string]any{}}},
				ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
			),
			scripted(
				ai.ResponseChunk{Type: "response_chunk", Content: "ok"},
				ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
			),
		},
	}
	agent := NewAgent(provider, nil, 0)
	events := collect(t, agent.Run(context.Background(), []ai.Message{ai.NewUser("go")}, nil))

	var end AgentEnd
	for _, e := range events {
		if v, ok := e.(AgentEnd); ok {
			end = v
		}
	}
	if end.Turns != 2 || end.FinishReason != "stop" {
		t.Fatalf("AgentEnd = %+v, want 2 turns / stop (unknown tool handled as error result)", end)
	}
}
