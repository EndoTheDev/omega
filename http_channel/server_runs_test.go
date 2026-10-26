package http_channel

// Detached-run tests: a client disconnect must not cancel the run, a
// subscriber (the reattach surface) must see the live stream, and the
// registry behavior the webui depends on (active flag, 404, 409).
// The mockProvider's scripts stream instantly, which cannot model a run
// still mid-flight - stepProvider streams on the test's schedule.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/store"
)

// stepProvider streams ONLY what the test hands to its next channel, in
// order: each send() batch is forwarded into the in-flight Stream. The
// test therefore controls both the pacing and the completeness of the run.
type stepProvider struct {
	modelName string
	next      chan []core.ProviderStreamEvent
}

func newStepProvider(modelName string) *stepProvider {
	return &stepProvider{modelName: modelName, next: make(chan []core.ProviderStreamEvent)}
}

func (p *stepProvider) ModelName() string { return p.modelName }

func (p *stepProvider) SetThinkingLevel(level string) {}

func (p *stepProvider) WithThinkingLevel(level string) ai.Provider {
	cp := *p
	return &cp
}

func (p *stepProvider) WithModel(model string) ai.Provider {
	cp := *p
	cp.modelName = model
	return &cp
}

func (p *stepProvider) ListModels() ([]string, error) {
	return []string{p.modelName}, nil
}

func (p *stepProvider) ModelInfo() (ai.ModelInfo, error) {
	return ai.ModelInfo{}, nil
}

func (p *stepProvider) Stream(_ context.Context, _ []core.Message, _ []ai.ToolSchema) <-chan core.ProviderStreamEvent {
	events := make(chan core.ProviderStreamEvent)
	go func() {
		defer close(events)
		for batch := range p.next {
			for _, e := range batch {
				events <- e
			}
		}
	}()
	return events
}

func (p *stepProvider) send(batch ...core.ProviderStreamEvent) {
	p.next <- batch
}

// done closes the script channel: the stream ends without a stop
// StreamEnd, so the running turn simply produces nothing more.
func (p *stepProvider) done() {
	close(p.next)
}

// finalStep batches the turn-closing tail: the response chunk plus the
// stop StreamEnd that testLoop answers with the assistant/turn_end/agent_end
// sequence.
func finalStep(content string) []core.ProviderStreamEvent {
	return []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response_chunk", Content: content},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
}

// newStepServer wires agent + store over a stepProvider and starts a
// live httptest server (real connections - the recorder cannot model a
// client dropping mid-stream).
func newStepServer(t *testing.T) (*Server, *stepProvider, *store.Store, *httptest.Server) {
	t.Helper()
	prov := newStepProvider("step")
	a := core.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := NewServer(a, nil, st, nil, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, prov, st, ts
}

// muteTitles parks a session at title state 2, so the /chat handler's
// provisional and final title runs never fire. They share the step
// provider: a title run would compete for the test's scripted batches
// and deadlock the run mid-flight.
func muteTitles(t *testing.T, st *store.Store, id string) {
	t.Helper()
	if err := st.SetSessionTitle(context.Background(), id, "", true); err != nil {
		t.Fatalf("mute titles: %v", err)
	}
}

// createSession POSTs /sessions with the given id.
func createSession(t *testing.T, ts *httptest.Server, id string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"id": id})
	resp, err := http.Post(ts.URL+"/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session status = %d, want 201", resp.StatusCode)
	}
}

// postChatStream posts a /chat with a cancellable context. The returned
// builder receives every line read from the stream so callers can assert
// on what arrived before the disconnect.
func postChatStream(t *testing.T, ts *httptest.Server, sessionID string) (*http.Response, context.CancelFunc, *bufio.Scanner) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
		"session_id": sessionID,
	})
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/chat", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new chat request: %v", err)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		cancel()
		t.Fatalf("chat request: %v", err)
	}
	return resp, cancel, bufio.NewScanner(resp.Body)
}

