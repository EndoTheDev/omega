package http_channel

// Variant-surface tests (t_17365247): retry creates the sibling + the
// variants block (no doubles on the active path), partials become
// variants, selection persists and drives the next send's context,
// fork re-plays at its target, and the wire stays backward compatible
// for sessions built before the tree existed.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// transcriptOf fetches the transcript envelope: status + body of the
// active path + variants metadata read.
func transcriptOf(t *testing.T, ts *httptest.Server, id string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(ts.URL + "/sessions/" + id + "/transcript")
	if err != nil {
		t.Fatalf("transcript get: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("transcript decode: %v", err)
	}
	return resp.StatusCode, out
}

// itemContents flattens transcript items to kind:content strings.
func itemContents(items []any) []string {
	var out []string
	for _, raw := range items {
		m, _ := raw.(map[string]any)
		kind, _ := m["kind"].(string)
		content, _ := m["content"].(string)
		out = append(out, kind+":"+content)
	}
	return out
}

// idOf finds the transcript id of the first item with the given kind
// (empty content = first-of-kind).
func idOf(items []any, kind, content string) string {
	for _, raw := range items {
		m, _ := raw.(map[string]any)
		if k, _ := m["kind"].(string); k == kind {
			c, _ := m["content"].(string)
			if content == "" || c == content {
				return fmt.Sprintf("%v", m["id"])
			}
		}
	}
	return ""
}

// postJSON POSTs any body, returns code + decoded envelope.
func postJSON(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// finishRun streams the scripted final answer and ends the run.
func finishRun(t *testing.T, prov *multiProvider, s *Server, sid, answer string) {
	t.Helper()
	prov.send(finalStep(answer)...)
	prov.done()
	waitFor(t, "run finished "+answer, func() bool { return !s.runs.active(sid) })
}

// startExchange opens the scripted first send (user "hi") and finishes
// the first run, cleaning up the stream.
func startExchange(t *testing.T, prov *multiProvider, s *Server, ts *httptest.Server, sid string) {
	t.Helper()
	resp, cancel, _ := postChatStream(t, ts, sid)
	finishRun(t, prov, s, sid, "first answer")
	resp.Body.Close()
	cancel()
}

// skeleton flattens provider-context messages the way assertions read.
func skeleton(msgs []core.Message) []string {
	var out []string
	for _, m := range msgs {
		switch v := m.(type) {
		case core.User:
			out = append(out, "user:"+v.Content)
		case core.Assistant:
			out = append(out, "assistant:"+v.Content)
		}
	}
	return out
}

// TestRetryCreatesSiblingAndVariantsBlock: retry = ONE active response
// on the transcript plus branch metadata - never a second bubble on
// the path, never a duplicate user turn.
func TestRetryCreatesSiblingAndVariantsBlock(t *testing.T) {
	s, prov, st, ts := newMultiServer(t)
	createSession(t, ts, "vr1")
	muteTitles(t, st, "vr1")
	startExchange(t, prov, s, ts, "vr1")

	// the retry
	rr := postRetry(t, ts, "vr1")
	defer rr.Body.Close()
	finishRun(t, prov, s, "vr1", "second answer")

	code, tx := transcriptOf(t, ts, "vr1")
	if code != http.StatusOK {
		t.Fatalf("transcript status = %d, want 200", code)
	}
	items, _ := tx["items"].([]any)
	var messages []string
	for _, c := range itemContents(items) {
		if strings.HasPrefix(c, "message:") {
			messages = append(messages, c)
		}
	}
	if len(messages) != 1 || messages[0] != "message:second answer" {
		t.Fatalf("transcript messages = %v, want exactly ONE active response (second answer)", messages)
	}
	variants, _ := tx["variants"].([]any)
	if len(variants) != 1 {
		t.Fatalf("variants = %+v, want one branch point", tx["variants"])
	}
	b, _ := variants[0].(map[string]any)
	if count, _ := b["count"].(float64); int(count) != 2 {
		t.Fatalf("variant count = %v, want 2", b["count"])
	}
	if active, _ := b["active"].(float64); int(active) != 1 {
		t.Fatalf("variant active = %v, want 1 (the newest retry)", b["active"])
	}
	// the store still carries BOTH runs (the append list stays)
	msgs := messagesOf(t, st, "vr1")
	if len(msgs) != 3 {
		t.Fatalf("store rows = %d, want 3 (user + two runs)", len(msgs))
	}
	users := 0
	for _, m := range msgs {
		if _, ok := m.(core.User); ok {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("user rows = %d, want 1 (no duplicate user bubble)", users)
	}
	// message items carry their ids (branch-point addressing surface)
	if idOf(items, "user", "") == "" || idOf(items, "message", "") == "" {
		t.Fatalf("transcript items missing ids: %+v", items)
	}
}

// TestVariantSelectPersistsAndDrivesContext: select variant 1, the
// transcript reload keeps it, and the next send's context continues
// that branch - asserted on the exact seam /chat consumes.
func TestVariantSelectPersistsAndDrivesContext(t *testing.T) {
	s, prov, st, ts := newMultiServer(t)
	createSession(t, ts, "vr2")
	muteTitles(t, st, "vr2")
	startExchange(t, prov, s, ts, "vr2")
	rr := postRetry(t, ts, "vr2")
	defer rr.Body.Close()
	finishRun(t, prov, s, "vr2", "second answer")

	// branch point id, straight from the transcript items
	_, tx := transcriptOf(t, ts, "vr2")
	items, _ := tx["items"].([]any)
	userID := idOf(items, "user", "")
	if userID == "" || userID == "<nil>" {
		t.Fatalf("user item carries no id: %+v", items)
	}

	// PIN (t_c6f13a82, chat-last 2a): the sink goes in BEFORE the
	// select so the capture sees only the select's repaint.
	var selPainted []core.Panel
	s.chat.SetPanelUpdate(func(p core.Panel) error {
		selPainted = append(selPainted, p)
		return nil
	})

	// select variant 1 (the first response)
	code, sel := postJSON(t, ts.URL+"/sessions/vr2/variants",
		fmt.Sprintf(`{"message_id": %q, "select": 0}`, userID))
	if code != http.StatusOK {
		t.Fatalf("variants select status = %d, want 200", code)
	}
	variants, _ := sel["variants"].([]any)
	b, _ := variants[0].(map[string]any)
	if active, _ := b["active"].(float64); int(active) != 0 {
		t.Fatalf("select response = %+v, want active 0", b)
	}
	// PIN (t_c6f13a82, chat-last 2a): a select is not a run - no run
	// event or delta carries the swap, and the canvas snapshot serves
	// the last PUSHED panel state. The select handler must repaint the
	// focused panel; otherwise the webui's post-select snapshot
	// refetch keeps the OLD variant rows forever (journey 30 red).
	if len(selPainted) == 0 {
		t.Fatal("variant select never repainted the chat panel")
	}
	last := selPainted[len(selPainted)-1].Payload.(core.ChatPayload)
	painted := ""
	for _, row := range last.Rows {
		for _, blk := range row.Blocks {
			painted += blk.Content + "|"
		}
	}
	if !strings.Contains(painted, "first answer") || strings.Contains(painted, "second answer") {
		t.Fatalf("panel after select = %q, want the selected variant (first answer) only", painted)
	}
	// PERSISTENCE: a fresh reload still selects variant 1
	_, tx = transcriptOf(t, ts, "vr2")
	items, _ = tx["items"].([]any)
	joined := "|" + strings.Join(itemContents(items), "|")
	if !strings.Contains(joined, "|message:first answer") {
		t.Fatalf("reloaded transcript = %v, want variant 1 active (first answer)", items)
	}

	// the NEXT send's context continues the selected branch: exactly
	// what /chat feeds the provider through sessionHistory
	raw, _ := json.Marshal(map[string]any{"role": "user", "content": "next question"})
	req := chatRequest{SessionID: "vr2", Messages: []json.RawMessage{raw}}
	incoming, err := decodeMessages(req.Messages)
	if err != nil {
		t.Fatalf("decode incoming: %v", err)
	}
	ctxMsgs, loadErr := s.sessionHistory(context.Background(), &req, incoming)
	if loadErr != nil {
		t.Fatalf("context load failed: %v", loadErr)
	}
	got := skeleton(ctxMsgs)
	want := []string{"user:hi", "assistant:first answer", "user:next question"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("context after select = %v, want %v", got, want)
	}
}

// TestPartialBecomesVariant: a cancelled run's partial stays its own
// selectable sibling; the next retry is the next variant; selecting
// the partial's variant renders it again.
func TestPartialBecomesVariant(t *testing.T) {
	s, prov, st, ts := newMultiServer(t)
	createSession(t, ts, "vr3")
	muteTitles(t, st, "vr3")
	startExchange(t, prov, s, ts, "vr3")

	// a retry that gets cancelled mid-stream
	rr := postRetry(t, ts, "vr3")
	defer rr.Body.Close()
	prov.send(core.ResponseChunk{Type: "response_chunk", Content: "kept partial"})
	stop, err := http.Post(ts.URL+"/sessions/vr3/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	stop.Body.Close()
	prov.done()
	waitFor(t, "cancelled retry finished", func() bool { return !s.runs.active("vr3") })

	// the partial is the active response; both runs are variants
	_, tx := transcriptOf(t, ts, "vr3")
	items, _ := tx["items"].([]any)
	joined := "|" + strings.Join(itemContents(items), "|")
	if !strings.Contains(joined, "|message:kept partial") {
		t.Fatalf("transcript = %v, want the partial as the active response", items)
	}
	variants, _ := tx["variants"].([]any)
	if len(variants) != 1 {
		t.Fatalf("variants = %+v, want the retry branch point", tx["variants"])
	}
	// select variant 1 -> the FIRST response renders again
	userID := idOf(items, "user", "")
	code, _ := postJSON(t, ts.URL+"/sessions/vr3/variants",
		fmt.Sprintf(`{"message_id": %q, "select": 0}`, userID))
	if code != http.StatusOK {
		t.Fatalf("select 0 status = %d, want 200", code)
	}
	_, tx = transcriptOf(t, ts, "vr3")
	items, _ = tx["items"].([]any)
	joined = "|" + strings.Join(itemContents(items), "|")
	if !strings.Contains(joined, "|message:first answer") {
		t.Fatalf("transcript after select = %v, want first answer active", items)
	}
}

// TestForkContinuesFromHistoricalMessage: fork at an assistant response
// chains the fork's message onto it; the fork run's head answers it;
// the fork chain is its own linear path (no variants block).
func TestForkContinuesFromHistoricalMessage(t *testing.T) {
	s, prov, st, ts := newMultiServer(t)
	createSession(t, ts, "vr4")
	muteTitles(t, st, "vr4")
	startExchange(t, prov, s, ts, "vr4")

	_, tx := transcriptOf(t, ts, "vr4")
	items, _ := tx["items"].([]any)
	aid := idOf(items, "message", "first answer")
	postJSON(t, ts.URL+"/chat",
		fmt.Sprintf(`{"session_id": "vr4", "fork": %q, "messages": [{"role": "user", "content": "continue here"}]}`, aid))
	finishRun(t, prov, s, "vr4", "forked answer")

	_, tx = transcriptOf(t, ts, "vr4")
	items, _ = tx["items"].([]any)
	joined := "|" + strings.Join(itemContents(items), "|")
	in := strings.Index(joined, "|message:first answer|")
	cont := strings.Index(joined, "|user:continue here|")
	ans := strings.Index(joined, "|message:forked answer")
	if in == -1 || cont == -1 || ans == -1 || cont < in || ans < in || ans < cont {
		t.Fatalf("transcript = %v, want the fork bubble + answer after the forked response", items)
	}
	variants, _ := tx["variants"].([]any)
	if len(variants) != 0 {
		t.Fatalf("fork chain must not report variants: %+v", tx["variants"])
	}
}

// TestForkOfUserTurnCreatesVariant: forking a user turn regenerates
// (retry pinned there) - a sibling head under that user, no new user
// bubble.
func TestForkOfUserTurnCreatesVariant(t *testing.T) {
	s, prov, st, ts := newMultiServer(t)
	createSession(t, ts, "vr5")
	muteTitles(t, st, "vr5")
	startExchange(t, prov, s, ts, "vr5")

	_, tx := transcriptOf(t, ts, "vr5")
	items, _ := tx["items"].([]any)
	userID := idOf(items, "user", "")
	postJSON(t, ts.URL+"/chat", fmt.Sprintf(`{"session_id": "vr5", "fork": %q}`, userID))
	finishRun(t, prov, s, "vr5", "regenerated")

	msgs := messagesOf(t, st, "vr5")
	users := 0
	for _, m := range msgs {
		if _, ok := m.(core.User); ok {
			users++
		}
	}
	if users != 1 || len(msgs) != 3 {
		t.Fatalf("rows = %d users = %d, want 3 rows, 1 user (sibling variant, no new bubble)", len(msgs), users)
	}
	_, tx2 := transcriptOf(t, ts, "vr5")
	v2, _ := tx2["variants"].([]any)
	if len(v2) != 1 {
		t.Fatalf("variants = %+v, want the fork branch point", tx2["variants"])
	}
}

// TestVariantsGuards: 501 without a store, 404 unknown session, 400 on
// unknown/invalid message ids and out-of-range selects, plus the
// /chat retry/fork/fork-session guards.
func TestVariantsGuards(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/x/variants", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("no-store variants = %d, want 501", rec.Code)
	}

	s, prov, st, ts := newMultiServer(t)
	createSession(t, ts, "vg1")
	muteTitles(t, st, "vg1")
	startExchange(t, prov, s, ts, "vg1")
	userID := idOfUser(t, ts, "vg1")

	if r, _ := http.Get(ts.URL + "/sessions/nope/variants"); r.StatusCode != http.StatusNotFound {
		r.Body.Close()
		t.Fatalf("unknown session variants = want 404")
	} else {
		r.Body.Close()
	}
	code, _ := postJSON(t, ts.URL+"/sessions/vg1/variants", `{"message_id": "999"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("unknown message select = %d, want 400", code)
	}
	code, _ = postJSON(t, ts.URL+"/sessions/vg1/variants", `{"message_id": "zap"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("malformed id select = %d, want 400", code)
	}
	code, _ = postJSON(t, ts.URL+"/sessions/vg1/variants", "")
	if code != http.StatusBadRequest {
		t.Fatalf("invalid body select = %d, want 400", code)
	}
	// out-of-range index (single run: only 0 exists)
	code, _ = postJSON(t, ts.URL+"/sessions/vg1/variants",
		fmt.Sprintf(`{"message_id": %q, "select": 3}`, userID))
	if code != http.StatusBadRequest {
		t.Fatalf("out-of-range select = %d, want 400", code)
	}
	// index select on a single-run branch: 0 is the whole extent and
	// stays a no-op 200 (re-pins the default)
	code, _ = postJSON(t, ts.URL+"/sessions/vg1/variants",
		fmt.Sprintf(`{"message_id": %q, "select": 0}`, userID))
	if code != http.StatusOK {
		t.Fatalf("single-run re-pin = %d, want 200", code)
	}
	// /chat guards: retry+fork conflict; fork without session; fork
	// target not on the path
	code, _ = postJSON(t, ts.URL+"/chat", `{"session_id": "vg1", "retry": true, "fork": "`+userID+`"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("retry+fork = %d, want 400", code)
	}
	code, _ = postJSON(t, ts.URL+"/chat", `{"fork": "`+userID+`"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("sessionless fork = %d, want 400", code)
	}
	code, _ = postJSON(t, ts.URL+"/chat", `{"session_id": "vg1", "fork": "not_on_path_1"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("off-path fork = %d, want 400", code)
	}
}

func idOfUser(t *testing.T, ts *httptest.Server, id string) string {
	t.Helper()
	_, tx := transcriptOf(t, ts, id)
	items, _ := tx["items"].([]any)
	return idOf(items, "user", "")
}

// TestLinearSessionHasNoVariantsBlock: backward compatibility - a
// session without branch points serves the exact pre-tree envelope
// (the variants key stays ABSENT off the wire).
func TestLinearSessionHasNoVariantsBlock(t *testing.T) {
	s, prov, st, ts := newMultiServer(t)
	createSession(t, ts, "vr6")
	muteTitles(t, st, "vr6")
	startExchange(t, prov, s, ts, "vr6")
	code, tx := transcriptOf(t, ts, "vr6")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if _, ok := tx["variants"]; ok {
		t.Fatalf("variants key present for a linear session: %+v", tx["variants"])
	}
	items, _ := tx["items"].([]any)
	if len(itemContents(items)) != 2 {
		t.Fatalf("linear transcript = %+v, want the two exchange items", items)
	}
}
