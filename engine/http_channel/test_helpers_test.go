package http_channel

// The shell's mock helpers. After the carve (card t_905fa502) the
// agent-surface tests took the original fixtures to agent/api; the
// shell-integration tests (plugin mount wiring, multi-bind, the live
// serve path) stay here and need the same doubles. Test-only
// duplication, same-shaped as agent/api's.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/EndoTheDev/omega/llm"
)

type mockProvider struct {
	modelName string
	scripts   [][]core.ProviderStreamEvent
	mu        *sync.Mutex
	calls     *int
	// thinkingLevel is the ladder value a WithThinkingLevel copy
	// carries; tests assert per-run threading from it.
	thinkingLevel string
	// seenLevels records the thinkingLevel of every Stream call,
	// shared with WithModel/WithThinkingLevel copies behind mu.
	seenLevels *[]string
	// seenModels records the modelName of every Stream call, shared
	// with WithModel copies behind mu (same pattern as seenLevels) so
	// tests can assert which model a call - e.g. the title_model-routed
	// title call - actually streamed through.
	seenModels *[]string
}
type testLoop struct{}
type testToolProvider struct{ tools map[string]core.Tool }

func newMockProvider(modelName string, scripts ...[]core.ProviderStreamEvent) *mockProvider {
	calls := 0
	levels := []string{}
	models := []string{}
	return &mockProvider{
		modelName:  modelName,
		scripts:    scripts,
		mu:         &sync.Mutex{},
		calls:      &calls,
		seenLevels: &levels,
		seenModels: &models,
	}
}
func newTestServer() *Server {
	provider := newMockProvider("mock",
		scripted(
			core.ResponseChunk{Type: "response_chunk", Content: "hello"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		),
	)
	a := core.NewAgent(provider, nil, 0)
	a.SetLoopProvider(testLoop{})
	return NewServer(a, nil, nil, nil, nil, nil)
}
func (m *mockProvider) ModelName() string             { return m.modelName }
func (m *mockProvider) SetThinkingLevel(level string) { m.thinkingLevel = level }
func (m *mockProvider) WithThinkingLevel(level string) llm.Provider {
	cp := *m
	cp.thinkingLevel = level
	return &cp
}
func (m *mockProvider) WithModel(model string) llm.Provider {
	cp := *m
	cp.modelName = model
	return &cp
}
func (m *mockProvider) ListModels() ([]string, error) {
	return []string{"test-model", "other-model"}, nil
}
func (m *mockProvider) ModelInfo() (llm.ModelInfo, error) {
	return llm.ModelInfo{}, nil
}
func (m *mockProvider) Stream(_ context.Context, _ []core.Message, _ []llm.ToolSchema) <-chan core.ProviderStreamEvent {
	m.mu.Lock()
	*m.seenLevels = append(*m.seenLevels, m.thinkingLevel)
	*m.seenModels = append(*m.seenModels, m.modelName)
	m.mu.Unlock()
	events := make(chan core.ProviderStreamEvent)
	go func() {
		defer close(events)
		index := m.nextIndex()
		if index >= len(m.scripts) {
			index = len(m.scripts) - 1
		}
		if index >= 0 {
			for _, e := range m.scripts[index] {
				events <- e
			}
		}
	}()
	return events
}
func (m *mockProvider) nextIndex() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := *m.calls
	*m.calls++
	return index
}
func (m *mockProvider) servedModelNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), *m.seenModels...)
}
func (m *mockProvider) streamCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.calls
}
func (p testToolProvider) Tools() map[string]core.Tool { return p.tools }
func (testLoop) Run(ctx context.Context, opts core.LoopOptions) error {
	sink := agent.RunTurnSink(ctx)
	// Persistence outlives the run ctx - same WithoutCancel law the
	// real loop applies, so a cancelled run still lands its copy.
	persistCtx := context.WithoutCancel(ctx)
	persist := func(msg core.Message) {
		if sink == nil {
			return
		}
		_ = sink.AppendTurnMessage(persistCtx, msg)
	}
	for _, msg := range agent.PersistInput(ctx) {
		persist(msg)
	}
	start := core.AgentStart{Type: "agent_start", ModelName: ""}
	if opts.Provider != nil {
		start.ModelName = opts.Provider.ModelName()
	}
	opts.Events <- start
	opts.Events <- core.TurnStart{Type: "turn_start", Turn: 1}
	if opts.Provider == nil {
		opts.Events <- core.AgentEnd{Type: "agent_end", FinishReason: "error", Error: "no provider configured"}
		return nil
	}
	var content strings.Builder
	persisted := false
	for event := range opts.Provider.Stream(ctx, opts.Messages, nil) {
		if chunk, ok := event.(core.ResponseChunk); ok {
			content.WriteString(chunk.Content)
		}
		opts.Events <- core.StreamEvent{Event: event}
		if end, ok := event.(core.StreamEnd); ok && end.FinishReason == "stop" {
			// Persist BEFORE the event, like the real loop: a reload
			// mid-run must find the record already in the store.
			assistant := core.NewAssistant(content.String())
			persist(assistant)
			persisted = true
			opts.Events <- core.AssistantMessageEvent{Type: "assistant_message", Message: assistant}
			opts.Events <- core.TurnEnd{Type: "turn_end", Turn: 1, ToolCalls: 0}
			opts.Events <- core.AgentEnd{Type: "agent_end", Turns: 1, FinishReason: "stop", Message: assistant}
		}
	}
	// The stream unwound without a stop StreamEnd - a cancelled run
	// (stop mid-turn) or a script that closed early. What streamed
	// persists as the turn's partial: the transcript keeps the copy
	// the user already saw rendered.
	if !persisted && content.Len() > 0 {
		persist(core.NewAssistant(content.String()))
	}
	return nil
}

