package http_channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// transcriptFixture builds the persisted history of a segmented session:
// user turn, assistant segment A (thinking + preamble + tool calls),
// tool results, assistant segment B (final answer) around tool results,
// then a second clean user/assistant exchange.
func transcriptFixture() []agent.StoredMessage {
	thinking := "let me check the file"
	preamble := "running the check"
	answer := "all green"
	assistantA := ai.NewAssistant(preamble)
	assistantA.Thinking = &thinking
	assistantA.ToolCalls = []ai.ToolCall{
		{ID: "tc1", Name: "go_test", Arguments: map[string]any{"pkg": "./..."}},
	}
	assistantB := ai.NewAssistant(answer)
	return []agent.StoredMessage{
		{Message: ai.NewUser("run tests"), CreatedAt: "2026-10-19T01:00:00Z"},
		{Message: assistantA, CreatedAt: "2026-10-19T01:00:01Z"},
		{Message: ai.NewToolResult("PASS", "tc1", false), CreatedAt: "2026-10-19T01:00:02Z"},
		{Message: assistantB, CreatedAt: "2026-10-19T01:00:03Z"},
	}
}

func TestTranscriptItemsWireOrder(t *testing.T) {
	msgs := transcriptFixture()
	// attach realistic timing: segment A thinking+response spans, the
	// result's elapsed, segment B's response span
	// (indices into transcriptFixture)
	aa, ok := msgs[1].Message.(ai.Assistant)
	if !ok {
		t.Fatalf("msgs[1] not assistant")
	}
	aa.Timing = &ai.Timing{ThinkingMs: 1500, ResponseMs: 200}
	msgs[1].Message = aa
	ab, ok := msgs[3].Message.(ai.Assistant)
	if !ok {
		t.Fatalf("msgs[3] not assistant")
	}
	ab.Timing = &ai.Timing{ResponseMs: 500}
	msgs[3].Message = ab
	res, ok := msgs[2].Message.(ai.ToolResult)
	if !ok {
		t.Fatalf("msgs[2] not tool result")
	}
	res.Timing = &ai.Timing{ElapsedMs: 3000}
	msgs[2].Message = res

	items := buildTranscriptItems(msgs)
	want := []string{"user", "thinking", "message", "tool_call", "tool_result", "message"}
	if len(items) != len(want) {
		t.Fatalf("len = %d (%v), want %d", len(items), kindsOf(items), len(want))
	}
	for i, k := range want {
		if items[i].Kind != k {
			t.Fatalf("items[%d].kind = %q, want %q (full order: %v)",
				i, items[i].Kind, k, kindsOf(items))
		}
	}
	// timing fields ride the right items and nothing else
	if items[1].ThinkingMs != 1500 || items[1].ResponseMs != 0 {
		t.Fatalf("thinking item timing = %+v", items[1])
	}
	if items[2].ResponseMs != 200 || items[2].ThinkingMs != 0 {
		t.Fatalf("segment A message timing = %+v", items[2])
	}
	if items[5].ResponseMs != 500 {
		t.Fatalf("segment B message timing = %+v", items[5])
	}
	if items[4].ElapsedMs != 3000 || items[4].ToolCallID != "tc1" || items[4].Content != "PASS" {
		t.Fatalf("tool_result = %+v", items[4])
	}
	// tool meta: same nested shape the live tool_call frame carries
	tc := items[3].ToolCall
	if tc == nil || tc.ID != "tc1" || tc.Name != "go_test" || tc.Arguments["pkg"] != "./..." {
		t.Fatalf("tool_call = %+v", tc)
	}
	// times come from the store, on user/message/thinking only
	for i, wantTime := range map[int]string{0: "2026-10-19T01:00:00Z", 1: "2026-10-19T01:00:01Z", 2: "2026-10-19T01:00:01Z", 5: "2026-10-19T01:00:03Z"} {
		if items[i].Time != wantTime {
			t.Fatalf("items[%d].time = %q, want %q", i, items[i].Time, wantTime)
		}
	}
	for _, i := range []int{3, 4} {
		if items[i].Time != "" {
			t.Fatalf("items[%d] (kind %s) carries time %q - tool items are elapsed-only", i, items[i].Kind, items[i].Time)
		}
	}
	// is_error rides only when true (omitempty): a clean result must not
	// leak a false flag the client would read identically, but the error
	// result below must carry it
	if items[4].IsError {
		t.Fatalf("clean result flagged is_error")
	}
}

func TestTranscriptItemsMultiTurnThinkingSplit(t *testing.T) {
	// thinking after tool results must land with the NEXT segment, not
	// merged onto the previous one (the reload-order bug class): segment
	// A [thinking? no - plain assistant with calls], results, segment B
	// thinking + answer.
	thinkingB := "now I can answer"
	a1 := ai.NewAssistant("")
	a1.ToolCalls = []ai.ToolCall{{ID: "tc9", Name: "go_test"}}
	a2 := ai.NewAssistant("done")
	a2.Thinking = &thinkingB
	msgs := []agent.StoredMessage{
		{Message: ai.NewUser("check"), CreatedAt: "t0"},
		{Message: a1, CreatedAt: "t1"},
		{Message: ai.NewToolResult("ok", "tc9", false), CreatedAt: "t2"},
		{Message: a2, CreatedAt: "t3"},
	}
	items := buildTranscriptItems(msgs)
	want := []string{"user", "tool_call", "tool_result", "thinking", "message"}
	if got := kindsOf(items); len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i, k := range want {
		if items[i].Kind != k {
			t.Fatalf("items[%d].kind = %q, want %q (order: %v)", i, items[i].Kind, k, kindsOf(items))
		}
	}
	if items[3].Content != thinkingB {
		t.Fatalf("thinking merged wrong: %q", items[3].Content)
	}
	// the empty segment-A assistant record (no content, no thinking)
	// produced nothing of its own - only its calls
	if items[1].ToolCall == nil || items[1].ToolCall.ID != "tc9" {
		t.Fatalf("tool_call lost: %+v", items[1])
	}
}

