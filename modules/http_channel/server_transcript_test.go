package http_channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// TestLiveTranscriptItemsWireOrderShapesRunState: the accumulator's
// closed segments + the open record flatten to wire items in the order
// the live stream produced them, with the running hints on the
// in-flight parts only (card t_692254f7 fix A).
func TestLiveTranscriptItemsWireOrderShapesRunState(t *testing.T) {
	// closed segment: thinking + preamble + a call the results answered
	closed := assistantRecord{
		thinking: "check first",
		content:  "running the check",
		toolCalls: []ai.ToolCall{
			{ID: "tc1", Name: "go_test", Arguments: map[string]any{"pkg": "./..."}},
		},
		results:  []ai.ToolResult{ai.NewToolResult("PASS", "tc1", false)},
		thinkDur: 1500 * time.Millisecond,
		respDur:  200 * time.Millisecond,
	}
	// open record: thinking streaming, one already-resulted call plus
	// one still-running (started 2s ago), no trailing chunk yet
	start := time.Now().Add(-2 * time.Second)
	open := assistantRecord{
		thinking: "and again",
		toolCalls: []ai.ToolCall{
			{ID: "tc2", Name: "go_test", Arguments: map[string]any{"pkg": "./modules/ui"}},
			{ID: "tc3", Name: "shell.run"},
		},
		results:    []ai.ToolResult{ai.NewToolResult("OK", "tc2", false)},
		callStarts: map[string]time.Time{"tc2": start, "tc3": start},
	}
	items := liveTranscriptItems([]assistantRecord{closed}, open, time.Now())

	want := []string{"thinking", "message", "tool_call", "tool_result",
		"thinking", "tool_call", "tool_call", "tool_result"}
	got := kindsOf(items)
	if len(got) != len(want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	for i, k := range want {
		if items[i].Kind != k {
			t.Fatalf("items[%d].kind = %q, want %q (order: %v)", i, items[i].Kind, k, got)
		}
	}
	// closed segment: complete spans measured, nothing running
	if items[0].Running || items[0].ThinkingMs != 1500 {
		t.Fatalf("closed thinking = %+v", items[0])
	}
	if items[1].Running || items[1].ResponseMs != 200 {
		t.Fatalf("closed message = %+v", items[1])
	}
	if items[2].Running {
		t.Fatalf("resulted call must not be running: %+v", items[2])
	}
	if items[3].ToolCallID != "tc1" || items[3].Content != "PASS" || items[3].Running {
		t.Fatalf("closed result = %+v", items[3])
	}
	// open record: in-flight parts carry running, completed ones do not
	if !items[4].Running || items[4].ThinkingMs != 0 {
		t.Fatalf("open thinking must stream with no final span: %+v", items[4])
	}
	// tc2 already resulted: plain call, no running flag
	if items[5].Running || items[5].ToolCall == nil || items[5].ToolCall.ID != "tc2" {
		t.Fatalf("resulted call in open record = %+v", items[5])
	}
	// tc3 still running: the hint + elapsed-since-call-start
	if !items[6].Running || items[6].ToolCall == nil || items[6].ToolCall.ID != "tc3" {
		t.Fatalf("running call = %+v", items[6])
	}
	if items[6].ElapsedMs < 1500 {
		t.Fatalf("running call elapsed = %d, want ~2000", items[6].ElapsedMs)
	}
	// the answered call's result rides as a plain result item - never
	// running (terminal), matched client-side by tool_call_id
	if items[7].Running || items[7].ToolCallID != "tc2" || items[7].Content != "OK" {
		t.Fatalf("open record result = %+v", items[7])
	}
}

// TestLiveTranscriptItemsEmptyOpenRecord: a bare open record (no chunks,
// no calls - a run sitting between turns) contributes nothing.
func TestLiveTranscriptItemsEmptyOpenRecord(t *testing.T) {
	items := liveTranscriptItems(nil, assistantRecord{}, time.Now())
	if items != nil {
		t.Fatalf("empty state must yield nil, got %v", kindsOf(items))
	}
}

// TestSessionTranscriptMergesLiveRun (fix A): persisted history + the
// run's live tail in wire order when a run holds the session's slot.
func TestSessionTranscriptMergesLiveRun(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", "Live run"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.AppendMessage(ctx, "s1", ai.NewUser("count to 30")); err != nil {
		t.Fatalf("append: %v", err)
	}
	run, ok := s.runs.start("s1")
	if !ok {
		t.Fatalf("start run")
	}
	// what launchRun snaps after each event: the open thinking block and
	// the running tool call
	s.runs.setTail(run, liveTranscriptItems(nil, assistantRecord{
		toolCalls:  []ai.ToolCall{{ID: "tcl", Name: "shell.run"}},
		callStarts: map[string]time.Time{"tcl": time.Now().Add(-5 * time.Second)},
	}, time.Now()))

	req := httptest.NewRequest(http.MethodGet, "/sessions/s1/transcript", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items   []transcriptItem `json:"items"`
		Running bool             `json:"running"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Running {
		t.Fatalf("the runHub holds the slot - running must be true")
	}
	if got, want := kindsOf(body.Items), []string{"user", "tool_call"}; len(got) != 2 {
		t.Fatalf("items = %v, want %v (persisted user, then the live call)", got, want)
	}
	if body.Items[0].Kind != "user" || body.Items[0].Content != "count to 30" {
		t.Fatalf("persisted head lost: %+v", body.Items[0])
	}
	tc := body.Items[1]
	if !tc.Running || tc.ToolCall == nil || tc.ToolCall.Name != "shell.run" {
		t.Fatalf("live tool item = %+v, want running shell.run", tc)
	}
	if tc.ElapsedMs < 4000 {
		t.Fatalf("live tool item elapsed = %d, want ~5000", tc.ElapsedMs)
	}
}

// TestTranscriptItemsUserImages (scope add 2026-10-20): persisted user
// images ride the restore - thumbnails survive a reload.
func TestTranscriptItemsUserImages(t *testing.T) {
	imgs := []ai.ImageContent{{MediaType: "image/png", Base64: "aGk="}}
	msgs := []agent.StoredMessage{
		{Message: ai.NewUserWithImages("what is this", imgs), CreatedAt: "t0"},
		{Message: ai.NewUser("text only"), CreatedAt: "t1"},
	}
	items := buildTranscriptItems(msgs)
	if len(items) != 2 {
		t.Fatalf("items = %v", kindsOf(items))
	}
	if len(items[0].Images) != 1 || items[0].Images[0].MediaType != "image/png" || items[0].Images[0].Base64 != "aGk=" {
		t.Fatalf("images lost: %+v", items[0])
	}
	if items[1].Images != nil {
		t.Fatalf("text-only user must not carry images: %+v", items[1])
	}
	// image-only user messages persist too (empty content, images set)
	items = buildTranscriptItems([]agent.StoredMessage{
		{Message: ai.NewUserWithImages("", imgs)},
	})
	if len(items) != 1 || len(items[0].Images) != 1 {
		t.Fatalf("image-only user dropped: %+v", items)
	}
}
