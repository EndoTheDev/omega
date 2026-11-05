package kanban

// The board REST tests (from the pre-dissolve http_channel tests (and
// server_boards_test.go, t_d7a7beaa): the capsule serves its own
// routes since the phase-F move - ?board= picks the board on every
// task route, GET /boards lists the registry, unknown boards fail
// loudly (a card on board A never answers on board B), and the wire
// shapes are the ones the webui consumes.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	httptest2 "net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// serveRoutes builds an http surface over the routes a mounted
// ctx.ModuleRoutes carries - the same contract the channel's module
// dispatch serves, reduced to the module mux itself.
func serveRoutes(t *testing.T, ctx *core.Context) *httptest2.Server {
	t.Helper()
	mux := http.NewServeMux()
	for _, rt := range ctx.ModuleRoutes {
		mux.HandleFunc(rt.Pattern, rt.Handler)
	}
	ts := httptest2.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func do(method, url, body string) (*http.Response, error) {
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func TestTasksEndpointsLifecycle(t *testing.T) {
	p, ctx, _ := mountPlugin(t)
	ts := serveRoutes(t, ctx)

	// POST /tasks -> 201 with a triage task
	res, err := http.Post(ts.URL+"/tasks", "application/json",
		strings.NewReader(`{"title":"wire the drawer","body":"full spec","assignee":"cody"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("post = %d", res.StatusCode)
	}
	var created KanbanTask
	json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if created.ID == "" || created.Status != "triage" {
		t.Fatalf("created = %+v", created)
	}

	// GET /tasks (all) and ?status=triage filter
	for _, q := range []string{"", "?status=triage"} {
		res, _ := http.Get(ts.URL + "/tasks" + q)
		var got struct {
			Tasks []KanbanTask `json:"tasks"`
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
	var patched KanbanTask
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
	var c KanbanComment
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
	_ = p
}

func TestTasksIllegalStatusFiltered400(t *testing.T) {
	_, ctx, _ := mountPlugin(t)
	ts := serveRoutes(t, ctx)
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

// multiBoardCtx mounts on a temp home registry with alpha and beta
// created before the mount, current board = main.
func multiBoardCtx(t *testing.T) (*core.Context, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	reg, err := MountRegistry(home)
	if err != nil {
		t.Fatalf("registry mount: %v", err)
	}
	for _, name := range []string{"alpha", "beta"} {
		if err := reg.CreateBoardRow(name); err != nil {
			t.Fatalf("create board %s: %v", name, err)
		}
	}
	if err := reg.SetCurrentBoard(DefaultBoardName); err != nil {
		t.Fatalf("current back to main: %v", err)
	}
	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := &core.Context{Configs: map[string]any{"kanban": Config{DBPath: "kanban.db"}}}
	p := NewPlugin()
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	// close on cleanup: the plugin's store opened kanban.db - a handle
	// left open keeps windows from removing the TempDir's data dir
	t.Cleanup(func() { _ = p.store.Close() })
	return ctx, home
}

func TestBoardsEndpointListsRegistry(t *testing.T) {
	ctx, home := multiBoardCtx(t)
	ts := serveRoutes(t, ctx)
	res, err := http.Get(ts.URL + "/boards")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Boards []BoardInfo `json:"boards"`
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = home
	if len(got.Boards) != 3 {
		t.Fatalf("boards = %+v", got.Boards)
	}
	if got.Boards[0].Name != DefaultBoardName || !got.Boards[0].Current {
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
	ctx, _ := multiBoardCtx(t)
	ts := serveRoutes(t, ctx)

	// create on alpha (board param)
	res, err := http.Post(ts.URL+"/tasks?board=alpha", "application/json", strings.NewReader(`{"title":"alpha card"}`))
	if err != nil {
		t.Fatal(err)
	}
	var created KanbanTask
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.StatusCode != http.StatusCreated || created.ID == "" {
		t.Fatalf("create = %d %+v", res.StatusCode, created)
	}

	// default board never sees it
	list := func(path string) []KanbanTask {
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Tasks []KanbanTask `json:"tasks"`
		}
		defer res.Body.Close()
		if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
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
	res, _ = http.Get(ts.URL + "/tasks/" + created.ID + "?board=alpha")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("alpha get = %d", res.StatusCode)
	}
	res, _ = http.Post(ts.URL+fmt.Sprintf("/tasks/%s/comments?board=beta", created.ID), "application/json", strings.NewReader(`{"body":"x"}`))
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("beta comment = %d, want 404", res.StatusCode)
	}

	// comments land on the right board
	res, _ = http.Post(ts.URL+"/tasks/"+created.ID+"/comments?board=alpha", "application/json", strings.NewReader(`{"body":"board-scoped"}`))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("comment = %d", res.StatusCode)
	}
	res, _ = http.Get(ts.URL + "/tasks/" + created.ID + "?board=alpha")
	var detail taskDetail
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(detail.Comments) != 1 || detail.Task.ID != created.ID {
		t.Fatalf("detail = %+v", detail)
	}

	// PATCH within the board works; unknown board fails loudly
	res, _ = do("PATCH", ts.URL+"/tasks/"+created.ID+"?board=alpha", `{"status":"todo"}`)
	var patched KanbanTask
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&patched); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if patched.Status != "todo" {
		t.Fatalf("patch = %+v", patched)
	}
	res, _ = do("PATCH", ts.URL+"/tasks/"+created.ID+"?board=gamma", `{"status":"todo"}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown board patch = %d, want 400", res.StatusCode)
	}

	// create on an unknown board -> 400
	res, _ = http.Post(ts.URL+"/tasks?board=gamma", "application/json", strings.NewReader(`{"title":"x"}`))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown board create = %d, want 400", res.StatusCode)
	}
	res.Body.Close()
}

func TestBoardsDeleteEndpoint(t *testing.T) {
	ctx, home := multiBoardCtx(t)
	ts := serveRoutes(t, ctx)

	// DELETE an empty non-default board -> 200, and its rows are gone
	res, err := do("DELETE", ts.URL+"/boards/beta", "")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete beta = %d, want 200", res.StatusCode)
	}

	// the registry answer agrees: main + alpha only
	res, _ = http.Get(ts.URL + "/boards")
	var got struct {
		Boards []BoardInfo `json:"boards"`
	}
	json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if len(got.Boards) != 2 || got.Boards[0].Name != DefaultBoardName || got.Boards[1].Name != "alpha" {
		t.Fatalf("boards after delete = %+v", got.Boards)
	}

	// the default board is refused -> 400 with the refusal text
	res, err = do("DELETE", ts.URL+"/boards/main", "")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("delete main = %d, want 400", res.StatusCode)
	}
	if !strings.Contains(string(body), "refusing to delete the default board") {
		t.Fatalf("delete main body = %q", string(body))
	}
	if _, serr := os.Stat(datadir.DBPath(home, "kanban.db")); serr != nil {
		t.Fatalf("main dsn vanished: %v", serr)
	}

	// unknown boards refused -> 400
	res, _ = do("DELETE", ts.URL+"/boards/ghost", "")
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("delete ghost = %d, want 400", res.StatusCode)
	}

	// the current board is refused (switch first). The flag lives in
	// the one database now: SetCurrentBoard through a second handle
	// on the same file, the served routes see it (the row is the one
	// truth).
	reg, rerr := MountRegistry(home)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if rerr := reg.SetCurrentBoard("alpha"); rerr != nil {
		reg.Close()
		t.Fatal(rerr)
	}
	reg.Close()
	res, _ = do("DELETE", ts.URL+"/boards/alpha", "")
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "current board") {
		t.Fatalf("delete current board = %d %q", res.StatusCode, string(body))
	}
	// back to main: the pre-delete guard order the old pointer test
	// walked
	reg, err = MountRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetCurrentBoard(DefaultBoardName); err != nil {
		reg.Close()
		t.Fatal(err)
	}
	reg.Close()

	// a board holding tasks is refused
	res, _ = http.Post(ts.URL+"/tasks?board=alpha", "application/json", strings.NewReader(`{"title":"stays"}`))
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("seed task = %d", res.StatusCode)
	}
	res, _ = do("DELETE", ts.URL+"/boards/alpha", "")
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "still holds") {
		t.Fatalf("delete non-empty = %d %q", res.StatusCode, string(body))
	}
}

