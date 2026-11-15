package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// transcriptFixture builds the persisted history of a segmented session:
// user turn, assistant segment A (thinking + preamble + tool calls),
// tool results, assistant segment B (final answer) around tool results,
// then a second clean user/assistant exchange.
func transcriptFixture() []core.StoredMessage {
	thinking := "let me check the file"
	preamble := "running the check"
	answer := "all green"
	assistantA := core.NewAssistant(preamble)
	assistantA.Thinking = &thinking
	assistantA.ToolCalls = []core.ToolCall{
		{ID: "tc1", Name: "go_test", Arguments: map[string]any{"pkg": "./..."}},
	}
	assistantB := core.NewAssistant(answer)
	return []core.StoredMessage{
		{Message: core.NewUser("run tests"), CreatedAt: "2026-10-19T01:00:00Z"},
		{Message: assistantA, CreatedAt: "2026-10-19T01:00:01Z"},
		{Message: core.NewToolResult("PASS", "tc1", false), CreatedAt: "2026-10-19T01:00:02Z"},
		{Message: assistantB, CreatedAt: "2026-10-19T01:00:03Z"},
	}
}

func TestTranscriptItemsWireOrder(t *testing.T) {
	msgs := transcriptFixture()
	// attach realistic timing: segment A thinking+response spans, the
	// result's elapsed, segment B's response span
	// (indices into transcriptFixture)
	aa, ok := msgs[1].Message.(core.Assistant)
	if !ok {
		t.Fatalf("msgs[1] not assistant")
	}
	aa.Timing = &core.Timing{ThinkingMs: 1500, ResponseMs: 200}
	msgs[1].Message = aa
	ab, ok := msgs[3].Message.(core.Assistant)
	if !ok {
		t.Fatalf("msgs[3] not assistant")
	}
	ab.Timing = &core.Timing{ResponseMs: 500}
	msgs[3].Message = ab
	res, ok := msgs[2].Message.(core.ToolResult)
	if !ok {
		t.Fatalf("msgs[2] not tool result")
	}
	res.Timing = &core.Timing{ElapsedMs: 3000}
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
	// the response-return window rides the result beside the call's
	// elapsed (card t_e27c8347): segment B's response span
	if items[4].AnswerMs != 500 {
		t.Fatalf("tool_result answer_ms = %d, want 500 (segment B's response span)", items[4].AnswerMs)
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
	a1 := core.NewAssistant("")
	a1.ToolCalls = []core.ToolCall{{ID: "tc9", Name: "go_test"}}
	a2 := core.NewAssistant("done")
	a2.Thinking = &thinkingB
	msgs := []core.StoredMessage{
		{Message: core.NewUser("check"), CreatedAt: "t0"},
		{Message: a1, CreatedAt: "t1"},
		{Message: core.NewToolResult("ok", "tc9", false), CreatedAt: "t2"},
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
	empty := core.NewAssistant("")
	withThinking := core.NewAssistant("")
	withThinking.Thinking = &thinking
	msgs := []core.StoredMessage{
		{Message: core.NewUser(""), CreatedAt: "t0"},                       // empty user: nothing
		{Message: empty, CreatedAt: "t1"},                                  // empty assistant record: nothing
		{Message: core.NewToolResult("", "", false), CreatedAt: "t2"},      // result without a call id: nothing
		{Message: empty, CreatedAt: "t3"},                                  // empty assistant record: nothing
		{Message: core.NewSystem("sysprompt"), CreatedAt: "t4"},            // system: nothing
		{Message: core.ModelChange{Model: "glm-5.3"}, CreatedAt: "t5"},     // model switch: nothing
		{Message: core.ThinkingLevelChange{}, CreatedAt: "t6"},             // level switch: nothing
		{Message: withThinking, CreatedAt: "t7"},                           // thinking-only assistant: one item
		{Message: core.NewToolResult("out", "tcX", true), CreatedAt: "t8"}, // error result with id: kept, flagged
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
	msgs := []core.StoredMessage{{Message: core.NewUser("hi")}}
	items := buildTranscriptItems(msgs)
	if len(items) != 1 || items[0].Time == "" {
		t.Fatalf("time fallback lost: %+v", items)
	}
	if items[0].Time != msgs[0].Message.(core.User).Timestamp {
		t.Fatalf("time = %q, want the message timestamp", items[0].Time)
	}
	// and a real created_at wins over the message stamp
	msgs[0].CreatedAt = "t-store"
	items = buildTranscriptItems(msgs)
	if items[0].Time != "t-store" {
		t.Fatalf("time = %q, want the store time", items[0].Time)
	}
}

// TestTranscriptItemsAnswerWindow (card t_e27c8347): the tool_result's
// answer_ms is the response span of the answer segment the result
// unlocked - and only then. Parallel results share the one answer; a
// contentless next segment (another call) and a user turn (the run
// ended) carry no second number.
func TestTranscriptItemsAnswerWindow(t *testing.T) {
	// answer assistant with a measured response span
	answer := core.NewAssistant("here is the answer")
	answer.Timing = &core.Timing{ResponseMs: 4200}
	// a) the common shape: result -> answer assistant
	msgs := []core.StoredMessage{
		{Message: core.NewToolResult("out", "tc1", false), CreatedAt: "t0"},
		{Message: answer, CreatedAt: "t1"},
	}
	items := buildTranscriptItems(msgs)
	if len(items) != 2 || items[0].AnswerMs != 4200 {
		t.Fatalf("answer window lost: %+v", items)
	}
	// b) parallel results share the one answer
	msgs = []core.StoredMessage{
		{Message: core.NewToolResult("a", "tc1", false), CreatedAt: "t0"},
		{Message: core.NewToolResult("b", "tc2", false), CreatedAt: "t1"},
		{Message: answer, CreatedAt: "t2"},
	}
	for _, it := range buildTranscriptItems(msgs) {
		if it.Kind != "tool_result" {
			continue
		}
		if it.AnswerMs != 4200 {
			t.Fatalf("parallel result %s answer_ms = %d, want 4200", it.ToolCallID, it.AnswerMs)
		}
	}
	// c) the next segment called another tool instead of answering
	chained := core.NewAssistant("")
	chained.ToolCalls = []core.ToolCall{{ID: "tc9", Name: "go_test"}}
	msgs = []core.StoredMessage{
		{Message: core.NewToolResult("out", "tc1", false), CreatedAt: "t0"},
		{Message: chained, CreatedAt: "t1"},
		{Message: core.NewToolResult("out2", "tc9", false), CreatedAt: "t2"},
		{Message: answer, CreatedAt: "t3"},
	}
	for _, it := range buildTranscriptItems(msgs) {
		if it.Kind != "tool_result" {
			continue
		}
		if it.ToolCallID == "tc1" && it.AnswerMs != 0 {
			t.Fatalf("chained call's result carries an answer window: %+v", it)
		}
		if it.ToolCallID == "tc9" && it.AnswerMs != 4200 {
			t.Fatalf("the answered call's result lost its window: %+v", it)
		}
	}
	// d) the run ended at the result (yield, error): a user turn
	// follows - no answer to measure
	msgs = []core.StoredMessage{
		{Message: core.NewToolResult("out", "tc1", false), CreatedAt: "t0"},
		{Message: core.NewUser("next turn"), CreatedAt: "t1"},
		{Message: answer, CreatedAt: "t2"},
	}
	for _, it := range buildTranscriptItems(msgs) {
		if it.Kind == "tool_result" && it.AnswerMs != 0 {
			t.Fatalf("result before a user turn carries an answer window: %+v", it)
		}
	}
	// e) an unmeasured answer (no timing record) stays zero, and a
	// tail result with no follow-up at all stays zero too
	items = buildTranscriptItems([]core.StoredMessage{
		{Message: core.NewToolResult("out", "tc1", false), CreatedAt: "t0"},
		{Message: core.NewAssistant("no timing"), CreatedAt: "t1"},
		{Message: core.NewToolResult("tail", "tc2", false), CreatedAt: "t2"},
	})
	if items[0].AnswerMs != 0 || items[2].AnswerMs != 0 {
		t.Fatalf("unmeasured/tail answers stamped: %+v", items)
	}
}

func TestSessionTranscriptHandler(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", "Run tests"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	thinking := "check first"
	asst := core.NewAssistant("")
	asst.Thinking = &thinking
	asst.ToolCalls = []core.ToolCall{{ID: "tc1", Name: "go_test"}}
	for _, m := range []core.Message{
		core.NewUser("run tests"),
		asst,
		core.NewToolResult("PASS", "tc1", false),
		core.NewAssistant("all green"),
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
		Session core.Session `json:"session"`
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
		toolCalls: []core.ToolCall{
			{ID: "tc1", Name: "go_test", Arguments: map[string]any{"pkg": "./..."}},
		},
		results:  []core.ToolResult{core.NewToolResult("PASS", "tc1", false)},
		thinkDur: 1500 * time.Millisecond,
		respDur:  200 * time.Millisecond,
	}
	// open record: thinking streaming, one already-resulted call plus
	// one still-running (started 2s ago), no trailing chunk yet
	start := time.Now().Add(-2 * time.Second)
	open := assistantRecord{
		thinking: "and again",
		toolCalls: []core.ToolCall{
			{ID: "tc2", Name: "go_test", Arguments: map[string]any{"pkg": "./modules/ui"}},
			{ID: "tc3", Name: "shell.run"},
		},
		results:    []core.ToolResult{core.NewToolResult("OK", "tc2", false)},
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

// TestLiveTranscriptItemsCarryStreamingElapsed: the open record's
// in-flight thinking/answer rows ship their elapsed-so-far (card
// t_95385aba) - the anchor a remounted transcript item counts on from
// after a tab switch or reload, the same shape a running tool line
// carries. A reasoning phase the answer already ended ships its
// measured span instead (it will never tick again).
func TestLiveTranscriptItemsCarryStreamingElapsed(t *testing.T) {
	now := time.Now()

	// reasoning still open: 3s accumulated + a 2s open span
	items := liveTranscriptItems(nil, assistantRecord{
		thinking:  "deliberating",
		thinkDur:  3 * time.Second,
		lastThink: now.Add(-2 * time.Second),
		// a chunk stamps the span's last mark and the window boundary
		// in the same call - the accumulator's invariant
		openAt: now.Add(-2 * time.Second),
	}, now)
	if len(items) != 1 || items[0].Kind != "thinking" || !items[0].Running {
		t.Fatalf("open thinking = %+v", items)
	}
	if got := items[0].ElapsedMs; got < 4900 || got > 5100 {
		t.Fatalf("thinking elapsed-so-far = %d, want ~5000", got)
	}
	if items[0].ThinkingMs != 0 {
		t.Fatalf("a live phase must not ship a final span: %+v", items[0])
	}

	// the answer streams behind a finished reasoning phase
	items = liveTranscriptItems(nil, assistantRecord{
		thinking:  "done deliberating",
		content:   "the answer so far",
		thinkDur:  1500 * time.Millisecond,
		respDur:   1200 * time.Millisecond,
		lastResp:  now.Add(-800 * time.Millisecond),
		openAt:    now.Add(-800 * time.Millisecond),
		lastThink: now.Add(-10 * time.Second), // closed by the answer's first chunk
	}, now)
	if len(items) != 2 || items[0].Kind != "thinking" || items[1].Kind != "message" {
		t.Fatalf("open answer items = %+v", kindsOf(items))
	}
	if items[0].Running {
		t.Fatalf("thinking is over once content started: %+v", items[0])
	}
	if items[0].ThinkingMs != 1500 {
		t.Fatalf("finished thinking span = %d, want 1500", items[0].ThinkingMs)
	}
	if !items[1].Running {
		t.Fatalf("the answer streams: %+v", items[1])
	}
	if got := items[1].ElapsedMs; got < 1900 || got > 2100 {
		t.Fatalf("answer elapsed-so-far = %d, want ~2000", got)
	}
	if items[1].ResponseMs != 0 {
		t.Fatalf("a live answer must not ship a final span: %+v", items[1])
	}

	// the answer ended and its tool is running: the tool call closed
	// the phase window, so the anchor is the measured span - the tool's
	// wall clock never bills to the answer a remount restores
	items = liveTranscriptItems(nil, assistantRecord{
		content:    "answered, now calling",
		respDur:    1500 * time.Millisecond,
		lastResp:   now.Add(-20 * time.Second),
		toolCalls:  []core.ToolCall{{ID: "c1", Name: "shell.run"}},
		callStarts: map[string]time.Time{"c1": now.Add(-19 * time.Second)},
	}, now)
	if len(items) != 2 || items[0].Kind != "message" || items[1].Kind != "tool_call" {
		t.Fatalf("answer-plus-tool items = %+v", kindsOf(items))
	}
	if items[0].ElapsedMs != 1500 {
		t.Fatalf("a closed answer window anchors at its measured 1500, got %d", items[0].ElapsedMs)
	}
}

// TestPhaseElapsedSpansTheBoundary: a phase that has not produced its
// first chunk yet still bills the wait from the opening boundary (a
// turn start or the last tool result) - otherwise a restored meter
// would start at zero through the model's latency.
func TestPhaseElapsedSpansTheBoundary(t *testing.T) {
	now := time.Now()
	if got := phaseElapsed(0, time.Time{}, now.Add(-3*time.Second), now); got < 2900 || got > 3100 {
		t.Fatalf("boundary-only span = %d, want ~3000", got)
	}
	if got := phaseElapsed(0, time.Time{}, time.Time{}, now); got != 0 {
		t.Fatalf("no boundary and no chunks = %d, want 0", got)
	}
	// a tool call closed the phase's window: the span froze at its last
	// chunk - the tool's wall clock never bills to the answer, so a
	// remounted meter must not count the tool's run time into it
	if got := phaseElapsed(2*time.Second, now.Add(-10*time.Second), time.Time{}, now); got != 2000 {
		t.Fatalf("closed window = %d, want the accumulated 2000", got)
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
	if err := st.AppendMessage(ctx, "s1", core.NewUser("count to 30")); err != nil {
		t.Fatalf("append: %v", err)
	}
	run, ok := s.runs.start("s1")
	if !ok {
		t.Fatalf("start run")
	}
	// what launchRun snaps after each event: the open thinking block and
	// the running tool call
	builtAt := time.Now()
	s.runs.setTail(run, liveTranscriptItems(nil, assistantRecord{
		toolCalls:  []core.ToolCall{{ID: "tcl", Name: "shell.run"}},
		callStarts: map[string]time.Time{"tcl": time.Now().Add(-5 * time.Second)},
	}, builtAt), builtAt)

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
		t.Fatalf("the RunHub holds the slot - running must be true")
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

// TestSessionTranscriptStreamsTheAnswerElapsed (card t_95385aba): a
// mid-run GET of the transcript - the tab-switch / reload path - serves
// the open answer and thinking rows with their elapsed-so-far on the
// wire, so the remounted meters count on instead of restarting at zero.
// Same wire a running tool line has used since the reattach merge.
func TestSessionTranscriptStreamsTheAnswerElapsed(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", "Streaming"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.AppendMessage(ctx, "s1", core.NewUser("explain the loop")); err != nil {
		t.Fatalf("append: %v", err)
	}
	run, ok := s.runs.start("s1")
	if !ok {
		t.Fatalf("start run")
	}
	// the run state a remount interrupts: reasoning 3s in, the answer
	// 1.2s in behind it
	builtAt := time.Now()
	s.runs.setTail(run, liveTranscriptItems(nil, assistantRecord{
		thinking: "weighing options",
		content:  "the answer so far",
		thinkDur: 3 * time.Second,
		respDur:  1200 * time.Millisecond,
		lastResp: time.Now().Add(-800 * time.Millisecond),
		openAt:   time.Now().Add(-800 * time.Millisecond),
	}, builtAt), builtAt)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/s1/transcript", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []transcriptItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := kindsOf(body.Items), []string{"user", "thinking", "message"}; len(got) != 3 {
		t.Fatalf("items = %v, want %v", got, want)
	}
	think, answer := body.Items[1], body.Items[2]
	if think.Kind != "thinking" || think.ThinkingMs != 3000 || think.Running {
		t.Fatalf("the finished reasoning row carries its measured span: %+v", think)
	}
	if !answer.Running {
		t.Fatalf("the answer streams: %+v", answer)
	}
	if answer.ElapsedMs < 1900 || answer.ElapsedMs > 2100 {
		t.Fatalf("answer elapsed-so-far = %d, want ~2000 (1.2s accumulated + the open span)", answer.ElapsedMs)
	}
	if answer.ResponseMs != 0 {
		t.Fatalf("a live answer ships no final span: %+v", answer)
	}
}

// TestFreshenRunningCallsAgesOnlyTheCall (card t_6bad9f6f): a snapshot's
// running tool line counts on by the snapshot's age, so a reader that
// serves the snapshot anchors at the call's true elapsed. A frozen
// thinking/answer span keeps the measurement the server stopped at (a
// tool call closes the phase's window - its wall clock bills to nobody),
// and the stored snapshot is never mutated in place.
func TestFreshenRunningCallsAgesOnlyTheCall(t *testing.T) {
	snapshot := []transcriptItem{
		{Kind: "thinking", Content: "weighing", Running: true, ElapsedMs: 3000},
		{Kind: "message", Content: "so far", Running: true, ElapsedMs: 1200},
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl", Name: "shell.run"}, Running: true, ElapsedMs: 100},
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "answered", Name: "files.read"}},
	}
	aged := freshenRunningCalls(snapshot, 2*time.Second)
	if aged[0].ElapsedMs != 3000 || aged[1].ElapsedMs != 1200 {
		t.Fatalf("frozen spans aged: thinking=%d answer=%d, want 3000/1200", aged[0].ElapsedMs, aged[1].ElapsedMs)
	}
	if aged[2].ElapsedMs != 2100 {
		t.Fatalf("running call = %d, want 2100 (100ms anchor + 2s snapshot age)", aged[2].ElapsedMs)
	}
	if aged[3].Running || aged[3].ElapsedMs != 0 {
		t.Fatalf("an answered call must not age: %+v", aged[3])
	}
	if snapshot[2].ElapsedMs != 100 {
		t.Fatalf("the snapshot was mutated in place: %d, want the stored 100", snapshot[2].ElapsedMs)
	}
	// a stamp from the future (clock skew) never subtracts from the anchor
	if got := freshenRunningCalls(snapshot, -time.Second)[2].ElapsedMs; got != 100 {
		t.Fatalf("negative age = %d, want the anchor untouched", got)
	}
}

// TestSessionTranscriptAgesTheRunningCall (card t_6bad9f6f): the
// reattach tail is a SNAPSHOT, rebuilt per event - and a tool call emits
// no event until its result. A reload mid-call therefore reads a tail
// frozen at the call-open anchor (~0) and must be served the anchor aged
// by the snapshot's age, or the restored meter restarts at 0:00.
func TestSessionTranscriptAgesTheRunningCall(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", "Mid-call reload"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.AppendMessage(ctx, "s1", core.NewUser("run the suite")); err != nil {
		t.Fatalf("append: %v", err)
	}
	run, ok := s.runs.start("s1")
	if !ok {
		t.Fatalf("start run")
	}
	// the snapshot the loop built AT the call event: the call had just
	// opened, so its baked anchor is ~0 - the value the pre-fix wire
	// served for the whole call
	builtAt := time.Now().Add(-3 * time.Second)
	s.runs.setTail(run, liveTranscriptItems(nil, assistantRecord{
		toolCalls:  []core.ToolCall{{ID: "tcl", Name: "shell.run"}},
		callStarts: map[string]time.Time{"tcl": builtAt},
	}, builtAt), builtAt)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/s1/transcript", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []transcriptItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := kindsOf(body.Items), []string{"user", "tool_call"}; len(got) != 2 {
		t.Fatalf("items = %v, want %v", got, want)
	}
	tc := body.Items[1]
	if !tc.Running {
		t.Fatalf("the call still runs: %+v", tc)
	}
	if tc.ElapsedMs < 2900 || tc.ElapsedMs > 3300 {
		t.Fatalf("elapsed = %d, want ~3000 (the snapshot's age added to its call-open anchor)", tc.ElapsedMs)
	}
	// the hub keeps serving the same snapshot: the aging read never
	// rewrote it
	if tail, _, live := s.runs.tailSnapshot("s1"); !live || tail[0].ElapsedMs != 0 {
		t.Fatalf("stored tail mutated: %+v", tail)
	}
}

// TestSessionTranscriptServesOneAnchoredCallRow (card t_6bad9f6f):
// mid-call the store already carries the assistant row WITH its call
// (the loop persists the turn before the tools execute) and the live
// tail carries the same call while it runs. The wire serves the call
// ONCE with the running anchor on it - a reload must render one row,
// not two, and that row must not restart its meter at zero.
func TestSessionTranscriptServesOneAnchoredCallRow(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", "Mid-call"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.AppendMessage(ctx, "s1", core.NewUser("run it")); err != nil {
		t.Fatalf("append user: %v", err)
	}
	assistant := core.NewAssistant("")
	assistant.ToolCalls = []core.ToolCall{{ID: "tcl", Name: "shell.run"}}
	if err := st.AppendMessage(ctx, "s1", assistant); err != nil {
		t.Fatalf("append assistant: %v", err)
	}
	run, ok := s.runs.start("s1")
	if !ok {
		t.Fatalf("start run")
	}
	builtAt := time.Now().Add(-2 * time.Second)
	s.runs.setTail(run, liveTranscriptItems(nil, assistantRecord{
		toolCalls:  []core.ToolCall{{ID: "tcl", Name: "shell.run"}},
		callStarts: map[string]time.Time{"tcl": builtAt},
	}, builtAt), builtAt)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/s1/transcript", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []transcriptItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := kindsOf(body.Items), []string{"user", "tool_call"}; len(got) != 2 {
		t.Fatalf("items = %v, want %v (the tail's twin folded into its persisted row)", got, want)
	}
	tc := body.Items[1]
	if !tc.Running {
		t.Fatalf("the served call row lost its running bit: %+v", tc)
	}
	if tc.ElapsedMs < 1900 || tc.ElapsedMs > 2300 {
		t.Fatalf("elapsed = %d, want ~2000 (the snapshot's age on the call's anchor)", tc.ElapsedMs)
	}
}

