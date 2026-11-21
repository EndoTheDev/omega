package api

// The chat panel's tests: the view- model vocabulary's mapping, the mount.

import (
	"github.com/EndoTheDev/omega/agent"
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	httptest2 "net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/EndoTheDev/omega/llm"
)

// chatRecorder collects the deltas a capsule pushes through its registered canvas.
type chatRecorder struct {
	ch   chan core.Panel
	done chan []core.Panel
	once sync.Once
}

func newChatRecorder() *chatRecorder {
	r := &chatRecorder{ch: make(chan core.Panel, 128), done: make(chan []core.Panel, 1)}
	go func() {
		var panels []core.Panel
		for p := range r.ch {
			panels = append(panels, p)
		}
		r.done <- panels
	}()
	return r
}

// closeDeltas closes the collection point.
func (r *chatRecorder) closeDeltas() { r.once.Do(func() { close(r.ch) }) }

// all returns every recorded delta.
func (r *chatRecorder) all() []core.Panel { return <-r.done }

// waitRunQuiescent spins until the session's run slot is free.
func waitRunQuiescent(t *testing.T, s *Server, id string) {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for time.Now().Before(deadline) {
		if !s.runs.active(id) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run on %q never finished", id)
}

// chatPanelPayload decodes one panel's payload verbatim.
func chatPanelPayload(t *testing.T, p core.Panel) core.ChatPayload {
	t.Helper()
	b, err := json.Marshal(p.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var payload core.ChatPayload
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

func chatRowRoles(rows []core.ChatRow) []string {
	roles := make([]string, len(rows))
	for i, r := range rows {
		roles[i] = r.Role
	}
	return roles
}

// TestChatRowsMapTheTranscriptVocabulary: the view-model row map is one row.
func TestChatRowsMapTheTranscriptVocabulary(t *testing.T) {
	rows := chatRows([]transcriptItem{
		{Kind: "user", ID: "u1", Content: "hi", Time: "2026-11-15T09:00:00Z", Images: []core.ImageContent{{MediaType: "image/png", Base64: "aGk="}}},
		{Kind: "thinking", ID: "a1", Content: "hmm", Running: true, ElapsedMs: 2600},
		{Kind: "message", ID: "a2", Content: "answer", ResponseMs: 1800, Time: "2026-11-15T09:00:51Z"},
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl", Name: "shell.run"}, Running: true, ElapsedMs: 5},
		{Kind: "tool_result", ID: "r1", ToolCallID: "tcl", Content: "out", IsError: true, ElapsedMs: 7, AnswerMs: 9},
	})
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rows))
	}
	if rows[0].Role != core.ChatRoleUser || rows[0].Status != core.ChatStatusComplete || rows[0].ID != "u1" {
		t.Fatalf("user row = %+v", rows[0])
	}
	if rows[0].Blocks[0].Type != core.ChatBlockText || rows[0].Blocks[0].Content != "hi" {
		t.Fatalf("user block = %+v", rows[0].Blocks[0])
	}
	if len(rows[0].Blocks[0].Images) != 1 {
		t.Fatalf("user images lost: %+v", rows[0].Blocks[0])
	}
	if rows[1].Role != core.ChatRoleAssistant || rows[1].Blocks[0].Type != core.ChatBlockThinking || rows[1].Status != core.ChatStatusStreaming {
		t.Fatalf("thinking row = %+v", rows[1])
	}
	if !rows[1].Blocks[0].Running || rows[1].Blocks[0].ElapsedMs != 2600 {
		t.Fatalf("thinking block anchor = %+v", rows[1].Blocks[0])
	}
	if rows[2].Status != core.ChatStatusComplete {
		t.Fatalf("message status = %+v", rows[2])
	}
	if rows[2].Blocks[0].ResponseMs != 1800 || rows[2].Blocks[0].Running {
		t.Fatalf("message block span = %+v", rows[2].Blocks[0])
	}
	if rows[3].Role != core.ChatRoleAssistant || rows[3].Blocks[0].Type != core.ChatBlockToolCall || rows[3].Blocks[0].ToolCall == nil || rows[3].Blocks[0].ToolCall.Name != "shell.run" {
		t.Fatalf("tool_call row = %+v", rows[3])
	}
	if rows[4].Role != core.ChatRoleTool || rows[4].Blocks[0].ToolCallID != "tcl" || !rows[4].Blocks[0].IsError {
		t.Fatalf("tool_result row = %+v", rows[4])
	}
	// the row's wall clock rides it: the answer row's turn-total badge reads the turn's
	// two stamps, so the live canvas must serve the same created_at the transcript wire
	// does
	if rows[0].Time != "2026-11-15T09:00:00Z" || rows[2].Time != "2026-11-15T09:00:51Z" {
		t.Fatalf("row stamps = %q / %q, want the transcript wire's times", rows[0].Time, rows[2].Time)
	}
	if rows[4].Blocks[0].AnswerMs != 9 {
		t.Fatalf("tool_result block answer_ms = %d, want 9", rows[4].Blocks[0].AnswerMs)
	}
}

// TestComposeItemsDropsPersistedTwins: the open record can carry items the loop already
// persisted (it empties only at the next segment split) - they duplicate the persisted
// rows and the compose drops them, while genuinely new stream content flows.
func TestComposeItemsDropsPersistedTwins(t *testing.T) {
	persisted := []transcriptItem{
		{Kind: "user", ID: "u1", Content: "hi"},
		{Kind: "message", ID: "a1", Content: "hello"},
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl", Name: "shell.run"}},
	}
	composed := composeItems(persisted, []transcriptItem{
		// already persisted twins.
		{Kind: "message", Content: "hello"},
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl", Name: "shell.run"}},
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl2", Name: "shell.run"}, Running: true},
		{Kind: "message", Content: "hel"},
	})
	kinds := make([]string, len(composed))
	for i, it := range composed {
		kinds[i] = it.Kind
	}
	if strings.Join(kinds, ",") != "user,message,tool_call,tool_call,message" {
		t.Fatalf("composed = %v", kinds)
	}
}

