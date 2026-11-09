package kanban

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
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

// dottedToolNames is the full split wire (the B ruling, t_f616ed78):
// exactly these seven, and no flat kanban tool.
var dottedToolNames = []string{
	"kanban.list",
	"kanban.create",
	"kanban.update",
	"kanban.complete",
	"kanban.comment",
	"kanban.delete",
	"kanban.batch",
}

func TestToolsRegistryCarriesSevenDottedTools(t *testing.T) {
	p, _ := toolProvider(t)
	tools := p.Tools()
	if len(tools) != len(dottedToolNames) {
		t.Fatalf("the registry carries the seven dotted kanban tools; got %d entries: %v", len(tools), tools)
	}
	for _, name := range dottedToolNames {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("missing tool %q (have %v)", name, tools)
		}
		if tool.Run == nil || tool.Description == "" || tool.Parameters == nil {
			t.Fatalf("%s: broken registration", name)
		}
	}
	if _, ok := tools["kanban"]; ok {
		t.Fatal(`the flat "kanban" tool must be gone - the B ruling split it`)
	}
}

// TestDottedToolSchemasCarryOnlyTheirOwnArgs pins the split's schema
// win (the card's acceptance): each tool's property set is exactly its
// own verb's args and its required list is exactly its own - comment
// carries no title, create carries no operations, list carries no
// required key at all, and no tool knows the retired action property.
func TestDottedToolSchemasCarryOnlyTheirOwnArgs(t *testing.T) {
	p, _ := toolProvider(t)
	tools := p.Tools()

	for name, tool := range tools {
		if tool.Actions != nil {
			t.Fatalf("%s carries Actions metadata - the dotted tools carry no action enum", name)
		}
		if _, ok := schemaProps(t, tool)["action"]; ok {
			t.Fatalf("%s schema still declares the action property - the verb IS the name", name)
		}
	}

	comment := tools["kanban.comment"]
	for _, arg := range []string{"id", "body", "author", "board"} {
		if _, ok := schemaProps(t, comment)[arg]; !ok {
			t.Fatalf("kanban.comment lost its own arg %q", arg)
		}
	}
	for _, foreign := range []string{"title", "priority", "status", "assignee", "source", "operations"} {
		if _, ok := schemaProps(t, comment)[foreign]; ok {
			t.Fatalf("kanban.comment schema carries %q - comment requires body, not another verb's args", foreign)
		}
	}
	assertRequired(t, comment, "id", "body")

	create := tools["kanban.create"]
	assertRequired(t, create, "title")
	for _, foreign := range []string{"id", "author", "operations"} {
		if _, ok := schemaProps(t, create)[foreign]; ok {
			t.Fatalf("kanban.create schema carries %q", foreign)
		}
	}

	list := tools["kanban.list"]
	if _, ok := list.Parameters["required"]; ok {
		t.Fatalf("kanban.list declares required args - the empty call is the default read")
	}
	props := schemaProps(t, list)
	if len(props) != 2 {
		t.Fatalf("kanban.list properties = %v, want exactly board + status", props)
	}

	assertRequired(t, tools["kanban.update"], "id")
	assertRequired(t, tools["kanban.complete"], "id")
	assertRequired(t, tools["kanban.delete"], "id")

	batch := tools["kanban.batch"]
	assertRequired(t, batch, "operations")
	batchProps := schemaProps(t, batch)
	if len(batchProps) != 1 {
		t.Fatalf("kanban.batch properties = %v, want exactly operations", batchProps)
	}
	items, _ := batchProps["operations"].(map[string]any)["items"].(map[string]any)
	enum, _ := items["properties"].(map[string]any)["action"].(map[string]any)["enum"].([]string)
	if len(enum) != len(taskVerbs) {
		t.Fatalf("batch op action enum = %v, want %v", enum, taskVerbs)
	}
}

// schemaProps reads a tool schema's property table.
func schemaProps(t *testing.T, tool core.Tool) map[string]any {
	t.Helper()
	props, ok := tool.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing: %+v", tool.Parameters)
	}
	return props
}

// assertRequired pins the schema's exact required list.
func assertRequired(t *testing.T, tool core.Tool, want ...string) {
	t.Helper()
	got, ok := tool.Parameters["required"].([]string)
	if !ok || len(got) != len(want) {
		t.Fatalf("required = %v, want %v", tool.Parameters["required"], want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("required = %v, want %v", got, want)
		}
	}
}