func TestBoardsCreateEndpoint(t *testing.T) {
	ctx, home := multiBoardCtx(t)
	ts := serveRoutes(t, ctx)

	// create -> 201, listed afterwards
	res, err := do("POST", ts.URL+"/boards", `{"name":"gamma"}`)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create gamma = %d %q", res.StatusCode, string(body))
	}
	if !strings.Contains(string(body), `"created":"gamma"`) {
		t.Fatalf("create body = %q", string(body))
	}
	res, _ = http.Get(ts.URL + "/boards")
	var got struct {
		Boards []BoardInfo `json:"boards"`
	}
	json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if len(got.Boards) != 4 || got.Boards[3].Name != "gamma" {
		t.Fatalf("boards after create = %+v", got.Boards)
	}

	// duplicate -> 400; illegal name -> 400
	for _, tc := range []struct {
		payload string
		want    string
	}{
		{`{"name":"gamma"}`, "already exists"},
		{`{"name":"Not A Board"}`, "illegal board name"},
		{`{"name":"main"}`, "already exists"},
		{`{"name":""}`, "illegal board name"},
	} {
		res, err := do("POST", ts.URL+"/boards", tc.payload)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), tc.want) {
			t.Fatalf("create %s = %d %q, want 400 %q", tc.payload, res.StatusCode, string(body), tc.want)
		}
	}

	// the row is the one truth: the file tree stayed a single db
	if _, serr := os.Stat(datadir.DBPath(home, "boards")); !os.IsNotExist(serr) {
		t.Fatalf("boards dir exists: %v", serr)
	}
}
