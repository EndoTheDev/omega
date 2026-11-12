package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// Compile-time assertions.
var _ agent.LoopProvider = Loop{}
var _ agent.Plugin = (*Plugin)(nil)

// TestLoopBasic verifies the loop runs a simple one-turn conversation.
func TestLoopBasic(t *testing.T) {
	provider := agent.NewFakeProvider("test",
		core.ResponseChunk{Type: "response_chunk", Content: "hello"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)

	events := make(chan core.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []core.Message{core.NewUser("hi")},
			Events:   events,
		})
	}()

	var got []string
	for e := range events {
		switch v := e.(type) {
		case core.AgentStart:
			got = append(got, "agent_start")
		case core.TurnStart:
			got = append(got, "turn_start")
		case core.StreamEvent:
			switch v.Event.(type) {
			case core.ResponseChunk:
				got = append(got, "response_chunk")
			case core.StreamEnd:
				got = append(got, "stream_end")
			}
		case core.AgentEnd:
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
	events := make(chan core.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: nil,
			Messages: []core.Message{core.NewUser("hi")},
			Events:   events,
		})
	}()

	for e := range events {
		if end, ok := e.(core.AgentEnd); ok {
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

	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if ctx.Services().Resolve("loop") == nil {
		t.Fatal("ctx.Loop not set after Mount")
	}
}

// TestLoopWithToolCalls verifies a two-turn loop with a tool call.
func TestLoopWithToolCalls(t *testing.T) {
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

	tools := map[string]core.Tool{
		"echo": {
			Description: "Echo back",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"msg": map[string]any{"type": "string"}}},
			Run: func(_ context.Context, args map[string]any) (string, error) {
				msg, _ := args["msg"].(string)
				return msg, nil
			},
		},
	}

	events := make(chan core.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []core.Message{core.NewUser("go")},
			Tools:    tools,
			Events:   events,
		})
	}()

	var got []string
	for e := range events {
		switch v := e.(type) {
		case core.AgentEnd:
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
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "memory", Arguments: map[string]any{"action": "add"}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "done"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	// memory rides a PROVIDER (the real mount shape): filtering the
	// caller's Tools map alone would not reach it
	providerTools := map[string]core.Tool{
		"memory": {
			Description: "Save a memory",
			Run: func(_ context.Context, _ map[string]any) (string, error) {
				t.Fatal("the denied tool executed")
				return "", nil
			},
		},
	}

	events := make(chan core.Event, 64)
	ctx := agent.WithDeniedTools(context.Background(), "memory")
	go func() {
		defer close(events)
		Loop{}.Run(ctx, agent.LoopOptions{
			Provider: provider,
			Messages: []core.Message{core.NewUser("go")},
			Tools:    map[string]core.Tool{},
			ToolProviders: []core.ToolProvider{
				core.DefaultToolProvider{ToolsMap: providerTools},
			},
			Events: events,
		})
	}()

	sawResult := false
	for e := range events {
		if te, ok := e.(core.ToolResultEvent); ok {
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

// TestLoopToolless pins the toolless run (agent.WithToolless): the merged
// registry - the caller's map PLUS every tool provider's contribution -
// is replaced wholesale, so a side-effectful tool (delegate.task is the
// live case: the title turn spawned real subagents) is absent from the
// schema list and the dispatch map, and a model that still cannot help
// calling it errors as an IsError result instead of executing. Unlike
// the deny list, no name is enumerated: the run is toolless, period.
func TestLoopToolless(t *testing.T) {
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "delegate.task", Arguments: map[string]any{"goal": "spawn"}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "done"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	providerTools := map[string]core.Tool{
		"delegate.task": {
			Description: "Delegate a task",
			Run: func(_ context.Context, _ map[string]any) (string, error) {
				t.Fatal("the toolless run executed a tool")
				return "", nil
			},
		},
	}
	callerTools := map[string]core.Tool{
		"shell": {
			Description: "Run a shell command",
			Run: func(_ context.Context, _ map[string]any) (string, error) {
				t.Fatal("the toolless run executed a tool")
				return "", nil
			},
		},
	}

	events := make(chan core.Event, 64)
	ctx := agent.WithToolless(context.Background())
	go func() {
		defer close(events)
		Loop{}.Run(ctx, agent.LoopOptions{
			Provider: provider,
			Messages: []core.Message{core.NewUser("go")},
			Tools:    callerTools,
			ToolProviders: []core.ToolProvider{
				core.DefaultToolProvider{ToolsMap: providerTools},
			},
			Events: events,
		})
	}()

	sawError := false
	for e := range events {
		if te, ok := e.(core.ToolResultEvent); ok {
			sawError = true
			if !te.Message.IsError {
				t.Errorf("toolless run result not an error: %+v", te.Message)
			}
		}
	}
	if !sawError {
		t.Fatal("no tool result event - the call path never ran")
	}
}

// fixedToolSet is the pinned plugin-shaped tool set both determinism
// tests mount: 7 tools (>8 is not required - a single Go map bucket is
// traversed from a randomized start offset, so even a small map yields a
// different range order per call). Tool names are deliberately NOT
// alphabetical in declaration order, so only the sort can make them
// alphabetical on the wire.
func fixedToolSet() map[string]core.Tool {
	return map[string]core.Tool{
		"shell.run": {
			Description: "Run a shell command",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}},
			Run:         func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
		},
		"memory": {
			Description: "Domain tool",
			Actions:     []core.ToolAction{{Name: "add"}, {Name: "remove"}},
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"action": map[string]any{"type": "string"}}},
			Run:         func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
		},
		"files.read": {
			Description: "Read a file",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
			Run:         func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
		},
		"kanban": {
			Description: "Board ops",
			Run:         func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
		},
		"cron": {
			Description: "Scheduled prompts",
			Run:         func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
		},
		"delegate.task": {
			Description: "Spawn a subagent",
			Run:         func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
		},
		"skills": {
			Description: "Skill inventory",
			Run:         func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
		},
	}
}

