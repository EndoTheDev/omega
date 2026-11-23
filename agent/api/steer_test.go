package api

// The steer queue's end-to-end law: a send while a turn is live is a ROW (rendered, deletable)
// that lands at the run's next batch boundary, and a result settling mid-turn waits for the
// same boundary. Rows are session-held, so a crashed turn hands them to the next one.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/EndoTheDev/omega/llm"
)

// newSteerServer wires the REAL loop over the step provider: the boundary law is the
// loop's, so a stub loop would prove nothing here.
func newSteerServer(t *testing.T) (*Server, *stepProvider, *store.Store, *httptest.Server) {
	t.Helper()
	prov := newStepProvider("step")
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	tools := map[string]core.Tool{"probe": {
		Description: "probe (inline)",
		Run: func(_ context.Context, _ map[string]any) (string, error) {
			return "probe-ok", nil
		},
	}}
	s := NewServer(a, tools, st, nil, nil, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, prov, st, ts
}

// postSteer sends one mid-turn message and returns the queue rows the server parked.
func postSteer(t *testing.T, ts *httptest.Server, sessionID, text string) []queuedRow {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"messages":   []map[string]any{{"role": "user", "content": text}},
		"session_id": sessionID,
	})
	resp, err := http.Post(ts.URL+"/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("steer post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("steer status = %d, want 202", resp.StatusCode)
	}
	var queued struct {
		Queued []queuedRow `json:"queued"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&queued); err != nil {
		t.Fatalf("decode queued: %v", err)
	}
	return queued.Queued
}

// fetchQueue reads the session's waiting rows.
func fetchQueue(t *testing.T, ts *httptest.Server, sessionID string) []queuedRow {
	t.Helper()
	resp, err := http.Get(ts.URL + "/sessions/" + sessionID + "/queue")
	if err != nil {
		t.Fatalf("get queue: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get queue status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Queued []queuedRow `json:"queued"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	return body.Queued
}

// rowContents renders the queue for assertions.
func rowContents(rows []queuedRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Content)
	}
	return out
}

// deleteRow issues DELETE /sessions/{id}/queue/{row} and returns its status.
func deleteRow(t *testing.T, ts *httptest.Server, sessionID, row string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/sessions/"+sessionID+"/queue/"+row, nil)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("delete queue row: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// TestSteerRowRendersAndDeletesBeforeDelivery: the visible queue - a mid-turn send is a
// row, and a row removed before the boundary never reaches the conversation.
func TestSteerRowRendersAndDeletesBeforeDelivery(t *testing.T) {
	_, prov, st, ts := newSteerServer(t)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	resp, cancel, sc := postChatStream(t, ts, "s1")
	defer cancel()
	prov.send(core.ResponseChunk{Type: "response_chunk", Content: "a"})
	readFrames(t, sc, "response_chunk", 1)

	rows := postSteer(t, ts, "s1", "scratch that")
	if len(rows) != 1 || rows[0].Content != "scratch that" || rows[0].ID == "" {
		t.Fatalf("parked rows = %+v, want one row", rows)
	}
	if got := rowContents(fetchQueue(t, ts, "s1")); len(got) != 1 || got[0] != "scratch that" {
		t.Fatalf("queue listing = %v, want the parked row", got)
	}

	if code := deleteRow(t, ts, "s1", rows[0].ID); code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", code)
	}
	if got := fetchQueue(t, ts, "s1"); len(got) != 0 {
		t.Fatalf("queue after delete = %v, want empty", rowContents(got))
	}
	// A second delete is a 404: the surface's row is stale, and saying so beats a silent
	// no-op.
	if code := deleteRow(t, ts, "s1", rows[0].ID); code != http.StatusNotFound {
		t.Fatalf("stale delete status = %d, want 404", code)
	}

	prov.done()
	readFrames(t, sc, "agent_end", 1)
	resp.Body.Close()

	for _, m := range storedMessages(t, st, "s1") {
		if u, ok := m.(core.User); ok && u.Content == "scratch that" {
			t.Fatal("a deleted queue row reached the conversation")
		}
	}
}

