package store

// routes_test.go: the session-MANAGEMENT REST face's gates (the
// shell-ring shrink wave 2, card t_490fed88). The handlers ported
// verbatim out of agent/api (server_test/server_branch_test/
// server_insights_test pre-move), so the assertions port with them -
// the face serves the same wire shapes under the same paths, owned by
// the capsule now (the three-layer sessions law). The fixture mounts
// the registrationRows() onto a real mux, so the method patterns
// (404/405 semantics of Go 1.22's ServeMux) ride under test too.
//
// The chat floors they left behind (POST /sessions id creation, the
// /sessions/{id} detail/delete) stay tested in agent/api - those live
// in the base set and die with it, never with this capsule.

import (
	"context"
	"encoding/json"

	// the chat floor parity read (the /insights command's own numbers)
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// fakeChatRuns is the ChatRunsReader double for the running-flag law:
// ids listed true stream live, everything else is idle.
type fakeChatRuns struct {
	live map[string]bool
}

func (f fakeChatRuns) Active(id string) bool { return f.live[id] }

// newRoutesFixture opens an in-memory store, mounts the capsule's
// registrationRows() on a real mux (the same registrations the plugin
// makes at mount), and returns the pieces the tests assert with.
func newRoutesFixture(t *testing.T) (*Routes, *Store, *http.ServeMux, *core.Context) {
	t.Helper()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := &core.Context{}
	rt := newRoutes(ctx, st)
	mux := http.NewServeMux()
	for _, row := range rt.registrationRows() {
		mux.HandleFunc(row.pattern, row.handler)
	}
	return rt, st, mux, ctx
}

// serve runs one request through the registered mux (a real pattern
// dispatch, not a direct handler call) and returns a recorder.
func serve(t *testing.T, mux *http.ServeMux, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// capsuleHTTP serves the fixture's mux on a real listener - a socket
// round-trip for the attachment/probe reads, not a recorder-only pass.
func capsuleHTTP(t *testing.T, mux *http.ServeMux) string {
	t.Helper()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts.URL
}

func createFixtureSession(t *testing.T, st *Store, id, label string) {
	t.Helper()
	if err := st.CreateSession(context.Background(), id, "", label); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
}

// --- the sessions list -------------------------------------------------

func TestSessionsListRows(t *testing.T) {
	_, st, mux, ctx := newRoutesFixture(t)
	createFixtureSession(t, st, "a", "alpha")
	createFixtureSession(t, st, "b", "")
	if err := st.AppendMessage(context.Background(), "a", core.NewUser("hello")); err != nil {
		t.Fatalf("append: %v", err)
	}
	// the live-run truth: "a" streams right now, "b" does not
	ctx.ApplyOffer("chat-runs", "test", fakeChatRuns{live: map[string]bool{"a": true}})

	rec := serve(t, mux, http.MethodGet, "/sessions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var rows []sessionRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		switch row.ID {
		case "a":
			if row.MessageCount != 1 || !row.Running {
				t.Fatalf("row a = %+v, want msg_count 1 and running true", row)
			}
			if row.Model != "" {
				t.Fatalf("row a model = %q, want the no-pin empty fallback", row.Model)
			}
		case "b":
			if row.Running {
				t.Fatalf("row b = %+v, want running false", row)
			}
		default:
			t.Fatalf("unexpected row %q", row.ID)
		}
	}

	// archived filtering: the default list hides archived rows; the
	// sessions tab drives the same archived=include param the search takes
	if err := st.SetSessionArchived(context.Background(), "b", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	rec = serve(t, mux, http.MethodGet, "/sessions", "")
	var visible []sessionRow
	json.Unmarshal(rec.Body.Bytes(), &visible)
	if len(visible) != 1 || visible[0].ID != "a" {
		t.Fatalf("default list = %v, want only a", visible)
	}
	rec = serve(t, mux, http.MethodGet, "/sessions?archived=include", "")
	json.Unmarshal(rec.Body.Bytes(), &visible)
	if len(visible) != 2 {
		t.Fatalf("archived=include list = %v, want both", visible)
	}
}

// TestSessionsListNoChatRunsOffer: no serving channel up (the offered
// reader is nil) - the list answers running=false everywhere, the
// honest empty state, never a fault.
func TestSessionsListNoChatRunsOffer(t *testing.T) {
	rt, st, mux, _ := newRoutesFixture(t)
	// no serving channel up: drop the offer before the read - the
	// reader resolves nil and the list answers running=false everywhere,
	// the honest empty state, never a fault
	rt.ctx.ApplyOffer("chat-runs", "test", fakeChatRuns{live: map[string]bool{}})
	rt.ctx.DropOffer("chat-runs", "test")
	createFixtureSession(t, st, "solo", "")
	rec := serve(t, mux, http.MethodGet, "/sessions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	var rows []sessionRow
	json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0].Running {
		t.Fatalf("rows = %+v, want running false with no chat-runs offer", rows)
	}
}

// --- the search seat ----------------------------------------------------

func TestSessionsSearchCapsule(t *testing.T) {
	_, st, mux, _ := newRoutesFixture(t)
	createFixtureSession(t, st, "hit", "the needle title")
	createFixtureSession(t, st, "quiet", "calm")
	if err := st.AppendMessage(context.Background(), "quiet", core.NewUser("needle in the body")); err != nil {
		t.Fatalf("append: %v", err)
	}

	rec := serve(t, mux, http.MethodGet, "/sessions/search?q=needle", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d: %s", rec.Code, rec.Body.String())
	}
	var hits []searchHit
	json.Unmarshal(rec.Body.Bytes(), &hits)
	if len(hits) != 2 {
		t.Fatalf("hits = %v, want one title + one content hit", hits)
	}
	// title hits rank first, the title one carries the titleMatch flag
	if hits[0].ID != "hit" || !hits[0].TitleMatch {
		t.Fatalf("first hit = %+v, want the title hit ranked first", hits[0])
	}

	// a missing/empty q is 200 [] - nothing to search
	rec = serve(t, mux, http.MethodGet, "/sessions/search", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty q = %d %q, want 200 []", rec.Code, rec.Body.String())
	}

	// The FTS5 400 branch is defensively unreachable through this read:
	// the store's SearchSessions phrase-escapes every term (quoteFTS
	// wraps the term in quotes and escapes embedded ones), so FTS5
	// never sees raw syntax - the driver can surface a reject only on
	// a real store fault (which trips the 500, or the list too). The
	// branch stays in the handler for the honest failure shape; the
	// honest test here is that a hostile term still answers 200 rows.
	rec = serve(t, mux, http.MethodGet, "/sessions/search?q=%22unclosed+needle", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("quoted-hostile term = %d, want 200 (the term escapes into a literal phrase)", rec.Code)
	}

	// archived sessions drop out of the default search; include returns them
	if err := st.SetSessionArchived(context.Background(), "quiet", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	rec = serve(t, mux, http.MethodGet, "/sessions/search?q=needle", "")
	var visible []searchHit
	json.Unmarshal(rec.Body.Bytes(), &visible)
	if len(visible) != 1 || visible[0].ID != "hit" {
		t.Fatalf("archived-excluded hits = %v, want only the title hit", visible)
	}
	rec = serve(t, mux, http.MethodGet, "/sessions/search?q=needle&archived=include", "")
	json.Unmarshal(rec.Body.Bytes(), &visible)
	if len(visible) != 2 {
		t.Fatalf("archived=include hits = %v, want both", visible)
	}
}

// --- branch --------------------------------------------------------------

func TestBranchEndpointCreatesChild(t *testing.T) {
	_, st, mux, _ := newRoutesFixture(t)
	createFixtureSession(t, st, "par", "parent")

	rec := serve(t, mux, http.MethodPost, "/sessions/par/branch", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("branch status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var child core.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	if child.ID == "" || child.ID == "par" || child.ParentID != "par" {
		t.Fatalf("child = %+v, want fresh id, parent par", child)
	}

	// the summary system row lives in the child's rows
	stored, err := st.GetMessages(context.Background(), child.ID)
	if err != nil {
		t.Fatalf("get child messages: %v", err)
	}
	found := false
	for _, m := range stored {
		if sys, ok := m.(core.System); ok && strings.Contains(sys.Content, "branched from par") {
			found = true
		}
	}
	if !found {
		t.Fatalf("child rows %v, want one summary system row naming the parent", stored)
	}
	// counts exclude the summary row - the child has no conversation yet
	counts, err := st.SessionCounts(context.Background())
	if err != nil {
		t.Fatalf("session counts: %v", err)
	}
	if counts[child.ID].MessageCount != 0 {
		t.Fatalf("child message_count = %d, want 0 (summary row excluded)", counts[child.ID].MessageCount)
	}
}

func TestBranchEndpointUnknownParent(t *testing.T) {
	_, _, mux, _ := newRoutesFixture(t)
	rec := serve(t, mux, http.MethodPost, "/sessions/missing/branch", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("branch status = %d, want 404", rec.Code)
	}
}

// TestBranchChildInheritsAncestorPath: the child's ACTIVE PATH - the
// read the transcript AND the next run's context assembly use - starts
// with the parent's conversation, and the child's first own message
// chains after the ancestor leaf (cross-session pointer accepted).
func TestBranchChildInheritsAncestorPath(t *testing.T) {
	_, st, mux, _ := newRoutesFixture(t)
	createFixtureSession(t, st, "src", "")
	if err := st.AppendMessage(context.Background(), "src", core.NewUser("parent question")); err != nil {
		t.Fatalf("seed parent user: %v", err)
	}
	if err := st.AppendMessage(context.Background(), "src", core.NewAssistant("parent answer")); err != nil {
		t.Fatalf("seed parent assistant: %v", err)
	}

	rec := serve(t, mux, http.MethodPost, "/sessions/src/branch", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("branch status = %d", rec.Code)
	}
	var child core.Session
	json.Unmarshal(rec.Body.Bytes(), &child)

	msgs, err := st.GetActiveMessages(context.Background(), child.ID)
	if err != nil {
		t.Fatalf("child active messages: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("child active path = %v, want the inherited prefix", msgs)
	}
	if u, ok := msgs[0].(core.User); !ok || u.Content != "parent question" {
		t.Fatalf("child path[0] = %T %+v, want the parent user", msgs[0], msgs[0])
	}
	if a, ok := msgs[1].(core.Assistant); !ok || a.Content != "parent answer" {
		t.Fatalf("child path[1] = %T %+v, want the parent assistant", msgs[1], msgs[1])
	}
	// the summary row lands after the lineage marker; the first OWN
	// message chains onto the ancestor leaf
	if err := st.AppendMessage(context.Background(), child.ID, core.NewUser("child question")); err != nil {
		t.Fatalf("append child message: %v", err)
	}
	msgs, err = st.GetActiveMessages(context.Background(), child.ID)
	if err != nil {
		t.Fatalf("child active view after append: %v", err)
	}
	last := msgs[len(msgs)-1]
	if u, ok := last.(core.User); !ok || u.Content != "child question" {
		t.Fatalf("child path last = %T %+v, want the child user last", last, last)
	}
}

// --- export ---------------------------------------------------------------

// TestExportJSONLShape: one {"role","content"} line per active-path
// message (the /export shape), served as an attachment download.
func TestExportJSONLShape(t *testing.T) {
	_, st, mux, _ := newRoutesFixture(t)
	createFixtureSession(t, st, "exp1", "")
	if err := st.AppendMessage(context.Background(), "exp1", core.NewUser("hello there")); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := st.AppendMessage(context.Background(), "exp1", core.NewAssistant("hi, human")); err != nil {
		t.Fatalf("seed assistant: %v", err)
	}

	resp, err := http.Get(capsuleHTTP(t, mux) + "/sessions/exp1/export")
	if err != nil {
		t.Fatalf("export get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d", resp.StatusCode)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, `exp1.jsonl`) {
		t.Fatalf("content-disposition = %q, want attachment named exp1.jsonl", cd)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/jsonl") {
		t.Fatalf("content-type = %q, want application/jsonl", ct)
	}
	var lines []map[string]string
	dec := json.NewDecoder(resp.Body)
	for dec.More() {
		var line map[string]string
		if err := dec.Decode(&line); err != nil {
			t.Fatalf("decode line: %v", err)
		}
		lines = append(lines, line)
	}
	if len(lines) != 2 ||
		lines[0]["role"] != "user" || lines[0]["content"] != "hello there" ||
		lines[1]["role"] != "assistant" || lines[1]["content"] != "hi, human" {
		t.Fatalf("export lines = %v, want /export shape {role, content} per message", lines)
	}

	// 404 for a missing session
	if resp, err := http.Get(capsuleHTTP(t, mux) + "/sessions/missing/export"); err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("export missing status = %d, want 404", resp.StatusCode)
		}
	}
}

// TestExportHead: the download probe (the webui anchor, curl -I) reads
// Content-Disposition without a body.
func TestExportHead(t *testing.T) {
	_, st, mux, _ := newRoutesFixture(t)
	createFixtureSession(t, st, "exph", "")
	if err := st.AppendMessage(context.Background(), "exph", core.NewUser("head seed")); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	resp, err := http.Head(capsuleHTTP(t, mux) + "/sessions/exph/export")
	if err != nil {
		t.Fatalf("export head: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export head status = %d, want 200", resp.StatusCode)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, `exph.jsonl`) {
		t.Fatalf("head content-disposition = %q, want attachment named exph.jsonl", cd)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Fatalf("head body = %q, want empty", body)
	}
}

// --- insights -------------------------------------------------------------

// seedInsightsRows writes the known rows into the scratch store: two
// sessions, 3 user messages, 2 assistants carrying 3 tool calls (the
// shell.run / files.read pair), plus one metadata message the view must skip.
func seedInsightsRows(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", "first session"); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if err := st.AppendMessage(ctx, "s1", core.NewUser("hello world one")); err != nil {
		t.Fatalf("append s1 u1: %v", err)
	}
	if err := st.AppendMessage(ctx, "s1", core.NewUser("hello world two")); err != nil {
		t.Fatalf("append s1 u2: %v", err)
	}
	asst := core.NewAssistant("response one")
	asst.ToolCalls = []core.ToolCall{
		{ID: "tc1", Name: "shell.run", Arguments: map[string]any{"cmd": "ls"}},
		{ID: "tc2", Name: "files.read", Arguments: map[string]any{"path": "x.go"}},
	}
	if err := st.AppendMessage(ctx, "s1", asst); err != nil {
		t.Fatalf("append s1 a1: %v", err)
	}
	if err := st.CreateSession(ctx, "s2", "", "second session"); err != nil {
		t.Fatalf("create s2: %v", err)
	}
	if err := st.AppendMessage(ctx, "s2", core.NewUser("another message here")); err != nil {
		t.Fatalf("append s2 u1: %v", err)
	}
	asst2 := core.NewAssistant("response two")
	asst2.ToolCalls = []core.ToolCall{
		{ID: "tc3", Name: "shell.run", Arguments: map[string]any{"cmd": "pwd"}},
	}
	if err := st.AppendMessage(ctx, "s2", asst2); err != nil {
		t.Fatalf("append s2 a1: %v", err)
	}
	// metadata rows ride the store but the analytics skip them
	if err := st.AppendMessage(ctx, "s1", core.NewModelChange("gpt-4o")); err != nil {
		t.Fatalf("append s1 metadata: %v", err)
	}
}

func insightsGet(t *testing.T, mux *http.ServeMux, url string) insightsPayload {
	t.Helper()
	resp, err := http.Get(capsuleHTTP(t, mux) + url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status = %d, body = %q", url, resp.StatusCode, string(body))
	}
	var p insightsPayload
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return p
}

func TestInsightsParity(t *testing.T) {
	_, st, mux, _ := newRoutesFixture(t)
	seedInsightsRows(t, st)
	p := insightsGet(t, mux, "/insights?days=30")
	if p.Period != "Last 30 days" || p.Days != 30 {
		t.Fatalf("period = %q days = %d, want the 30-day window", p.Period, p.Days)
	}
	if p.Sessions != 2 || p.Messages != 5 || p.UserMessages != 3 || p.ToolCalls != 3 {
		t.Fatalf("counts = %+v, want sessions 2, messages 5 (3 convo + 2 assistant), user 3, tools 3", p)
	}
	if p.TotalTokens <= 0 {
		t.Fatalf("total tokens = %d, want > 0 (the fixture's messages carry the per-message trail)", p.TotalTokens)
	}
	if len(p.Tools) != 2 || p.Tools[0].Name != "shell.run" || p.Tools[0].Count != 2 ||
		p.Tools[1].Name != "files.read" || p.Tools[1].Count != 1 {
		t.Fatalf("tools = %+v, want shell.run x2 then files.read x1, ranked", p.Tools)
	}
	if p.Tools == nil {
		t.Fatalf("tools = null, want [] - the SPA boot dereferences the field (the null crashed the render)")
	}
	// days=0 means all time (the insights command's semantics)
	p = insightsGet(t, mux, "/insights?days=0")
	if p.Days != 0 || p.Period != "All time" {
		t.Fatalf("days=0 -> %+v, want the all-time window (the command-capitalized voice)", p)
	}
	// a negative value falls back to the default window
	p = insightsGet(t, mux, "/insights?days=-5")
	if p.Days != 30 {
		t.Fatalf("days=-5 -> %d, want the 30-day default", p.Days)
	}
}

// --- the mux patterns themselves (the mount is the proof) ------------------

func TestRoutePatternsAreMethodScoped(t *testing.T) {
	_, _, mux, _ := newRoutesFixture(t)
	// GET on a POST-only pattern: ServeMux answers 405 (the method is
	// known, the verb is not)
	rec := serve(t, mux, http.MethodGet, "/sessions/a/branch", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET branch status = %d, want 405", rec.Code)
	}
	// POST on an export GET-only pattern: same law
	rec = serve(t, mux, http.MethodPost, "/sessions/a/export", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST export status = %d, want 405", rec.Code)
	}
}
