package http_channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/verdict"
)

// vo builds pick rows from labels: the test shorthand for
// []core.VerdictOption (labels only, no description).
func vo(labels ...string) []core.VerdictOption {
	out := make([]core.VerdictOption, len(labels))
	for i, l := range labels {
		out[i] = core.VerdictOption{Label: l}
	}
	return out
}

// verdictServer builds a server with the verdicts seam mounted on a
// fresh hub, plus a helper to issue a blocking card in the background
// (the answer POST or the hub's expiry resolves it).
func verdictServer(t *testing.T) (*httptest.Server, *verdict.Hub) {
	t.Helper()
	s := NewServer(nil, nil, nil, nil, nil, nil)
	h := verdict.NewHub(nil, nil)
	s.seams.verdicts = h
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, h
}

func verdictGet(t *testing.T, ts *httptest.Server, path string) map[string]any {
	t.Helper()
	r, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer r.Body.Close()
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode >= 400 {
		t.Fatalf("GET %s status %d: %s", path, r.StatusCode, body)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("GET %s decode: %v (body %s)", path, err, body)
	}
	return out
}

// issueCard runs hub.Ask in the background and returns a done channel
// plus the card id once it is registered.
func issueCard(t *testing.T, h *verdict.Hub, session, id string) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_, _ = h.Ask(context.Background(), core.VerdictPrompt{
			ID: id, SessionID: session, Title: "Approve?", Options: vo("yes", "no"),
		})
		close(done)
	}()
	// wait for the card to register before the caller inspects it.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(h.Pending(session)) > 0 {
			return done
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("card %s did not register in session %s", id, session)
	return done
}