// TestSteerDeliversAtTheNextBoundary: the steer lands in the SAME run, after the tool
// batch, and renders as an ordinary user message (no transcript badge).
func TestSteerDeliversAtTheNextBoundary(t *testing.T) {
	toolStarted := make(chan struct{})
	toolRelease := make(chan struct{})
	var startedOnce sync.Once
	tool := core.Tool{
		Description: "probe (gated)",
		Run: func(_ context.Context, _ map[string]any) (string, error) {
			startedOnce.Do(func() { close(toolStarted) })
			<-toolRelease
			return "probe-ok", nil
		},
	}
	deck := newTurnDeck(toolCallBatch("c1"), finalStep("adjusted"))
	s, ts, st := newTurnServer(t, deck, tool)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	resp, cancel, sc := postChatStream(t, ts, "s1")
	defer cancel()
	<-toolStarted // the turn is live, its tool running: the send must park

	rows := postSteer(t, ts, "s1", "use the staging key")
	if len(rows) != 1 {
		t.Fatalf("parked rows = %+v, want one", rows)
	}
	if got := fetchQueue(t, ts, "s1"); len(got) != 1 {
		t.Fatalf("queue while live = %v, want the parked row", rowContents(got))
	}
	close(toolRelease)

	readFrames(t, sc, "steer_delivered", 1)
	if got := fetchQueue(t, ts, "s1"); len(got) != 0 {
		t.Fatalf("queue after delivery = %v, want empty (the row is spent)", rowContents(got))
	}
	readFrames(t, sc, "agent_end", 1)
	resp.Body.Close()

	if n := deck.callCount(); n != 2 {
		t.Fatalf("provider calls = %d, want 2 (the steer adjusted the SAME run)", n)
	}
	second := deck.messagesAt(1)
	steerAt := indexOfMsg(second, func(m core.Message) bool {
		u, ok := m.(core.User)
		return ok && u.Content == "use the staging key"
	})
	resultAt := indexOfMsg(second, func(m core.Message) bool {
		tr, ok := m.(core.ToolResult)
		return ok && tr.ToolCallID == "c1"
	})
	if steerAt < 0 || resultAt < 0 {
		t.Fatalf("steer at %d, tool result at %d - both must reach the model's next call", steerAt, resultAt)
	}
	if steerAt < resultAt {
		t.Errorf("steer at %d landed before the tool result at %d - the boundary law broke", steerAt, resultAt)
	}
	// One run served both turns: no second launch, no wake turn.
	if n := s.wakeLaunchCount("s1"); n != 0 {
		t.Errorf("wake launches = %d, want 0 (the steer rode the live turn)", n)
	}
	msgs := storedMessages(t, st, "s1")
	userAt, answerAt := -1, -1
	for i, m := range msgs {
		if u, ok := m.(core.User); ok && u.Content == "use the staging key" {
			userAt = i
		}
		if a, ok := m.(core.Assistant); ok && a.Content == "adjusted" {
			answerAt = i
		}
	}
	if userAt < 0 {
		t.Fatal("the delivered steer never persisted - history lost the human's words")
	}
	if answerAt < userAt {
		t.Errorf("steer row at %d, adjusted answer at %d - the turn never adjusted after the boundary", userAt, answerAt)
	}
}