func TestTaskCreateAutoLinksSession(t *testing.T) {
	p, _ := toolProvider(t)
	out, _, err := p.runCreate(withSession(context.Background(), "sess_9"), p.board, "", map[string]any{
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
	if _, _, err := p.runCreate(context.Background(), p.board, "", map[string]any{"title": "one"}); err != nil {
		t.Fatal(err)
	}
	out, err := p.runList(context.Background(), p.board, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "one") {
		t.Fatalf("list = %q", out)
	}
	out, err = p.runList(context.Background(), p.board, "", map[string]any{"status": StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	if out != "no tasks" {
		t.Fatalf("done filter = %q", out)
	}
}

func TestTaskUpdateCompleteCommentLink(t *testing.T) {
	p, _ := toolProvider(t)
	out, _, _ := p.runCreate(context.Background(), p.board, "", map[string]any{"title": "flow"})
	id := strings.Fields(out)[1]

	status := StatusDone
	if _, err := p.runUpdate(context.Background(), p.board, "", map[string]any{"id": id, "status": status}); err != nil {
		t.Fatalf("update: %v", err)
	}
	task, _ := p.board.GetTask(context.Background(), "", id)
	if task.Status != StatusDone || task.CompletedAt == "" {
		t.Fatalf("task = %+v", task)
	}

	if out, err := p.runComplete(context.Background(), p.board, "", map[string]any{"id": id}); err != nil || !strings.Contains(out, "done") {
		t.Fatalf("complete: %q %v", out, err)
	}

	if _, err := p.runComment(context.Background(), p.board, "", map[string]any{"id": "t_00000000", "body": "missing"}); err != ErrNotFound {
		t.Fatalf("comment on missing: %v", err)
	}
	if _, err := p.runComment(context.Background(), p.board, "", map[string]any{"id": id, "body": "note", "author": "endo"}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	comments, _ := p.board.GetTaskComments(context.Background(), "", id)
	if len(comments) != 1 || comments[0].Author != "endo" {
		t.Fatalf("comments = %+v", comments)
	}
}

func TestTaskUpdateEmptyAssigneeUnassigns(t *testing.T) {
	p, _ := toolProvider(t)
	out, _, _ := p.runCreate(context.Background(), p.board, "", map[string]any{"title": "assignme", "assignee": "cody"})
	id := strings.Fields(out)[1]
	empty := ""
	if _, err := p.runUpdate(context.Background(), p.board, "", map[string]any{"id": id, "assignee": empty}); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	task, _ := p.board.GetTask(context.Background(), "", id)
	if task.Assignee != "" {
		t.Errorf("assignee = %q, want empty", task.Assignee)
	}
}

// TestDottedToolsCoverAllVerbs is the split's behavior gate: every
// pre-split verb's behavior is reachable through its own dotted tool,
// driven through the real Run (the wire surface the loop calls).
func TestDottedToolsCoverAllVerbs(t *testing.T) {
	p, board := toolProvider(t)
	tools := p.Tools()
	ctx := withSession(context.Background(), "sess_split")

	// the empty call is a legal list: no args at all
	out, err := tools["kanban.list"].Run(ctx, nil)
	if err != nil || out != "no tasks" {
		t.Fatalf("empty list: %q %v", out, err)
	}

	// create
	out, err = tools["kanban.create"].Run(ctx, map[string]any{"title": "split flow", "body": "spec"})
	if err != nil || !strings.Contains(out, "created t_") {
		t.Fatalf("create: %q %v", out, err)
	}
	id := strings.Fields(out)[1]

	// list
	out, err = tools["kanban.list"].Run(ctx, map[string]any{})
	if err != nil || !strings.Contains(out, "split flow") {
		t.Fatalf("list: %q %v", out, err)
	}

	// update
	out, err = tools["kanban.update"].Run(ctx, map[string]any{"id": id, "status": StatusRunning})
	if err != nil || !strings.Contains(out, id) {
		t.Fatalf("update: %q %v", out, err)
	}

	// comment
	out, err = tools["kanban.comment"].Run(ctx, map[string]any{"id": id, "body": "hello"})
	if err != nil || !strings.Contains(out, "commented on") {
		t.Fatalf("comment: %q %v", out, err)
	}
	comments, _ := board.GetTaskComments(context.Background(), "", id)
	if len(comments) != 1 {
		t.Fatalf("comments = %+v", comments)
	}

	// complete
	out, err = tools["kanban.complete"].Run(ctx, map[string]any{"id": id})
	if err != nil || !strings.Contains(out, "done") {
		t.Fatalf("complete: %q %v", out, err)
	}
	task, _ := board.GetTask(context.Background(), "", id)
	if task.Status != StatusDone || task.CompletedAt == "" {
		t.Fatalf("task = %+v", task)
	}

	// delete
	out, err = tools["kanban.delete"].Run(ctx, map[string]any{"id": id})
	if err != nil || !strings.Contains(out, "deleted") {
		t.Fatalf("delete: %q %v", out, err)
	}
	if _, err := board.GetTask(context.Background(), "", id); err == nil {
		t.Fatal("the deleted task is still on the board")
	}
}

// TestRunTaskBatch covers the batch surface: create several in one
// call, and update+comment together.
func TestRunTaskBatch(t *testing.T) {
	p, board := toolProvider(t)
	batch := p.Tools()["kanban.batch"]
	ctx := withSession(context.Background(), "sess_batch")

	out, err := batch.Run(ctx, map[string]any{"operations": []map[string]any{
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

	out, err = batch.Run(ctx, map[string]any{"operations": []map[string]any{
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

// TestRunTaskBatchRollback pins the atomic batch contract: the batch
// fails at the first bad op, everything earlier rolled back - the
// board is exactly what it was before the call, and the result says
// so (no orphaned creates).
func TestRunTaskBatchRollback(t *testing.T) {
	p, _ := toolProvider(t)
	batch := p.Tools()["kanban.batch"]
	ctx := context.Background()

	out, _ := batch.Run(ctx, map[string]any{"operations": []any{
		map[string]any{"action": "create", "title": "kept"},
		map[string]any{"action": "update", "id": "t_missing", "status": StatusDone},
		map[string]any{"action": "create", "title": "never created"},
	}})
	if !strings.Contains(out, "failed at operations[1]") {
		t.Fatalf("expected the failure report, got %q", out)
	}
	if !strings.Contains(out, "batch rolled back") {
		t.Fatalf("the rollback is not reported: %q", out)
	}
	tasks, _ := p.board.ListTasks(ctx, "", "")
	if len(tasks) != 0 {
		t.Fatalf("a failed atomic batch left rows behind: %+v", tasks)
	}
}

// nonBatcher is a KanbanProvider without the Batcher capability: it
// pins the honest fail-forward fallback for embedders the store's
// transaction cannot cover.
type nonBatcher struct{ inner *Store }

func (b nonBatcher) CreateTask(ctx context.Context, board string, in KanbanTask) (KanbanTask, error) {
	return b.inner.CreateTask(ctx, board, in)
}
func (b nonBatcher) ListTasks(ctx context.Context, board, status string) ([]KanbanTask, error) {
	return b.inner.ListTasks(ctx, board, status)
}
func (b nonBatcher) GetTask(ctx context.Context, board, id string) (KanbanTask, error) {
	return b.inner.GetTask(ctx, board, id)
}
func (b nonBatcher) GetTaskComments(ctx context.Context, board, taskID string) ([]KanbanComment, error) {
	return b.inner.GetTaskComments(ctx, board, taskID)
}
func (b nonBatcher) GetTaskEvents(ctx context.Context, board, taskID string) ([]KanbanEvent, error) {
	return b.inner.GetTaskEvents(ctx, board, taskID)
}
func (b nonBatcher) UpdateTask(ctx context.Context, board, taskID string, patch KanbanPatch) (KanbanTask, error) {
	return b.inner.UpdateTask(ctx, board, taskID, patch)
}
func (b nonBatcher) AddComment(ctx context.Context, board, taskID, author, body string) (KanbanComment, error) {
	return b.inner.AddComment(ctx, board, taskID, author, body)
}
func (b nonBatcher) Close() error { return nil }

var _ KanbanProvider = nonBatcher{}

// TestRunTaskBatchFailForward pins the no-Batcher fallback: the board
// cannot roll back, so the batch stops at the first failure and the
// applied prefix honestly stands.
func TestRunTaskBatchFailForward(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	p := &ToolProvider{board: nonBatcher{inner: s}}
	batch := p.Tools()["kanban.batch"]
	ctx := context.Background()

	out, _ := batch.Run(ctx, map[string]any{"operations": []any{
		map[string]any{"action": "create", "title": "kept"},
		map[string]any{"action": "update", "id": "t_missing", "status": StatusDone},
		map[string]any{"action": "create", "title": "never created"},
	}})
	if !strings.Contains(out, "failed at operations[1]") {
		t.Fatalf("expected fail-forward report, got %q", out)
	}
	if strings.Contains(out, "batch rolled back") {
		t.Fatalf("the fail-forward board must not claim a rollback: %q", out)
	}
	if strings.Contains(out, "never created") && !strings.Contains(out, "kept") {
		t.Fatalf("partial results lost: %q", out)
	}
	tasks, _ := p.board.ListTasks(ctx, "", "")
	if len(tasks) != 1 || tasks[0].Title != "kept" {
		t.Fatalf("task after failed batch = %+v", tasks)
	}
}

// TestTaskAliasesRetired pins the v0.12 phase-C1 retirement plus the
// B ruling (t_f616ed78): the wire is exactly the seven dotted names -
// the legacy alias shims and the flat domain tool never register.
func TestTaskAliasesRetired(t *testing.T) {
	p, _ := toolProvider(t)
	tools := p.Tools()
	if len(tools) != len(dottedToolNames) {
		t.Fatalf("the wire must carry exactly the seven dotted tools: %d entries", len(tools))
	}
	for name, tool := range tools {
		if tool.AliasOf != "" {
			t.Fatalf("%s registered as an alias of %q - the shim era is closed", name, tool.AliasOf)
		}
	}
}
