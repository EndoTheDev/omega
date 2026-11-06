package kanban

// The CRUD-round tests: the Priority field (create/update, the refusal
// of a negative, the audit trail) and the TaskDeleter capability (the
// store's cascade, the DELETE route, the board isolation). Priority is
// an integer urgency tiebreak on the Hermes shape (higher = more
// urgent, 0 = normal); delete takes the comments and events with the
// card - archive is the reversible path, this file pins that the
// irreversible one actually removes.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPriorityCreateAndUpdate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	out, err := s.CreateTask(ctx, "", KanbanTask{Title: "urgent one", Priority: 2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.Priority != 2 {
		t.Fatalf("created priority = %d", out.Priority)
	}
	got, err := s.GetTask(ctx, "", out.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Priority != 2 {
		t.Fatalf("persisted priority = %d", got.Priority)
	}

	updated, err := s.UpdateTask(ctx, "", out.ID, KanbanPatch{Priority: intPtr(5)})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Priority != 5 {
		t.Fatalf("updated priority = %d", updated.Priority)
	}

	events, err := s.GetTaskEvents(ctx, "", out.ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	var edited string
	for _, e := range events {
		if e.Kind == "edited" {
			edited += e.Detail + ";"
		}
	}
	if !strings.Contains(edited, "priority 5") {
		t.Fatalf("no priority edit event: %q", edited)
	}

	// a zero patch leaves the value alone
	noop, err := s.UpdateTask(ctx, "", out.ID, KanbanPatch{Title: strPtr("retitled")})
	if err != nil {
		t.Fatalf("title update: %v", err)
	}
	if noop.Priority != 5 {
		t.Fatalf("unspecified priority lost: %d", noop.Priority)
	}
}

func intPtr(n int) *int { return &n }

func strPtr(s string) *string { return &s }

func TestPriorityNegativeRefused(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.CreateTask(ctx, "", KanbanTask{Title: "x", Priority: -1}); err == nil {
		t.Fatal("negative priority accepted at create")
	}
	out, err := s.CreateTask(ctx, "", KanbanTask{Title: "x"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.UpdateTask(ctx, "", out.ID, KanbanPatch{Priority: intPtr(-3)}); err == nil {
		t.Fatal("negative priority accepted at update")
	}
}

func TestDeleteCascade(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	out, err := s.CreateTask(ctx, "", KanbanTask{Title: "doomed", Body: "spec"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.AddComment(ctx, "", out.ID, "web", "a comment"); err != nil {
		t.Fatalf("comment: %v", err)
	}

	if err := s.DeleteTask(ctx, "", out.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetTask(ctx, "", out.ID); err == nil {
		t.Fatal("deleted task still readable")
	}
	comments, _ := s.GetTaskComments(ctx, "", out.ID)
	if len(comments) != 0 {
		t.Fatalf("comments survived: %v", comments)
	}
	events, _ := s.GetTaskEvents(ctx, "", out.ID)
	if len(events) != 0 {
		t.Fatalf("events survived: %v", events)
	}
	if err := s.DeleteTask(ctx, "", out.ID); err == nil {
		t.Fatal("deleting twice accepted")
	}
}

func TestDeleteBoardIsolation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateBoardRow("side"); err != nil {
		t.Fatalf("create board: %v", err)
	}
	out, err := s.CreateTask(ctx, "side", KanbanTask{Title: "other board"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// board "" is main; the id only answers on its board
	if err := s.DeleteTask(ctx, "", out.ID); err == nil {
		t.Fatal("cross-board delete accepted")
	}
	if err := s.DeleteTask(ctx, "side", out.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	tasks, err := s.ListTasks(ctx, "side", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("list after delete: %v", tasks)
	}
}

func TestRestPriorityAndDelete(t *testing.T) {
	p, ctx, _ := mountPlugin(t)
	ts := serveRoutes(t, ctx)

	// POST /tasks carries priority
	res, err := http.Post(ts.URL+"/tasks", "application/json",
		strings.NewReader(`{"title":"hot","priority":3,"assignee":"cody"}`))
	if err != nil {
		t.Fatal(err)
	}
	var created KanbanTask
	json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if created.Priority != 3 || created.Assignee != "cody" {
		t.Fatalf("created = %+v", created)
	}

	// PATCH /tasks/{id} re-prioritizes
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/tasks/"+created.ID,
		strings.NewReader(`{"priority":1}`))
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var patched KanbanTask
	json.NewDecoder(res.Body).Decode(&patched)
	res.Body.Close()
	if patched.Priority != 1 {
		t.Fatalf("patched = %+v", patched)
	}

	// DELETE /tasks/{id} removes the row
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/tasks/"+created.ID, nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete = %d %s", res.StatusCode, body)
	}
	var out struct {
		Deleted string `json:"deleted"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Deleted != created.ID {
		t.Fatalf("delete payload = %s (%v)", body, err)
	}

	// a follow-up GET is a 404, a second DELETE is too
	res, err = http.Get(ts.URL + "/tasks/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete = %d", res.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/tasks/"+created.ID, nil)
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("second delete = %d", res.StatusCode)
	}

	// the tool rides the same store: delete on a missing id answers the error
	if _, err := p.tools.runTaskOp(context.Background(), map[string]any{
		"action": "delete", "id": "nope-missing",
	}); err == nil {
		t.Fatal("tool delete on missing id accepted")
	}
}