// TestWakeResultJoinsTheLiveTurn: a settled call's result arriving while a turn is live
// waits for the same boundary and re-enters THAT run - never a parallel wake launch.
func TestWakeResultJoinsTheLiveTurn(t *testing.T) {
	toolStarted := make(chan struct{})
	toolRelease := make(chan struct{})
	var startedOnce sync.Once
	tool := core.Tool{
		Description: "probe (gated)",
		Run: func(_ context.Context, _ map[string]any) (string, error) {
			startedOnce.Do(func() { close(toolStarted) })
			<-toolRelease
			return "probe-ok", nil
		},
	}
	deck := newTurnDeck(toolCallBatch("c1"), finalStep("used the late result"))
	s, ts, st := newTurnServer(t, deck, tool)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	resp, cancel, sc := postChatStream(t, ts, "s1")
	defer cancel()
	<-toolStarted

	// The result settles mid-turn: it must QUEUE, not launch.
	s.wakeHome("s1", core.YieldResult{
		ToolCallID: "cLate",
		ToolName:   "probe",
		Content:    "async:late",
		Timestamp:  core.NowISO(),
	})
	if n := s.wakeLaunchCount("s1"); n != 0 {
		t.Fatalf("wake launches = %d during a live turn, want 0", n)
	}
	if rows := s.steers.list("s1"); len(rows) != 1 || rows[0].Kind != queuedWake {
		t.Fatalf("queued wake rows = %+v, want the parked result", rows)
	}
	close(toolRelease)

	readFrames(t, sc, "steer_delivered", 1)
	readFrames(t, sc, "agent_end", 1)
	resp.Body.Close()

	if n := s.wakeLaunchCount("s1"); n != 0 {
		t.Errorf("wake launches = %d, want 0 - the live turn took its own result", n)
	}
	second := deck.messagesAt(1)
	if indexOfMsg(second, func(m core.Message) bool {
		y, ok := m.(core.YieldResult)
		return ok && y.ToolCallID == "cLate" && y.Content == "async:late"
	}) < 0 {
		t.Errorf("the settled result never reached the live turn's next call: %+v", second)
	}
	if !hasYieldResult(storedMessages(t, st, "s1"), "cLate", core.YieldResult{Content: "async:late"}) {
		t.Error("the joined result never persisted")
	}
}

// TestQueuedRowsSurviveACrashedTurn: the row is session-held. A turn dying on a provider
// error eats nothing - the next turn delivers the row first.
func TestQueuedRowsSurviveACrashedTurn(t *testing.T) {
	s, prov, st, ts := newSteerServer(t)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	resp, cancel, sc := postChatStream(t, ts, "s1")
	defer cancel()
	prov.send(core.ResponseChunk{Type: "response_chunk", Content: "starting"})
	readFrames(t, sc, "response_chunk", 1)

	if rows := postSteer(t, ts, "s1", "after the crash"); len(rows) != 1 {
		t.Fatalf("parked rows = %+v, want one", rows)
	}

	// The turn dies before any boundary can take the row. (The stream closes only when the
	// provider's channel does, so the error and the close both ride here.)
	prov.send(core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "provider fell over"})
	prov.done()
	readFrames(t, sc, "agent_end", 1)
	resp.Body.Close()

	// The finish hook launches the queued row as its own turn: it is input, not a loss.
	// (The provider stream is closed now, so that turn answers empty - its INPUT is what
	// this test is about.)

	waitFor(t, "the queued row delivered by the next turn", func() bool {
		return hasUserText(storedMessages(t, st, "s1"), "after the crash")
	})
	waitFor(t, "the recovery turn finished", func() bool { return !s.runs.active("s1") })
	msgs := storedMessages(t, st, "s1")
	if !hasUserText(msgs, "after the crash") {
		t.Fatalf("the queued row was eaten by the crashed turn: %+v", msgs)
	}
	if got := fetchQueue(t, ts, "s1"); len(got) != 0 {
		t.Fatalf("queue after the recovery turn = %v, want empty", rowContents(got))
	}
}

// TestIdleSendStartsATurn: with no live run the send is a normal turn (200 stream), never a
// queued row - the composer hint's third state.
func TestIdleSendStartsATurn(t *testing.T) {
	_, prov, st, ts := newSteerServer(t)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	resp, cancel, sc := postChatStream(t, ts, "s1")
	defer cancel()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("idle send status = %d, want 200 (a normal run)", resp.StatusCode)
	}
	prov.send(finalStep("hello")...)
	prov.done()
	readFrames(t, sc, "agent_end", 1)
	resp.Body.Close()
}

