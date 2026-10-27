package kanban

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func toolProvider(t *testing.T) (*ToolProvider, KanbanProvider) {
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
	tool, ok := tools["kanban.task"]
	if !ok {
		t.Fatal(`missing tool "kanban.task"`)
	}
	if tool.Run == nil || tool.Description == "" {
		t.Fatal("broken registration")
	}
	if len(tools) != 1 {
		t.Fatalf("the registry carries the one folded namespaced tool; got %d entries", len(tools))
	}
}

func TestTaskCreateAutoLinksSession(t *testing.T) {
	p, _ := toolProvider(t)
	out, err := p.runCreate(withSession(context.Background(), "sess_9"), "", map[string]any{
		"title": "wire the seam",
		"body":  "spec body",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := strings.Fields(out)[1]
	task, err := p.board.GetTask(context.Background(), "", id)
	if err != nil {
		t.Fatal(err)
	}
	if task.SessionID != "sess_9" {
		t.Errorf("session = %q, want sess_9", task.SessionID)
	}
}

func TestTaskListFormatsAndFilters(t *testing.T) {
	p, _ := toolProvider(t)
	if _, err := p.runCreate(context.Background(), "", map[string]any{"title": "one"}); err != nil {
		t.Fatal(err)
	}
	out, err := p.runList(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "one") {
		t.Fatalf("list = %q", out)
	}
	out, err = p.runList(context.Background(), "", map[string]any{"status": StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	if out != "no tasks" {
		t.Fatalf("done filter = %q", out)
	}
}

func TestTaskUpdateCompleteCommentLink(t *testing.T) {
	p, _ := toolProvider(t)
	out, _ := p.runCreate(context.Background(), "", map[string]any{"title": "flow"})
	id := strings.Fields(out)[1]

	status := StatusDone
	if _, err := p.runUpdate(context.Background(), "", map[string]any{"id": id, "status": status}); err != nil {
		t.Fatalf("update: %v", err)
	}
	task, _ := p.board.GetTask(context.Background(), "", id)
	if task.Status != StatusDone || task.CompletedAt == "" {
		t.Fatalf("task = %+v", task)
	}

	if out, err := p.runComplete(context.Background(), "", map[string]any{"id": id}); err != nil || !strings.Contains(out, "done") {
		t.Fatalf("complete: %q %v", out, err)
	}

	if _, err := p.runComment(context.Background(), "", map[string]any{"id": "t_00000000", "body": "missing"}); err != ErrNotFound {
		t.Fatalf("comment on missing: %v", err)
	}
	if _, err := p.runComment(context.Background(), "", map[string]any{"id": id, "body": "note", "author": "endo"}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	comments, _ := p.board.GetTaskComments(context.Background(), "", id)
	if len(comments) != 1 || comments[0].Author != "endo" {
		t.Fatalf("comments = %+v", comments)
	}

	// run_link with no session in ctx and no arg is an error
	if _, err := p.runLink(context.Background(), "", map[string]any{"id": id}); err == nil {
		t.Fatal("link without any session succeeded")
	}
	if _, err := p.runLink(withSession(context.Background(), "sess_1"), "", map[string]any{"id": id}); err != nil {
		t.Fatalf("link via ctx: %v", err)
	}
	task, _ = p.board.GetTask(context.Background(), "", id)
	if task.SessionID != "sess_1" {
		t.Errorf("session = %q, want sess_1", task.SessionID)
	}
	if _, err := p.runLink(context.Background(), "", map[string]any{"id": id, "session_id": "sess_2"}); err != nil {
		t.Fatalf("link via arg: %v", err)
	}
	task, _ = p.board.GetTask(context.Background(), "", id)
	if task.SessionID != "sess_2" {
		t.Errorf("session = %q, want sess_2", task.SessionID)
	}
}

func TestTaskUpdateEmptyAssigneeUnassigns(t *testing.T) {
	p, _ := toolProvider(t)
	out, _ := p.runCreate(context.Background(), "", map[string]any{"title": "assignme", "assignee": "cody"})
	id := strings.Fields(out)[1]
	empty := ""
	if _, err := p.runUpdate(context.Background(), "", map[string]any{"id": id, "assignee": empty}); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	task, _ := p.board.GetTask(context.Background(), "", id)
	if task.Assignee != "" {
		t.Errorf("assignee = %q, want empty", task.Assignee)
	}
}

// TestRunTaskActionEnumCoversAllVerbs is the consolidation gate: every
// pre-fold verb's behavior is reachable through the folded task tool's
// action enum.
func TestRunTaskActionEnumCoversAllVerbs(t *testing.T) {
	p, board := toolProvider(t)
	ctx := withSession(context.Background(), "sess_fold")

	// create
	out, err := p.runTask(ctx, map[string]any{"action": "create", "title": "folded flow", "body": "spec"})
	if err != nil || !strings.Contains(out, "created t_") {
		t.Fatalf("create: %q %v", out, err)
	}
	id := strings.Fields(out)[1]

	// list
	out, err = p.runTask(ctx, map[string]any{"action": "list"})
	if err != nil || !strings.Contains(out, "folded flow") {
		t.Fatalf("list: %q %v", out, err)
	}

	// update
	out, err = p.runTask(ctx, map[string]any{"action": "update", "id": id, "status": StatusRunning})
	if err != nil || !strings.Contains(out, id) {
		t.Fatalf("update: %q %v", out, err)
	}

	// comment
	out, err = p.runTask(ctx, map[string]any{"action": "comment", "id": id, "body": "hello"})
	if err != nil || !strings.Contains(out, "commented on") {
		t.Fatalf("comment: %q %v", out, err)
	}
	comments, _ := board.GetTaskComments(context.Background(), "", id)
	if len(comments) != 1 {
		t.Fatalf("comments = %+v", comments)
	}

	// link_session
	out, err = p.runTask(ctx, map[string]any{"action": "link_session", "id": id, "session_id": "sess_x"})
	if err != nil || !strings.Contains(out, "linked") {
		t.Fatalf("link_session: %q %v", out, err)
	}

	// complete
	out, err = p.runTask(ctx, map[string]any{"action": "complete", "id": id})
	if err != nil || !strings.Contains(out, "done") {
		t.Fatalf("complete: %q %v", out, err)
	}
	task, _ := board.GetTask(context.Background(), "", id)
	if task.Status != StatusDone || task.CompletedAt == "" {
		t.Fatalf("task = %+v", task)
	}

	// unknown / empty action
	if _, err := p.runTask(ctx, map[string]any{}); err == nil || !strings.Contains(err.Error(), "action is required") {
		t.Fatalf("empty action: %v", err)
	}
}

// TestRunTaskBatch covers the batch surface: create several in one
// call, and update+comment together.
func TestRunTaskBatch(t *testing.T) {
	p, board := toolProvider(t)
	ctx := withSession(context.Background(), "sess_batch")

	out, err := p.runTask(ctx, map[string]any{"operations": []map[string]any{
		{"action": "create", "title": "batch one"},
		{"action": "create", "title": "batch two"},
	}})
	if err != nil || !strings.Contains(out, "2 operations applied") {
		t.Fatalf("create batch: %q %v", out, err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), out)
	}
	id1 := strings.Fields(strings.TrimPrefix(lines[0], "1: "))[1]

	out, err = p.runTask(ctx, map[string]any{"operations": []map[string]any{
		{"action": "update", "id": id1, "status": StatusBlocked},
		{"action": "comment", "id": id1, "body": "blocked for a reason"},
	}})
	if err != nil || !strings.Contains(out, "2 operations applied") {
		t.Fatalf("update+comment batch: %q %v", out, err)
	}
	task, _ := board.GetTask(context.Background(), "", id1)
	if task.Status != StatusBlocked {
		t.Fatalf("status = %s", task.Status)
	}
	comments, _ := board.GetTaskComments(context.Background(), "", id1)
	if len(comments) != 1 {
		t.Fatalf("comments = %+v", comments)
	}
}

// TestRunTaskBatchFailForward pins the DB-surface batch semantics: the
// batch stops at the first failure, reporting what already applied.
func TestRunTaskBatchFailForward(t *testing.T) {
	p, _ := toolProvider(t)
	ctx := context.Background()

	out, _ := p.runTask(ctx, map[string]any{"operations": []map[string]any{
		{"action": "create", "title": "kept"},
		{"action": "update", "id": "t_missing", "status": StatusDone},
		{"action": "create", "title": "never created"},
	}})
	if !strings.Contains(out, "failed at operations[1]") {
		t.Fatalf("expected fail-forward report, got %q", out)
	}
	if strings.Contains(out, "never created") && !strings.Contains(out, "kept") {
		t.Fatalf("partial results lost: %q", out)
	}
	tasks, _ := p.board.ListTasks(ctx, "", "")
	if len(tasks) != 1 || tasks[0].Title != "kept" {
		t.Fatalf("task after failed batch = %+v", tasks)
	}
}

// TestTaskAliasesRetired pins the v0.12 phase-C1 retirement: the
// machine-facing identity is the module.verb namespace pair, so the
// legacy dotted alias shims no longer register at all. The folded
// dispatcher keeps the same action vocabulary.
func TestTaskAliasesRetired(t *testing.T) {
	p, _ := toolProvider(t)
	tools := p.Tools()
	if len(tools) != 1 {
		t.Fatalf("alias shims must not return: %d entries", len(tools))
	}
	for name, tool := range tools {
		if tool.AliasOf != "" {
			t.Fatalf("%s registered as an alias of %q - the shim era is closed", name, tool.AliasOf)
		}
	}
}