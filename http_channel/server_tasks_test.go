package http_channel

import (
	"encoding/json"
	"net/http"
	httptest "net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/kanban"
)

// kanbanTestServer is a Server with a real board mounted.
func kanbanTestServer(t *testing.T) *Server {
	t.Helper()
	s := NewServer(nil, nil, nil, nil, nil)
	board, err := kanban.Open(":memory:")
	if err != nil {
		t.Fatalf("open board: %v", err)
	}
	t.Cleanup(func() { board.Close() })
	s.Kanban = board
	return s
}

func serveBoard(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(kanbanTestServer(t).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestTasksEndpointsLifecycle(t *testing.T) {
	ts := serveBoard(t)

	// POST /tasks -> 201 with a triage task
	res, err := http.Post(ts.URL+"/tasks", "application/json",
		strings.NewReader(`{"title":"wire the drawer","body":"full spec","assignee":"cody"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("post = %d", res.StatusCode)
	}
	var created core.KanbanTask
	json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if created.ID == "" || created.Status != "triage" {
		t.Fatalf("created = %+v", created)
	}

	// GET /tasks (all) and ?status=triage filter
	for _, q := range []string{"", "?status=triage"} {
		res, _ := http.Get(ts.URL + "/tasks" + q)
		var got struct {
			Tasks []core.KanbanTask `json:"tasks"`
		}
		json.NewDecoder(res.Body).Decode(&got)
		res.Body.Close()
		if len(got.Tasks) != 1 {
			t.Fatalf("q=%q tasks = %d", q, len(got.Tasks))
		}
	}

	// GET /tasks/{id}: comments + events present
	res, _ = http.Get(ts.URL + "/tasks/" + created.ID)
	var detail taskDetail
	json.NewDecoder(res.Body).Decode(&detail)
	res.Body.Close()
	if detail.Task.ID != created.ID || detail.Comments == nil || detail.Events == nil || len(detail.Events) != 1 {
		t.Fatalf("detail = %+v comments=%d events=%d", detail.Task, len(detail.Comments), len(detail.Events))
	}

	// PATCH /tasks/{id}: move to running (stamps started_at), then done
	res, _ = do("PATCH", ts.URL+"/tasks/"+created.ID, `{"status":"running"}`)
	var patched core.KanbanTask
	json.NewDecoder(res.Body).Decode(&patched)
	res.Body.Close()
	if patched.Status != "running" || patched.StartedAt == "" {
		t.Fatalf("patched = %+v", patched)
	}
	res, _ = do("PATCH", ts.URL+"/tasks/"+created.ID, `{"status":"done"}`)
	json.NewDecoder(res.Body).Decode(&patched)
	res.Body.Close()
	if patched.Status != "done" || patched.CompletedAt == "" {
		t.Fatalf("patched = %+v", patched)
	}

	// PATCH absent task -> 404
	res, _ = do("PATCH", ts.URL+"/tasks/t_00000000", `{"status":"done"}`)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing patch = %d", res.StatusCode)
	}

	// POST /tasks/{id}/comments -> created; comment lands in the detail
	res, _ = http.Post(ts.URL+"/tasks/"+created.ID+"/comments", "application/json",
		strings.NewReader(`{"body":"looks good","author":""}`))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("comment = %d", res.StatusCode)
	}
	var c core.KanbanComment
	json.NewDecoder(res.Body).Decode(&c)
	res.Body.Close()
	if c.Author != "web" {
		t.Fatalf("comment author = %q, want web", c.Author)
	}
	res, _ = http.Get(ts.URL + "/tasks/" + created.ID)
	json.NewDecoder(res.Body).Decode(&detail)
	res.Body.Close()
	if len(detail.Comments) != 1 || detail.Comments[0].Body != "looks good" {
		t.Fatalf("comments = %+v", detail.Comments)
	}

	// PATCH title with empty string -> 400 (title cannot be empty)
	res, _ = do("PATCH", ts.URL+"/tasks/"+created.ID, `{"title":"  "}`)
	if res.StatusCode == http.StatusOK {
		t.Fatal("empty retitle accepted")
	}
}

func TestTasksNotConfigured501(t *testing.T) {
	ts := httptest.NewServer(NewServer(nil, nil, nil, nil, nil).Handler())
	defer ts.Close()
	res, _ := http.Get(ts.URL + "/tasks")
	res.Body.Close()
	if res.StatusCode != http.StatusNotImplemented {
		t.Fatalf("GET /tasks without board = %d, want 501", res.StatusCode)
	}
}

func TestTasksIllegalStatusFiltered400(t *testing.T) {
	ts := serveBoard(t)
	res, _ := http.Get(ts.URL + "/tasks?status=bogus")
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("illegal filter = %d, want 400", res.StatusCode)
	}
	res, _ = http.Post(ts.URL+"/tasks", "application/json", strings.NewReader(`{"title":"x","status":"bogus"}`))
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("illegal create status = %d, want 400", res.StatusCode)
	}
}

func do(method, url, body string) (*http.Response, error) {
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}