// readFrames drains the scanner until `want` SSE frames of the named
// event type have been seen, returning the lines read. Fails the test if
// the stream ends first.
func readFrames(t *testing.T, sc *bufio.Scanner, want string, n int) []string {
	t.Helper()
	var lines []string
	events := 0
	for sc.Scan() {
		line := sc.Text()
		lines = append(lines, line)
		if line == "event: "+want {
			events++
			if events == n {
				return lines
			}
		}
	}
	t.Fatalf("stream ended early: saw %d/%d %q frames (lines so far: %v)", events, n, want, lines)
	return nil
}

// waitFor polls cond until it passes or the deadline lapses.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// detachClient cuts the client side of a streaming POST: cancel the
// request context (tears the connection down server-side) and close the
// body.
func detachClient(cancel context.CancelFunc, resp *http.Response) {
	cancel()
	resp.Body.Close()
}

// sessionMessages fetches the persisted history and returns assistant
// content strings in order.
func sessionMessages(t *testing.T, ts *httptest.Server, id string) []string {
	t.Helper()
	resp, err := http.Get(ts.URL + "/sessions/" + id)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get session status = %d, want 200", resp.StatusCode)
	}
	var detail struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	var out []string
	for _, m := range detail.Messages {
		if m.Role == "assistant" && m.Content != "" {
			out = append(out, m.Content)
		}
	}
	return out
}

// TestDetachedRunSurvivesClientDisconnect is the core refresh scenario:
// the /chat client drops mid-stream, the run keeps going, the reattach
// subscriber receives the rest of the stream, and everything persists.
func TestDetachedRunSurvivesClientDisconnect(t *testing.T) {
	s, prov, st, ts := newStepServer(t)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	resp, cancel, sc := postChatStream(t, ts, "s1")
	defer cancel()

	// First step lands on the wire, run is mid-flight.
	prov.send(core.ResponseChunk{Type: "response_chunk", Content: "a"})
	readFrames(t, sc, "response_chunk", 1)
	waitFor(t, "run registered active", func() bool { return s.runs.active("s1") })

	// Drop the client. The RUN must not notice.
	detachClient(cancel, resp)
	waitFor(t, "request context released", func() bool { return true })
	time.Sleep(50 * time.Millisecond)
	if !s.runs.active("s1") {
		t.Fatal("run ended after client disconnect - run is still coupled to the request")
	}

	// A fresh subscriber (the reattached page) receives the remainder.
	sseResp, err := http.Get(ts.URL + "/sessions/s1/events")
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	sseScanner := bufio.NewScanner(sseResp.Body)
	defer sseResp.Body.Close()

	// Finish the run with a second subscriber attached. done() closes the
	// provider stream: the loop's for-range must end or the run never
	// deregisters (agent_end alone does not end it - same as the real loop).
	prov.send(finalStep("b")...)
	prov.done()
	lines := readFrames(t, sseScanner, "agent_end", 1)
	if !containsFrame(lines, "response_chunk", "b") {
		t.Fatalf("reattached stream never saw the second chunk; frames: %v", lines)
	}
	// Clean close: nothing but the agent_end frame's own data line after it.
	for sseScanner.Scan() {
		if line := sseScanner.Text(); strings.HasPrefix(line, "event: ") {
			t.Fatalf("unexpected frame after agent_end: %q", line)
		}
	}

	// The run deregisters once persistence lands...
	waitFor(t, "run deregistered after finish", func() bool { return !s.runs.active("s1") })
	// ...and the run with ZERO attached clients at send-time still
	// persisted the whole turn ("a" streamed + "b" streamed after attach).
	msgs := sessionMessages(t, ts, "s1")
	if len(msgs) != 1 || msgs[0] != "ab" {
		t.Fatalf("persisted assistant = %q, want [ab]", msgs)
	}

	// The events surface no longer serves the finished run.
	resp404, err := http.Get(ts.URL + "/sessions/s1/events")
	if err != nil {
		t.Fatalf("events after finish: %v", err)
	}
	defer resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("events after finish status = %d, want 404", resp404.StatusCode)
	}
}