// TestToolSchemasDeterministicJSON pins the sort in toolSchemas: five
// serializations of the same tool map are byte-identical, and the names
// arrive in sorted order. Without the sort the map's randomized range
// order rotates the list from call to call, and the tools[] block - the
// wire's largest stable prefix - diverges at its first tool, which drops
// the provider's prompt-cache hit back to the system prompt alone.
func TestToolSchemasDeterministicJSON(t *testing.T) {
	tools := fixedToolSet()

	var first []byte
	var firstNames []string
	for i := 0; i < 5; i++ {
		schemas := toolSchemas(tools)
		if len(schemas) != len(tools) {
			t.Fatalf("call %d: schemas = %d, want %d", i, len(schemas), len(tools))
		}
		names := make([]string, len(schemas))
		for j, s := range schemas {
			names[j] = s.Name
		}
		if !sort.StringsAreSorted(names) {
			t.Fatalf("call %d: tool names not sorted: %v", i, names)
		}
		raw, err := json.Marshal(schemas)
		if err != nil {
			t.Fatalf("call %d: marshal: %v", i, err)
		}
		if i == 0 {
			first, firstNames = raw, names
			continue
		}
		if !bytes.Equal(first, raw) {
			t.Fatalf("call %d: tools[] JSON differs from call 0\n first: %s\n call %d: %s", i, first, i, raw)
		}
		if !reflect.DeepEqual(firstNames, names) {
			t.Fatalf("call %d: order %v != call 0 order %v", i, names, firstNames)
		}
	}
}

// recordingProvider wraps a Provider and records the tools argument of
// every Stream call - the exact slice the wire serializes.
type recordingProvider struct {
	agent.Provider
	toolsJSON [][]byte
	toolsAddr []uintptr
}

func (r *recordingProvider) Stream(ctx context.Context, messages []core.Message, tools []core.ToolSchema) <-chan core.ProviderStreamEvent {
	raw, err := json.Marshal(tools)
	if err != nil {
		panic(err)
	}
	r.toolsJSON = append(r.toolsJSON, raw)
	r.toolsAddr = append(r.toolsAddr, reflect.ValueOf(tools).Pointer())
	return r.Provider.Stream(ctx, messages, tools)
}

// TestLoopToolsBlockStableAcrossTurns pins the HOIST and the sort at the
// real path (agent_loop.go's turn loop -> Provider.Stream):
//
//   - every turn of a multi-turn run hands the provider byte-identical
//     tools[] JSON (the prefix a cache can reuse),
//   - every turn hands it the SAME backing array - the schema list is
//     computed once per Run, not re-derived per turn,
//   - a second run over the same tool map produces the same bytes again
//     (determinism does not depend on run-time state).
func TestLoopToolsBlockStableAcrossTurns(t *testing.T) {
	tools := fixedToolSet()

	run := func(t *testing.T) *recordingProvider {
		t.Helper()
		inner := agent.NewFakeProviderScripts("test",
			[]core.ProviderStreamEvent{
				core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "shell.run", Arguments: map[string]any{"command": "true"}}},
				core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
			},
			[]core.ProviderStreamEvent{
				core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c2", Name: "cron", Arguments: map[string]any{}}},
				core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
			},
			[]core.ProviderStreamEvent{
				core.ResponseChunk{Type: "response_chunk", Content: "done"},
				core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
			},
		)
		rec := &recordingProvider{Provider: inner}

		events := make(chan core.Event, 64)
		done := make(chan error, 1)
		go func() {
			defer close(events)
			done <- Loop{}.Run(context.Background(), agent.LoopOptions{
				Provider: rec,
				Messages: []core.Message{core.NewUser("go")},
				Tools:    tools,
				Events:   events,
			})
		}()
		for range events {
		}
		if err := <-done; err != nil {
			t.Fatalf("run: %v", err)
		}
		return rec
	}

	first := run(t)
	if len(first.toolsJSON) != 3 {
		t.Fatalf("turns = %d, want 3", len(first.toolsJSON))
	}
	for i, raw := range first.toolsJSON {
		var got []core.ToolSchema
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("turn %d: unmarshal tools[]: %v", i, err)
		}
		names := make([]string, len(got))
		for j, s := range got {
			names[j] = s.Name
		}
		if !sort.StringsAreSorted(names) {
			t.Fatalf("turn %d: wire tool order not sorted: %v", i, names)
		}
	}
	for i := 1; i < len(first.toolsJSON); i++ {
		if !bytes.Equal(first.toolsJSON[0], first.toolsJSON[i]) {
			t.Fatalf("turn %d tools[] differs from turn 0\n turn 0: %s\n turn %d: %s", i, first.toolsJSON[0], i, first.toolsJSON[i])
		}
		if first.toolsAddr[i] != first.toolsAddr[0] {
			t.Fatalf("turn %d tools slice re-allocated (addr %x != %x) - schema list not hoisted out of the turn loop", i, first.toolsAddr[i], first.toolsAddr[0])
		}
	}

	second := run(t)
	if !bytes.Equal(first.toolsJSON[0], second.toolsJSON[0]) {
		t.Fatalf("tools[] differs across runs\n run 1: %s\n run 2: %s", first.toolsJSON[0], second.toolsJSON[0])
	}
}
