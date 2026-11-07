package kanban

// Batch tests (t_02a8beea): in-batch referencing (LAST / #N), the
// preflight door for bad references, and the atomic rollback that
// keeps a failed batch from orphaning its own creates.

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

// TestRunTaskBatchInBatchRefs covers create-then-act inside one atomic
// batch: LAST tracks the most recent create, "#N" the create at
// operations[N-1] (1-based, matching the result lines).
func TestRunTaskBatchInBatchRefs(t *testing.T) {
	p, board := toolProvider(t)
	ctx := withSession(context.Background(), "sess_refs")

	// the card's exact shape, now passing: comment/update/complete via LAST
	out, err := p.runTask(ctx, map[string]any{"operations": []any{
		map[string]any{"action": "create", "title": "referred"},
		map[string]any{"action": "comment", "id": "LAST", "body": "via LAST"},
		map[string]any{"action": "update", "id": "LAST", "status": StatusRunning},
		map[string]any{"action": "complete", "id": "LAST"},
	}})
	if err != nil || !strings.Contains(out, "4 operations applied") {
		t.Fatalf("LAST batch: %q %v", out, err)
	}
	id := extractCreatedID(t, out, 1)
	if id == "" {
		t.Fatalf("cannot read the created id from %q", out)
	}
	task, gerr := board.GetTask(ctx, "", id)
	if gerr != nil {
		t.Fatal(gerr)
	}
	if task.Status != StatusDone {
		t.Fatalf("status = %q, want done", task.Status)
	}
	comments, _ := board.GetTaskComments(ctx, "", id)
	if len(comments) != 1 || comments[0].Body != "via LAST" {
		t.Fatalf("comments = %+v", comments)
	}

	// positional: two creates, a comment on #1, a complete on #2
	out, err = p.runTask(ctx, map[string]any{"operations": []any{
		map[string]any{"action": "create", "title": "alpha"},
		map[string]any{"action": "create", "title": "beta"},
		map[string]any{"action": "comment", "id": "#1", "body": "to alpha"},
		map[string]any{"action": "comment", "id": "#2", "body": "to beta"},
	}})
	if err != nil || !strings.Contains(out, "4 operations applied") {
		t.Fatalf("positional batch: %q %v", out, err)
	}
	id1, id2 := extractCreatedID(t, out, 1), extractCreatedID(t, out, 2)
	if id1 == id2 || id1 == "" || id2 == "" {
		t.Fatalf("created ids unclear from %q", out)
	}
	if cs, _ := board.GetTaskComments(ctx, "", id1); len(cs) != 1 || cs[0].Body != "to alpha" {
		t.Fatalf("#1 comments = %+v", cs)
	}
	if cs, _ := board.GetTaskComments(ctx, "", id2); len(cs) != 1 || cs[0].Body != "to beta" {
		t.Fatalf("#2 comments = %+v", cs)
	}
}

// extractCreatedID pulls the task id from batch result line n (1-based).
func extractCreatedID(t *testing.T, out string, n int) string {
	t.Helper()
	for _, ln := range strings.Split(out, "\n") {
		parts := strings.Fields(ln)
		if len(parts) >= 3 && parts[0] == strconv.Itoa(n)+":" && parts[1] == "created" {
			return parts[2] // 1: created <id> [<status>] <title...>
		}
	}
	return ""
}

// TestRunTaskBatchRollbackRefCoversTheRepro: the exact batch from the
// card's repro - [create, comment(LAST), ...] failing on a bad later
// op - rolls back fully instead of orphaning the create.
func TestRunTaskBatchRollbackRefCoversTheRepro(t *testing.T) {
	p, board := toolProvider(t)
	ctx := context.Background()

	// a valid prefix that would have applied: create + comment LAST,
	// then an update on an id that does not exist -> failure -> rollback
	out, _ := p.runTask(ctx, map[string]any{"operations": []any{
		map[string]any{"action": "create", "title": "orphan probe"},
		map[string]any{"action": "comment", "id": "LAST", "body": "resolved"},
		map[string]any{"action": "update", "id": "t_none_such", "status": StatusRunning},
	}})
	if !strings.Contains(out, "failed at operations[2]") || !strings.Contains(out, "batch rolled back") {
		t.Fatalf("report: %q", out)
	}
	tasks, _ := board.ListTasks(ctx, "", "")
	if len(tasks) != 0 {
		t.Fatalf("orphaned rows survived the failed batch: %+v", tasks)
	}
	every, _ := board.ListTasks(ctx, "", StatusAll)
	if len(every) != 0 {
		t.Fatalf("rolled rows visible on the archive view: %+v", every)
	}
}

// TestRunTaskBatchPreflightRefusesBadRefs: a malformed or
// forward-pointing reference fails BEFORE anything applies - zero side
// effects, and the result is a tool error (not a rolled-back report).
func TestRunTaskBatchPreflightRefusesBadRefs(t *testing.T) {
	p, board := toolProvider(t)
	ctx := context.Background()

	for _, probe := range []struct {
		name string
		ops  []any
		ref  string
	}{
		{"LAST without a create", []any{map[string]any{"action": "comment", "id": "LAST", "body": "x"}}, "LAST"},
		{"forward reference", []any{
			map[string]any{"action": "comment", "id": "#2", "body": "x"},
			map[string]any{"action": "create", "title": "later"},
		}, "#2"},
		{"reference to a non-create", []any{
			map[string]any{"action": "create", "title": "a"},
			map[string]any{"action": "comment", "id": "#2", "body": "x"},
		}, "#2"},
		{"out-of-range position", []any{
			map[string]any{"action": "create", "title": "a"},
			map[string]any{"action": "comment", "id": "#99", "body": "x"},
		}, "#99"},
	} {
		_, err := p.runTask(ctx, map[string]any{"operations": probe.ops})
		if err == nil || !strings.Contains(err.Error(), probe.ref) {
			t.Fatalf("%s: err = %v, want a %q refusal", probe.name, err, probe.ref)
		}
		tasks, _ := board.ListTasks(ctx, "", StatusAll)
		if len(tasks) != 0 {
			t.Fatalf("%s: the door failure left rows behind: %+v", probe.name, tasks)
		}
	}
}

// TestRunTaskBatchDoneEmitsOnlyOnCommit: a done move inside a batch
// emits its finished-things record after the commit, and a rolled-back
// batch emits nothing at all.
func TestRunTaskBatchDoneEmitsOnlyOnCommit(t *testing.T) {
	p, _ := toolProvider(t)
	n := &fakeNotifier{}
	p.board.(*Store).SetNotifier(n)
	ctx := context.Background()

	// commit path: one emit for the completed create
	if _, err := p.runTask(ctx, map[string]any{"operations": []any{
		map[string]any{"action": "create", "title": "done in batch"},
		map[string]any{"action": "complete", "id": "LAST"},
	}}); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	if len(n.got) != 1 {
		t.Fatalf("committed batch emitted %d records, want 1", len(n.got))
	}
	n.got = nil
	n.mu.Unlock()

	// rollback path: no emit survives
	p.runTask(ctx, map[string]any{"operations": []any{
		map[string]any{"action": "create", "title": "never done"},
		map[string]any{"action": "complete", "id": "LAST"},
		map[string]any{"action": "update", "id": "t_none_such", "status": StatusRunning},
	}})
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.got) != 0 {
		t.Fatalf("a rolled-back batch emitted %d records", len(n.got))
	}
}
