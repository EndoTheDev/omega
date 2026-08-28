package agent_loop

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// mockLogger records all Printf/Errorf calls so tests can assert on
// the logging branches in Run().
type mockLogger struct {
	printfCalls []string
	errorfCalls []string
}

func (m *mockLogger) Printf(format string, args ...any) {
	m.printfCalls = append(m.printfCalls, fmt.Sprintf(format, args...))
}

func (m *mockLogger) Errorf(format string, args ...any) {
	m.errorfCalls = append(m.errorfCalls, fmt.Sprintf(format, args...))
}

func (m *mockLogger) Close() error { return nil }

// contains checks whether any recorded call contains substr.
func contains(calls []string, substr string) bool {
	for _, c := range calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

// drainEvents runs the loop in a goroutine and blocks until the events
// channel is closed, returning an AgentEnd if one was observed.
func drainEvents(t *testing.T, events chan agent.Event) agent.AgentEnd {
	t.Helper()
	var endEvent *agent.AgentEnd
	for e := range events {
		if v, ok := e.(agent.AgentEnd); ok {
			ec := v
			endEvent = &ec
		}
	}
	if endEvent == nil {
		t.Fatal("expected AgentEnd event, got none")
	}
	return *endEvent
}

// TestLoopWithLogger verifies the logger records "agent loop starting"
// and "agent ended" for a basic one-turn conversation.
func TestLoopWithLogger(t *testing.T) {
	provider := ai.NewFakeProvider("test",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	logger := &mockLogger{}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []ai.Message{ai.NewUser("hi")},
			Events:   events,
			Logger:   logger,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "stop" {
		t.Errorf("FinishReason = %s, want stop", end.FinishReason)
	}
	if !contains(logger.printfCalls, "agent loop starting") {
		t.Errorf("printf calls missing 'agent loop starting': %v", logger.printfCalls)
	}
	if !contains(logger.printfCalls, "agent ended") {
		t.Errorf("printf calls missing 'agent ended': %v", logger.printfCalls)
	}
}

// TestLoopLoggerMaxTurns verifies the logger records "max turns reached"
// when the turn cap is hit.
func TestLoopLoggerMaxTurns(t *testing.T) {
	provider := ai.NewFakeProviderScripts("test",
		// Turn 1: emit a tool call so the loop continues.
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// Turn 2+: repeat — but MaxTurns=1 means we hit the cap before turn 2.
		[]ai.StreamEvent{
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
	)
	logger := &mockLogger{}

	tools := map[string]agent.Tool{
		"echo": {
			Description: "Echo back",
			Parameters:  map[string]any{"type": "object"},
			Run: func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		},
	}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []ai.Message{ai.NewUser("go")},
			Tools:    tools,
			Events:   events,
			Logger:   logger,
			MaxTurns: 1,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "max_turns" {
		t.Errorf("FinishReason = %s, want max_turns", end.FinishReason)
	}
	if !contains(logger.printfCalls, "max turns reached") {
		t.Errorf("printf calls missing 'max turns reached': %v", logger.printfCalls)
	}
}

// TestLoopLoggerStreamError verifies the logger records "stream error"
// when the provider sends a StreamEnd with an Error.
func TestLoopLoggerStreamError(t *testing.T) {
	provider := ai.NewFakeProvider("test",
		ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "something broke"},
	)
	logger := &mockLogger{}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []ai.Message{ai.NewUser("hi")},
			Events:   events,
			Logger:   logger,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "error" {
		t.Errorf("FinishReason = %s, want error", end.FinishReason)
	}
	if !contains(logger.errorfCalls, "stream error") {
		t.Errorf("errorf calls missing 'stream error': %v", logger.errorfCalls)
	}
}

// TestLoopLoggerToolError verifies the logger records a tool error when
// a tool's Run returns an error.
func TestLoopLoggerToolError(t *testing.T) {
	provider := ai.NewFakeProviderScripts("test",
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "boom", Arguments: map[string]any{}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "done"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	logger := &mockLogger{}

	tools := map[string]agent.Tool{
		"boom": {
			Description: "Always fails",
			Parameters:  map[string]any{"type": "object"},
			Run: func(_ context.Context, _ map[string]any) (string, error) {
				return "", fmt.Errorf("tool exploded")
			},
		},
	}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []ai.Message{ai.NewUser("go")},
			Tools:    tools,
			Events:   events,
			Logger:   logger,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "stop" {
		t.Errorf("FinishReason = %s, want stop", end.FinishReason)
	}
	if !contains(logger.errorfCalls, "tool boom error") {
		t.Errorf("errorf calls missing 'tool boom error': %v", logger.errorfCalls)
	}
	if !contains(logger.errorfCalls, "tool exploded") {
		t.Errorf("errorf calls missing 'tool exploded': %v", logger.errorfCalls)
	}
}

// TestLoopLoggerOverflowNoCompactor verifies the logger records
// "context full, no compactor loaded" when the provider sends an
// overflow error and no compactor is configured.
func TestLoopLoggerOverflowNoCompactor(t *testing.T) {
	provider := ai.NewFakeProvider("test",
		ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "context length exceeded"},
	)
	logger := &mockLogger{}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []ai.Message{ai.NewUser("hi")},
			Events:   events,
			Logger:   logger,
			// No CompactionProvider → "context full, no compactor loaded" branch.
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "error" {
		t.Errorf("FinishReason = %s, want error", end.FinishReason)
	}
	if !contains(logger.errorfCalls, "context full, no compactor loaded") {
		t.Errorf("errorf calls missing 'context full, no compactor loaded': %v", logger.errorfCalls)
	}
}

// TestPluginRequires verifies that Plugin.Requires() returns nil.
func TestPluginRequires(t *testing.T) {
	p := NewPlugin()
	req := p.Requires()
	if req != nil {
		t.Errorf("Requires() = %v, want nil", req)
	}
}