// --- helpers ---------------------------------------------------------------------------

// turnDeck scripts exactly one batch per provider call and remembers what each call saw.
// A real provider's stream ENDS after its batch, so the loop can advance to its next turn;
// the step provider stays open (right for streaming-shape tests, wrong for boundary ones).
type turnDeck struct {
	mu      sync.Mutex
	batches [][]core.ProviderStreamEvent
	calls   int
	seen    [][]core.Message
}

func newTurnDeck(batches ...[]core.ProviderStreamEvent) *turnDeck {
	return &turnDeck{batches: batches}
}

func (d *turnDeck) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// messagesAt returns the message list the i-th provider call received.
func (d *turnDeck) messagesAt(i int) []core.Message {
	d.mu.Lock()
	defer d.mu.Unlock()
	if i < 0 || i >= len(d.seen) {
		return nil
	}
	return d.seen[i]
}

// turnProvider serves the deck: call N gets batch N (an exhausted deck answers empty, so a
// runaway run terminates instead of hanging).
type turnProvider struct {
	modelName string
	deck      *turnDeck
}

func (p *turnProvider) ModelName() string              { return p.modelName }
func (p *turnProvider) SetThinkingLevel(_ string)      {}
func (p *turnProvider) WithThinkingLevel(_ string) agent.Provider { return p }
func (p *turnProvider) WithModel(model string) agent.Provider {
	return &turnProvider{modelName: model, deck: p.deck}
}
func (p *turnProvider) ListModels() ([]string, error)       { return []string{p.modelName}, nil }
func (p *turnProvider) ModelInfo() (llm.ModelInfo, error)   { return llm.ModelInfo{}, nil }

func (p *turnProvider) Stream(_ context.Context, msgs []core.Message, _ []llm.ToolSchema) <-chan core.ProviderStreamEvent {
	p.deck.mu.Lock()
	i := p.deck.calls
	p.deck.calls++
	p.deck.seen = append(p.deck.seen, append([]core.Message(nil), msgs...))
	var batch []core.ProviderStreamEvent
	if i < len(p.deck.batches) {
		batch = p.deck.batches[i]
	}
	p.deck.mu.Unlock()

	events := make(chan core.ProviderStreamEvent, len(batch))
	for _, e := range batch {
		events <- e
	}
	close(events)
	return events
}

// newTurnServer wires the real loop over a turnScript provider with one gated tool.
func newTurnServer(t *testing.T, deck *turnDeck, tool core.Tool) (*Server, *httptest.Server, *store.Store) {
	t.Helper()
	a := agent.NewAgent(&turnProvider{modelName: "turn", deck: deck}, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := NewServer(a, map[string]core.Tool{"probe": tool}, st, nil, nil, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts, st
}

// toolCallBatch is one completion that calls the probe tool.
func toolCallBatch(id string) []core.ProviderStreamEvent {
	return []core.ProviderStreamEvent{
		core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: id, Name: "probe", Arguments: map[string]any{}}},
		core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
	}
}

// indexOfMsg returns the position of the first message matching pred, or -1.
func indexOfMsg(msgs []core.Message, pred func(core.Message) bool) int {
	for i, m := range msgs {
		if pred(m) {
			return i
		}
	}
	return -1
}

// storedMessages reads the session's persisted rows.
func storedMessages(t *testing.T, st *store.Store, id string) []core.Message {
	t.Helper()
	msgs, err := st.GetMessages(context.Background(), id)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	return msgs
}

// hasUserText reports whether a user row carries the text exactly (no delivery marker).
func hasUserText(msgs []core.Message, text string) bool {
	for _, m := range msgs {
		if u, ok := m.(core.User); ok && u.Content == text {
			return true
		}
	}
	return false
}