func containsFrame(lines []string, event, content string) bool {
	for i := 0; i+1 < len(lines); i++ {
		if lines[i] == "event: "+event && strings.Contains(lines[i+1], content) {
			return true
		}
	}
	return false
}

// TestZeroClientsRunCompletes: nobody ever reads the stream - the client
// drops before the first frame - and the run still completes and
// persists everything (segmented persistence unchanged).
func TestZeroClientsRunCompletes(t *testing.T) {
	s, prov, st, ts := newStepServer(t)
	createSession(t, ts, "s2")
	muteTitles(t, st, "s2")

	resp, cancel, _ := postChatStream(t, ts, "s2")
	// Drop before any streaming, then finish the script.
	// The run must not see the disconnect as an end signal.
	waitFor(t, "run registered active", func() bool { return s.runs.active("s2") })
	resp.Body.Close()
	cancel()

	prov.send(finalStep("all by my lonesome")...)
	prov.done()
	waitFor(t, "run finished", func() bool { return !s.runs.active("s2") })

	msgs := sessionMessages(t, ts, "s2")
	if len(msgs) != 1 || msgs[0] != "all by my lonesome" {
		t.Fatalf("persisted assistant = %q, want the full run content", msgs)
	}
}

// TestSessionEvents404: the events surface answers 404 for a session
// that has never run and 405 for non-GET.
func TestSessionEvents404(t *testing.T) {
	_, _, st0, ts := newStepServer(t)
	createSession(t, ts, "sx")
	muteTitles(t, st0, "sx")

	resp, err := http.Get(ts.URL + "/sessions/sx/events")
	if err != nil {
		t.Fatalf("events for idle session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("events for idle session status = %d, want 404", resp.StatusCode)
	}

	pr, _ := http.NewRequest(http.MethodPost, ts.URL+"/sessions/sx/events", nil)
	resp2, err := (&http.Client{}).Do(pr)
	if err != nil {
		t.Fatalf("post events: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST events status = %d, want 405", resp2.StatusCode)
	}
}

// TestDetachedRunConflict: a second /chat for a session with a live run
// is a 409, not a doubled run (the server-side backstop to the webui's
// per-session busy gate).
func TestDetachedRunConflict(t *testing.T) {
	s, prov, st3, ts := newStepServer(t)
	createSession(t, ts, "s3")
	muteTitles(t, st3, "s3")

	resp, cancel, sc := postChatStream(t, ts, "s3")
	defer cancel()
	prov.send(core.ResponseChunk{Type: "response_chunk", Content: "busy"})
	readFrames(t, sc, "response_chunk", 1)
	waitFor(t, "run registered active", func() bool { return s.runs.active("s3") })

	body, _ := json.Marshal(map[string]any{
		"messages":   []map[string]any{{"role": "user", "content": "again"}},
		"session_id": "s3",
	})
	resp2, err := http.Post(ts.URL+"/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("conflict request: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("double chat status = %d, want 409", resp2.StatusCode)
	}

	// The 409 must not persist anything: the slot gate runs BEFORE
	// loadSessionMessages, so the rejected user message never lands -
	// the next successful send would otherwise double it.
	rows := fetchList(t, ts.URL+"/sessions")
	for _, row := range rows {
		if row["id"] == "s3" {
			if row["message_count"] != float64(1) {
				t.Fatalf("409'd chat leaked a message: message_count = %v, want 1", row["message_count"])
			}
			break
		}
	}

	// unwind
	prov.done()
	waitFor(t, "run finished", func() bool { return !s.runs.active("s3") })
	_ = resp.Body.Close()
}

// TestSessionsListRunningFlag: the list and detail surfaces report the
// server-owned running flag the webui pulse reconciles against - and
// the list row carries it ALWAYS (running:false too, not omitted), so
// a false absence can never be read as "unknown".
func TestSessionsListRunningFlag(t *testing.T) {
	s, prov, st4, ts := newStepServer(t)
	createSession(t, ts, "s4")
	muteTitles(t, st4, "s4")

	// Before any run: the row explicitly reports running:false.
	rows := fetchList(t, ts.URL+"/sessions")
	assertRunning(t, rows, "s4", false, "list idle")
	if _, ok := rows[0]["running"]; !ok {
		t.Fatal("list idle: running key absent - the row must carry it explicitly")
	}

	resp, cancel, sc := postChatStream(t, ts, "s4")
	defer cancel()
	prov.send(core.ResponseChunk{Type: "response_chunk", Content: "live"})
	readFrames(t, sc, "response_chunk", 1)
	waitFor(t, "run registered active", func() bool { return s.runs.active("s4") })

	// List row running == true mid-run.
	rows = fetchList(t, ts.URL+"/sessions")
	assertRunning(t, rows, "s4", true, "list mid-run")
	// Detail running == true mid-run.
	detail := fetchDetail(t, ts.URL+"/sessions/s4")
	if !detail["running"].(bool) {
		t.Fatal("detail mid-run: running = false, want true")
	}

	prov.send(finalStep("done")...)
	prov.done()
	waitFor(t, "run finished", func() bool { return !s.runs.active("s4") })

	rows = fetchList(t, ts.URL+"/sessions")
	assertRunning(t, rows, "s4", false, "list after finish")
	detail = fetchDetail(t, ts.URL+"/sessions/s4")
	if detail["running"].(bool) {
		t.Fatal("detail after finish: running = true, want false")
	}
	_ = resp.Body.Close()
}

func fetchList(t *testing.T, url string) []map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return rows
}

