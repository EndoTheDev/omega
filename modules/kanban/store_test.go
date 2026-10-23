package kanban

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateDefaultsAndID(t *testing.T) {
	s := testStore(t)
	out, err := s.CreateTask(context.Background(), "", core.KanbanTask{Title: "write it"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(out.ID) != 10 || out.ID[:2] != "t_" {
		t.Errorf("id = %q, want t_ + 8 hex", out.ID)
	}
	if out.Status != StatusTriage {
		t.Errorf("status = %q, want triage", out.Status)
	}
	if out.CreatedAt == "" || out.CompletedAt != "" || out.StartedAt != "" {
		t.Errorf("timestamps set wrong: %+v", out)
	}
	got, err := s.GetTask(context.Background(), "", out.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "write it" || got.Body != "" {
		t.Errorf("got %+v", got)
	}
}

func TestCreateRequiresTitle(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateTask(context.Background(), "", core.KanbanTask{}); err == nil {
		t.Fatal("empty title accepted")
	}
	if _, err := s.CreateTask(context.Background(), "", core.KanbanTask{Status: "bogus"}); err == nil {
		t.Fatal("illegal status accepted at create")
	}
}

func TestGetTaskNotFound(t *testing.T) {
	s := testStore(t)
	if _, err := s.GetTask(context.Background(), "", "t_00000000"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListStatusFilter(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	if _, err := s.CreateTask(ctx, "", core.KanbanTask{Title: "a"}); err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateTask(ctx, "", core.KanbanTask{Title: "b", Status: StatusTodo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateTask(ctx, "", b.ID, core.KanbanPatch{Status: StatusRunning}); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListTasks(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2", len(all))
	}
	running, err := s.ListTasks(ctx, "", StatusRunning)
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 1 || running[0].Title != "b" {
		t.Fatalf("running filter: %+v", running)
	}
	if _, err := s.ListTasks(ctx, "", "bogus"); err == nil {
		t.Fatal("illegal status filter accepted")
	}
}

func TestUpdateStatusStamps(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	tk, err := s.CreateTask(ctx, "", core.KanbanTask{Title: "flow"})
	if err != nil {
		t.Fatal(err)
	}
	if tk.StartedAt != "" {
		t.Fatalf("started_at pre-run: %q", tk.StartedAt)
	}
	running, err := s.UpdateTask(ctx, "", tk.ID, core.KanbanPatch{Status: StatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	if running.StartedAt == "" {
		t.Error("running did not stamp started_at")
	}
	if running.CompletedAt != "" {
		t.Error("running stamped completed_at")
	}
	done, err := s.UpdateTask(ctx, "", tk.ID, core.KanbanPatch{Status: StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	if done.CompletedAt == "" {
		t.Error("done did not stamp completed_at")
	}
	// reopen: the completion stamp clears
	reopen, err := s.UpdateTask(ctx, "", tk.ID, core.KanbanPatch{Status: StatusTodo})
	if err != nil {
		t.Fatal(err)
	}
	if reopen.StartedAt == "" {
		t.Error("reopen cleared started_at")
	}
	if reopen.CompletedAt != "" {
		t.Error("reopen kept completed_at")
	}
}

func TestUpdateArchivedKeepsStamp(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	tk, _ := s.CreateTask(ctx, "", core.KanbanTask{Title: "done first"})
	if _, err := s.UpdateTask(ctx, "", tk.ID, core.KanbanPatch{Status: StatusDone}); err != nil {
		t.Fatal(err)
	}
	arch, err := s.UpdateTask(ctx, "", tk.ID, core.KanbanPatch{Status: StatusArchived})
	if err != nil {
		t.Fatal(err)
	}
	if arch.CompletedAt == "" {
		t.Error("done->archived lost completed_at")
	}
}

func TestUpdatePatchSemantics(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	tk, _ := s.CreateTask(ctx, "", core.KanbanTask{Title: "before"})
	body := "full spec"
	title := "after"
	out, err := s.UpdateTask(ctx, "", tk.ID, core.KanbanPatch{Title: &title, Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != "after" || out.Body != body {
		t.Fatalf("patch lost: %+v", out)
	}
	events, err := s.GetTaskEvents(ctx, "", tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	if len(kinds) != 2 || kinds[0] != "created" || kinds[1] != "edited" {
		t.Fatalf("event kinds = %v", kinds)
	}
}

func TestUpdateNoopPatch(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	tk, _ := s.CreateTask(ctx, "", core.KanbanTask{Title: "same"})
	same := "same"
	out, err := s.UpdateTask(ctx, "", tk.ID, core.KanbanPatch{Title: &same})
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != "same" {
		t.Fatalf("noop patch changed title: %+v", out)
	}
	events, _ := s.GetTaskEvents(ctx, "", tk.ID)
	if len(events) != 1 {
		t.Fatalf("noop patch wrote events: %+v", events)
	}
}

func TestUpdateNotFound(t *testing.T) {
	s := testStore(t)
	if _, err := s.UpdateTask(context.Background(), "", "t_00000000", core.KanbanPatch{Status: StatusDone}); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCommentsAndEvents(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	tk, _ := s.CreateTask(ctx, "", core.KanbanTask{Title: "t"})
	c1, err := s.AddComment(ctx, "", tk.ID, "endo", "first")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := s.AddComment(ctx, "", tk.ID, "", "author defaults to user")
	if err != nil {
		t.Fatal(err)
	}
	if c2.Author != "user" {
		t.Errorf("author = %q, want user", c2.Author)
	}
	if _, err := s.AddComment(ctx, "", tk.ID, "endo", "   "); err == nil {
		t.Fatal("blank comment accepted")
	}
	if _, err := s.AddComment(ctx, "", "t_00000000", "endo", "x"); err != ErrNotFound {
		t.Fatalf("comment on missing task: %v", err)
	}
	got, err := s.GetTaskComments(ctx, "", tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != c1.ID || got[0].Body != "first" {
		t.Fatalf("thread = %+v", got)
	}
	events, err := s.GetTaskEvents(ctx, "", tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %+v", events)
	}
	if events[1].Kind != "comment" || events[2].Kind != "comment" {
		t.Fatalf("comment events missing: %+v", events)
	}
}

func TestSessionLinkEvent(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	tk, _ := s.CreateTask(ctx, "", core.KanbanTask{Title: "linked"})
	sess := "sess_abc"
	out, err := s.UpdateTask(ctx, "", tk.ID, core.KanbanPatch{SessionID: &sess})
	if err != nil {
		t.Fatal(err)
	}
	if out.SessionID != sess {
		t.Fatalf("session = %q", out.SessionID)
	}
	events, _ := s.GetTaskEvents(ctx, "", tk.ID)
	if len(events) != 2 || events[1].Kind != "linked" {
		t.Fatalf("events = %+v", events)
	}
}