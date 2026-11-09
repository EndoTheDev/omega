package memory

// dotted_e2e_test.go: the split's acceptance witness (t_d8fbad57,
// phase 2). A scripted provider drives every dotted memory tool
// through the REAL agent loop - the same wire a model rides - and
// the assertions read the store files. No server, no REST: the
// loop's tool dispatch on the dotted names is the surface under test.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
)

func TestDottedToolsEndToEndThroughTheLoop(t *testing.T) {
	dir := t.TempDir()
	mem := NewFileMemory(Config{
		Enabled:              true,
		UserProfileEnabled:   true,
		CharLimit:            2000,
		UserProfileCharLimit: 1000,
		File:                 filepath.Join(dir, "memory.md"),
		UserProfileFile:      filepath.Join(dir, "user.md"),
	})
	// Seeded entries give each scripted call its own known target:
	// the loop executes tool calls concurrently, so no call may depend
	// on another call's landing first.
	mem.Add("memory", "alpha")
	mem.Add("memory", "beta")
	p := &memoryToolProvider{mem: mem}

	prov := agent.NewFakeProviderScripts("test",
		// Turn 1: the model exercises every dotted tool back to back -
		// one call per new tool name.
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c1", Name: "memory.add", Arguments: map[string]any{
					"target": "memory", "content": "born on the wire",
				},
			}},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c2", Name: "memory.replace", Arguments: map[string]any{
					"target": "memory", "old_text": "alpha", "content": "replaced on the wire",
				},
			}},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c3", Name: "memory.batch", Arguments: map[string]any{
					"operations": []any{
						map[string]any{"action": "add", "target": "user", "content": "user row via batch"},
					},
				},
			}},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c4", Name: "memory.remove", Arguments: map[string]any{
					"target": "memory", "old_text": "beta",
				},
			}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// Turn 2: respond and stop.
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "done"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})

	events := a.RunWithProvider(context.Background(), prov,
		[]core.Message{core.NewUser("exercise the memory tools")}, p.Tools())

	results := 0
	for e := range events {
		tre, ok := e.(core.ToolResultEvent)
		if !ok {
			continue
		}
		results++
		if tre.Message.IsError {
			t.Fatalf("tool result %s errored inside the loop: %s", tre.Message.ToolCallID, tre.Message.Content)
		}
		var res memoryResult
		if err := json.Unmarshal([]byte(tre.Message.Content), &res); err != nil {
			t.Fatalf("tool result not the JSON contract: %v (%q)", err, tre.Message.Content)
		}
		if !res.Success {
			t.Fatalf("tool result %s reported failure: %s", tre.Message.ToolCallID, tre.Message.Content)
		}
	}
	if results != 4 {
		t.Fatalf("the loop executed %d tool results, want 4", results)
	}

	// the store is the truth: the add landed, the replace won over
	// alpha, the batch landed its user row, the remove took beta out
	entries, _ := mem.List("memory")
	if len(entries) != 2 {
		t.Fatalf("memory store = %v, want the add + the replaced entry", entries)
	}
	joined := strings.Join(entries, "\n")
	if !strings.Contains(joined, "replaced on the wire") || !strings.Contains(joined, "born on the wire") {
		t.Fatalf("memory store missing the add/replace results: %v", entries)
	}
	if strings.Contains(joined, "beta") {
		t.Fatalf("memory.remove never landed: %v", entries)
	}
	userEntries, _ := mem.List("user")
	if len(userEntries) != 1 || !strings.Contains(userEntries[0], "user row via batch") {
		t.Fatalf("user store = %v, want the one batch row", userEntries)
	}
}
