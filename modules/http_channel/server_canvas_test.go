package http_channel

// Canvas transport tests (arc 3b, card t_e143f41f): the snapshot GET
// shape, the action pipe's routing and 404s, and the delta riding the
// live chat/reattach streams. Journey-style REST probes are pinned
// here - the same shapes the webui will consume in arc 3c.

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

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/store"
)

// canvasProbeProvider is a minimal PanelProvider stub for transport
// tests: one panel, one module-qualified action, and a capturable
// handler. The module name comes from the action row itself - the
// capsule's name on its own declaration (arc 3a law).
type canvasProbeProvider struct {
	mu      sync.Mutex
	last    *core.ActionCall
	handler func(call core.ActionCall) (any, error)
}

func (p *canvasProbeProvider) Panels() []core.Panel {
	return []core.Panel{{
		ID:      "deck",
		Title:   "The Deck",
		Kind:    "kanban_board",
		Payload: map[string]any{"cards": 1},
		Actions: []core.PanelAction{{ID: "shuffle", Label: "Shuffle", Module: "probe"}},
	}}
}

func (p *canvasProbeProvider) HandleAction(call core.ActionCall) (any, error) {
	p.mu.Lock()
	p.last = &call
	handler := p.handler
	p.mu.Unlock()
	if handler != nil {
		return handler(call)
	}
	return map[string]any{"shuffled": true}, nil
}

func (p *canvasProbeProvider) captured() *core.ActionCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last == nil {
		return nil
	}
	cp := *p.last
	return &cp
}

// canvasServer builds a bare server wired to the probe provider's
// canvas - the snapshot and action tests ride it.
func canvasServer(t *testing.T) (*Server, *httptest2.Server, *canvasProbeProvider) {
	t.Helper()
	probe := &canvasProbeProvider{}
	s := NewServer(nil, nil, nil, nil)
	if err := s.newCanvas(&core.Context{PanelProviders: []core.PanelProvider{probe}}); err != nil {
		t.Fatalf("newCanvas: %v", err)
	}
	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts, probe
}

func TestCanvasSnapshotListsMountedPanels(t *testing.T) {
	_, ts, _ := canvasServer(t)
	res, err := http.Get(ts.URL + "/ui/canvas")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var panels []core.Panel
	if err := json.NewDecoder(res.Body).Decode(&panels); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(panels) != 1 {
		t.Fatalf("panels = %+v, want exactly the probe's deck", panels)
	}
	panel := panels[0]
	if panel.ID != "deck" || panel.Title != "The Deck" || panel.Kind != "kanban_board" {
		t.Fatalf("descriptor = %+v", panel)
	}
	payload, ok := panel.Payload.(map[string]any)
	if !ok || payload["cards"] != float64(1) {
		t.Fatalf("payload = %#v, want cards:1", panel.Payload)
	}
	if len(panel.Actions) != 1 || panel.Actions[0].ID != "shuffle" || panel.Actions[0].Module != "probe" {
		t.Fatalf("actions = %+v, want the module-qualified shuffle", panel.Actions)
	}
}

// Zero mounted providers is a state, not an error: the webui renders
// the empty dock off the same 200.
func TestCanvasSnapshotEmptyWhenNothingMounted(t *testing.T) {
	s := NewServer(nil, nil, nil, nil)
	ts := httptest2.NewServer(s.Handler())
	defer ts.Close()
	res, err := http.Get(ts.URL + "/ui/canvas")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	raw, _ := io.ReadAll(res.Body)
	if got := strings.TrimSpace(string(raw)); got != "[]" {
		t.Fatalf("body = %q, want []", got)
	}
}

func TestCanvasSnapshotMethodGuard(t *testing.T) {
	_, ts, _ := canvasServer(t)
	res, err := postRaw(ts.URL, "/ui/canvas", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /ui/canvas = %d, want 405", res.StatusCode)
	}
}