func assertRunning(t *testing.T, rows []map[string]any, id string, want bool, where string) {
	t.Helper()
	for _, row := range rows {
		if row["id"] == id {
			got, _ := row["running"].(bool)
			if got != want {
				t.Fatalf("%s: running = %v, want %v", where, got, want)
			}
			return
		}
	}
	t.Fatalf("%s: session %s missing from list", where, id)
}

func fetchDetail(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	return m
}

// hangLoop mirrors the real loop's timeout path (modules/agent_loop):
// turn one streams a tool call and executes it with the run ctx (the
// hang tool blocks there), and every turn boundary checks ctx.Err()
// first - so cancelling the run ctx ends the run with a cancelled
// agent_end, exactly as the real loop unwinds.
type hangLoop struct{}

func (hangLoop) Run(ctx context.Context, opts core.LoopOptions) error {
	opts.Events <- core.AgentStart{Type: "agent_start"}
	turns := 0
	for {
		if ctx.Err() != nil {
			opts.Events <- core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "cancelled", Error: ctx.Err().Error()}
			return nil
		}
		turns++
		opts.Events <- core.TurnStart{Type: "turn_start", Turn: turns}
		if turns > 1 {
			// The tool returned without a deadline hit (cannot happen
			// with hangTool, but the shape stays loop-honest).
			opts.Events <- core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "stop"}
			return nil
		}
		call := core.ToolCall{ID: "c1", Name: "hang"}
		opts.Events <- core.StreamEvent{Event: core.ToolCallEvent{Type: "tool_call", ToolCall: call}}
		opts.Events <- core.StreamEvent{Event: core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"}}
		result, err := opts.Tools["hang"].Run(ctx, call.Arguments)
		if err != nil {
			opts.Events <- core.ToolResultEvent{Type: "tool_result", Message: core.NewToolResult(err.Error(), call.ID, true)}
		} else {
			opts.Events <- core.ToolResultEvent{Type: "tool_result", Message: core.NewToolResult(result, call.ID, false)}
		}
		opts.Events <- core.TurnEnd{Type: "turn_end", Turn: turns, ToolCalls: 1}
	}
}

