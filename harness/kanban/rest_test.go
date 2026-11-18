package kanban

// The board REST tests (from the pre-dissolve http_channel tests.

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

// TestTasksListArchivedByDefaultHidden: the REST list carries the store's new default.
func TestTasksListArchivedByDefaultHidden(t *testing.T) {
	_, ctx, _ := mountPlugin(t)
	ts := serveRoutes(t, ctx)

	for _, status := range []string{"triage", "archived"} {
		res, err := http.Post(ts.URL+"/tasks", "application/json",
			strings.NewReader(fmt.Sprintf(`{"title":"row %s","status":"%s"}`, status, status)))
		if err != nil || res.StatusCode != http.StatusCreated {
			t.Fatalf("post %s = %v %d", status, err, res.StatusCode)
		}
		res.Body.Close()
	}

	// count rows through the REST filter
	list := func(q string) []KanbanTask {
		res, _ := http.Get(ts.URL + "/tasks" + q)
		var got struct {
			Tasks []KanbanTask `json:"tasks"`
		}
		json.NewDecoder(res.Body).Decode(&got)
		res.Body.Close()
		return got.Tasks
	}

	defaults := list("")
	if len(defaults) != 1 || defaults[0].Status != "triage" {
		t.Fatalf("default list = %+v, archived must hide", defaults)
	}
	arch := list("?status=archived")
	if len(arch) != 1 || arch[0].Status != "archived" {
		t.Fatalf("archived view = %+v", arch)
	}
	every := list("?status=all")
	if len(every) != 2 {
		t.Fatalf("all view = %+v", every)
	}
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

// multiBoardCtx mounts on a temp home registry with alpha and beta created before the.
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
	// close on cleanup: the plugin's store opened kanban.db.
	t.Cleanup(func() { _ = p.store.Close() })
	return ctx, home
}

func TestBoardsEndpointListsRegistry(t *testing.T) {
	ctx, _ := multiBoardCtx(t)
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

	res, _ = http.Post(ts.URL+"/tasks?board=gamma", "application/json", strings.NewReader(`{"title":"x"}`))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown board create = %d, want 400", res.StatusCode)
	}
	res.Body.Close()
}

func TestBoardsDeleteEndpoint(t *testing.T) {
	ctx, _ := multiBoardCtx(t)
	ts := serveRoutes(t, ctx)

	// 0.17.3 make-over: every board leaves, main included, and a home may end on ZERO.
	res, _ := http.Post(ts.URL+"/tasks?board=beta", "application/json", strings.NewReader(`{"title":"live"}`))
	var created KanbanTask
	json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	res, _ = do("DELETE", ts.URL+"/boards/beta", "")
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "live card") {
		t.Fatalf("delete of a board holding a live card = %d %q", res.StatusCode, string(body))
	}

	// the archive lane is the last stop.
	res, _ = do("PATCH", ts.URL+"/tasks/"+created.ID+"?board=beta", `{"status":"archived"}`)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("archive card = %d", res.StatusCode)
	}
	res, _ = do("DELETE", ts.URL+"/boards/beta", "")
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete of an all-archived board = %d", res.StatusCode)
	}

	// the default board is deletable now (it is the current one here).
	res, _ = do("DELETE", ts.URL+"/boards/main", "")
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete main = %d, want 200", res.StatusCode)
	}
	res, _ = http.Get(ts.URL + "/boards")
	var got struct {
		Boards []BoardInfo `json:"boards"`
	}
	json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if len(got.Boards) != 1 || got.Boards[0].Name != "alpha" || !got.Boards[0].Current {
		t.Fatalf("boards after deleting main = %+v, want [alpha current]", got.Boards)
	}

	res, _ = do("DELETE", ts.URL+"/boards/ghost", "")
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "no such board") {
		t.Fatalf("delete ghost = %d %q", res.StatusCode, string(body))
	}

	// the last board leaves too.
	res, _ = do("DELETE", ts.URL+"/boards/alpha", "")
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete of the last board = %d", res.StatusCode)
	}
	res, _ = http.Get(ts.URL + "/boards")
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.TrimSpace(string(raw)) != `{"boards":[]}` {
		t.Fatalf("zero-board registry answer = %q", string(raw))
	}
	res, _ = http.Get(ts.URL + "/tasks?status=all")
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || strings.TrimSpace(string(raw)) != `{"tasks":[]}` {
		t.Fatalf("board-less tasks answer = %d %q", res.StatusCode, string(raw))
	}

	res, _ = do("POST", ts.URL+"/boards", `{"name":"reborn"}`)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create after zero boards = %d", res.StatusCode)
	}
	res, _ = http.Get(ts.URL + "/boards")
	json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if len(got.Boards) != 1 || got.Boards[0].Name != "reborn" || !got.Boards[0].Current {
		t.Fatalf("boards after the first create = %+v, want [reborn current]", got.Boards)
	}
}