func TestUIActionsRoutesToCapsuleHandler(t *testing.T) {
	_, ts, probe := canvasServer(t)
	res, err := postRaw(ts.URL, "/ui/actions",
		`{"module":"probe","action":"shuffle","args":{"direction":"left"}}`)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var body struct {
		Result any `json:"result"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m, ok := body.Result.(map[string]any); !ok || m["shuffled"] != true {
		t.Fatalf("result = %#v, want the handler's return value", body.Result)
	}
	call := probe.captured()
	if call == nil || call.Module != "probe" || call.Action != "shuffle" {
		t.Fatalf("routed call = %+v, want probe/shuffle", call)
	}
	// Args ride verbatim - the capsule's handler owns their shape,
	// the pipe only carries the raw message.
	if string(call.Args) != `{"direction":"left"}` {
		t.Fatalf("args = %s, want {\"direction\":\"left\"}", call.Args)
	}
}

// The 404 shapes, by the sentinels: unknown module, known module
// unknown action, and the empty module (journey probe - the webui's
// wiring bugs must read as 404s, not 500s).
func TestUIActions404Shapes(t *testing.T) {
	_, ts, _ := canvasServer(t)
	for _, probe := range []struct {
		body   string
		wants  string
	}{
		{`{"module":"ghost","action":"x"}`, "unknown module"},
		{`{"module":"probe","action":"vanish"}`, "unknown action"},
		{`{"action":"shuffle"}`, "unknown module"},
	} {
		res, err := postRaw(ts.URL, "/ui/actions", probe.body)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("POST %s = %d, want 404", probe.body, res.StatusCode)
		}
		if !strings.Contains(string(raw), probe.wants) {
			t.Fatalf("body = %q, want it to name %q", raw, probe.wants)
		}
	}
	// And nothing mutated: the handler never saw a failed route.
}

// A handler error passes through untouched and reads as a 500 - the
// wire translation of a failed action, distinct from a misrouted one.
func TestUIActionsHandlerError(t *testing.T) {
	_, ts, probe := canvasServer(t)
	probe.mu.Lock()
	probe.handler = func(core.ActionCall) (any, error) {
		return nil, errStub("capsule exploded")
	}
	probe.mu.Unlock()
	res, err := postRaw(ts.URL, "/ui/actions", `{"module":"probe","action":"shuffle"}`)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", res.StatusCode)
	}
	if !strings.Contains(string(raw), "capsule exploded") {
		t.Fatalf("body = %q, want the handler's error", raw)
	}
}

// gatedProvider streams one chunk, then holds the turn until release -
// the mid-run window the delta test injects a panel update into. Every
// Stream call gates (the run and any detached title call alike), so
// frame order on the run's stream never depends on who grabbed call
// index 0.
type gatedProvider struct {
	release chan struct{}
}

func newGatedProvider() *gatedProvider {
	return &gatedProvider{release: make(chan struct{})}
}

func (p *gatedProvider) ModelName() string { return "gated" }
func (p *gatedProvider) SetThinkingLevel(string) {}
func (p *gatedProvider) WithModel(string) ai.Provider   { return p }
func (p *gatedProvider) WithThinkingLevel(string) ai.Provider { return p }
func (p *gatedProvider) ListModels() ([]string, error) { return []string{"gated"}, nil }
func (p *gatedProvider) ModelInfo() (ai.ModelInfo, error) { return ai.ModelInfo{}, nil }

func (p *gatedProvider) Stream(context.Context, []ai.Message, []ai.ToolSchema) <-chan ai.StreamEvent {
	events := make(chan ai.StreamEvent)
	go func() {
		defer close(events)
		events <- ai.ResponseChunk{Type: "response_chunk", Content: "a"}
		<-p.release
		events <- ai.StreamEnd{Type: "stream_end", FinishReason: "stop"}
	}()
	return events
}

func errStub(msg string) error { return &stubError{msg} }

type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }

// gatedCanvasServer wires a store-backed server with the gated
// provider AND the probe canvas, and opens an httptest server. The
// returned release channel unblocks the runs.
func gatedCanvasServer(t *testing.T) (*Server, *httptest2.Server, *gatedProvider, chan struct{}) {
	t.Helper()
	gated := newGatedProvider()
	a := core.NewAgent(gated, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.store = st
	if err := s.newCanvas(&core.Context{PanelProviders: []core.PanelProvider{&canvasProbeProvider{}}}); err != nil {
		t.Fatalf("newCanvas: %v", err)
	}
	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts, gated, gated.release
}

// nextFrame reads one SSE frame off a live stream, with a watchdog - a
// wedged stream fails the test instead of hanging the suite.
func nextFrame(t *testing.T, rd *bufio.Reader) (sseFrame, bool) {
	t.Helper()
	type result struct {
		frame sseFrame
		ok    bool
	}
	done := make(chan result, 1)
	go func() {
		var frame sseFrame
		haveEvent := false
		for {
			raw, err := rd.ReadString('\n')
			line := strings.TrimRight(raw, "\r\n")
			switch {
			case line == "":
				if haveEvent {
					done <- result{frame, true}
					return
				}
				frame = sseFrame{}
				haveEvent = false
			case strings.HasPrefix(line, "event: "):
				frame.event = strings.TrimPrefix(line, "event: ")
				haveEvent = true
			case strings.HasPrefix(line, "data: "):
				frame.data = []byte(strings.TrimPrefix(line, "data: "))
				haveEvent = true
			}
			if err != nil {
				done <- result{frame, false}
				return
			}
		}
	}()
	select {
	case r := <-done:
		return r.frame, r.ok
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for an SSE frame")
		return sseFrame{}, false
	}
}

func expectFrame(t *testing.T, rd *bufio.Reader, event string) sseFrame {
	t.Helper()
	frame, ok := nextFrame(t, rd)
	if !ok {
		t.Fatalf("stream ended while waiting for %s", event)
	}
	if frame.event != event {
		t.Fatalf("event = %s, want %s", frame.event, event)
	}
	return frame
}

// The arc 3b centerpiece journey: a /chat run streams on the existing
// pipe; a panel update lands mid-run as ONE view_model_delta frame on
// that stream, the second subscriber (reattach) sees the same frame,
// and the stream's own event order is untouched by the interleaving.
func TestViewModelDeltaRidesChatStream(t *testing.T) {
	s, ts, _, release := gatedCanvasServer(t)
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "go"}},
	})
	res, err := postRaw(ts.URL, "/chat", string(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("chat status = %d", res.StatusCode)
	}
	origin := bufio.NewReader(res.Body)

	// Auto-created session: the id rides the first frame.
	session := expectFrame(t, origin, "session")
	var id struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(session.data, &id); err != nil {
		t.Fatalf("session frame: %v", err)
	}

	expectFrame(t, origin, "agent_start")
	expectFrame(t, origin, "turn_start")
	expectFrame(t, origin, "response_chunk")

	// The reattach subscriber attaches mid-run: same frame pipe, and
	// headers go out on attach.
	eventsRes, err := http.Get(ts.URL + "/sessions/" + id.ID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer eventsRes.Body.Close()
	if eventsRes.StatusCode != http.StatusOK {
		t.Fatalf("reattach status = %d", eventsRes.StatusCode)
	}
	reattach := bufio.NewReader(eventsRes.Body)

	// Mid-run panel update: the delta must land on BOTH streams.
	if err := s.canvas.Update("probe", core.Panel{ID: "deck", Payload: map[string]any{"cards": 2}}); err != nil {
		t.Fatalf("update: %v", err)
	}
	for _, stream := range []struct {
		name string
		rd   *bufio.Reader
	}{
		{"origin", origin},
		{"reattach", reattach},
	} {
		delta := expectFrame(t, stream.rd, "view_model_delta")
		var vd core.ViewModelDelta
		if err := json.Unmarshal(delta.data, &vd); err != nil {
			t.Fatalf("delta decode: %v", err)
		}
		if vd.Panel.ID != "deck" {
			t.Fatalf("%s: delta panel = %+v", stream.name, vd.Panel)
		}
		if vd.Type != "view_model_delta" {
			t.Fatalf("%s: delta type = %q", stream.name, vd.Type)
		}
		payload, ok := vd.Panel.Payload.(map[string]any)
		if !ok || payload["cards"] != float64(2) {
			t.Fatalf("%s: delta payload = %#v, want cards:2", stream.name, vd.Panel.Payload)
		}
		if len(vd.Panel.Actions) != 1 || vd.Panel.Actions[0].ID != "shuffle" {
			t.Fatalf("%s: delta actions = %+v, want the descriptor's actions restored", stream.name, vd.Panel.Actions)
		}
	}

	// Release the gate: the run unwinds through its normal path and the
	// stream's own event order continues untouched.
	close(release)
	for _, event := range []string{"stream_end", "assistant_message", "turn_end", "agent_end"} {
		expectFrame(t, origin, event)
	}
}

// A delta landing outside any run's lifetime is dropped - the snapshot
// is the catch-up, exactly like missed session history lives in the
// store. Pinned: the Update succeeds, nothing panics on the empty hub,
// and GET /ui/canvas reflects the new state.
func TestCanvasUpdateWithoutRunIsSnapshotCatchUp(t *testing.T) {
	s, ts, _ := canvasServer(t)
	if err := s.canvas.Update("probe", core.Panel{ID: "deck", Payload: map[string]any{"cards": 9}}); err != nil {
		t.Fatalf("update: %v", err)
	}
	res, err := http.Get(ts.URL + "/ui/canvas")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var panels []core.Panel
	if err := json.NewDecoder(res.Body).Decode(&panels); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(panels) != 1 || panels[0].Payload.(map[string]any)["cards"] != float64(9) {
		t.Fatalf("panels = %+v, want the updated payload", panels)
	}
}