// TestTranscriptItemsUserImages (scope add 2026-10-20): persisted user
// images ride the restore - thumbnails survive a reload.
func TestTranscriptItemsUserImages(t *testing.T) {
	imgs := []core.ImageContent{{MediaType: "image/png", Base64: "aGk="}}
	msgs := []core.StoredMessage{
		{Message: core.NewUserWithImages("what is this", imgs), CreatedAt: "t0"},
		{Message: core.NewUser("text only"), CreatedAt: "t1"},
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
	items = buildTranscriptItems([]core.StoredMessage{
		{Message: core.NewUserWithImages("", imgs)},
	})
	if len(items) != 1 || len(items[0].Images) != 1 {
		t.Fatalf("image-only user dropped: %+v", items)
	}
}

// ---------------------------------------------------------------------------
// The smart gate's decision record (card t_1b3e2a8b): the stamp rides
// the persisted tool result into the transcript wire (restore), and
// the RunHub accumulator's results into the live tail - the same item
// in both paths, rendered on the call's own row.
// ---------------------------------------------------------------------------

func TestTranscriptItemsCarryGateStamp(t *testing.T) {
	msgs := []core.StoredMessage{
		{Message: core.NewUser("gated, please"), CreatedAt: "2026-11-01T00:00:00Z"},
		{Message: func() core.Assistant {
			a := core.NewAssistant("let me run it")
			a.ToolCalls = []core.ToolCall{{ID: "tc1", Name: "shell.run", Arguments: map[string]any{"command": "rm x"}}}
			return a
		}(), CreatedAt: "2026-11-01T00:00:01Z"},
		{Message: core.ToolResult{
			Content:    "approval escalated, command not executed (human_card): the user refused",
			ToolCallID: "tc1", IsError: true,
			Gate: "smart gate: escalated -> denied - command skipped",
		}, CreatedAt: "2026-11-01T00:00:02Z"},
	}
	items := buildTranscriptItems(msgs)
	var res *transcriptItem
	for i := range items {
		if items[i].Kind == "tool_result" {
			res = &items[i]
		}
	}
	if res == nil {
		t.Fatal("tool_result item missing")
	}
	if res.Gate != "smart gate: escalated -> denied - command skipped" {
		t.Fatalf("transcript wire lost the gate stamp: %q", res.Gate)
	}
	// an ungated result carries no gate - the wire stays sparse
	stamped := buildTranscriptItems([]core.StoredMessage{
		{Message: core.NewToolResult("ok", "tc2", false), CreatedAt: "t"},
	})
	for _, it := range stamped {
		if it.Kind == "tool_result" && it.Gate != "" {
			t.Fatalf("ungated result stamped: %q", it.Gate)
		}
	}
}

func TestLiveTranscriptItemsCarryGateStamp(t *testing.T) {
	now := time.Now()
	open := assistantRecord{
		openAt:    now.Add(-2 * time.Second),
		toolCalls: []core.ToolCall{{ID: "tc1", Name: "shell.run", Arguments: map[string]any{"command": "rm x"}}},
		results: []core.ToolResult{{
			Content: "approval denied, command not executed", ToolCallID: "tc1", IsError: true,
			Gate: "smart gate: auto-denied",
		}},
	}
	items := liveTranscriptItems(nil, open, now)
	seen := map[string]bool{}
	for _, it := range items {
		if it.Kind == "tool_result" {
			seen[it.Gate] = true
		}
	}
	if !seen["smart gate: auto-denied"] {
		t.Fatalf("live tail lost the gate stamp: %v", items)
	}
}

func TestSessionTranscriptGateStampRoundTrip(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", "Gate check"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	asst := core.NewAssistant("gated")
	asst.ToolCalls = []core.ToolCall{{ID: "tc1", Name: "shell.run", Arguments: map[string]any{"command": "lock the temp notes wide"}}}
	res := core.NewToolResult("approval denied, command not executed", "tc1", true)
	res.Gate = "smart gate: auto-denied"
	for _, m := range []core.Message{
		core.NewUser("go on"),
		asst,
		res,
	} {
		if err := st.AppendMessage(ctx, "s1", m); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/sessions/s1/transcript", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			Kind    string `json:"kind"`
			Content string `json:"content"`
			Gate    string `json:"gate"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var gate string
	for _, it := range body.Items {
		if it.Kind == "tool_result" {
			gate = it.Gate
		}
	}
	if gate != "smart gate: auto-denied" {
		t.Fatalf("reloaded transcript lost the gate stamp: %q", gate)
	}
}
