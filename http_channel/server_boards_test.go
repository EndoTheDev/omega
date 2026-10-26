package http_channel

// Multi-board HTTP tests (t_d7a7beaa): ?board= picks the board on
// every task route, GET /boards lists the registry, and unknown
// boards fail loudly - a card on board A never answers on board B.

import (
	"encoding/json"
	"fmt"
	"net/http"
	httptest2 "net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/kanban"
)

// multiBoardServer mounts a real store wired to a temp home registry
// with alpha and beta created, pointer on main.
func multiBoardServer(t *testing.T) *httptest2.Server {
	t.Helper()
	s := NewServer(nil, nil, nil, nil, nil)
	home := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := kanban.CreateBoard(home, name); err != nil {
			t.Fatalf("create board %s: %v", name, err)
		}
	}
	dsn, err := kanban.PointerDSN(home)
	if err != nil {
		t.Fatal(err)
	}
	board, err := kanban.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	board.ConfigureBoards(kanban.BoardsDir(home), kanban.DefaultBoardName, home+"/data/kanban.db")
	s.Kanban = board
	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(func() {
		ts.Close()
		board.Close()
	})
	return ts
}

func boardJSON[anyT any](t *testing.T, res *http.Response, into *anyT) {
	t.Helper()
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(into); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func fetchRaw(tsURL, path string) (*http.Response, error) {
	return http.Get(tsURL + path)
}

func postRaw(tsURL, path, body string) (*http.Response, error) {
	return http.Post(tsURL+path, "application/json", strings.NewReader(body))
}

func TestBoardsEndpointListsRegistry(t *testing.T) {
	ts := multiBoardServer(t)
	res, err := http.Get(ts.URL + "/boards")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Boards []core.BoardInfo `json:"boards"`
	}
	boardJSON(t, res, &got)
	if len(got.Boards) != 3 {
		t.Fatalf("boards = %+v", got.Boards)
	}
	if got.Boards[0].Name != "main" || !got.Boards[0].Current {
		t.Fatalf("first = %+v", got.Boards[0])
	}
	found := map[string]bool{}
	for _, b := range got.Boards {
		found[b.Name] = true
	}
	if !found["alpha"] || !found["beta"] {
		t.Fatalf("named boards missing: %+v", got.Boards)
	}
}

func TestBoardParamIsolation(t *testing.T) {
	ts := multiBoardServer(t)

	// create on alpha (board param)
	res, err := postRaw(ts.URL, "/tasks?board=alpha", `{"title":"alpha card"}`)
	if err != nil {
		t.Fatal(err)
	}
	var created core.KanbanTask
	boardJSON(t, res, &created)
	if res.StatusCode != http.StatusCreated || created.ID == "" {
		t.Fatalf("create = %d %+v", res.StatusCode, created)
	}

	// default board never sees it
	list := func(path string) []core.KanbanTask {
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Tasks []core.KanbanTask `json:"tasks"`
		}
		boardJSON(t, res, &got)
		return got.Tasks
	}
	if n := len(list("/tasks")); n != 0 {
		t.Fatalf("default saw %d alpha cards", n)
	}
	if tasks := list("/tasks?board=alpha"); len(tasks) != 1 || tasks[0].ID != created.ID {
		t.Fatalf("alpha list = %+v", tasks)
	}

	// cross-board reads/mutations 404
	res, _ = http.Get(ts.URL + "/tasks/" + url.PathEscape(created.ID))
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("default get = %d", res.StatusCode)
	}
	res, _ = fetchRaw(ts.URL, "/tasks/"+created.ID+"?board=alpha")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("alpha get = %d", res.StatusCode)
	}
	res, _ = postRaw(ts.URL, fmt.Sprintf("/tasks/%s/comments?board=beta", created.ID), `{"body":"x"}`)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("beta comment = %d, want 404", res.StatusCode)
	}

	// comments land on the right board
	res, _ = postRaw(ts.URL, "/tasks/"+created.ID+"/comments?board=alpha", `{"body":"board-scoped"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("comment = %d", res.StatusCode)
	}
	res, _ = fetchRaw(ts.URL, "/tasks/"+created.ID+"?board=alpha")
	var detail taskDetail
	boardJSON(t, res, &detail)
	if len(detail.Comments) != 1 || detail.Task.ID != created.ID {
		t.Fatalf("detail = %+v", detail)
	}

	// PATCH within the board works; unknown board fails loudly
	res, _ = do("PATCH", ts.URL+"/tasks/"+created.ID+"?board=alpha", `{"status":"todo"}`)
	var patched core.KanbanTask
	boardJSON(t, res, &patched)
	if patched.Status != "todo" {
		t.Fatalf("patch = %+v", patched)
	}
	res, _ = do("PATCH", ts.URL+"/tasks/"+created.ID+"?board=gamma", `{"status":"todo"}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown board patch = %d, want 400", res.StatusCode)
	}

	// create on an unknown board -> 400
	res, _ = postRaw(ts.URL, "/tasks?board=gamma", `{"title":"x"}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown board create = %d, want 400", res.StatusCode)
	}
	res.Body.Close()
}