func TestBoardsRenameEndpoint(t *testing.T) {
	ctx, _ := multiBoardCtx(t)
	ts := serveRoutes(t, ctx)

	res, _ := http.Post(ts.URL+"/tasks?board=alpha", "application/json", strings.NewReader(`{"title":"kept"}`))
	var created KanbanTask
	json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()

	res, err := do("PATCH", ts.URL+"/boards/alpha", `{"name":"alpha-2"}`)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"renamed":"alpha-2"`) {
		t.Fatalf("rename alpha = %d %q", res.StatusCode, string(body))
	}
	res, _ = http.Get(ts.URL + "/tasks?board=alpha-2&status=all")
	var listed struct {
		Tasks []KanbanTask `json:"tasks"`
	}
	json.NewDecoder(res.Body).Decode(&listed)
	res.Body.Close()
	if len(listed.Tasks) != 1 || listed.Tasks[0].ID != created.ID {
		t.Fatalf("cards after the rename = %+v", listed.Tasks)
	}

	res, _ = do("PATCH", ts.URL+"/boards/alpha-2", `{"name":"main"}`)
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "already exists") {
		t.Fatalf("rename onto an existing board = %d %q", res.StatusCode, string(body))
	}
	res, _ = do("PATCH", ts.URL+"/boards/ghost", `{"name":"haunt"}`)
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "no such board") {
		t.Fatalf("rename of an unknown board = %d %q", res.StatusCode, string(body))
	}

}

func TestTasksAllBoardsScope(t *testing.T) {
	ctx, _ := multiBoardCtx(t)
	ts := serveRoutes(t, ctx)

	for _, b := range []string{"main", "alpha", "beta"} {
		res, _ := http.Post(ts.URL+"/tasks?board="+b, "application/json", strings.NewReader(`{"title":"on `+b+`"}`))
		res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("seed task on %s = %d", b, res.StatusCode)
		}
	}

	res, _ := http.Get(ts.URL + "/tasks?scope=all&status=all")
	var listed struct {
		Tasks []KanbanTask `json:"tasks"`
	}
	json.NewDecoder(res.Body).Decode(&listed)
	res.Body.Close()
	if len(listed.Tasks) != 3 {
		t.Fatalf("scope=all listed %d cards, want 3", len(listed.Tasks))
	}
	seen := map[string]bool{}
	for _, task := range listed.Tasks {
		if task.Board == "" {
			t.Fatalf("scope=all row %s carries no board name", task.ID)
		}
		seen[task.Board] = true
	}
	for _, b := range []string{"main", "alpha", "beta"} {
		if !seen[b] {
			t.Fatalf("scope=all missed board %q (rows: %+v)", b, listed.Tasks)
		}
	}

	// the board-scoped read still carries NO board field (byte-stable wire)
	res, _ = http.Get(ts.URL + "/tasks?board=alpha&status=all")
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(raw), `"board":`) {
		t.Fatalf("a board-scoped read grew a board field: %q", string(raw))
	}
}

func TestBoardsCreateEndpoint(t *testing.T) {
	ctx, home := multiBoardCtx(t)
	ts := serveRoutes(t, ctx)

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

	if _, serr := os.Stat(datadir.DBPath(home, "boards")); !os.IsNotExist(serr) {
		t.Fatalf("boards dir exists: %v", serr)
	}
}

// TestRESTMutationsPushPanels: the board renders from canvas deltas now instead of a 5s
// GET poll, so the HTTP surface must speak them - a 2xx POST/PATCH refreshes the
// module's panels through the installed sink, a GET read never pushes, and a refused
// mutation pushes nothing (the error text is the answer).
func TestRESTMutationsPushPanels(t *testing.T) {
	p, ctx, _ := mountPlugin(t)
	pushed := capture(t, p.capsule)
	ts := serveRoutes(t, ctx)

	res, err := http.Get(ts.URL + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if len(*pushed) != 0 {
		t.Fatalf("GET pushed %d panels, want 0", len(*pushed))
	}

	created := postTask(t, ts.URL, "rest push")
	if len(*pushed) != 1 || (*pushed)[0].ID != listPanelID {
		t.Fatalf("POST pushed %+v, want one %q panel", *pushed, listPanelID)
	}

	res2, body := patchBody(t, ts.URL, created, `{"status":"done"}`)
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("patch = %d %s", res2.StatusCode, body)
	}
	if len(*pushed) != 2 {
		t.Fatalf("PATCH pushed %d panels total, want 2", len(*pushed))
	}
	payload, ok := (*pushed)[1].Payload.(taskListPayload)
	if !ok {
		t.Fatalf("pushed payload is %T, want taskListPayload", (*pushed)[1].Payload)
	}
	found := false
	for _, row := range payload.Rows {
		if row.ID == created && row.Status == StatusDone && row.RowAction == nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("pushed panel does not carry %s as a settled done row: %+v", created, payload.Rows)
	}

	res3, _ := patchBody(t, ts.URL, "t_ffffffffffffffff", `{"status":"done"}`)
	if res3.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown-id patch = %d, want 404", res3.StatusCode)
	}
	if len(*pushed) != 2 {
		t.Fatalf("a refused mutation pushed (%d panels, want 2)", len(*pushed))
	}
}
