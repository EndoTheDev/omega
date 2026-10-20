package http_channel

// Server-side retry tests (t_357cbe9c): POST /chat {"retry": true}
// re-runs the session's PERSISTED last user message - no new user turn
// is stored (option a, keep-and-append: the previous cancelled/partial
// response stays visible, the fresh response appends below it), the run
// slot's 409 gate still applies, and cancel mid-retry keeps the partial.

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/modules/store"
)

// multiProvider is stepProvider re-armable across runs (t_357cbe9c
// tests need two+ sequential runs on one server): done() closes the
// current script channel, ends the run, and arms fresh for the next -
// each run's Stream holds ITS channel by value at call time.
type multiProvider struct {
	modelName string
	next      chan []ai.StreamEvent
}

func newMultiProvider(modelName string) *multiProvider {
	return &multiProvider{modelName: modelName, next: make(chan []ai.StreamEvent)}
}

func (p *multiProvider) arm() { p.next = make(chan []ai.StreamEvent) }

func (p *multiProvider) ModelName() string { return p.modelName }
func (p *multiProvider) SetThinkingLevel(level string) {}
func (p *multiProvider) WithThinkingLevel(level string) ai.Provider {
	cp := *p
	return &cp
}
func (p *multiProvider) WithModel(model string) ai.Provider {
	cp := *p
	cp.modelName = model
	return &cp
}
func (p *multiProvider) ListModels() ([]string, error) {
	return []string{p.modelName}, nil
}
func (p *multiProvider) ModelInfo() (ai.ModelInfo, error) {
	return ai.ModelInfo{}, nil
}

func (p *multiProvider) Stream(_ context.Context, _ []ai.Message, _ []ai.ToolSchema) <-chan ai.StreamEvent {
	events := make(chan ai.StreamEvent)
	next := p.next
	go func() {
		defer close(events)
		for batch := range next {
			for _, e := range batch {
				events <- e
			}
		}
	}()
	return events
}

func (p *multiProvider) send(batch ...ai.StreamEvent) { p.next <- batch }

// done closes the current script (run ends without a StreamEnd) and
// re-arms for the next run.
func (p *multiProvider) done() {
	close(p.next)
	p.arm()
}

// newMultiServer wires agent + store over a multiProvider (the retry
// tests run several sequential scripted runs on one server).
func newMultiServer(t *testing.T) (*Server, *multiProvider, *store.Store, *httptest.Server) {
	t.Helper()
	prov := newMultiProvider("step")
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := NewServer(a, nil, st, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, prov, st, ts
}

// postRetryBody POSTs an arbitrary body to /chat and returns the
// response (caller closes).
func postRetryBody(t *testing.T, ts *httptest.Server, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(ts.URL+"/chat", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("retry post: %v", err)
	}
	return resp
}

// postRetry is the bare retry body the webui sends.
func postRetry(t *testing.T, ts *httptest.Server, id string) *http.Response {
	t.Helper()
	return postRetryBody(t, ts, fmt.Sprintf(`{"retry": true, "session_id": %q}`, id))
}

// messagesOf fetches the session's messages through the store.
func messagesOf(t *testing.T, st *store.Store, id string) []ai.Message {
	t.Helper()
	msgs, err := st.GetMessages(context.Background(), id)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	return msgs
}

// TestChatRetryAppendsBelowLastUserTurn pins the decided semantics:
// after a completed exchange, {"retry":true} re-runs the last user
// message with NO new user turn persisted and the fresh response
// appended below the previous one.
func TestChatRetryAppendsBelowLastUserTurn(t *testing.T) {
	s, prov, st, ts := newMultiServer(t)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	// first exchange: user "hi" -> "first answer"
	resp, cancel, _ := postChatStream(t, ts, "s1")
	prov.send(finalStep("first answer")...)
	prov.done()
	waitFor(t, "first run finished", func() bool { return !s.runs.active("s1") })
	resp.Body.Close()
	cancel()

	msgs := messagesOf(t, st, "s1")
	if len(msgs) != 2 {
		t.Fatalf("after first exchange len = %d, want 2 (user + assistant)", len(msgs))
	}

	// the retry: no messages on the wire
	resp2 := postRetry(t, ts, "s1")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("retry status = %d, want 200", resp2.StatusCode)
	}
	prov.send(finalStep("second answer")...)
	prov.done()
	waitFor(t, "retry run finished", func() bool { return !s.runs.active("s1") })

	msgs = messagesOf(t, st, "s1")
	if len(msgs) != 3 {
		t.Fatalf("after retry len = %d, want 3 (user + two assistants)", len(msgs))
	}
	if _, ok := msgs[0].(ai.User); !ok {
		t.Fatalf("messages[0] = %T, want ai.User", msgs[0])
	}
	users := 0
	for _, m := range msgs {
		if _, ok := m.(ai.User); ok {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("user turns = %d, want 1 (no duplicate user bubble)", users)
	}
	if msgs[1].(ai.Assistant).Content != "first answer" ||
		msgs[2].(ai.Assistant).Content != "second answer" {
		t.Fatalf("responses = %q/%q, want first answer/second answer (appended, not replaced)",
			msgs[1].(ai.Assistant).Content, msgs[2].(ai.Assistant).Content)
	}
}

// TestChatRetryGatesOnLiveRun: a retry while the session's run slot is
// taken is the same 409 a normal send gets - and it persists nothing.
func TestChatRetryGatesOnLiveRun(t *testing.T) {
	s, prov, st, ts := newMultiServer(t)
	createSession(t, ts, "s9")
	muteTitles(t, st, "s9")

	resp, cancel, sc := postChatStream(t, ts, "s9")
	defer cancel()
	defer resp.Body.Close()
	prov.send(ai.ResponseChunk{Type: "response_chunk", Content: "busy"})
	readFrames(t, sc, "response_chunk", 1)
	waitFor(t, "run registered active", func() bool { return s.runs.active("s9") })

	resp2 := postRetry(t, ts, "s9")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("retry during live run status = %d, want 409", resp2.StatusCode)
	}

	// unwind the live run, then assert nothing extra persisted
	prov.done()
	waitFor(t, "run finished", func() bool { return !s.runs.active("s9") })
	msgs := messagesOf(t, st, "s9")
	if len(msgs) != 2 {
		t.Fatalf("len = %d, want 2 (the live run's user + assistant) - the 409'd retry leaked nothing", len(msgs))
	}
}

