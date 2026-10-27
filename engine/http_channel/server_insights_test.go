package http_channel

// server_insights_test.go: the /insights parity gates (card
// t_e1e61680). The endpoint must return the same numbers the TUI's
// /insights command prints for the same window, so every parity test
// takes its reference from FormatInsights - the exact text the TUI
// transcript shows - never by re-deriving from the store.
//
// Route note: the mux mount line lives in server.go, which is owned
// by card t_d7a7beaa while it runs (same-file ruling, recorded on the
// card). These tests serve s.handleInsights through a real listener
// (httptest.NewServer), exercising the real handler end-to-end over
// HTTP without touching server.go; the review pass lands the mount
// line and a Handler()-level test is a natural follow-up there.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/store"
)

// seedInsightsRows writes the known rows into the scratch store: two
// sessions, 3 user messages, 2 assistants carrying 3 tool calls
// (shell.run x2, files.read x1), plus one metadata message the
// command must skip.
func seedInsightsRows(t *testing.T, st *store.Store) {
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
	if err := st.AppendMessage(ctx, "s1", core.NewModelChange("gpt-4o")); err != nil {
		t.Fatalf("append s1 metadata: %v", err)
	}
}

// insightsGet serves the handler through a one-route mux - the same
// path shape the serve-time mux uses - and decodes the payload. The
// request rides a real listener (httptest.NewServer), so it is a
// socket-level round-trip, not a recorder-only one.
func insightsGet(t *testing.T, s *Server, url string) insightsPayload {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(s.handleInsights))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + url)
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

// tuiNumbers parses the exact numbers the TUI's /insights command
// prints out of its transcript text.
func tuiNumbers(t *testing.T, st *store.Store, args string) (map[string]int, map[string]int) {
	t.Helper()
	res, err := store.HandleInsightsCommand(context.Background(), st, args)
	if err != nil {
		t.Fatalf("HandleInsightsCommand(%q): %v", args, err)
	}
	num := func(label string) int {
		re := regexp.MustCompile(label + `\s+([0-9,]+)`)
		m := re.FindStringSubmatch(res.Text)
		if m == nil {
			t.Fatalf("TUI insights text missing %q:\n%s", label, res.Text)
		}
		n, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
		if err != nil {
			t.Fatalf("parse %q: %v", m[1], err)
		}
		return n
	}
	overview := map[string]int{
		"sessions":      num(`Sessions:`),
		"messages":      num(`Messages:`),
		"user_messages": num(`User messages:`),
		"tool_calls":    num(`Tool calls:`),
		"total_tokens":  num(`Tokens \(est\.\):`),
	}
	// tool breakdown lines: "<name>  <count>  <pct>%" under Top Tools
	tools := map[string]int{}
	for _, line := range strings.Split(res.Text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 3 && strings.HasSuffix(fields[2], "%") {
			if c, err := strconv.Atoi(fields[1]); err == nil {
				tools[fields[0]] = c
			}
		}
	}
	return overview, tools
}

// TestInsightsParityWithTUI: endpoint numbers == TUI transcript text
// for the same window (default 30, explicit days=7, all time).
func TestInsightsParityWithTUI(t *testing.T) {
	s, st := newTestServerWithStore(t)
	seedInsightsRows(t, st)

	cases := []struct {
		name string
		url  string
		args string
	}{
		{"default window", "/insights", ""},
		{"explicit days=7", "/insights?days=7", "7"},
		{"all time", "/insights?days=0", "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := insightsGet(t, s, tc.url)
			overview, tools := tuiNumbers(t, st, tc.args)
			endpoint := map[string]int{
				"sessions":      payload.Sessions,
				"messages":      payload.Messages,
				"user_messages": payload.UserMessages,
				"tool_calls":    payload.ToolCalls,
				"total_tokens":  payload.TotalTokens,
			}
			for label, want := range overview {
				if got := endpoint[label]; got != want {
					t.Errorf("%s: endpoint = %d, TUI prints %d", label, got, want)
				}
			}
			if len(payload.Tools) != len(tools) {
				t.Errorf("tool rows: endpoint %v, TUI prints %v", payload.Tools, tools)
			}
			for _, row := range payload.Tools {
				if tools[row.Name] != row.Count {
					t.Errorf("tool %s: endpoint = %d, TUI prints %d", row.Name, row.Count, tools[row.Name])
				}
			}
		})
	}
}

// seedOneUserTurn writes the minimal realistic row set: one session
// holding exactly one persisted user turn - no assistant reply, no
// tool calls. A live session is in exactly this shape on disk while
// its first turn is still running.
func seedOneUserTurn(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	if err := st.CreateSession(ctx, "solo", "", "only turn"); err != nil {
		t.Fatalf("create solo: %v", err)
	}
	if err := st.AppendMessage(ctx, "solo", core.NewUser("first and only user turn")); err != nil {
		t.Fatalf("append solo u1: %v", err)
	}
}