func TestVerdictsEndpoints501WithoutSeam(t *testing.T) {
	ts := httptest.NewServer(NewServer(nil, nil, nil, nil, nil, nil).Handler())
	t.Cleanup(ts.Close)

	for _, path := range []string{"/verdicts", "/sessions/s1/verdicts"} {
		r, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if r.StatusCode != http.StatusNotImplemented {
			t.Errorf("GET %s without seam = %d, want 501", path, r.StatusCode)
		}
		r.Body.Close()
	}

	// answering without the seam also 501s (not 400): the seam is the
	// precondition, the bad-method/status layering comes after.
	r, err := http.Post(ts.URL+"/sessions/s1/verdicts", "application/json", strings.NewReader(`{"option_index":1}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if r.StatusCode != http.StatusNotImplemented {
		t.Errorf("POST without seam = %d, want 501", r.StatusCode)
	}
	r.Body.Close()
}

func TestVerdictsListEmpty(t *testing.T) {
	ts, _ := verdictServer(t)
	out := verdictGet(t, ts, "/verdicts")
	if out["count"] != float64(0) {
		t.Fatalf("count = %v, want 0", out["count"])
	}
}

func TestVerdictsListPendingAndAnswer(t *testing.T) {
	ts, h := verdictServer(t)
	done := issueCard(t, h, "s1", "c1")
	t.Cleanup(func() { <-done })

	one := verdictGet(t, ts, "/sessions/s1/verdicts")
	if one["count"] != float64(1) {
		t.Fatalf("per-session count = %v, want 1", one["count"])
	}
	all := verdictGet(t, ts, "/verdicts")
	if all["count"] != float64(1) {
		t.Fatalf("cross-session count = %v, want 1", all["count"])
	}

	// answer the oldest pending card (no card_id in the body).
	r, err := http.Post(ts.URL+"/sessions/s1/verdicts", "application/json",
		strings.NewReader(`{"option_index":1}`))
	if err != nil {
		t.Fatalf("POST answer: %v", err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("answer status = %d, want 200: %s", r.StatusCode, body)
	}
	var ans map[string]any
	if err := json.Unmarshal(body, &ans); err != nil {
		t.Fatalf("answer decode: %v", err)
	}
	if ans["verdict"] != "answer" || ans["option"] != "yes" || ans["card_id"] != "c1" {
		t.Fatalf("answer body = %v, want verdict=answer option=yes card_id=c1", ans)
	}
}

func TestVerdictsSecondAnswerReturns409(t *testing.T) {
	ts, h := verdictServer(t)
	done := issueCard(t, h, "s1", "c1")
	// first answer wins
	r, _ := http.Post(ts.URL+"/sessions/s1/verdicts", "application/json", strings.NewReader(`{"option_index":1}`))
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("first answer = %d, want 200", r.StatusCode)
	}
	<-done
	// the race loser names the SAME card (its own surface's card id):
	// settled -> 409 with the reason, never double-applied
	r, _ = http.Post(ts.URL+"/sessions/s1/verdicts", "application/json", strings.NewReader(`{"card_id":"c1","option_index":2}`))
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusConflict {
		t.Fatalf("second answer = %d, want 409: %s", r.StatusCode, body)
	}
	var e map[string]any
	_ = json.Unmarshal(body, &e)
	if !strings.Contains(e["error"].(string), "already answered") {
		t.Fatalf("409 error = %v, want containing 'already answered'", e["error"])
	}
}

func TestVerdictsMultiAnswerEchoesIndexes(t *testing.T) {
	ts, h := verdictServer(t)
	done := make(chan struct{})
	go func() {
		_, _ = h.Ask(context.Background(), core.VerdictPrompt{
			ID: "cm", SessionID: "s1", Title: "Pick lanes", Multi: true,
			Options: []core.VerdictOption{{Label: "Ship", Description: "merge now"}, {Label: "Hold"}, {Label: "Stop"}},
		})
		close(done)
	}()
	// wait for the card; the pending list must carry the described
	// options and the multi flag (the card the webui re-renders from).
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(h.Pending("s1")) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	list := verdictGet(t, ts, "/sessions/s1/verdicts")
	rows, _ := list["cards"].([]any)
	if len(rows) != 1 {
		t.Fatalf("pending cards = %v, want one", list["cards"])
	}
	card, _ := rows[0].(map[string]any)
	if card["multi"] != true {
		t.Fatalf("pending card multi = %v, want true", card["multi"])
	}
	opts, _ := card["options"].([]any)
	first, _ := opts[0].(map[string]any)
	if first["label"] != "Ship" || first["description"] != "merge now" {
		t.Fatalf("pending option 0 = %v, want label Ship description merge-now", first)
	}

	r, err := http.Post(ts.URL+"/sessions/s1/verdicts", "application/json",
		strings.NewReader(`{"card_id":"cm","option_indexes":[1,3]}`))
	if err != nil {
		t.Fatalf("POST answer: %v", err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("multi answer status = %d, want 200: %s", r.StatusCode, body)
	}
	var ans map[string]any
	if err := json.Unmarshal(body, &ans); err != nil {
		t.Fatalf("answer decode: %v", err)
	}
	ixs, _ := ans["option_indexes"].([]any)
	if len(ixs) != 2 || ixs[0] != float64(1) || ixs[1] != float64(3) {
		t.Fatalf("answer indexes = %v, want [1 3]", ans["option_indexes"])
	}
	labels, _ := ans["options"].([]any)
	if len(labels) != 2 || labels[0] != "Ship" || labels[1] != "Stop" {
		t.Fatalf("answer labels = %v, want [Ship Stop]", ans["options"])
	}
	<-done
}

func TestVerdictsAnswerUnknownCard404(t *testing.T) {
	ts, h := verdictServer(t)
	// no pending card in s1 at all -> answering by a ghost card id 404s
	r, _ := http.Post(ts.URL+"/sessions/s1/verdicts", "application/json",
		strings.NewReader(`{"card_id":"ghost","option_index":1}`))
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown card = %d, want 404: %s", r.StatusCode, body)
	}
	_ = h // hub unused beyond mount
}

func TestVerdictsInvalidBody400(t *testing.T) {
	ts, _ := verdictServer(t)
	r, _ := http.Post(ts.URL+"/sessions/s1/verdicts", "application/json",
		strings.NewReader(`{not json`))
	r.Body.Close()
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid body = %d, want 400", r.StatusCode)
	}
}

func TestVerdictsPostWithoutSession400(t *testing.T) {
	ts, _ := verdictServer(t)
	r, _ := http.Post(ts.URL+"/verdicts", "application/json", strings.NewReader(`{"option_index":1}`))
	r.Body.Close()
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /verdicts = %d, want 400 (answering needs a session)", r.StatusCode)
	}
}

func TestVerdictsMethodNotAllowed(t *testing.T) {
	ts, _ := verdictServer(t)
	r, err := http.NewRequest(http.MethodDelete, ts.URL+"/verdicts", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /verdicts = %d, want 405", resp.StatusCode)
	}
}