// TestComposeItemsAdoptsTheRunningAnchor: the loop persists the assistant row.
func TestComposeItemsAdoptsTheRunningAnchor(t *testing.T) {
	persisted := []transcriptItem{
		{Kind: "user", ID: "u1", Content: "run it"},
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl", Name: "shell.run"}},
	}
	composed := composeItems(persisted, []transcriptItem{
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl", Name: "shell.run"}, Running: true, ElapsedMs: 4200, StartedAt: "2026-11-15T10:00:00Z"},
	})
	if len(composed) != 2 {
		t.Fatalf("composed = %d items, want the twin folded into its persisted row", len(composed))
	}
	call := composed[1]
	if !call.Running || call.ElapsedMs != 4200 {
		t.Fatalf("persisted call row = %+v, want running with the 4200ms anchor", call)
	}
	// the start stamp rides the same adoption: the row the renderer
	// keeps is the one that must carry the clock (round 3 - a duration
	// goes stale in a cached payload, the stamp does not)
	if call.StartedAt != "2026-11-15T10:00:00Z" {
		t.Fatalf("persisted call row started_at = %q, want the twin's stamp", call.StartedAt)
	}
	composed = composeItems(persisted, []transcriptItem{
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl", Name: "shell.run"}},
	})
	if composed[1].Running || composed[1].ElapsedMs != 0 || composed[1].StartedAt != "" {
		t.Fatalf("a settled twin must not move the row: %+v", composed[1])
	}
}

// TestChatPanelSnapshotDeclaresNewestSession: the load-time catch-up names.
func TestChatPanelSnapshotDeclaresNewestSession(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "old", "", "Old"); err != nil {
		t.Fatalf("create: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := st.CreateSession(ctx, "new", "", "New"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.AppendMessage(ctx, "new", core.NewUser("hi")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := st.AppendMessage(ctx, "new", core.NewAssistant("hello")); err != nil {
		t.Fatalf("append: %v", err)
	}
	panels := s.chat.Panels()
	if len(panels) != 1 {
		t.Fatalf("panels = %d, want 1", len(panels))
	}
	p := panels[0]
	if p.ID != ChatPanelID || p.Title != "Chat" || p.Kind != core.PanelKindChat {
		t.Fatalf("panel = %+v", p)
	}
	ids := map[string]bool{}
	for _, a := range p.Actions {
		ids[a.ID] = true
		if a.Module != ChatModuleNamespace {
			t.Fatalf("action %q module = %q", a.ID, a.Module)
		}
	}
	if !ids[ChatActionStop] || !ids[ChatActionRetry] {
		t.Fatalf("actions missing: %+v", p.Actions)
	}
	payload := chatPanelPayload(t, p)
	if payload.Session != "new" || payload.Running {
		t.Fatalf("payload = %+v", payload)
	}
	if got := chatRowRoles(payload.Rows); strings.Join(got, ",") != "user,assistant" {
		t.Fatalf("rows = %v", got)
	}
	if payload.Rows[1].Blocks[0].Content != "hello" {
		t.Fatalf("assistant content = %q", payload.Rows[1].Blocks[0].Content)
	}
}

// TestChatPanelDeltasRunTheBatchLaw: token chunks never flush on their own when
// batching.
// batch=1h pins the law: exactly the structural flushes plus the exit one, no token
// deltas in between.
func TestChatPanelDeltasRunTheBatchLaw(t *testing.T) {
	prov := newMockProvider("mock", scripted(
		core.ResponseChunk{Type: "response_chunk", Content: "hel"},
		core.ResponseChunk{Type: "response_chunk", Content: "lo"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	))
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil, nil)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.store = st
	s.chat.batch = time.Hour // no cadence ticks: structural flushes only
	recorder := newChatRecorder()
	s.chat.SetPanelUpdate(func(p core.Panel) error {
		recorder.ch <- p
		return nil
	})

	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	sessionID := postChatGetSessionID(t, ts)
	waitRunQuiescent(t, s, sessionID)
	recorder.closeDeltas()

	panels := recorder.all()
	// begin, turn_start, stream_end, assistant_message, exit.
	if len(panels) != 5 {
		for i, p := range panels {
			b, _ := json.Marshal(chatPanelPayload(t, p))
			t.Logf("delta %d: running=%v rows=%s", i, chatPanelPayload(t, p).Running, string(b))
		}
		t.Fatalf("deltas = %d, want 5", len(panels))
	}
	for _, p := range panels {
		if p.Kind != core.PanelKindChat || p.ID != ChatPanelID || p.Title != "Chat" {
			t.Fatalf("delta panel = %+v", p)
		}
	}
	first := chatPanelPayload(t, panels[0])
	if !first.Running {
		t.Fatalf("first delta running = %+v", first)
	}
	last := chatPanelPayload(t, panels[len(panels)-1])
	if last.Running || last.Session != sessionID {
		t.Fatalf("final delta = %+v", last)
	}
	if got := chatRowRoles(last.Rows); strings.Join(got, ",") != "user,assistant" {
		t.Fatalf("final rows = %v", got)
	}
	if last.Rows[1].Status != core.ChatStatusComplete || last.Rows[1].Blocks[0].Content != "hello" {
		t.Fatalf("final assistant row = %+v", last.Rows[1])
	}
	for _, row := range last.Rows {
		if row.Status == core.ChatStatusStreaming {
			t.Fatalf("exit flush still streams: %+v", last)
		}
	}
}