// TestInsightsSingleUserTurnParity: the Overview over the minimal
// non-empty store - one session, one persisted user turn, zero tool
// calls. The endpoint must agree with the TUI here too, the TUI must
// print the full report (not the "[no sessions...]" special case), and
// a window with real data but no tool calls must keep Tools a JSON
// array (t_92f7274a's crash class: null broke the webui's boot).
func TestInsightsSingleUserTurnParity(t *testing.T) {
	s, st := newTestServerWithStore(t)
	seedOneUserTurn(t, st)

	payload := insightsGet(t, s, "/insights")
	overview, tools := tuiNumbers(t, st, "")
	endpoint := map[string]int{
		"sessions":      payload.Sessions,
		"messages":      payload.Messages,
		"user_messages": payload.UserMessages,
		"tool_calls":    payload.ToolCalls,
		"total_tokens":  payload.TotalTokens,
	}
	for label, want := range overview {
		if got := endpoint[label]; got != want {
			t.Errorf("%s: endpoint = %d, TUI prints %d", label, got, want)
		}
	}

	// The Overview, pinned: one session, the turn is the only
	// message, no tool calls, 24 chars / 4 = 6 est. tokens, avg 1.0.
	if payload.Sessions != 1 || payload.Messages != 1 || payload.UserMessages != 1 {
		t.Errorf("counts = sessions %d, messages %d, user_messages %d, want 1/1/1",
			payload.Sessions, payload.Messages, payload.UserMessages)
	}
	if payload.ToolCalls != 0 {
		t.Errorf("tool_calls = %d, want 0", payload.ToolCalls)
	}
	if payload.TotalTokens != 6 {
		t.Errorf("total_tokens = %d, want 6 (24 chars / 4 per token)", payload.TotalTokens)
	}
	if payload.AvgSessionMsgs != 1.0 {
		t.Errorf("avg_session_msgs = %f, want 1.0", payload.AvgSessionMsgs)
	}

	// The TUI transcript shows the full report for this window and
	// its Avg msgs/session line agrees with the wire.
	res, err := store.HandleInsightsCommand(context.Background(), st, "")
	if err != nil {
		t.Fatalf("HandleInsightsCommand(): %v", err)
	}
	if strings.Contains(res.Text, "[no sessions") {
		t.Errorf("TUI prints the empty-store special case:\n%s", res.Text)
	}
	m := regexp.MustCompile(`Avg msgs/session:\s+([0-9.]+)`).FindStringSubmatch(res.Text)
	if m == nil {
		t.Fatalf("TUI insights text missing the avg line:\n%s", res.Text)
	}
	avg, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("parse TUI avg %q: %v", m[1], err)
	}
	if avg != payload.AvgSessionMsgs {
		t.Errorf("avg: endpoint = %f, TUI prints %s", payload.AvgSessionMsgs, m[1])
	}

	// Tools stays an array on the wire: nil after decode means the
	// JSON was null, and the SPA dereferences tools on every boot.
	if payload.Tools == nil || len(payload.Tools) != 0 {
		t.Errorf("tools = %v, want non-nil empty array", payload.Tools)
	}
	if len(tools) != 0 {
		t.Errorf("TUI prints tool rows %v, want none", tools)
	}
}

// TestInsightsDefaultsAndParams: default 30; honor day>=0 (period
// label tracks the TUI's shape); junk and negatives fall back to 30.
func TestInsightsDefaultsAndParams(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	empty, _ := newTestServerWithStore(t)

	if p := insightsGet(t, s, "/insights"); p.Days != 30 || p.Period != "Last 30 days" {
		t.Errorf("default: days=%d period=%q, want 30 / Last 30 days", p.Days, p.Period)
	}
	if p := insightsGet(t, s, "/insights?days=0"); p.Days != 0 || p.Period != "All time" || p.PeriodStart != "" {
		t.Errorf("days=0: days=%d period=%q start=%q, want 0 / All time / empty", p.Days, p.Period, p.PeriodStart)
	}
	if p := insightsGet(t, s, "/insights?days=junk"); p.Days != 30 {
		t.Errorf("junk days: days=%d, want fallback 30", p.Days)
	}
	if p := insightsGet(t, s, "/insights?days=-5"); p.Days != 30 {
		t.Errorf("negative days: days=%d, want fallback 30", p.Days)
	}
	// an empty store answers with an honest zeroed payload
	p := insightsGet(t, empty, "/insights")
	if p.Sessions != 0 || p.Messages != 0 || len(p.Tools) != 0 || p.TotalTokens != 0 {
		t.Errorf("empty window: %+v, want all-zero counts, no tool rows", p)
	}
}

// TestInsightsMethodAndNoStore: GET-only; a storeless server says so.
func TestInsightsMethodAndNoStore(t *testing.T) {
	s := newTestServer() // no store wired
	mux := http.NewServeMux()
	mux.HandleFunc("/insights", s.handleInsights)

	req := httptest.NewRequest(http.MethodGet, "/insights", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("no store: status = %d, want 501", rec.Code)
	}

	s2, _ := newTestServerWithStore(t)
	mux2 := http.NewServeMux()
	mux2.HandleFunc("/insights", s2.handleInsights)
	rec2 := httptest.NewRecorder()
	mux2.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/insights", nil))
	if rec2.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: status = %d, want 405", rec2.Code)
	}
}
