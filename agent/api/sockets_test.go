package api

// Tests for the push channel's hub.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// recvFrame reads one pushed frame, failing the test on timeout.
func recvFrame(t *testing.T, sub *SocketSub) SocketMessage {
	t.Helper()
	select {
	case raw, ok := <-sub.Messages():
		if !ok {
			t.Fatal("subscriber channel closed, want a frame")
		}
		var msg SocketMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("no frame within 2s")
		return SocketMessage{}
	}
}

// expectNoFrame asserts the subscriber receives nothing for a short window (the.
func expectNoFrame(t *testing.T, sub *SocketSub) {
	t.Helper()
	select {
	case raw := <-sub.Messages():
		t.Fatalf("unexpected frame: %s", raw)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestSocketHubFanOutToConnections: one publish reaches EVERY attached connection, in.
func TestSocketHubFanOutToConnections(t *testing.T) {
	hub := NewSocketHub()
	subs := []*SocketSub{hub.Attach(), hub.Attach(), hub.Attach()}
	defer func() {
		for _, sub := range subs {
			sub.Close()
		}
	}()

	hub.Publish(TopicHealth, "", HealthState{Status: "ok", UptimeSeconds: 7})

	var lastSeq uint64
	for i, sub := range subs {
		msg := recvFrame(t, sub)
		if msg.Topic != TopicHealth {
			t.Fatalf("conn %d topic = %q, want %q", i, msg.Topic, TopicHealth)
		}
		if msg.Seq != 1 {
			t.Fatalf("conn %d seq = %d, want 1", i, msg.Seq)
		}
		var state HealthState
		if err := json.Unmarshal(msg.Data, &state); err != nil {
			t.Fatalf("conn %d decode data: %v", i, err)
		}
		if state.Status != "ok" || state.UptimeSeconds != 7 {
			t.Fatalf("conn %d payload = %+v, want ok/7", i, state)
		}
		lastSeq = msg.Seq
	}
	if lastSeq != 1 {
		t.Fatalf("last seq = %d, want 1", lastSeq)
	}
}

// TestSocketHubTopicTyping: the envelope carries the topic, the session scope and the
// chat event name.
func TestSocketHubTopicTyping(t *testing.T) {
	hub := NewSocketHub()
	narrow := hub.Attach()
	defer narrow.Close()
	if accepted := narrow.Subscribe([]string{TopicChat, "nope"}); len(accepted) != 1 || accepted[0] != TopicChat {
		t.Fatalf("accepted = %v, want [chat] (unknown names dropped)", accepted)
	}
	wide := hub.Attach()
	defer wide.Close()

	hub.PublishEvent(TopicChat, "sess-1", "response_chunk", []byte(`{"type":"response_chunk","content":"hi"}`))
	hub.PublishEvent(TopicCanvas, "", "view_model_delta", []byte(`{"type":"view_model_delta"}`))

	msg := recvFrame(t, narrow)
	if msg.Topic != TopicChat || msg.Session != "sess-1" || msg.Event != "response_chunk" {
		t.Fatalf("narrow frame = %+v, want chat/sess-1/response_chunk", msg)
	}
	if string(msg.Data) != `{"type":"response_chunk","content":"hi"}` {
		t.Fatalf("chat payload = %s, want the SSE bytes verbatim", msg.Data)
	}
	expectNoFrame(t, narrow) // the canvas frame never reaches a chat-only subscriber

	first := recvFrame(t, wide)
	second := recvFrame(t, wide)
	if first.Topic != TopicChat || second.Topic != TopicCanvas {
		t.Fatalf("wide topics = %q,%q, want chat,canvas", first.Topic, second.Topic)
	}
	if second.Session != "" {
		t.Fatalf("canvas session = %q, want global (empty)", second.Session)
	}
	if second.Seq <= first.Seq {
		t.Fatalf("seq did not advance: %d then %d", first.Seq, second.Seq)
	}
}

// TestSocketHubDeliveryLaw: a stalled subscriber is CUT on an
// authoritative topic (a silent loss would look like state that never
// changed) and only DROPS frames on the lossy chips topic.
func TestSocketHubDeliveryLaw(t *testing.T) {
	hub := NewSocketHub()
	sub := hub.Attach()
	defer sub.Close()

	for i := 0; i < socketSubBuffer+1; i++ {
		hub.Publish(TopicChat, "s", RunState{Active: true})
	}
	for i := 0; i < socketSubBuffer; i++ {
		recvFrame(t, sub)
	}
	select {
	case _, ok := <-sub.Messages():
		if ok {
			t.Fatal("chat overflow must close the subscriber, not keep the wire open")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("overflowed subscriber was not cut")
	}
	if hub.SubscriberCount() != 0 {
		t.Fatalf("subscribers = %d, want 0 (the cut one detached)", hub.SubscriberCount())
	}

	lossy := NewSocketHub()
	chips := lossy.Attach()
	defer chips.Close()
	for i := 0; i < socketSubBuffer*2; i++ {
		lossy.Publish(TopicChips, "s", ChipFrame{Tool: "shell.run", Text: "x"})
	}
	for i := 0; i < socketSubBuffer; i++ {
		if msg := recvFrame(t, chips); msg.Topic != TopicChips {
			t.Fatalf("frame topic = %q, want chips", msg.Topic)
		}
	}
	if lossy.SubscriberCount() != 1 {
		t.Fatalf("subscribers = %d, want the chips overflow to keep the wire", lossy.SubscriberCount())
	}
}

// TestSocketHubCloseIdempotent: both transport loops close on their way
// out, so Close must tolerate a second call.
func TestSocketHubCloseIdempotent(t *testing.T) {
	hub := NewSocketHub()
	sub := hub.Attach()
	sub.Close()
	sub.Close()
	if hub.SubscriberCount() != 0 {
		t.Fatalf("subscribers = %d, want 0", hub.SubscriberCount())
	}
	hub.Publish(TopicChat, "s", RunState{Active: true}) // no panic, no subscriber
}

// TestRunFramesRideChatTopic: the run broadcast seam's push half.
func TestRunFramesRideChatTopic(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	sub := s.Sockets().Attach()
	defer sub.Close()

	run := &activeRun{id: "sess-7", subs: map[chan runFrame]struct{}{}}
	sse := s.runs.subscribe(run)
	defer s.runs.unsubscribe(run, sse)

	payload := []byte(`{"type":"response_chunk","content":"hello"}`)
	s.emitRunFrame(run, runFrame{event: "response_chunk", data: payload})

	frame := <-sse
	if frame.event != "response_chunk" {
		t.Fatalf("sse event = %q, want response_chunk", frame.event)
	}
	msg := recvFrame(t, sub)
	if msg.Topic != TopicChat || msg.Session != "sess-7" || msg.Event != "response_chunk" {
		t.Fatalf("socket frame = %+v, want chat/sess-7/response_chunk", msg)
	}
	if string(msg.Data) != string(payload) {
		t.Fatalf("socket payload = %s, want %s", msg.Data, payload)
	}
}

// TestCanvasEmitPublishesCanvasTopic: a panel delta (notifications toast, board.
func TestCanvasEmitPublishesCanvasTopic(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	sub := s.Sockets().Attach()
	defer sub.Close()

	s.CanvasEmit(core.ViewModelDelta{Type: "view_model_delta", Panel: core.Panel{ID: "board", Title: "Board", Kind: "panel"}})

	msg := recvFrame(t, sub)
	if msg.Topic != TopicCanvas || msg.Event != "view_model_delta" {
		t.Fatalf("frame = %+v, want canvas/view_model_delta", msg)
	}
	var delta core.ViewModelDelta
	if err := json.Unmarshal(msg.Data, &delta); err != nil {
		t.Fatalf("decode delta: %v", err)
	}
	if delta.Panel.ID != "board" {
		t.Fatalf("panel id = %q, want board", delta.Panel.ID)
	}
}

// TestChipTapStreamsToolOutput: the shell's capped buffer feeds the run output tap.
func TestChipTapStreamsToolOutput(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	sub := s.Sockets().Attach()
	defer sub.Close()

	ctx := core.WithRunOutputTap(context.Background(), s.chipTap("sess-3"))
	ctx = core.WithToolCall(ctx, "call-9", "shell.run")
	core.EmitToolOutput(ctx, "building...\n", false)
	core.EmitToolOutput(ctx, "", true)

	chunk := recvFrame(t, sub)
	if chunk.Topic != TopicChips || chunk.Session != "sess-3" {
		t.Fatalf("frame = %+v, want chips/sess-3", chunk)
	}
	var first ChipFrame
	if err := json.Unmarshal(chunk.Data, &first); err != nil {
		t.Fatalf("decode chunk: %v", err)
	}
	if first.CallID != "call-9" || first.Tool != "shell.run" || first.Text != "building...\n" || first.Fill || first.Truncated {
		t.Fatalf("chunk = %+v, want the call's live text, no fill, no marker", first)
	}

	marker := recvFrame(t, sub)
	var second ChipFrame
	if err := json.Unmarshal(marker.Data, &second); err != nil {
		t.Fatalf("decode marker: %v", err)
	}
	if !second.Truncated || second.Text != "" {
		t.Fatalf("marker frame = %+v, want truncated with no text", second)
	}

	// no tap on the context (the CLI loop) is a silent no-op
	core.EmitToolOutput(context.Background(), "unwatched", false)
	expectNoFrame(t, sub)
}

// TestWakeFillPublishesChipFrame: wakeHome is the one settle entry point, so the.
func TestWakeFillPublishesChipFrame(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	sub := s.Sockets().Attach()
	defer sub.Close()

	s.publishChipFill("sess-4", core.YieldResult{
		ToolCallID: "call-2",
		ToolName:   "delegate.task",
		Content:    "done",
		Timing:     &core.Timing{ElapsedMs: 1234},
	})

	msg := recvFrame(t, sub)
	var fill ChipFrame
	if err := json.Unmarshal(msg.Data, &fill); err != nil {
		t.Fatalf("decode fill: %v", err)
	}
	if !fill.Fill || fill.CallID != "call-2" || fill.Content != "done" || fill.ElapsedMs != 1234 {
		t.Fatalf("fill = %+v, want a filled call-2 with its content and elapsed", fill)
	}

	// The hook itself: wakeHome is the settle entry point, and it publishes before.
	s.wakeHome("eph-gone", core.YieldResult{ToolCallID: "call-3", ToolName: "shell.run", Content: "tail"})
	fromWake := recvFrame(t, sub)
	var wakeFill ChipFrame
	if err := json.Unmarshal(fromWake.Data, &wakeFill); err != nil {
		t.Fatalf("decode wake fill: %v", err)
	}
	if !wakeFill.Fill || wakeFill.CallID != "call-3" || wakeFill.Content != "tail" {
		t.Fatalf("wake fill = %+v, want the settled call-3", wakeFill)
	}
}

// TestRunStateEdges: taking and releasing a run slot publishes the runs topic's two
// edges.
func TestRunStateEdges(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	sub := s.Sockets().Attach()
	defer sub.Close()

	s.publishRunState("sess-5", true)
	s.publishRunState("sess-5", false)

	start := recvFrame(t, sub)
	end := recvFrame(t, sub)
	for i, msg := range []SocketMessage{start, end} {
		if msg.Topic != TopicRuns || msg.Session != "sess-5" {
			t.Fatalf("frame %d = %+v, want runs/sess-5", i, msg)
		}
	}
	var active, idle RunState
	if err := json.Unmarshal(start.Data, &active); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if err := json.Unmarshal(end.Data, &idle); err != nil {
		t.Fatalf("decode end: %v", err)
	}
	if !active.Active || idle.Active {
		t.Fatalf("edges = %+v then %+v, want active then idle", active, idle)
	}
}

// TestReleaseEdgePublishesAfterTheSlotFrees pins the ORDER of launchRun's two exit. The
// measurement: a blocking store parks the run goroutine in its persist step - the last
// work between the loop's final frame and the exit defers - and the test then takes the
// SOCKET hub's mutex. While it is held the slot MUST still release: that happens only
// when finish runs before the publish.
func TestReleaseEdgePublishesAfterTheSlotFrees(t *testing.T) {
	const sid = "sess-release-edge-order"

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	pause := &pauseOnPersist{StoreProvider: st, entered: make(chan struct{}), release: make(chan struct{})}

	// Empty script: the run streams no provider events, so what it publishes never.
	prov := agent.NewFakeProviderScripts("fake", nil)
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	s := NewServer(a, nil, pause, nil, nil, nil)

	sub := s.Sockets().Attach()
	defer sub.Close()
	sub.Subscribe([]string{TopicRuns})

	// The take edge, published by prepareRun in the live path.
	s.publishRunState(sid, true)
	if take := recvFrame(t, sub); take.Topic != TopicRuns || take.Session != sid {
		t.Fatalf("take edge = %+v, want runs/%s", take, sid)
	}

	run, ok := s.runs.start(sid)
	if !ok {
		t.Fatalf("run slot for %q was already taken", sid)
	}
	s.launchRun(context.Background(), run, prov, []core.Message{core.User{Content: "hi"}}, nil, chatRequest{SessionID: sid})

	// The run is parked on its persist step.
	select {
	case <-pause.entered:
	case <-time.After(testWaitTimeout):
		t.Fatalf("the run on %q never reached its persist step", sid)
	}

	s.sockets.mu.Lock()
	held := true
	defer func() {
		if held {
			s.sockets.mu.Unlock()
		}
	}()
	close(pause.release)

	// The pinned property: with the release edge unable to reach the wire, the slot. LIFO
	// runs the LAST declaration first, so this holds only when publishRunState is declared
	// before runs.finish - the other order parks the publish, and the slot is then never
	// released.
	deadline := time.Now().Add(testWaitTimeout)
	for s.runs.active(sid) {
		if time.Now().After(deadline) {
			t.Fatalf("the run slot on %q never released while the release edge was parked on the socket hub: the edge published BEFORE the slot freed (the exit defers run LIFO - declare publishRunState before runs.finish)", sid)
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case raw := <-sub.Messages():
		t.Fatalf("release edge published while the test still held the hub: %s", raw)
	default:
	}
	s.sockets.mu.Unlock()
	held = false

	release := recvFrame(t, sub)
	if release.Topic != TopicRuns || release.Session != sid {
		t.Fatalf("release edge = %+v, want runs/%s", release, sid)
	}
	var state RunState
	if err := json.Unmarshal(release.Data, &state); err != nil {
		t.Fatalf("decode release edge: %v", err)
	}
	if state.Active {
		t.Fatalf("release edge carries Active = true, want the idle edge")
	}
}

// pauseOnPersist parks the run's persist step (AddSessionTokens) until the test opens.
type pauseOnPersist struct {
	agent.StoreProvider
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *pauseOnPersist) AddSessionTokens(_ context.Context, _ string, _, _ int) error {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return nil
}

func (p *pauseOnPersist) SetSessionLastRun(_ context.Context, _ string, _ *core.LastRunStats) error {
	return nil
}

// TestSocketTopicsDocumented pins the topic vocabulary to its doc home.
func TestSocketTopicsDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../docs/api.md")
	if err != nil {
		t.Fatalf("read docs/api.md: %v", err)
	}
	text := string(doc)
	for _, want := range []string{"GET /ws"} {
		if !strings.Contains(text, want) {
			t.Fatalf("docs/api.md does not document %q", want)
		}
	}
	for _, topic := range Topics {
		if !strings.Contains(text, "`"+topic+"`") {
			t.Fatalf("docs/api.md does not name topic `%s`", topic)
		}
	}
	arch, err := os.ReadFile("../../docs/architecture.md")
	if err != nil {
		t.Fatalf("read docs/architecture.md: %v", err)
	}
	if !strings.Contains(string(arch), "/ws") {
		t.Fatal("docs/architecture.md does not document the push seam (/ws)")
	}
}

// TestBindSocketHubSharesOneFanOutAcrossBinds pins the multi-bind push seam.
func TestBindSocketHubSharesOneFanOutAcrossBinds(t *testing.T) {
	hub := NewSocketHub()
	sA := NewServer(nil, nil, nil, nil, nil, nil)
	sB := NewServer(nil, nil, nil, nil, nil, nil)
	sA.BindSocketHub(hub)
	sB.BindSocketHub(hub)
	if sA.Sockets() != sB.Sockets() {
		t.Fatalf("bound binds serve different hubs (%p vs %p): one hub cannot be per-bind and shared", sA.Sockets(), sB.Sockets())
	}

	subA := sA.Sockets().Attach()
	defer subA.Close()
	subB := sB.Sockets().Attach()
	defer subB.Close()

	// Published through SERVER B's own hook (the run-state edge every
	// launch takes); both binds' subscribers must see it.
	sB.publishRunState("mb-shared", true)
	for name, sub := range map[string]*SocketSub{"bind A": subA, "bind B": subB} {
		frame := recvFrame(t, sub)
		if frame.Topic != TopicRuns || frame.Session != "mb-shared" {
			t.Fatalf("%s received %+v, want the runs/mb-shared frame published on bind B", name, frame)
		}
		var state RunState
		if err := json.Unmarshal(frame.Data, &state); err != nil {
			t.Fatalf("%s: decode run state: %v", name, err)
		}
		if !state.Active {
			t.Fatalf("%s: run state active = false, want true", name)
		}
	}
}