// TestChatRetryCancelMidRunKeepsPartial: a cancelled RETRY run persists
// its partial output like any cancelled send, and the transcript then
// reads user - partial - next-response (the prior response stays
// visible - keep-and-append).
func TestChatRetryCancelMidRunKeepsPartial(t *testing.T) {
	s, prov, st, ts := newMultiServer(t)
	createSession(t, ts, "s7")
	muteTitles(t, st, "s7")

	// first exchange completes
	resp, cancel, _ := postChatStream(t, ts, "s7")
	prov.send(finalStep("first")...)
	prov.done()
	waitFor(t, "first run finished", func() bool { return !s.runs.active("s7") })
	resp.Body.Close()
	cancel()

	// the retry streams one chunk, then the run is cancelled mid-flight
	resp2 := postRetry(t, ts, "s7")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("retry status = %d, want 200", resp2.StatusCode)
	}
	prov.send(ai.ResponseChunk{Type: "response_chunk", Content: "kept partial"})
	stop, err := http.Post(ts.URL+"/sessions/s7/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	defer stop.Body.Close()
	if stop.StatusCode != http.StatusOK {
		t.Fatalf("stop status = %d, want 200", stop.StatusCode)
	}
	prov.done() // the run unwinds on the cancelled context
	waitFor(t, "cancelled retry finished", func() bool { return !s.runs.active("s7") })

	msgs := messagesOf(t, st, "s7")
	users := 0
	for _, m := range msgs {
		if _, ok := m.(ai.User); ok {
			users++
		}
	}
	var contents []string
	for _, m := range msgs {
		if a, ok := m.(ai.Assistant); ok && a.Content != "" {
			contents = append(contents, a.Content)
		}
	}
	if users != 1 || len(contents) != 2 || contents[0] != "first" || contents[1] != "kept partial" {
		t.Fatalf("after cancelled retry: users=%d assistants=%v, want 1 user + [first, kept partial]", users, contents)
	}
}

// TestChatRetryGuards: a retry only makes sense for an existing
// session's persisted history - sessionless, message-carrying, and
// user-less-session retries are all rejected BEFORE anything happens
// (and before auto-create creates an empty session to fail into).
func TestChatRetryGuards(t *testing.T) {
	_, _, st, ts := newMultiServer(t)
	if err := st.CreateSession(context.Background(), "s-empty", "", ""); err != nil {
		t.Fatalf("create empty session: %v", err)
	}

	// no session id at all
	resp := postRetry(t, ts, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("sessionless retry status = %d, want 400", resp.StatusCode)
	}

	// messages alongside retry are a client bug - never silently dropped
	if r, err := http.Post(ts.URL+"/chat", "application/json",
		bytes.NewReader([]byte(`{"retry": true, "session_id": "s1", "messages": [{"role": "user", "content": "hi"}]}`))); err != nil {
		t.Fatalf("retry+messages post: %v", err)
	} else if code := r.StatusCode; code != http.StatusBadRequest {
		t.Fatalf("retry+messages status = %d, want 400", code)
	} else {
		r.Body.Close()
	}

	// an existing but EMPTY session has nothing to replay
	if r, err := http.Post(ts.URL+"/chat", "application/json",
		bytes.NewReader([]byte(`{"retry": true, "session_id": "s-empty"}`))); err != nil {
		t.Fatalf("empty-session retry post: %v", err)
	} else if code := r.StatusCode; code != http.StatusBadRequest {
		t.Fatalf("empty-session retry status = %d, want 400", code)
	} else {
		r.Body.Close()
	}
}