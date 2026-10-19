package kanban

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func toolProvider(t *testing.T) (*ToolProvider, agent.KanbanProvider) {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return &ToolProvider{board: s}, s
}

// withSession wraps ctx like the hosts do before RunWithProvider.
func withSession(ctx context.Context, id string) context.Context {
	return agent.WithSessionID(ctx, id)
}

func TestToolsRegistryComplete(t *testing.T) {
	p, _ := toolProvider(t)
	tools := p.Tools()
	for _, name := range []string{
		"task_create", "task_list", "task_update",
		"task_comment", "task_complete", "task_link_session",
	} {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		if tool.Run == nil || tool.Description == "" {
			t.Fatalf("%s: broken registration", name)
		}
	}
}

func TestTaskCreateAutoLinksSession(t *testing.T) {
	p, _ := toolProvider(t)
	out, err := p.runCreate(withSession(context.Background(), "sess_9"), map[string]any{
		"title": "wire the seam",
		"body":  "spec body",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := strings.Fields(out)[1]
	task, err := p.board.GetTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if task.SessionID != "sess_9" {
		t.Errorf("session = %q, want sess_9", task.SessionID)
	}
}

func TestTaskListFormatsAndFilters(t *testing.T) {
	p, _ := toolProvider(t)
	if _, err := p.runCreate(context.Background(), map[string]any{"title": "one"}); err != nil {
		t.Fatal(err)
	}
	out, err := p.runList(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "one") {
		t.Fatalf("list = %q", out)
	}
	out, err = p.runList(context.Background(), map[string]any{"status": StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	if out != "no tasks" {
		t.Fatalf("done filter = %q", out)
	}
}

func TestTaskUpdateCompleteCommentLink(t *testing.T) {
	p, _ := toolProvider(t)
	out, _ := p.runCreate(context.Background(), map[string]any{"title": "flow"})
	id := strings.Fields(out)[1]

	status := StatusDone
	if _, err := p.runUpdate(context.Background(), map[string]any{"id": id, "status": status}); err != nil {
		t.Fatalf("update: %v", err)
	}
	task, _ := p.board.GetTask(context.Background(), id)
	if task.Status != StatusDone || task.CompletedAt == "" {
		t.Fatalf("task = %+v", task)
	}

	if out, err := p.runComplete(context.Background(), map[string]any{"id": id}); err != nil || !strings.Contains(out, "done") {
		t.Fatalf("complete: %q %v", out, err)
	}

	if _, err := p.runComment(context.Background(), map[string]any{"id": "t_00000000", "body": "missing"}); err != ErrNotFound {
		t.Fatalf("comment on missing: %v", err)
	}
	if _, err := p.runComment(context.Background(), map[string]any{"id": id, "body": "note", "author": "endo"}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	comments, _ := p.board.GetTaskComments(context.Background(), id)
	if len(comments) != 1 || comments[0].Author != "endo" {
		t.Fatalf("comments = %+v", comments)
	}

	// run_link with no session in ctx and no arg is an error
	if _, err := p.runLink(context.Background(), map[string]any{"id": id}); err == nil {
		t.Fatal("link without any session succeeded")
	}
	if _, err := p.runLink(withSession(context.Background(), "sess_1"), map[string]any{"id": id}); err != nil {
		t.Fatalf("link via ctx: %v", err)
	}
	task, _ = p.board.GetTask(context.Background(), id)
	if task.SessionID != "sess_1" {
		t.Errorf("session = %q, want sess_1", task.SessionID)
	}
	if _, err := p.runLink(context.Background(), map[string]any{"id": id, "session_id": "sess_2"}); err != nil {
		t.Fatalf("link via arg: %v", err)
	}
	task, _ = p.board.GetTask(context.Background(), id)
	if task.SessionID != "sess_2" {
		t.Errorf("session = %q, want sess_2", task.SessionID)
	}
}

func TestTaskUpdateEmptyAssigneeUnassigns(t *testing.T) {
	p, _ := toolProvider(t)
	out, _ := p.runCreate(context.Background(), map[string]any{"title": "assignme", "assignee": "cody"})
	id := strings.Fields(out)[1]
	empty := ""
	if _, err := p.runUpdate(context.Background(), map[string]any{"id": id, "assignee": empty}); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	task, _ := p.board.GetTask(context.Background(), id)
	if task.Assignee != "" {
		t.Errorf("assignee = %q, want empty", task.Assignee)
	}
}