// TestChatPanelCadenceFlushesTokenBatches: with the cadence ticking, a token-dirty.
func TestChatPanelCadenceFlushesTokenBatches(t *testing.T) {
	s := newTestServer() // no store: rows compose empty, the cadence still rides
	s.chat.batch = 20 * time.Millisecond
	var mu sync.Mutex
	pushes := 0
	s.chat.SetPanelUpdate(func(p core.Panel) error {
		mu.Lock()
		pushes++
		mu.Unlock()
		return nil
	})
	s.chat.beginRun("sess-panel")
	mu.Lock()
	afterBegin := pushes
	mu.Unlock()
	s.chat.runEvent("sess-panel", core.StreamEvent{Event: core.ResponseChunk{Type: "response_chunk", Content: "tok"}})
	s.chat.runEvent("sess-panel", core.StreamEvent{Event: core.ResponseChunk{Type: "response_chunk", Content: "tok"}})
	deadline := time.Now().Add(testWaitTimeout)
	for {
		mu.Lock()
		now := pushes
		mu.Unlock()
		if now > afterBegin {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cadence never flushed: pushes stuck at %d", pushes)
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.chat.endRun("sess-panel")
}

// TestChatPanelRunningCallRepushesFreshElapsed: a tool run emits NO event between the
// call opening and its result, so the token batch never re-arms and only the batcher
// can keep the panel's running row honest. The row must re-push at the cadence carrying
// the call's true elapsed-so-far (the snapshot's anchor plus the snapshot's age) - that
// anchor is what a tab-switch remount counts on from, and without it the remount
// restarted the visible meter at 0:00.
func TestChatPanelRunningCallRepushesFreshElapsed(t *testing.T) {
	s, st := newTestServerWithStore(t)
	if err := st.CreateSession(context.Background(), "sess-tool", "", "Tool run"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	s.chat.batch = 20 * time.Millisecond
	var mu sync.Mutex
	var panels []core.Panel
	s.chat.SetPanelUpdate(func(p core.Panel) error {
		mu.Lock()
		panels = append(panels, p)
		mu.Unlock()
		return nil
	})
	run, ok := s.runs.start("sess-tool")
	if !ok {
		t.Fatalf("start run")
	}
	s.chat.beginRun("sess-tool")
	builtAt := time.Now().Add(-2 * time.Second)
	s.runs.setOpenTail(run, liveTranscriptItems(nil, assistantRecord{
		toolCalls:  []core.ToolCall{{ID: "tcl", Name: "shell.run"}},
		callStarts: map[string]time.Time{"tcl": builtAt},
	}, builtAt), builtAt)

	toolElapsed := func(p core.Panel) int64 {
		payload := chatPanelPayload(t, p)
		for _, row := range payload.Rows {
			for _, b := range row.Blocks {
				if b.Type == core.ChatBlockToolCall {
					return b.ElapsedMs
				}
			}
		}
		return -1
	}
	// NO run event: the batcher alone must re-push, and each push must carry the aged.
	deadline := time.Now().Add(testWaitTimeout)
	seen := int64(-1) // the newest push's call anchor (-1 = no row)
	for time.Now().Before(deadline) {
		mu.Lock()
		last, n := lastPanel(panels)
		mu.Unlock()
		if n > 0 {
			seen = toolElapsed(last)
			if seen >= 1900 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if seen < 1900 {
		t.Fatalf("the panel never re-pushed a fresh call anchor while the call ran: newest elapsed = %d, want >= 1900", seen)
	}
	mu.Lock()
	last, _ := lastPanel(panels)
	mu.Unlock()
	stamp := toolStartStamp(t, last)
	if got, err := time.Parse(time.RFC3339Nano, stamp); err != nil || got.After(time.Now()) || got.Before(builtAt.Add(-time.Second)) {
		t.Fatalf("running call started_at = %q (parse err %v), want the call's open clock ~%v", stamp, err, builtAt)
	}
	s.chat.endRun("sess-tool")
	if open, _, live := s.runs.openSnapshot("sess-tool"); live && len(open) > 0 && open[0].ElapsedMs != 0 {
		t.Fatalf("the stored snapshot was mutated in place: %d, want the call-open 0", open[0].ElapsedMs)
	}
}

// lastPanel returns the newest recorded panel and the count.
func lastPanel(panels []core.Panel) (core.Panel, int) {
	if len(panels) == 0 {
		return core.Panel{}, 0
	}
	return panels[len(panels)-1], len(panels)
}

// toolStartStamp reads the single tool_call block's started_at out of a recorded panel.
func toolStartStamp(t *testing.T, p core.Panel) string {
	t.Helper()
	return toolCallBlock(t, chatPanelPayload(t, p)).StartedAt
}

// slowCallLoop mirrors the real loop around ONE tool call.
type slowCallLoop struct{}

func (slowCallLoop) Run(ctx context.Context, opts agent.LoopOptions) error {
	sink := agent.RunTurnSink(ctx)
	persist := func(msg core.Message) {
		if sink != nil {
			_ = sink.AppendTurnMessage(context.WithoutCancel(ctx), msg)
		}
	}
	opts.Events <- core.AgentStart{Type: "agent_start"}
	opts.Events <- core.TurnStart{Type: "turn_start", Turn: 1}
	call := core.ToolCall{ID: "c1", Name: "slow"}
	opts.Events <- core.StreamEvent{Event: core.ToolCallEvent{Type: "tool_call", ToolCall: call}}
	opts.Events <- core.StreamEvent{Event: core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"}}
	assistant := core.NewAssistant("")
	assistant.ToolCalls = []core.ToolCall{call}
	persist(assistant)
	opts.Events <- core.AssistantMessageEvent{Type: "assistant_message", Message: assistant}
	result, err := opts.Tools["slow"].Run(ctx, call.Arguments)
	if err != nil {
		opts.Events <- core.ToolResultEvent{Type: "tool_result", Message: core.NewToolResult(err.Error(), call.ID, true)}
	} else {
		opts.Events <- core.ToolResultEvent{Type: "tool_result", Message: core.NewToolResult(result, call.ID, false)}
	}
	answer := core.NewAssistant("done")
	opts.Events <- core.TurnEnd{Type: "turn_end", Turn: 1, ToolCalls: 1}
	opts.Events <- core.AgentEnd{Type: "agent_end", Turns: 1, FinishReason: "stop", Message: answer}
	return nil
}

// TestChatPanelToolRowStaysFreshThroughTheCall: the end-to-end shape of the live repro.
// The payload must keep re-pushing during the call carrying the row's true
// elapsed-so-far; before the fix the canvas held the call-open anchor (~0) for the
// whole call and the remounted meter restarted at 0:00.
func TestChatPanelToolRowStaysFreshThroughTheCall(t *testing.T) {
	a := agent.NewAgent(newMockProvider("mock", scripted()), nil, 0)
	a.SetLoopProvider(slowCallLoop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	entered := make(chan struct{})
	release := make(chan struct{})
	s := NewServer(a, map[string]core.Tool{
		"slow": {
			Description: "blocks until the test releases it",
			Run: func(context.Context, map[string]any) (string, error) {
				close(entered)
				<-release
				return "done", nil
			},
		},
	}, st, nil, nil, nil)
	s.chat.batch = 20 * time.Millisecond
	if err := s.newCanvas(&core.Context{PanelProviders: []core.PanelProvider{s.chat}}); err != nil {
		t.Fatalf("newCanvas: %v", err)
	}
	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	createSession(t, ts, "t1")
	muteTitles(t, st, "t1")

	resp, cancel, _ := postChatStream(t, ts, "t1")
	defer cancel()
	defer resp.Body.Close()
	select {
	case <-entered:
	case <-time.After(testWaitTimeout):
		t.Fatal("the run never reached its tool call")
	}

	toolElapsed := func() int64 {
		for _, p := range s.canvas.Snapshot() {
			if p.Kind != core.PanelKindChat {
				continue
			}
			payload := chatPanelPayload(t, p)
			for _, row := range payload.Rows {
				for _, b := range row.Blocks {
					if b.Type == core.ChatBlockToolCall && b.Running {
						return b.ElapsedMs
					}
				}
			}
		}
		return -1
	}
	waitFor(t, "the running tool row on the canvas", func() bool { return toolElapsed() >= 0 })
	first := toolElapsed()
	waitFor(t, "the panel to re-push a grown call anchor", func() bool { return toolElapsed() > first })

	close(release)
	waitRunQuiescent(t, s, "t1")
}

// TestChatPanelStaleRunNeverPaints: a run whose session lost focus.
func TestChatPanelStaleRunNeverPaints(t *testing.T) {
	s := newTestServer()
	s.chat.batch = 0 // flush per token event
	var mu sync.Mutex
	pushes := 0
	s.chat.SetPanelUpdate(func(p core.Panel) error {
		mu.Lock()
		pushes++
		mu.Unlock()
		return nil
	})
	s.chat.beginRun("sess-first")
	s.chat.beginRun("sess-second")
	mu.Lock()
	before := pushes
	mu.Unlock()
	s.chat.runEvent("sess-first", core.StreamEvent{Event: core.ResponseChunk{Type: "response_chunk", Content: "stale"}})
	s.chat.endRun("sess-first")
	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	after := pushes
	mu.Unlock()
	if after != before {
		t.Fatalf("stale run flushed from %d to %d pushes", before, after)
	}
	s.chat.runEvent("sess-second", core.ToolResultEvent{})
	mu.Lock()
	if pushes == after {
		t.Fatal("focus session stopped painting")
	}
	mu.Unlock()
	s.chat.endRun("sess-second")
}

// TestChatPanelStaleBatcherNeverRepaints: the running-call dirty arm reads the RunHub,
// so it must also be gated on the panel's lifecycle. A session that lost the panel
// while mid-call (a newer run took it over) keeps its batcher until endRun, and the
// single ChatPanelID has one owner - the stale session must never paint it again.
func TestChatPanelStaleBatcherNeverRepaints(t *testing.T) {
	s, st := newTestServerWithStore(t)
	for _, id := range []string{"sess-stale", "sess-focus"} {
		if err := st.CreateSession(context.Background(), id, "", "Panel "+id); err != nil {
			t.Fatalf("create session %q: %v", id, err)
		}
	}
	s.chat.batch = 20 * time.Millisecond // a real batcher per session
	var mu sync.Mutex
	var panels []core.Panel
	s.chat.SetPanelUpdate(func(p core.Panel) error {
		mu.Lock()
		panels = append(panels, p)
		mu.Unlock()
		return nil
	})
	run, ok := s.runs.start("sess-stale")
	if !ok {
		t.Fatalf("start run")
	}
	s.chat.beginRun("sess-stale")
	s.runs.setOpenTail(run, []transcriptItem{
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl-stale", Name: "shell.run"}, Running: true, ElapsedMs: 5},
	}, time.Now())
	// a second session takes the panel over (beginRun moves the focus)
	s.chat.beginRun("sess-focus")
	mu.Lock()
	mark := len(panels)
	mu.Unlock()
	if mark == 0 {
		t.Fatalf("no panel pushes recorded - the harness never started")
	}
	last, _ := lastPanel(panels)
	if payload := chatPanelPayload(t, last); payload.Session != "sess-focus" {
		t.Fatalf("the focus change did not take the panel: last payload = %+v", payload)
	}
	time.Sleep(6 * s.chat.batch)
	mu.Lock()
	stale := panels[mark:]
	mu.Unlock()
	if len(stale) != 0 {
		t.Fatalf("a stale session repainted the panel %d time(s): first = %+v", len(stale), stale[0].Payload)
	}
	s.chat.endRun("sess-stale")
	s.chat.endRun("sess-focus")
}

// TestChatPanelTickAfterTheExitFlushStaysQuiet: endRun runs before the run's finish. A
// batcher tick that got past its select before endRun must stay quiet in that window: a
// running=true push after the exit flush is the failure the paint lock exists to
// prevent.
func TestChatPanelTickAfterTheExitFlushStaysQuiet(t *testing.T) {
	s, st := newTestServerWithStore(t)
	if err := st.CreateSession(context.Background(), "sess-cancel", "", "Cancelled run"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	s.chat.batch = 0 // no background batcher: the tick under test is the only one
	var mu sync.Mutex
	var panels []core.Panel
	s.chat.SetPanelUpdate(func(p core.Panel) error {
		mu.Lock()
		panels = append(panels, p)
		mu.Unlock()
		return nil
	})
	run, ok := s.runs.start("sess-cancel")
	if !ok {
		t.Fatalf("start run")
	}
	s.chat.beginRun("sess-cancel")
	s.runs.setOpenTail(run, []transcriptItem{
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl-cancel", Name: "shell.run"}, Running: true, ElapsedMs: 5},
	}, time.Now())
	s.chat.endRun("sess-cancel")
	if !s.runs.active("sess-cancel") {
		t.Fatalf("the harness must leave the hub slot held - that IS the window")
	}
	if !s.chat.hasRunningCall("sess-cancel") {
		t.Fatalf("the unresolved call must still read running - the harness did not stage the window")
	}
	mu.Lock()
	before := len(panels)
	mu.Unlock()
	// the tick the window's race would run once the flush releases the paint lock
	s.chat.tick("sess-cancel")
	mu.Lock()
	after, _ := lastPanel(panels)
	n := len(panels)
	mu.Unlock()
	if n != before {
		t.Fatalf("a tick after the exit flush pushed again: %d -> %d panels", before, n)
	}
	if payload := chatPanelPayload(t, after); payload.Running {
		t.Fatalf("the panel ends running=true with no live run: %+v", payload)
	}
}

// TestChatActionStopThroughThePipe: the pipe's stop verb cancels the run.
func TestChatActionStopThroughThePipe(t *testing.T) {
	gated := newGatedProvider()
	a := agent.NewAgent(gated, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil, nil)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.store = st
	s.chat.batch = time.Hour
	recorder := newChatRecorder()
	// The real wiring order: newCanvas registers the capsule.
	if err := s.newCanvas(&core.Context{PanelProviders: []core.PanelProvider{s.chat}}); err != nil {
		t.Fatalf("newCanvas: %v", err)
	}
	s.chat.SetPanelUpdate(func(p core.Panel) error {
		recorder.ch <- p
		return nil
	})
	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	sessionID := postChatGetSessionID(t, ts)
	if !s.runs.active(sessionID) {
		t.Fatal("run not live")
	}
	stopBody := `{"module":"http_channel","action":"stop","args":{"session":"` + sessionID + `"}}`
	var out map[string]any
	status := 0
	deadline := time.Now().Add(testWaitTimeout)
	for {
		res, err := http.Post(ts.URL+"/ui/actions", "application/json", strings.NewReader(stopBody))
		if err != nil {
			t.Fatalf("post action: %v", err)
		}
		var body map[string]any
		json.NewDecoder(res.Body).Decode(&body)
		res.Body.Close()
		status = res.StatusCode
		if status == http.StatusOK {
			out = body
			break
		}
		// The start-to-launch window answers "run not started" while the cancel registers - a
		// retry is exactly how a frontend rides that transient.
		if time.Now().After(deadline) {
			t.Fatalf("stop never landed: %d", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	result, _ := out["result"].(map[string]any)
	if result["status"] != "cancelling" {
		t.Fatalf("stop result = %v", out)
	}
	close(gated.release)
	waitRunQuiescent(t, s, sessionID)
	recorder.closeDeltas()
	delta := recorder.all()
	if len(delta) == 0 {
		t.Fatal("no chat deltas at all")
	}
	last := chatPanelPayload(t, delta[len(delta)-1])
	if last.Running || last.Session != sessionID {
		t.Fatalf("exit payload = %+v", last)
	}
}

// TestBranchPathCutsTheOldSiblingKeepsRunRows: the pure cut behind the in-place retry.
func TestBranchPathCutsTheOldSiblingKeepsRunRows(t *testing.T) {
	path := []core.StoredMessage{
		{ID: 1},
		{ID: 2},
		{ID: 3},
	}
	if got := branchPath(path, "", 3); len(got) != 3 {
		t.Fatalf("unpinned path = %d rows, want 3", len(got))
	}
	if got := branchPath(path, "99", 3); len(got) != 3 {
		t.Fatalf("dangling branch = %d rows, want 3", len(got))
	}
	if got := branchPath(path, "zap", 3); len(got) != 3 {
		t.Fatalf("malformed branch = %d rows, want 3", len(got))
	}
	if got := branchPath(path, "1", 3); len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("retry cut = %+v, want row 1 only", got)
	}
	live := []core.StoredMessage{
		{ID: 1},
		{ID: 4},
		{ID: 5},
	}
	if got := branchPath(live, "1", 3); len(got) != 3 {
		t.Fatalf("mid-run cut = %+v, want rows 1,4,5", got)
	} else if got[1].ID != 4 || got[2].ID != 5 {
		t.Fatalf("mid-run cut = %+v, want rows 1,4,5", got)
	}
}

// gateHoldProvider streams one response chunk, then holds the stream open until
// released.
type gateHoldProvider struct {
	release chan struct{}
}

func (p *gateHoldProvider) ModelName() string                     { return "gate" }
func (p *gateHoldProvider) SetThinkingLevel(string)               {}
func (p *gateHoldProvider) WithModel(string) agent.Provider         { return p }
func (p *gateHoldProvider) WithThinkingLevel(string) agent.Provider { return p }
func (p *gateHoldProvider) ListModels() ([]string, error)         { return []string{"gate"}, nil }
func (p *gateHoldProvider) ModelInfo() (llm.ModelInfo, error)     { return llm.ModelInfo{}, nil }

func (p *gateHoldProvider) Stream(context.Context, []core.Message, []llm.ToolSchema) <-chan core.ProviderStreamEvent {
	events := make(chan core.ProviderStreamEvent)
	go func() {
		defer close(events)
		events <- core.ResponseChunk{Type: "response_chunk", Content: "fresh"}
		<-p.release
		events <- core.StreamEnd{Type: "stream_end", FinishReason: "stop"}
	}()
	return events
}

// TestRetryRendersInPlaceMidRun: while a retry streams, the panel payload carries.
func TestRetryRendersInPlaceMidRun(t *testing.T) {
	prov := &gateHoldProvider{release: make(chan struct{})}
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil, nil)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.store = st
	s.chat.batch = time.Hour
	if err := s.newCanvas(&core.Context{PanelProviders: []core.PanelProvider{s.chat}}); err != nil {
		t.Fatalf("newCanvas: %v", err)
	}
	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	ctx := context.Background()
	if err := st.CreateSession(ctx, "s-ip", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.AppendMessage(ctx, "s-ip", core.NewUser("say hi")); err != nil {
		t.Fatalf("append user: %v", err)
	}
	if err := st.AppendMessage(ctx, "s-ip", core.NewAssistant("first answer")); err != nil {
		t.Fatalf("append assistant: %v", err)
	}
	idle := chatPanelPayload(t, s.chat.buildPanel("s-ip", false))
	if got := chatRowRoles(idle.Rows); strings.Join(got, ",") != "user,assistant" {
		t.Fatalf("idle rows = %v", got)
	}
	if idle.Rows[1].Blocks[0].Content != "first answer" {
		t.Fatalf("idle assistant = %q", idle.Rows[1].Blocks[0].Content)
	}

	retryBody := `{"module":"http_channel","action":"retry","args":{"session":"s-ip"}}`
	res, err := http.Post(ts.URL+"/ui/actions", "application/json", strings.NewReader(retryBody))
	if err != nil {
		t.Fatalf("post action: %v", err)
	}
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("retry = %d: %v", res.StatusCode, out)
	}

	deadline := time.Now().Add(testWaitTimeout)
	var mid []transcriptItem
	for time.Now().Before(deadline) {
		mid = s.chat.sessionItems("s-ip", true)
		if len(mid) == 2 && mid[1].Kind == "message" && mid[1].Content == "fresh" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(mid) != 2 || mid[1].Content != "fresh" {
		t.Fatalf("mid-run items = %+v, want user + streaming fresh", mid)
	}
	if mid[1].ID != "" || !mid[1].Running {
		t.Fatalf("mid-run fresh row = %+v, want a running live row", mid[1])
	}
	for _, it := range mid {
		if it.Content == "first answer" {
			t.Fatalf("old answer still rendered mid-run: %+v", mid)
		}
	}
	midPanel := chatPanelPayload(t, s.chat.buildPanel("s-ip", true))
	if got := chatRowRoles(midPanel.Rows); strings.Join(got, ",") != "user,assistant" {
		t.Fatalf("mid-run panel rows = %v", got)
	}
	if midPanel.Rows[1].Blocks[0].Content != "fresh" || midPanel.Rows[1].Status != core.ChatStatusStreaming {
		t.Fatalf("mid-run panel assistant = %+v", midPanel.Rows[1])
	}

	tres, err := http.Get(ts.URL + "/sessions/s-ip/transcript")
	if err != nil {
		t.Fatalf("get transcript: %v", err)
	}
	var body struct {
		Items []transcriptItem `json:"items"`
	}
	json.NewDecoder(tres.Body).Decode(&body)
	tres.Body.Close()
	if len(body.Items) != 2 || body.Items[1].Content != "fresh" {
		t.Fatalf("mid-run transcript items = %+v, want user + fresh", body.Items)
	}
	for _, it := range body.Items {
		if it.Content == "first answer" {
			t.Fatalf("old answer still served by the transcript mid-run: %+v", body.Items)
		}
	}

	close(prov.release)
	waitRunQuiescent(t, s, "s-ip")
	final := chatPanelPayload(t, s.chat.buildPanel("s-ip", false))
	if got := chatRowRoles(final.Rows); strings.Join(got, ",") != "user,assistant" {
		t.Fatalf("final rows = %v, got payload %+v", got, final)
	}
	if final.Rows[1].Blocks[0].Content != "fresh" || final.Rows[1].Status != core.ChatStatusComplete {
		t.Fatalf("final assistant = %+v", final.Rows[1])
	}
	view, err := st.GetActiveView(ctx, "s-ip")
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if len(view.Branches) != 1 || view.Branches[0].Count != 2 {
		t.Fatalf("branches after retry = %+v, want one point with 2 siblings", view.Branches)
	}
}

// TestChatActionRetryThroughThePipe: the pipe's retry verb replays the session's last.
func TestChatActionRetryThroughThePipe(t *testing.T) {
	a := agent.NewAgent(newMockProvider("mock", scripted(
		core.ResponseChunk{Type: "response_chunk", Content: "first answer"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)), nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil, nil)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.store = st
	s.chat.batch = time.Hour
	if err := s.newCanvas(&core.Context{PanelProviders: []core.PanelProvider{s.chat}}); err != nil {
		t.Fatalf("newCanvas: %v", err)
	}
	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	sessionID := postChatGetSessionID(t, ts)
	waitRunQuiescent(t, s, sessionID)
	view, err := s.store.GetActiveView(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if len(view.Path) != 2 {
		t.Fatalf("pre-retry path = %d messages", len(view.Path))
	}

	retryBody := `{"module":"http_channel","action":"retry","args":{"session":"` + sessionID + `"}}`
	res, err := http.Post(ts.URL+"/ui/actions", "application/json", strings.NewReader(retryBody))
	if err != nil {
		t.Fatalf("post action: %v", err)
	}
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("retry = %d: %v", res.StatusCode, out)
	}
	// The retry run is live - OR already finished. The run slot is registered
	// synchronously inside the action's 200 (prepareRun's runs.start), so "not live at
	// this instant" has exactly one product meaning: the whole run (dummy provider,
	// near-instant; persist -> finish) completed inside the gap between the action
	// response and this check - a window that widens under runner load, where this test
	// process can be descheduled long past a run's entire lifetime.
	deadline := time.Now().Add(testWaitTimeout)
	for {
		if s.runs.active(sessionID) {
			break
		}
		v, err := s.store.GetActiveView(context.Background(), sessionID)
		if err == nil && len(v.Branches) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retry run not live and no sibling branch persisted - the retry launched nothing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitRunQuiescent(t, s, sessionID)
	// The retry replays the same turn: the store gains a sibling
	// assistant, the user message is NOT duplicated.
	view, err = s.store.GetActiveView(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("view after retry: %v", err)
	}
	if len(view.Branches) == 0 {
		t.Fatalf("no sibling variants after retry: path=%d users, no branches", len(view.Path))
	}
}

// TestChatPanelEphemeralRowsFromRegistry: a store-less session composes its rows.
func TestChatPanelEphemeralRowsFromRegistry(t *testing.T) {
	s := newTestServer()
	s.chat.batch = time.Hour
	s.eph.create("eph-panel")
	s.eph.appendUser("eph-panel", []core.Message{core.NewUser("hi")}, time.Now())
	items := s.chat.sessionItems("eph-panel", false)
	if len(items) != 1 || items[0].Kind != "user" || items[0].Content != "hi" {
		t.Fatalf("eph items = %+v", items)
	}
}

// postChatGetSessionID posts one /chat send and returns the session id.
func postChatGetSessionID(t *testing.T, ts *httptest2.Server) string {
	t.Helper()
	res, err := http.Post(ts.URL+"/chat", "application/json",
		strings.NewReader(`{"messages":[{"role":"user","content":"say hi"}]}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	rd := bufio.NewReader(res.Body)
	frame, ok := nextFrame(t, rd)
	if !ok {
		t.Fatal("stream closed before the session frame")
	}
	var f struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(frame.data, &f); err != nil || f.ID == "" {
		t.Fatalf("session frame = %q (%v)", frame.data, err)
	}
	return f.ID
}

// TestChatDeltaRidesTheRunStream.
func TestChatDeltaRidesTheRunStream(t *testing.T) {
	a := agent.NewAgent(newMockProvider("mock", scripted(
		core.ResponseChunk{Type: "response_chunk", Content: "hel"},
		core.ResponseChunk{Type: "response_chunk", Content: "lo"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)), nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil, nil)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.store = st
	// Cadence OFF: every token flushes.
	s.chat.batch = 0
	if err := s.newCanvas(&core.Context{PanelProviders: []core.PanelProvider{s.chat}}); err != nil {
		t.Fatalf("newCanvas: %v", err)
	}
	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Post(ts.URL+"/chat", "application/json",
		strings.NewReader(`{"messages":[{"role":"user","content":"say hi"}]}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	rd := bufio.NewReader(res.Body)
	sawDelta := false
	for {
		frame, ok := nextFrameW(t, rd)
		if !ok {
			break
		}
		if frame.event != "view_model_delta" {
			continue
		}
		var delta core.ViewModelDelta
		if err := json.Unmarshal(frame.data, &delta); err != nil {
			t.Fatalf("delta frame: %v", err)
		}
		if delta.Panel.Kind != core.PanelKindChat || delta.Panel.ID != ChatPanelID {
			t.Fatalf("delta panel = %+v", delta.Panel)
		}
		sawDelta = true
		break
	}
	res.Body.Close()
	if !sawDelta {
		t.Fatal("no view_model_delta frame rode the run stream")
	}
}

// nextFrameW is nextFrame's non-fatal variant: the caller breaks at the first.
func nextFrameW(t *testing.T, rd *bufio.Reader) (sseFrame, bool) {
	t.Helper()
	type result struct {
		frame sseFrame
		ok    bool
	}
	done := make(chan result, 1)
	go func() {
		var frame sseFrame
		haveEvent := false
		var event string
		for {
			raw, err := rd.ReadString('\n')
			line := strings.TrimRight(raw, "\r\n")
			switch {
			case err != nil:
				done <- result{frame, false}
				return
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
				haveEvent = true
			case strings.HasPrefix(line, "data: "):
				if haveEvent {
					frame = sseFrame{event: event, data: []byte(strings.TrimPrefix(line, "data: "))}
					done <- result{frame, true}
					return
				}
			}
		}
	}()
	select {
	case r := <-done:
		return r.frame, r.ok
	case <-time.After(5 * time.Second):
		t.Fatal("stream wedged")
		return sseFrame{}, false
	}
}

// gateStore arms ONCE: the first GetActiveView after arm blocks until release - the
// chat panel's build composes through this store, so the test pins a batcher tick
// inside its build window exactly, and fires endRun there.
type gateStore struct {
	agent.StoreProvider
	mu      sync.Mutex
	armed   bool
	entered chan struct{}
	release chan struct{}
	fired   bool
}

func (g *gateStore) arm() (<-chan struct{}, func()) {
	entered := make(chan struct{})
	release := make(chan struct{})
	g.mu.Lock()
	g.armed = true
	g.entered, g.release = entered, release
	g.mu.Unlock()
	return entered, func() { close(release) }
}

func (g *gateStore) GetActiveView(ctx context.Context, sessionID string) (core.ActiveView, error) {
	g.mu.Lock()
	take := g.armed && !g.fired
	if take {
		g.fired = true
	}
	entered, release := g.entered, g.release
	g.mu.Unlock()
	if take {
		close(entered)
		<-release
	}
	return g.StoreProvider.GetActiveView(ctx, sessionID)
}

// TestBatcherPushCannotOutliveTheRun: a batcher tick past takeDirty builds.
func TestBatcherPushCannotOutliveTheRun(t *testing.T) {
	prov := newMockProvider("mock", scripted(
		core.ResponseChunk{Type: "response_chunk", Content: "hel"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	))
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	gated := &gateStore{StoreProvider: st}
	s := NewServer(a, nil, gated, nil, nil, nil)
	s.chat.batch = 20 * time.Millisecond
	if err := s.newCanvas(&core.Context{PanelProviders: []core.PanelProvider{s.chat}}); err != nil {
		t.Fatalf("newCanvas: %v", err)
	}
	recorder := newChatRecorder()
	s.chat.SetPanelUpdate(func(p core.Panel) error {
		recorder.ch <- p
		return nil
	})
	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	sessionID := postChatGetSessionID(t, ts)
	waitRunQuiescent(t, s, sessionID)

	s.chat.beginRun(sessionID) // the batcher spawns; its begin push composes behind the unarmed store
	entered, closeRelease := gated.arm()
	s.chat.runEvent(sessionID, core.StreamEvent{Event: core.ResponseChunk{Type: "response_chunk", Content: "tok"}})
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("batcher never entered its build - the race window never opened")
	}
	done := make(chan struct{})
	go func() {
		s.chat.endRun(sessionID)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	closeRelease()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("endRun never returned - the paint lock does not release")
	}

	time.Sleep(80 * time.Millisecond)
	recorder.closeDeltas()
	panels := recorder.all()
	last := chatPanelPayload(t, panels[len(panels)-1])
	if last.Running {
		t.Fatalf("canvas panel ends running=true with no live run: last delta = %+v", last)
	}
	if last.Session != sessionID {
		t.Fatalf("last delta session = %q, want %q", last.Session, sessionID)
	}
	snap := s.canvas.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot = %d panels, want 1", len(snap))
	}
	if payload := chatPanelPayload(t, snap[0]); payload.Running {
		t.Fatalf("canvas snapshot ends running=true with no live run: %+v", payload)
	}
	if s.runs.active(sessionID) {
		t.Fatal("no run should hold the slot")
	}
}

// TestChatPanelPendingCallCarriesItsStart: a YIELD-class call.
func TestChatPanelPendingCallCarriesItsStart(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "sess-yield", "", "Yielded run"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.AppendMessage(ctx, "sess-yield", core.NewUser("run the suite")); err != nil {
		t.Fatalf("append user: %v", err)
	}
	assistant := core.NewAssistant("")
	assistant.ToolCalls = []core.ToolCall{{ID: "cyield", Name: "shell.run", Arguments: map[string]any{"command": "sleep 30"}}}
	if err := st.AppendMessage(ctx, "sess-yield", assistant); err != nil {
		t.Fatalf("append assistant: %v", err)
	}
	panels := s.chat.Panels()
	if len(panels) != 1 {
		t.Fatalf("panels = %d, want 1", len(panels))
	}
	payload := chatPanelPayload(t, panels[0])
	if payload.Running {
		t.Fatalf("no run is live: %+v", payload)
	}
	block := toolCallBlock(t, payload)
	if block.Running {
		t.Fatalf("the persisted pending row is not a running block: %+v", block)
	}
	if block.StartedAt == "" {
		t.Fatalf("the pending row carries no start stamp: %+v", block)
	}
	start, err := time.Parse(time.RFC3339Nano, block.StartedAt)
	if err != nil {
		t.Fatalf("start stamp %q does not parse: %v", block.StartedAt, err)
	}
	msgs, err := st.GetActiveView(ctx, "sess-yield")
	if err != nil {
		t.Fatalf("active view: %v", err)
	}
	var persisted string
	for _, sm := range msgs.Path {
		if _, ok := sm.Message.(core.Assistant); ok {
			persisted = sm.CreatedAt
		}
	}
	if persisted == "" {
		t.Fatal("no persisted assistant row")
	}
	want, err := time.Parse(time.RFC3339Nano, persisted)
	if err != nil {
		t.Fatalf("persisted stamp %q does not parse: %v", persisted, err)
	}
	if !start.Equal(want) {
		t.Fatalf("start stamp = %v, want the assistant row's write time %v", start, want)
	}
	rec := httptest2.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest2.NewRequest(http.MethodGet, "/sessions/sess-yield/transcript", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("transcript status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []transcriptItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode transcript: %v", err)
	}
	var wire *transcriptItem
	for i := range body.Items {
		if body.Items[i].Kind == "tool_call" {
			wire = &body.Items[i]
		}
	}
	if wire == nil || wire.StartedAt != block.StartedAt {
		t.Fatalf("transcript call row = %+v, want started_at %q", wire, block.StartedAt)
	}
}

// toolCallBlock pulls the single tool_call block out of a panel payload.
func toolCallBlock(t *testing.T, payload core.ChatPayload) core.ChatBlock {
	t.Helper()
	for _, row := range payload.Rows {
		for _, b := range row.Blocks {
			if b.Type == core.ChatBlockToolCall {
				return b
			}
		}
	}
	t.Fatalf("the payload carries no tool_call block: %+v", payload)
	return core.ChatBlock{}
}

// TestUIActionRetryOnLiveRunIsA409: handleUIActions passes a runError's own HTTP.
func TestUIActionRetryOnLiveRunIsA409(t *testing.T) {
	prov := &gateHoldProvider{release: make(chan struct{})}
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil, nil)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.store = st
	if err := s.newCanvas(&core.Context{PanelProviders: []core.PanelProvider{s.chat}}); err != nil {
		t.Fatalf("newCanvas: %v", err)
	}
	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	sessionID := postChatGetSessionID(t, ts)
	body := `{"module":"http_channel","action":"retry","args":{"session":"` + sessionID + `"}}`
	res, err := http.Post(ts.URL+"/ui/actions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post action: %v", err)
	}
	out, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("retry on live run = %d (%s), want 409", res.StatusCode, out)
	}
	close(prov.release)
	waitRunQuiescent(t, s, sessionID)
}
