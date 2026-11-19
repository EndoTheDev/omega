package kanban

// dotted_e2e_test.go: the split's acceptance witness.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
)

func TestDottedToolsEndToEndThroughTheLoop(t *testing.T) {
	// A FILE-backed board, not:memory: the loop executes tool calls concurrently,.
	s, err := Open(filepath.Join(t.TempDir(), "kanban.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	p := &ToolProvider{board: s}
	board := KanbanProvider(s)

	// a seeded task gives the scripted comment a known id (the script
	// is static; a create inside the turn cannot feed it)
	seeded, err := board.CreateTask(context.Background(), "", KanbanTask{Title: "seeded"})
	if err != nil {
		t.Fatal(err)
	}

	prov := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c1", Name: "kanban.list", Arguments: map[string]any{},
			}},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c2", Name: "kanban.create", Arguments: map[string]any{
					"title": "born on the wire", "body": "created through kanban.create",
				},
			}},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c3", Name: "kanban.comment", Arguments: map[string]any{
					"id": seeded.ID, "body": "commented through kanban.comment", "author": "script",
				},
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
		[]core.Message{core.NewUser("exercise the board tools")}, p.Tools())

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
	if results != 3 {
		t.Fatalf("the loop executed %d tool results, want 3", results)
	}

	tasks, err := board.ListTasks(context.Background(), "", "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("the board carries %d tasks, want the seed + the wire create: %+v", len(tasks), tasks)
	}
	var created bool
	for _, task := range tasks {
		if task.Title == "born on the wire" {
			created = true
		}
	}
	if !created {
		t.Fatalf("kanban.create never landed: %+v", tasks)
	}
	comments, err := board.GetTaskComments(context.Background(), "", seeded.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "kanban.comment") || comments[0].Author != "script" {
		t.Fatalf("comments = %+v", comments)
	}
}
