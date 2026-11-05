package kanban

// The reorder route tests (card t_09b44af1): PATCH after_id carries the
// drag-order slot over the same /tasks/{id} route, the wire shape never
// grows a position field (store-internal state), and bad slots answer
// 400 - the board refetches and the card snaps back.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func postTask(t *testing.T, tsURL, title string) string {
	t.Helper()
	res, err := http.Post(tsURL+"/tasks", "application/json", strings.NewReader(`{"title":"`+title+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("post %q = %d", title, res.StatusCode)
	}
	var created KanbanTask
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return created.ID
}

func patchBody(t *testing.T, tsURL, id, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPatch, tsURL+"/tasks/"+id, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(raw)
}

func listOrder(t *testing.T, tsURL string) []string {
	t.Helper()
	res, err := http.Get(tsURL + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got struct {
		Tasks []KanbanTask `json:"tasks"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, t2 := range got.Tasks {
		ids = append(ids, t2.ID)
		if strings.Contains(fmt.Sprint(t2), "Position") {
			t.Fatalf("wire shape leaked the position field: %+v", t2)
		}
	}
	return ids
}

func TestPatchReorder(t *testing.T) {
	_, ctx, _ := mountPlugin(t)
	ts := serveRoutes(t, ctx)

	a := postTask(t, ts.URL, "a-card")
	b := postTask(t, ts.URL, "b-card")
	c := postTask(t, ts.URL, "c-card")

	// wire shape: the list arrives position-ordered with NO position field
	got := listOrder(t, ts.URL)
	if len(got) != 3 || got[0] != c || got[1] != b || got[2] != a {
		t.Fatalf("initial order = %v (c,b,a expected)", got)
	}

	// slot a behind c: [c, a, b]
	res, _ := patchBody(t, ts.URL, a, `{"status":"","after_id":"`+c+`"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("after_id patch = %d", res.StatusCode)
	}
	got = listOrder(t, ts.URL)
	if fmt.Sprint(got) != fmt.Sprint([]string{c, a, b}) {
		t.Fatalf("after reorder = %v, want [c a b]", got)
	}

	// cross-column: status rides the same PATCH
	res, _ = patchBody(t, ts.URL, c, `{"status":"todo","after_id":""}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("move patch = %d", res.StatusCode)
	}
	got = listOrder(t, ts.URL)
	if len(got) != 3 || got[0] != c {
		t.Fatalf("cross-column order = %v, want c first", got)
	}

	// a bad slot answers 400 (board refetches, card snaps back)
	res, raw := patchBody(t, ts.URL, a, `{"after_id":"t_ffffffff"}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad after_id = %d", res.StatusCode)
	}
	if raw != "" && !strings.Contains(raw, "not in column") {
		t.Fatalf("refusal text = %q", raw)
	}

	// malformed JSON answers 400
	res, _ = patchBody(t, ts.URL, a, "{")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad body = %d", res.StatusCode)
	}
}
