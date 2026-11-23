package kanban

// The card menu's Stop verb: a running card lands in blocked through the SAME
// PATCH /tasks/{id} the lanes already ride - no new route, one roundtrip.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestTasksStopRoundtrip(t *testing.T) {
	_, ctx, _ := mountPlugin(t)
	ts := serveRoutes(t, ctx)

	res, err := http.Post(ts.URL+"/tasks", "application/json",
		strings.NewReader(`{"title":"stop me","assignee":"cody"}`))
	if err != nil {
		t.Fatal(err)
	}
	var created KanbanTask
	json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()

	// the lane move that starts work
	res, _ = do("PATCH", ts.URL+"/tasks/"+created.ID, `{"status":"running"}`)
	var running KanbanTask
	json.NewDecoder(res.Body).Decode(&running)
	res.Body.Close()
	if running.Status != StatusRunning || running.StartedAt == "" {
		t.Fatalf("running = %+v", running)
	}

	// Stop: running -> blocked, the started stamp kept (the card did start)
	res, _ = do("PATCH", ts.URL+"/tasks/"+created.ID, `{"status":"blocked"}`)
	var stopped KanbanTask
	json.NewDecoder(res.Body).Decode(&stopped)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stop patch = %d", res.StatusCode)
	}
	if stopped.Status != StatusBlocked {
		t.Fatalf("stopped status = %q, want blocked", stopped.Status)
	}
	if stopped.StartedAt != running.StartedAt {
		t.Fatalf("started_at moved on the stop: %q -> %q", running.StartedAt, stopped.StartedAt)
	}

	// the detail read agrees (the board refetches, never trusts its own echo)
	detail, _ := http.Get(ts.URL + "/tasks/" + created.ID)
	var got taskDetail
	json.NewDecoder(detail.Body).Decode(&got)
	detail.Body.Close()
	if got.Task.Status != StatusBlocked {
		t.Fatalf("detail status = %q, want blocked", got.Task.Status)
	}

	// an invented status still dies at the wire
	res, _ = do("PATCH", ts.URL+"/tasks/"+created.ID, `{"status":"stopped"}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invented status patch = %d, want 400", res.StatusCode)
	}
	res.Body.Close()
}