// newTestServerWithStore returns a server with an in-memory store and
// a handle to the store for assertions. The store seams onto the base
// at construction: the base field is unexported to the shell, so the
// constructor path carries it.
func newTestServerWithStore(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	provider := newMockProvider("mock",
		scripted(
			core.ResponseChunk{Type: "response_chunk", Content: "hello"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		),
	)
	a := core.NewAgent(provider, nil, 0)
	a.SetLoopProvider(testLoop{})
	return NewServer(a, nil, st, nil, nil, nil), st
}

// scripted hands one provider script through, matching agent/api.
func scripted(events ...core.ProviderStreamEvent) []core.ProviderStreamEvent { return events }

type multiProvider struct {
	modelName string
	next      chan []core.ProviderStreamEvent
}

func newMultiProvider(modelName string) *multiProvider {
	return &multiProvider{modelName: modelName, next: make(chan []core.ProviderStreamEvent)}
}

func (p *multiProvider) arm() { p.next = make(chan []core.ProviderStreamEvent) }

func (p *multiProvider) ModelName() string { return p.modelName }

func (p *multiProvider) SetThinkingLevel(level string) {}

func (p *multiProvider) WithThinkingLevel(level string) llm.Provider {
	cp := *p
	return &cp
}

func (p *multiProvider) WithModel(model string) llm.Provider {
	cp := *p
	cp.modelName = model
	return &cp
}

func (p *multiProvider) ListModels() ([]string, error) {
	return []string{p.modelName}, nil
}

func (p *multiProvider) ModelInfo() (llm.ModelInfo, error) {
	return llm.ModelInfo{}, nil
}

func (p *multiProvider) Stream(_ context.Context, _ []core.Message, _ []llm.ToolSchema) <-chan core.ProviderStreamEvent {
	events := make(chan core.ProviderStreamEvent)
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

func (p *multiProvider) send(batch ...core.ProviderStreamEvent) { p.next <- batch }

func (p *multiProvider) done() {
	close(p.next)
	p.arm()
}

func finalStep(content string) []core.ProviderStreamEvent {
	return []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response_chunk", Content: content},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
}

func muteTitles(t *testing.T, st *store.Store, id string) {
	t.Helper()
	if err := st.SetSessionTitle(context.Background(), id, "", true); err != nil {
		t.Fatalf("mute titles: %v", err)
	}
}

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

func transcriptOf(t *testing.T, ts *httptest.Server, id string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(ts.URL + "/sessions/" + id + "/transcript")
	if err != nil {
		t.Fatalf("transcript get: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("transcript decode: %v", err)
	}
	return resp.StatusCode, out
}

func postJSON(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func startExchange(t *testing.T, prov *multiProvider, s *Server, ts *httptest.Server, sid string) {
	t.Helper()
	resp, cancel, _ := postChatStream(t, ts, sid)
	finishRun(t, prov, s, sid, "first answer")
	resp.Body.Close()
	cancel()
}

// newTestServerWithStoreSeam is newTestServerWithStore with the
// registry-toggle seam injected at construction (the base field is
// unexported to the shell; the constructor path is the same wire the
// host uses).
func newTestServerWithStoreSeam(t *testing.T, rt core.RegistryToggles) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	provider := newMockProvider("mock",
		scripted(
			core.ResponseChunk{Type: "response_chunk", Content: "hello"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		),
	)
	a := core.NewAgent(provider, nil, 0)
	a.SetLoopProvider(testLoop{})
	return NewServer(a, nil, st, nil, nil, rt), st
}
func getJSON(t *testing.T, url string) (int, []byte) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, body
}

// postAny posts a JSON body and returns the status only (the generic
// postJSON helper in server_variants_test.go returns a decoded map,

// postChatStream POSTs a "/chat" run and leaves the stream open (the
// agents' agents-test original moved to agent/api).
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

// finishRun closes the scripted run, waiting through the binder seam.
func finishRun(t *testing.T, prov *multiProvider, s *Server, sid, answer string) {
	t.Helper()
	prov.send(finalStep(answer)...)
	prov.done()
	waitFor(t, "run finished "+answer, func() bool { return !s.ActiveRun(sid) })
}