// TestDetachedRunTimeoutUnbricksSession: a run blocked in a hung tool
// call (the shell-command-that-never-returns case) must die at the
// wall-clock cap - agent_end, an error frame naming the timeout,
// deregistration - and the session accepts a new /chat afterwards. The
// 409 double-run backstop (TestDetachedRunConflict) must stay separate
// and green: it covers the LIVE run, this covers the runaway one.
func TestDetachedRunTimeoutUnbricksSession(t *testing.T) {
	prov := newStepProvider("step")
	a := core.NewAgent(prov, nil, 0)
	a.SetLoopProvider(hangLoop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := NewServer(a, map[string]core.Tool{
		"hang": {
			Description: "blocks until its context ends",
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				<-ctx.Done()
				return "", ctx.Err()
			},
		},
	}, st, nil, nil)
	s.runTimeout = 250 * time.Millisecond
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	createSession(t, ts, "t1")
	muteTitles(t, st, "t1")

	// The run blocks in the hang tool; nothing from the provider arrives.
	resp, cancel, sc := postChatStream(t, ts, "t1")
	defer cancel()
	defer resp.Body.Close()
	waitFor(t, "run registered active", func() bool { return s.runs.active("t1") })

	// The wall-clock cap ends the run through the normal path: agent_end.
	readFrames(t, sc, "agent_end", 1)

	// The error frame naming the timeout follows (it is emitted after
	// the event loop drains, before persistence).
	sawTimeout := false
	for sc.Scan() {
		if strings.Contains(sc.Text(), "run timeout") {
			sawTimeout = true
			break
		}
	}
	if !sawTimeout {
		t.Fatal("no error frame naming the timeout after agent_end")
	}

	// The hub lets go: no more 409 for the session.
	waitFor(t, "run deregistered after timeout", func() bool { return !s.runs.active("t1") })

	body, _ := json.Marshal(map[string]any{
		"messages":   []map[string]any{{"role": "user", "content": "again"}},
		"session_id": "t1",
	})
	resp2, err := http.Post(ts.URL+"/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post-chat after timeout: %v", err)
	}
	defer resp2.Body.Close()
	defer cancel()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("post-chat after timeout status = %d, want 200 (got %s)", resp2.StatusCode, resp2.Status)
	}

	// The replacement run also runs under the cap and lets go again.
	waitFor(t, "replacement run deregistered", func() bool { return !s.runs.active("t1") })
}

