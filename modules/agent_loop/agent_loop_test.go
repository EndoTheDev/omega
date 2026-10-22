package agent_loop

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// Compile-time assertions.
var _ agent.LoopProvider = Loop{}
var _ agent.Plugin = (*Plugin)(nil)

// TestLoopBasic verifies the loop runs a simple one-turn conversation.
func TestLoopBasic(t *testing.T) {
	provider := ai.NewFakeProvider("test",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []ai.Message{ai.NewUser("hi")},
			Events:   events,
		})
	}()

	var got []string
	for e := range events {
		switch v := e.(type) {
		case agent.AgentStart:
			got = append(got, "agent_start")
		case agent.TurnStart:
			got = append(got, "turn_start")
		case agent.StreamEvent:
			switch v.Event.(type) {
			case ai.ResponseChunk:
				got = append(got, "response_chunk")
			case ai.StreamEnd:
				got = append(got, "stream_end")
			}
		case agent.AgentEnd:
			got = append(got, "agent_end")
			if v.FinishReason != "stop" {
				t.Errorf("FinishReason = %s, want stop", v.FinishReason)
			}
		}
	}

	want := []string{"agent_start", "turn_start", "response_chunk", "stream_end", "agent_end"}
	if len(got) != len(want) {
		t.Fatalf("event count = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("event %d = %s, want %s", i, got[i], w)
		}
	}
}

// TestLoopNilProvider verifies the loop handles a nil provider gracefully.
func TestLoopNilProvider(t *testing.T) {
	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: nil,
			Messages: []ai.Message{ai.NewUser("hi")},
			Events:   events,
		})
	}()

	for e := range events {
		if end, ok := e.(agent.AgentEnd); ok {
			if end.FinishReason != "error" {
				t.Errorf("FinishReason = %s, want error", end.FinishReason)
			}
			if end.Error != "no provider configured" {
				t.Errorf("Error = %s, want 'no provider configured'", end.Error)
			}
			return
		}
	}
	t.Fatal("expected AgentEnd event")
}

// TestPluginMount verifies the plugin sets ctx.Loop.
func TestPluginMount(t *testing.T) {
	p := NewPlugin()
	if p.Name() != "agent-loop" {
		t.Errorf("Name = %q, want %q", p.Name(), "agent-loop")
	}
	provides := p.Provides()
	if len(provides) != 1 || provides[0] != "loop" {
		t.Errorf("Provides = %v, want [loop]", provides)
	}

	ctx := &agent.Context{}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if ctx.Loop == nil {
		t.Fatal("ctx.Loop not set after Mount")
	}
}

// TestLoopWithToolCalls verifies a two-turn loop with a tool call.
func TestLoopWithToolCalls(t *testing.T) {
	provider := ai.NewFakeProviderScripts("test",
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"msg": "hi"}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "done"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	tools := map[string]agent.Tool{
		"echo": {
			Description: "Echo back",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"msg": map[string]any{"type": "string"}}},
			Run: func(_ context.Context, args map[string]any) (string, error) {
				msg, _ := args["msg"].(string)
				return msg, nil
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
		})
	}()

	var got []string
	for e := range events {
		switch v := e.(type) {
		case agent.AgentEnd:
			got = append(got, "agent_end")
			if v.Turns != 2 {
				t.Errorf("Turns = %d, want 2", v.Turns)
			}
			if v.FinishReason != "stop" {
				t.Errorf("FinishReason = %s, want stop", v.FinishReason)
			}
		}
	}
	if len(got) == 0 {
		t.Fatal("no events received")
	}
}
// TestLoopDeniedTool pins the run-scoped tool deny (agent.WithDeniedTools):
// a provider-merged registry drops the denied name BEFORE the model sees
// schemas or dispatch - a memory tool riding a ToolProvider vanishes for
// a denied run, and a tool the model still cannot help calling errors as
// an IsError result instead of executing.
func TestLoopDeniedTool(t *testing.T) {
	provider := ai.NewFakeProviderScripts("test",
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "memory", Arguments: map[string]any{"action": "add"}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "done"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	// memory rides a PROVIDER (the real mount shape): filtering the
	// caller's Tools map alone would not reach it
	providerTools := map[string]agent.Tool{
		"memory": {
			Description: "Save a memory",
			Run: func(_ context.Context, _ map[string]any) (string, error) {
				t.Fatal("the denied tool executed")
				return "", nil
			},
		},
	}

	events := make(chan agent.Event, 64)
	ctx := agent.WithDeniedTools(context.Background(), "memory")
	go func() {
		defer close(events)
		Loop{}.Run(ctx, agent.LoopOptions{
			Provider: provider,
			Messages: []ai.Message{ai.NewUser("go")},
			Tools:    map[string]agent.Tool{},
			ToolProviders: []agent.ToolProvider{
				agent.DefaultToolProvider{ToolsMap: providerTools},
			},
			Events: events,
		})
	}()

	sawResult := false
	for e := range events {
		if te, ok := e.(agent.ToolResultEvent); ok {
			sawResult = true
			if !te.Message.IsError {
				t.Errorf("denied tool result not an error: %+v", te.Message)
			}
		}
	}
	if !sawResult {
		t.Fatal("no tool result event - the call path never ran")
	}
}