func TestTranscriptItemsSkipsEmptyAndForeign(t *testing.T) {
	thinking := "hmm"
	empty := ai.NewAssistant("")
	withThinking := ai.NewAssistant("")
	withThinking.Thinking = &thinking
	msgs := []agent.StoredMessage{
		{Message: ai.NewUser(""), CreatedAt: "t0"},                       // empty user: nothing
		{Message: empty, CreatedAt: "t1"},                                // empty assistant record: nothing
		{Message: ai.NewToolResult("", "", false), CreatedAt: "t2"},      // result without a call id: nothing
		{Message: empty, CreatedAt: "t3"},                                // empty assistant record: nothing
		{Message: ai.NewSystem("sysprompt"), CreatedAt: "t4"},            // system: nothing
		{Message: ai.ModelChange{Model: "glm-5.3"}, CreatedAt: "t5"},     // model switch: nothing
		{Message: ai.ThinkingLevelChange{}, CreatedAt: "t6"},             // level switch: nothing
		{Message: withThinking, CreatedAt: "t7"},                         // thinking-only assistant: one item
		{Message: ai.NewToolResult("out", "tcX", true), CreatedAt: "t8"}, // error result with id: kept, flagged
	}
	items := buildTranscriptItems(msgs)
	if got := kindsOf(items); len(got) != 2 || got[0] != "thinking" || got[1] != "tool_result" {
		t.Fatalf("items = %v, want [thinking tool_result]", got)
	}
	if items[0].Content != thinking || items[0].Time != "t7" {
		t.Fatalf("thinking item = %+v", items[0])
	}
	if !items[1].IsError || items[1].ToolCallID != "tcX" || items[1].Content != "out" {
		t.Fatalf("error result = %+v", items[1])
	}
	if len(buildTranscriptItems(nil)) != 0 {
		t.Fatalf("nil history must yield zero (empty, not null) items")
	}
}

func TestTranscriptItemsTimeFallback(t *testing.T) {
	// rows saved before the store recorded created_at fall back to the
	// message's own timestamp
	msgs := []agent.StoredMessage{{Message: ai.NewUser("hi")}}
	items := buildTranscriptItems(msgs)
	if len(items) != 1 || items[0].Time == "" {
		t.Fatalf("time fallback lost: %+v", items)
	}
	if items[0].Time != msgs[0].Message.(ai.User).Timestamp {
		t.Fatalf("time = %q, want the message timestamp", items[0].Time)
	}
	// and a real created_at wins over the message stamp
	msgs[0].CreatedAt = "t-store"
	items = buildTranscriptItems(msgs)
	if items[0].Time != "t-store" {
		t.Fatalf("time = %q, want the store time", items[0].Time)
	}
}

func TestSessionTranscriptHandler(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", "Run tests"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	thinking := "check first"
	asst := ai.NewAssistant("")
	asst.Thinking = &thinking
	asst.ToolCalls = []ai.ToolCall{{ID: "tc1", Name: "go_test"}}
	for _, m := range []ai.Message{
		ai.NewUser("run tests"),
		asst,
		ai.NewToolResult("PASS", "tc1", false),
		ai.NewAssistant("all green"),
	} {
		if err := st.AppendMessage(ctx, "s1", m); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/sessions/s1/transcript", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Session agent.Session `json:"session"`
		Items   []transcriptItem
		Running bool `json:"running"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Session.ID != "s1" || body.Session.Label != "Run tests" {
		t.Fatalf("session row = %+v", body.Session)
	}
	if body.Running {
		t.Fatalf("no run is live - running must be false")
	}
	want := []string{"user", "thinking", "tool_call", "tool_result", "message"}
	if got := kindsOf(body.Items); len(got) != len(want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	for i, k := range want {
		if body.Items[i].Kind != k {
			t.Fatalf("items[%d].kind = %q, want %q (order: %v)", i, body.Items[i].Kind, k, kindsOf(body.Items))
		}
	}
}

func TestSessionTranscriptHandlerGates(t *testing.T) {
	// unknown session -> 404
	s, _ := newTestServerWithStore(t)
	req := httptest.NewRequest(http.MethodGet, "/sessions/nope/transcript", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	// no store -> 501 (same gate as the other session endpoints)
	req = httptest.NewRequest(http.MethodGet, "/sessions/x/transcript", nil)
	rec = httptest.NewRecorder()
	newTestServer().Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
	// POST -> 405
	s2, st := newTestServerWithStore(t)
	if err := st.CreateSession(context.Background(), "s1", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/sessions/s1/transcript", nil)
	rec = httptest.NewRecorder()
	s2.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	s2, st2 := newTestServerWithStore(t)
	if err := st2.CreateSession(context.Background(), "s1", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	// empty history -> items renders as [] not null (the client's
	// render loop must never see null)
	req = httptest.NewRequest(http.MethodGet, "/sessions/s1/transcript", nil)
	rec = httptest.NewRecorder()
	s2.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Items []transcriptItem
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Items == nil || len(body.Items) != 0 {
		t.Fatalf("items = %v, want empty non-nil", body.Items)
	}
}

func kindsOf(items []transcriptItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Kind
	}
	return out
}