// stopHangTool is the hang tool the stop tests block in: it fires
// started() on entry (the "tool is running" signal), then parks until
// its context ends - the exact shape a cancelled long shell call takes.
func stopHangTool(started func()) core.Tool {
	return core.Tool{
		Description: "blocks until its context ends",
		Run: func(ctx context.Context, _ map[string]any) (string, error) {
			if started != nil {
				started()
			}
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
}

// scriptedStopLoop mirrors the real loop's parked-tool shape: turn one
// streams partial content, calls the hang tool (executed with the RUN
// ctx - the loop's own convention), and the turn-boundary ctx check
// produces the cancelled agent_end. The cancel parks in the tool.
type scriptedStopLoop struct{ started func() }

func (l scriptedStopLoop) Run(ctx context.Context, opts core.LoopOptions) error {
	opts.Events <- core.AgentStart{Type: "agent_start"}
	opts.Events <- core.TurnStart{Type: "turn_start", Turn: 1}
	opts.Events <- core.StreamEvent{Event: core.ResponseChunk{Type: "response_chunk", Content: "par"}}
	opts.Events <- core.StreamEvent{Event: core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "hang"}}}
	opts.Events <- core.StreamEvent{Event: core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"}}
	// Mirror the real loop: the assistant persists before the tool
	// runs, and both survive the cancelled ctx (WithoutCancel law).
	if sink := agent.RunTurnSink(ctx); sink != nil {
		_ = sink.AppendTurnMessage(context.WithoutCancel(ctx), core.NewAssistant("par"))
	}
	if l.started != nil {
		l.started()
	}
	result, err := opts.Tools["hang"].Run(ctx, nil)
	if err != nil {
		if sink := agent.RunTurnSink(ctx); sink != nil {
			_ = sink.AppendTurnMessage(context.WithoutCancel(ctx), core.NewToolResult(err.Error(), "c1", true))
		}
		opts.Events <- core.ToolResultEvent{Type: "tool_result", Message: core.NewToolResult(err.Error(), "c1", true)}
	} else {
		if sink := agent.RunTurnSink(ctx); sink != nil {
			_ = sink.AppendTurnMessage(context.WithoutCancel(ctx), core.NewToolResult(result, "c1", false))
		}
		opts.Events <- core.ToolResultEvent{Type: "tool_result", Message: core.NewToolResult(result, "c1", false)}
	}
	opts.Events <- core.TurnEnd{Type: "turn_end", Turn: 1, ToolCalls: 1}
	if ctx.Err() != nil {
		opts.Events <- core.AgentEnd{Type: "agent_end", Turns: 1, FinishReason: "cancelled", Error: ctx.Err().Error()}
		return nil
	}
	opts.Events <- core.AgentEnd{Type: "agent_end", Turns: 1, FinishReason: "stop"}
	return nil
}

// TestDetachedRunStopCancelsLiveRun is the cancel scenario card
// t_6c97a95a settles: a run streaming its first partial content, parked
// in a blocked tool, receives POST /sessions/{id}/stop - the run's own
// ctx cancels, the loop unwinds through its NORMAL path (agent_end with
// finish_reason "cancelled", no extra error frame), the partial output
// persists, the hub slot frees, and the session takes a new /chat.
func TestDetachedRunStopCancelsLiveRun(t *testing.T) {
	toolStarted := make(chan struct{})
	prov := newStepProvider("step")
	a := core.NewAgent(prov, nil, 0)
	a.SetLoopProvider(scriptedStopLoop{started: sync.OnceFunc(func() { close(toolStarted) })})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := NewServer(a, map[string]core.Tool{"hang": stopHangTool(nil)}, st, nil, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	createSession(t, ts, "t2")
	muteTitles(t, st, "t2")

	resp, cancel, sc := postChatStream(t, ts, "t2")
	defer cancel()
	defer resp.Body.Close()

	readFrames(t, sc, "response_chunk", 1)
	waitFor(t, "run registered active", func() bool { return s.runs.active("t2") })

	// The loop parks in the hang tool; the started signal proves it.
	// A stop before it lands ends the run at the turn boundary
	// instead - same unwind either way.
	<-toolStarted

	// Cancel.
	stopResp, err := http.Post(ts.URL+"/sessions/t2/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("stop request: %v", err)
	}
	stopResp.Body.Close()
	if stopResp.StatusCode != http.StatusOK {
		t.Fatalf("stop status = %d, want 200", stopResp.StatusCode)
	}

	// The unwind is the normal one: agent_end with finish_reason
	// "cancelled", carrying "context canceled" - NOT the deadline error.
	lines := readFrames(t, sc, "agent_end", 1)
	// readFrames returns at the "event: agent_end" line; the frame's
	// data line is the next scan - grab it for the assertions below.
	if sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if !containsFrame(lines, "agent_end", `"cancelled"`) {
		t.Fatalf("no cancelled agent_end; frames: %v", lines)
	}
	if containsFrame(lines, "agent_end", "context deadline exceeded") {
		t.Fatalf("agent_end claims a deadline; frames: %v", lines)
	}
	if containsFrame(lines, "error", "run timeout") {
		t.Fatalf("cancel must not ride the timeout error frame; frames: %v", lines)
	}

	// The hub lets go: the freed slot proves the unwinding deregistered.
	// finish runs after persistRun (launchRun's defers), so this wait is
	// also the persistence barrier: reading earlier races the detached
	// store writes - the parent-pointer resolution (t_17365247) slowed
	// the persist enough to lose that race under -race, deterministically.
	waitFor(t, "run deregistered after stop", func() bool { return !s.runs.active("t2") })

	// What streamed before the cancel persists: the partial chunk. The
	// errored tool call persists with it (same as a timeout unwind).
	msgs := sessionMessages(t, ts, "t2")
	if len(msgs) < 1 || msgs[0] != "par" {
		t.Fatalf("persisted assistant = %q, want [par] first", msgs)
	}

	// The freed slot takes a new run - no stale 409. The replacement
	// run parks in the same hang tool, so the cap is tightened first:
	// it self-completes through the normal timeout unwind and frees the
	// slot again on its own.
	s.runTimeout = 400 * time.Millisecond
	resp2, cancel2, _ := postChatStream(t, ts, "t2")
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second chat status = %d, want 200", resp2.StatusCode)
	}
	_ = resp2.Body.Close()
	cancel2()
	waitFor(t, "replacement run deregistered", func() bool { return !s.runs.active("t2") })

	// A stop after everything calms down: 404, the run is gone.
	stop2, err := http.Post(ts.URL+"/sessions/t2/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("post-unwind stop: %v", err)
	}
	defer stop2.Body.Close()
	if stop2.StatusCode != http.StatusNotFound {
		t.Fatalf("post-unwind stop status = %d, want 404", stop2.StatusCode)
	}
}

// TestDetachedRunStopIdleNotFound: stop on an idle session is a clean
// 404, and non-POST is a 405.
func TestDetachedRunStopIdleNotFound(t *testing.T) {
	_, _, st5, ts := newStepServer(t)
	createSession(t, ts, "t5")
	muteTitles(t, st5, "t5")

	resp, err := http.Post(ts.URL+"/sessions/t5/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("stop request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("stop on idle session status = %d, want 404", resp.StatusCode)
	}

	get, err := http.Get(ts.URL + "/sessions/t5/stop")
	if err != nil {
		t.Fatalf("get stop: %v", err)
	}
	defer get.Body.Close()
	if get.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET stop status = %d, want 405", get.StatusCode)
	}
}

