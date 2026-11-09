package files

// dotted_e2e_test.go: the split's acceptance witness (t_d8fbad57,
// phase 2). A scripted provider drives every dotted files tool
// through the REAL agent loop - the same wire a model rides - and
// the assertions read the disk. No server, no REST: the loop's tool
// dispatch on the dotted names is the surface under test.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
)

func TestDottedToolsEndToEndThroughTheLoop(t *testing.T) {
	dir := t.TempDir()
	seed := filepath.Join(dir, "seed.txt")
	if err := os.WriteFile(seed, []byte("seed line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &FileProvider{}

	prov := agent.NewFakeProviderScripts("test",
		// Turn 1: the model exercises every dotted tool back to back -
		// one call per new tool name. Each call targets its own file:
		// the loop executes tool calls concurrently, so no call may
		// depend on another call's landing first.
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c1", Name: "files.read", Arguments: map[string]any{"path": seed},
			}},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c2", Name: "files.write", Arguments: map[string]any{
					"path": filepath.Join(dir, "born.txt"), "content": "born on the wire\n",
				},
			}},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c3", Name: "files.edit", Arguments: map[string]any{
					"path": seed, "old_string": "seed", "new_string": "patched on the wire",
				},
			}},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c4", Name: "files.batch", Arguments: map[string]any{
					"operations": []any{
						map[string]any{"action": "write", "path": filepath.Join(dir, "batch-a.txt"), "content": "A\n"},
						map[string]any{"action": "write", "path": filepath.Join(dir, "batch-b.txt"), "content": "B\n"},
					},
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
		[]core.Message{core.NewUser("exercise the file tools")}, p.Tools())

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
	}
	if results != 4 {
		t.Fatalf("the loop executed %d tool results, want 4", results)
	}

	// the disk is the truth: the write landed, the edit patched the
	// seed, the batch applied both files
	born, err := os.ReadFile(filepath.Join(dir, "born.txt"))
	if err != nil || !strings.Contains(string(born), "born on the wire") {
		t.Fatalf("files.write never landed: %q %v", string(born), err)
	}
	patched, err := os.ReadFile(seed)
	if err != nil || !strings.Contains(string(patched), "patched on the wire") {
		t.Fatalf("files.edit never landed: %q %v", string(patched), err)
	}
	for _, name := range []string{"batch-a.txt", "batch-b.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("files.batch never landed %s: %v", name, err)
		}
	}
}
