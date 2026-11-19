package skills

// dotted_e2e_test.go: the split's acceptance witness.

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
	dir := makeSkillDir(t)
	// a second seeded skill gives the update its own target: the loop executes tool calls
	// concurrently, so no call may depend on another call's landing first.
	if err := os.MkdirAll(filepath.Join(dir, "update-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "update-skill", "SKILL.md"),
		[]byte("---\ndescription: A patch target\n---\npatch me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sp := NewSkillsProvider(dir)

	prov := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c1", Name: "skills.read", Arguments: map[string]any{"name": "test-skill"},
			}},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c2", Name: "skills.write", Arguments: map[string]any{
					"name":        "born-on-the-wire",
					"description": "Made by the loop",
					"body":        "# born\n\nOn the wire.\n",
				},
			}},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c3", Name: "skills.update", Arguments: map[string]any{
					"name": "update-skill", "old_text": "patch me", "new_text": "patched on the wire",
				},
			}},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c4", Name: "skills.curate", Arguments: map[string]any{"verb": "stale"},
			}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "done"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})

	events := a.RunWithProvider(context.Background(), prov,
		[]core.Message{core.NewUser("exercise the skills tools")}, sp.Tools())

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

	raw, err := os.ReadFile(filepath.Join(dir, "born-on-the-wire", "SKILL.md"))
	if err != nil {
		t.Fatalf("skills.write never landed: %v", err)
	}
	if !strings.HasPrefix(string(raw), "---\ndescription: Made by the loop\nauthor: agent\n---\n") {
		t.Fatalf("written SKILL.md frontmatter = %q", string(raw))
	}
	patched, err := os.ReadFile(filepath.Join(dir, "update-skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(patched), "patched on the wire") {
		t.Fatalf("skills.update never landed: %q", string(patched))
	}
}