// TestRunHubStopNotStartedConflict: a run registered in the hub but not
// yet launched (the start->launchRun window) exists without a cancel
// func - the stop endpoint must say 409, not silently no-op.
func TestRunHubStopNotStartedConflict(t *testing.T) {
	s, _, st6, ts := newStepServer(t)
	createSession(t, ts, "t6")
	muteTitles(t, st6, "t6")

	run, ok := s.runs.start("t6")
	if !ok {
		t.Fatal("hub start")
	}
	resp, err := http.Post(ts.URL+"/sessions/t6/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("stop request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stop on unlaunched run status = %d, want 409", resp.StatusCode)
	}
	s.runs.finish(run)
}

// TestRunHubCutSlowSubscriber: a subscriber that stops reading fills its
// queue and gets cut instead of stalling the run - the run keeps serving
// other subscribers and finishes (the webui renders a cut stream as an
// interrupted stream: honest, and it must not matter to the run).
func TestRunHubCutSlowSubscriber(t *testing.T) {
	var h runHub
	run, ok := h.start("cut")
	if !ok {
		t.Fatal("start run")
	}
	slow := h.subscribe(run)

	// Overrun the slow subscriber's buffer: the (subscriberBuffer+1)-th
	// frame has no room left, so the hub CUTS the channel. A cut channel
	// still drains its buffered frames before reading reports closed.
	for i := 0; i < subscriberBuffer+1; i++ {
		h.broadcast(run, runFrame{event: "x", data: []byte("1")})
	}
	for {
		_, open := <-slow
		if !open {
			break
		}
	}

	// The cut must not have ended the run: a FRESH subscriber (a page
	// reattaching after the dead one was cut) attaches and receives.
	fresh := h.subscribe(run)
	h.broadcast(run, runFrame{event: "y", data: []byte("2")})
	select {
	case f, ok := <-fresh:
		if !ok || f.event != "y" {
			t.Fatalf("fresh subscriber got %+v (open=%v)", f, ok)
		}
	default:
		t.Fatal("fresh subscriber never received the post-cut frame")
	}

	h.finish(run)
	h.unsubscribe(run, fresh) // double-close guard: finish already closed it
	// unsubscribe on an unknown channel must not panic
	h.unsubscribe(run, make(chan runFrame))
}
