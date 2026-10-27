package http_channel

// The chat panel's tests (chat-last 1/3, card t_ec45b670): the view-
// model vocabulary's mapping, the mount registration, the delta
// cadence law (tokens batch, structure flushes) and the action pipe's
// chat verbs. Chat is the repo's most journey-asserted surface - these
// tests pin the shapes the chained renderer cards (2a webui, 2b TUI)
// render from.

import (
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
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/engine/ai"
	"github.com/EndoTheDev/omega/modules/store"
)

// chatRecorder collects the deltas a capsule pushes through its
// registered canvas closure. all() blocks until closeDeltas().
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

// closeDeltas closes the collection point; every queued delta is
// consumed before all() returns.
func (r *chatRecorder) closeDeltas() { r.once.Do(func() { close(r.ch) }) }

// all returns every recorded delta. Blocks until closeDeltas.
func (r *chatRecorder) all() []core.Panel { return <-r.done }

// waitRunQuiescent spins until the session's run slot is free.
func waitRunQuiescent(t *testing.T, s *Server, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !s.runs.active(id) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run on %q never finished", id)
}

// chatPanelPayload decodes one panel's payload verbatim (the wire
// shape, not the Go struct's convenience).
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

// TestChatRowsMapTheTranscriptVocabulary: the view-model row map is
// one row per transcript item - roles, blocks and statuses straight
// off the item's kind and running hint.
func TestChatRowsMapTheTranscriptVocabulary(t *testing.T) {
	rows := chatRows([]transcriptItem{
		{Kind: "user", ID: "u1", Content: "hi", Images: []core.ImageContent{{MediaType: "image/png", Base64: "aGk="}}},
		{Kind: "thinking", ID: "a1", Content: "hmm", Running: true},
		{Kind: "message", ID: "a2", Content: "answer"},
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl", Name: "shell.run"}, Running: true, ElapsedMs: 5},
		{Kind: "tool_result", ID: "r1", ToolCallID: "tcl", Content: "out", IsError: true, ElapsedMs: 7},
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
	if rows[2].Status != core.ChatStatusComplete {
		t.Fatalf("message status = %+v", rows[2])
	}
	if rows[3].Role != core.ChatRoleAssistant || rows[3].Blocks[0].Type != core.ChatBlockToolCall || rows[3].Blocks[0].ToolCall == nil || rows[3].Blocks[0].ToolCall.Name != "shell.run" {
		t.Fatalf("tool_call row = %+v", rows[3])
	}
	if rows[4].Role != core.ChatRoleTool || rows[4].Blocks[0].ToolCallID != "tcl" || !rows[4].Blocks[0].IsError {
		t.Fatalf("tool_result row = %+v", rows[4])
	}
}

// TestComposeItemsDropsPersistedTwins: the open record can carry items
// the loop already persisted (it empties only at the next segment
// split) - they duplicate the persisted rows and the compose drops
// them, while genuinely new stream content flows.
func TestComposeItemsDropsPersistedTwins(t *testing.T) {
	persisted := []transcriptItem{
		{Kind: "user", ID: "u1", Content: "hi"},
		{Kind: "message", ID: "a1", Content: "hello"},
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl", Name: "shell.run"}},
	}
	composed := composeItems(persisted, []transcriptItem{
		// already persisted twins (AssistantMessageEvent landed, the
		// accumulator's split has not)
		{Kind: "message", Content: "hello"},
		{Kind: "tool_call", ToolCall: &core.ToolCall{ID: "tcl", Name: "shell.run"}},
		// genuinely in-flight: the next turn's fresh call and its
		// growing answer prefix
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

// TestChatPanelSnapshotDeclaresNewestSession: the load-time catch-up
// names the module's panel kind, its actions and the most recent
// conversation's rows.
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
	if p.ID != chatPanelID || p.Title != "Chat" || p.Kind != core.PanelKindChat {
		t.Fatalf("panel = %+v", p)
	}
	ids := map[string]bool{}
	for _, a := range p.Actions {
		ids[a.ID] = true
		if a.Module != chatModuleNamespace {
			t.Fatalf("action %q module = %q", a.ID, a.Module)
		}
	}
	if !ids[chatActionStop] || !ids[chatActionRetry] {
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

// TestChatPanelRegistersThroughTheStartSeam (chat-last 2b ruling): the
// plugin's Mount only CREATES the capsule - registration is gated on
// the channel actually starting. One chat panel per live frontend: a
// TUI-only process never reaches Start, so its canvas carries no dead
// http chat panel; here the channel-start handoff registers it with
// the canvas seam, and the pipe routes to the capsule's handler.
func TestChatPanelRegistersThroughTheMountSeam(t *testing.T) {
	s, st := newTestServerWithStore(t)
	plugin := &Plugin{}
	pctx := &core.Context{Configs: map[string]any{"http_channel": Config{Port: 1}}}
	if _, err := agent.MountAllAPI([]agent.Plugin{plugin}, pctx, nil, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	// Mount registers nothing: the panel exists only once a live
	// frontend starts.
	if len(pctx.PanelProviders) != 0 {
		t.Fatalf("panel providers = %d, want 0 (registration is Start-gated)", len(pctx.PanelProviders))
	}
	// The channel-start handoff (HTTPChannel.Start): bind the capsule
	// to its server and register it before the canvas runtime builds.
	plugin.chat.srv.Store(s)
	plugin.chat.registerAt(pctx)
	canvas, err := agent.NewCanvas(pctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	snapshot := canvas.Snapshot()
	if len(snapshot) != 1 || snapshot[0].Kind != core.PanelKindChat {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	// The action pipe routes: an unknown session is the capsule's
	// error, verbatim - module known, action routed.
	_, stopErr := canvas.Dispatch(core.ActionCall{Module: chatModuleNamespace, Action: chatActionStop, Args: json.RawMessage(`{"session":"ghost"}`)})
	if stopErr == nil || !strings.Contains(stopErr.Error(), "session not active") {
		t.Fatalf("stop dispatch = %v", stopErr)
	}
	// A retry against a session with no user message is the replay
	// guard's answer, and a failed prepare releases the run slot.
	if err := st.CreateSession(context.Background(), "s-empty", "", "Empty"); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, retryErr := canvas.Dispatch(core.ActionCall{Module: chatModuleNamespace, Action: chatActionRetry, Args: json.RawMessage(`{"session":"s-empty"}`)})
	if retryErr == nil || !strings.Contains(retryErr.Error(), "nothing to retry") {
		t.Fatalf("retry dispatch = %v", retryErr)
	}
	if s.runs.active("s-empty") {
		t.Fatal("failed prepare left the run slot held")
	}
}

// TestChatPanelDeltasRunTheBatchLaw: token chunks never flush on their
// own when the cadence holds - the deltas carry whole turn states at
// the structural boundaries, and the run's exit paints the persisted
// truth with running off. batch=1h pins the law: exactly the
// structural flushes plus the exit one, no token deltas in between.
func TestChatPanelDeltasRunTheBatchLaw(t *testing.T) {
	prov := newMockProvider("mock", scripted(
		core.ResponseChunk{Type: "response_chunk", Content: "hel"},
		core.ResponseChunk{Type: "response_chunk", Content: "lo"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	))
	a := core.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil)
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
		if p.Kind != core.PanelKindChat || p.ID != chatPanelID || p.Title != "Chat" {
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
	// The persisted truth, not the live tail: no row streams at exit.
	for _, row := range last.Rows {
		if row.Status == core.ChatStatusStreaming {
			t.Fatalf("exit flush still streams: %+v", last)
		}
	}
}

// TestChatPanelCadenceFlushesTokenBatches: with the cadence ticking,
// a token-dirty run gets its rows pushed without any structural
// event - the batcher owns the between-token deltas.
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
	// the cadence tick carries the batch without any structural event
	deadline := time.Now().Add(3 * time.Second)
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

// TestChatPanelStaleRunNeverPaints: a run whose session lost focus (a
// newer run took the panel over) stops pushing; the focus session
// keeps painting.
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
	// The focus session still paints.
	s.chat.runEvent("sess-second", core.ToolResultEvent{})
	mu.Lock()
	if pushes == after {
		t.Fatal("focus session stopped painting")
	}
	mu.Unlock()
	s.chat.endRun("sess-second")
}

// TestChatActionStopThroughThePipe: the pipe's stop verb cancels the
// run - the same face POST /sessions/{id}/stop serves - and the run's
// exit flush paints the completed state.
func TestChatActionStopThroughThePipe(t *testing.T) {
	gated := newGatedProvider()
	a := core.NewAgent(gated, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.store = st
	s.chat.batch = time.Hour
	recorder := newChatRecorder()
	// The real wiring order: newCanvas registers the capsule (the
	// runtime installs its own closure), then the test overrides the
	// closure to record - one frontend is live per process.
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
	deadline := time.Now().Add(5 * time.Second)
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
		// The start-to-launch window answers "run not started" while
		// the cancel registers - a retry is exactly how a frontend
		// rides that transient.
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

// TestBranchPathCutsTheOldSiblingKeepsRunRows: the pure cut behind the
// in-place retry composition (t_19c100dd) - rows up to and including
// the branch row stay, old rows after it go, and rows above the
// pre-run floor (the run's own mid-stream persisted segments) stay.
func TestBranchPathCutsTheOldSiblingKeepsRunRows(t *testing.T) {
	path := []core.StoredMessage{
		{ID: 1},
		{ID: 2},
		{ID: 3},
	}
	// plain send: no pin, the path renders whole
	if got := branchPath(path, "", 3); len(got) != 3 {
		t.Fatalf("unpinned path = %d rows, want 3", len(got))
	}
	// dangling branch id pins nothing
	if got := branchPath(path, "99", 3); len(got) != 3 {
		t.Fatalf("dangling branch = %d rows, want 3", len(got))
	}
	// malformed branch id pins nothing
	if got := branchPath(path, "zap", 3); len(got) != 3 {
		t.Fatalf("malformed branch = %d rows, want 3", len(got))
	}
	// the retry window: everything after the branch row pre-dates the
	// run - the old sibling's subtree leaves
	if got := branchPath(path, "1", 3); len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("retry cut = %+v, want row 1 only", got)
	}
	// mid-run persisted segments (id > floor) stream into the slot
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

// gateHoldProvider streams one response chunk, then holds the stream
// open until release closes, then ends - the deterministic mid-run
// window the in-place retry test pins the payload in. Every Stream
// call gates the same way, so a detached title call rides the gate
// harmlessly.
type gateHoldProvider struct {
	release chan struct{}
}

func (p *gateHoldProvider) ModelName() string                    { return "gate" }
func (p *gateHoldProvider) SetThinkingLevel(string)              {}
func (p *gateHoldProvider) WithModel(string) ai.Provider         { return p }
func (p *gateHoldProvider) WithThinkingLevel(string) ai.Provider { return p }
func (p *gateHoldProvider) ListModels() ([]string, error)        { return []string{"gate"}, nil }
func (p *gateHoldProvider) ModelInfo() (ai.ModelInfo, error)     { return ai.ModelInfo{}, nil }

func (p *gateHoldProvider) Stream(context.Context, []core.Message, []ai.ToolSchema) <-chan core.ProviderStreamEvent {
	events := make(chan core.ProviderStreamEvent)
	go func() {
		defer close(events)
		events <- core.ResponseChunk{Type: "response_chunk", Content: "fresh"}
		<-p.release
		events <- core.StreamEnd{Type: "stream_end", FinishReason: "stop"}
	}()
	return events
}

// TestRetryRendersInPlaceMidRun (t_19c100dd, Endo ruling 1): while a
// retry streams, the panel payload carries the old response NOWHERE -
// the fresh answer streams into the old row's place. The exit keeps
// one assistant row (the fresh sibling active), the transcript
// endpoint's reattach merge agrees, and the store still holds the old
// answer as a selectable sibling.
func TestRetryRendersInPlaceMidRun(t *testing.T) {
	prov := &gateHoldProvider{release: make(chan struct{})}
	a := core.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil)
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

	// one complete exchange, seeded straight into the store (legacy
	// adjacency rows): user + the answer the retry will re-run.
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
	// idle panel: the exchange renders whole
	idle := chatPanelPayload(t, s.chat.buildPanel("s-ip", false))
	if got := chatRowRoles(idle.Rows); strings.Join(got, ",") != "user,assistant" {
		t.Fatalf("idle rows = %v", got)
	}
	if idle.Rows[1].Blocks[0].Content != "first answer" {
		t.Fatalf("idle assistant = %q", idle.Rows[1].Blocks[0].Content)
	}

	// retry through the pipe
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

	// mid-run: the fresh chunk is in the open tail and the OLD answer
	// is gone from the payload - the retry streams in place
	deadline := time.Now().Add(5 * time.Second)
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

	// the transcript endpoint's reattach merge agrees: a mid-retry
	// refresh renders the fresh stream in the old row's place
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

	// release: the run completes, the exit paints the persisted truth
	close(prov.release)
	waitRunQuiescent(t, s, "s-ip")
	final := chatPanelPayload(t, s.chat.buildPanel("s-ip", false))
	if got := chatRowRoles(final.Rows); strings.Join(got, ",") != "user,assistant" {
		t.Fatalf("final rows = %v, got payload %+v", got, final)
	}
	if final.Rows[1].Blocks[0].Content != "fresh" || final.Rows[1].Status != core.ChatStatusComplete {
		t.Fatalf("final assistant = %+v", final.Rows[1])
	}
	// the old answer survived as a selectable sibling, not as a row
	view, err := st.GetActiveView(ctx, "s-ip")
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if len(view.Branches) != 1 || view.Branches[0].Count != 2 {
		t.Fatalf("branches after retry = %+v, want one point with 2 siblings", view.Branches)
	}
}

// TestChatActionRetryThroughThePipe: the pipe's retry verb replays
// the session's last user turn as a sibling variant, detached - the
// same variant hop /chat's retry body drives.
func TestChatActionRetryThroughThePipe(t *testing.T) {
	a := core.NewAgent(newMockProvider("mock", scripted(
		core.ResponseChunk{Type: "response_chunk", Content: "first answer"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)), nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil)
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

	// One complete exchange: user + assistant on the active path.
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
	// The retry run is live - OR already finished. The run slot is
	// registered synchronously inside the action's 200 (prepareRun's
	// runs.start), so "not live at this instant" has exactly one
	// product meaning: the whole run (dummy provider, near-instant;
	// persist -> finish) completed inside the gap between the action
	// response and this check - a window that widens under runner
	// load, where this test process can be descheduled long past a
	// run's entire lifetime. active() is a transient; the persisted
	// sibling branch is the durable proof the retry launched. Accept
	// either; a single instantaneous check reds ~1 run per nightly.
	deadline := time.Now().Add(5 * time.Second)
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

// TestChatPanelEphemeralRowsFromRegistry: a store-less session
// composes its rows from the registry plus the live tail - the
// conversation the canvas can render even when nothing persists.
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

// postChatGetSessionID posts one /chat send and returns the session
// id from the session frame.
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
	// The run is detached; this client's remaining frames stream while
	// the test lives (ts.Close cuts the wire, the run persists on).
	return f.ID
}

// TestChatDeltaRidesTheRunStream (settled Q1): the chat panel's deltas
// serialize as view_model_delta frames on the same stream the chat
// already rides - no new transport.
func TestChatDeltaRidesTheRunStream(t *testing.T) {
	a := core.NewAgent(newMockProvider("mock", scripted(
		core.ResponseChunk{Type: "response_chunk", Content: "hel"},
		core.ResponseChunk{Type: "response_chunk", Content: "lo"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)), nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.store = st
	// Cadence OFF: every token flushes - deltas flow while the stream
	// is open, so the frames are guaranteed inside its lifetime.
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
		if delta.Panel.Kind != core.PanelKindChat || delta.Panel.ID != chatPanelID {
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

// nextFrameW is nextFrame's non-fatal variant: the caller breaks at
// the first interesting frame, and a live stream's tail (closed early)
// is just ok=false - not a wedge.
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

// gateStore arms ONCE: the first GetActiveView after arm blocks until
// release - the chat panel's build composes through this store, so the
// test pins a batcher tick inside its build window exactly, and fires
// endRun there (t_877f4bce's race window, made deterministic).
type gateStore struct {
	core.StoreProvider
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

// TestBatcherPushCannotOutliveTheRun (t_877f4bce, audit gate): a
// batcher tick past takeDirty builds for ms-scale with no liveness
// check; endRun fires inside that window and the stale running=true
// push lands after the exit push - the canvas then serves a panel that
// claims the session streams forever, and the webui reattach busy-gate
// blocks the composer until an unrelated push heals it. The tick and
// the exit flush now serialize under the capsule's paint lock, so the
// exit push is last: the recorded deltas end running=false and the
// canvas snapshot agrees.
func TestBatcherPushCannotOutliveTheRun(t *testing.T) {
	prov := newMockProvider("mock", scripted(
		core.ResponseChunk{Type: "response_chunk", Content: "hel"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	))
	a := core.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	gated := &gateStore{StoreProvider: st}
	s := NewServer(a, nil, gated, nil, nil)
	s.chat.batch = 20 * time.Millisecond
	// Real wiring order: the canvas registers the capsule, the test
	// overrides the closure to record (one frontend is live).
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

	// One real run streamed to completion behind the gate-unarmed
	// store: persisted truth exists, no run is live from here on.
	sessionID := postChatGetSessionID(t, ts)
	waitRunQuiescent(t, s, sessionID)

	// Stage the doomed interleaving: a batcher session dirty with a
	// pending token batch, pinned inside its build, while endRun fires.
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
	// Old code: the exit push landed here already, and the tick still
	// stuck mid-build pushed running=true after it. New code: endRun
	// holds for the paint lock until the build returns.
	time.Sleep(30 * time.Millisecond)
	closeRelease()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("endRun never returned - the paint lock does not release")
	}

	// Settle past any further tick (3+ cadences), then close the gate.
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
	// The canvas state agrees (GET /ui/canvas's snapshot source).
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

// TestUIActionRetryOnLiveRunIsA409: handleUIActions passes a runError's
// own HTTP status through - a retry against a session with a live run
// answers 409 through the action pipe, the same face POST /chat serves,
// not a flattened 500.
func TestUIActionRetryOnLiveRunIsA409(t *testing.T) {
	prov := &gateHoldProvider{release: make(chan struct{})}
	a := core.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil)
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
	// The run is live (held on the gate) until release closes - the
	// retry below lands while a run holds the slot.
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
