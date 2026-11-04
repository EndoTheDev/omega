package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/EndoTheDev/omega/llm"
)

// mockProvider is a scripted Provider for http_channel tests. Each call
// returns the next scripted stream; the final script repeats so loops
// that must keep going stay alive. The call counter lives behind a
// mutex - title generation streams concurrently with the main run.
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

// sseFrame is one parsed SSE frame.
type sseFrame struct {
	event string
	data  []byte
}

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

func (m *mockProvider) ModelName() string { return m.modelName }

func (m *mockProvider) SetThinkingLevel(level string) { m.thinkingLevel = level }

// WithThinkingLevel returns a copy at the given thinking level under
// the same lock-free value-copy discipline as WithModel: the receiver
// is untouched, so concurrent derivations stay isolated. thinkingLevel
// is read by tests; Stream itself never touches it.
func (m *mockProvider) WithThinkingLevel(level string) agent.Provider {
	cp := *m
	cp.thinkingLevel = level
	return &cp
}

func (m *mockProvider) WithModel(model string) agent.Provider {
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

// nextIndex hands out the next script index under the lock. The
// increment happens here, not after streaming, so concurrent callers
// cannot claim the same script.
func (m *mockProvider) nextIndex() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := *m.calls
	*m.calls++
	return index
}

// servedModelNames copies the model behind every Stream call.
func (m *mockProvider) servedModelNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), *m.seenModels...)
}

// streamCalls reports how many Stream calls were made. Concurrent with
// in-flight streams it is advisory; callers poll for quiescence.
func (m *mockProvider) streamCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.calls
}

func scripted(events ...core.ProviderStreamEvent) []core.ProviderStreamEvent { return events }

func newTestServer() *Server {
	provider := newMockProvider("mock",
		scripted(
			core.ResponseChunk{Type: "response_chunk", Content: "hello"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		),
	)
	a := agent.NewAgent(provider, nil, 0)
	a.SetLoopProvider(testLoop{})
	return NewServer(a, nil, nil, nil, nil, nil)
}

// testLoop is a minimal loop for http_channel tests. It mirrors the
// real loop's event sequence AND its turn-copy persistence (t_471f6d4a):
// the caller's PersistInput writes first, then the streamed turn's
// assistant record - built from the content the provider actually sent,
// the same shape the real loop persists. The real loop lives in
// agent/loop/ (the timing suites run it directly).
type testLoop struct{}

// testToolProvider is a stub ToolProvider for the /tools inventory test.
type testToolProvider struct{ tools map[string]core.Tool }

func (p testToolProvider) Tools() map[string]core.Tool { return p.tools }

func (testLoop) Run(ctx context.Context, opts agent.LoopOptions) error {
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

// newTestServerWithStore returns a server with an in-memory store and a
// handle to the store for assertions.
func newTestServerWithStore(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	s := newTestServer()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.store = st
	return s, st
}

func TestModels(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Current      string              `json:"current"`
		Models       []string            `json:"models"`
		Capabilities map[string][]string `json:"capabilities"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Current != "mock" {
		t.Fatalf("current = %q, want mock", body.Current)
	}
	if len(body.Models) != 2 || body.Models[0] != "test-model" || body.Models[1] != "other-model" {
		t.Fatalf("models = %v, want [test-model, other-model]", body.Models)
	}
	// a name-only provider (the interface, un-upgraded) carries no
	// capabilities map - the pickers' curated fallback owns it then
	if len(body.Capabilities) != 0 {
		t.Fatalf("capabilities = %v, want absent for a name-only provider", body.Capabilities)
	}
}

// capsProvider is mockProvider with the ListModelEntries upgrade
// (card t_70c28128): the /models payload carries the capability map.
type capsProvider struct {
	*mockProvider
}

func (p *capsProvider) ListModelEntries() ([]core.ModelEntry, error) {
	return []core.ModelEntry{
		{Name: "clef:27b", Capabilities: []string{"decision", "vision"}},
		{Name: "test-model", Capabilities: []string{"completion", "tools"}},
		{Name: "blank-caps"}, // caps-less entries ride plain
	}, nil
}

// TestModelsCapabilities: the upgraded provider's capability flags
// ride the /models payload alongside the plain name list, and entries
// without flags are omitted from the map (not sent as empty lists).
func TestModelsCapabilities(t *testing.T) {
	provider := &capsProvider{newMockProvider("mock",
		scripted(
			core.ResponseChunk{Type: "response_chunk", Content: "hi"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		),
	)}
	a := agent.NewAgent(provider, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Models       []string            `json:"models"`
		Capabilities map[string][]string `json:"capabilities"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !reflect.DeepEqual(body.Models, []string{"clef:27b", "test-model", "blank-caps"}) {
		t.Fatalf("models = %v, want all three names in order", body.Models)
	}
	want := map[string][]string{
		"clef:27b":   {"decision", "vision"},
		"test-model": {"completion", "tools"},
	}
	if !reflect.DeepEqual(body.Capabilities, want) {
		t.Fatalf("capabilities = %v, want %v", body.Capabilities, want)
	}
}

func TestChatSSE(t *testing.T) {
	s := newTestServer()
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}

	events := parseSSE(t, rec.Body.String())
	types := make([]string, 0, len(events))
	for _, e := range events {
		types = append(types, e.event)
	}
	want := []string{"agent_start", "turn_start", "response_chunk", "stream_end", "assistant_message", "turn_end", "agent_end"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("event types = %v, want %v", types, want)
	}

	// The response_chunk data must carry the streamed content.
	var chunk core.ResponseChunk
	if err := json.Unmarshal(events[2].data, &chunk); err != nil {
		t.Fatalf("decode response_chunk: %v", err)
	}
	if chunk.Content != "hello" {
		t.Fatalf("content = %q, want hello", chunk.Content)
	}
}

// chatThinking is a /chat POST body with one user message and the
// given thinking_level (-1 = omitted: unset on the wire).
func chatThinking(t *testing.T, level int) []byte {
	t.Helper()
	msg := map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	}
	if level != -1 {
		msg["thinking_level"] = level
	}
	body, _ := json.Marshal(msg)
	return body
}

// postChatBody runs one stateless /chat against s and returns the recorder.
func postChatBody(s *Server, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestChatThinkingLevel threads the ladder index into a per-run
// provider copy: the Stream the loop runs sees the mapped ladder
// level while the mounted base provider stays untouched.
func TestChatThinkingLevel(t *testing.T) {
	s := newTestServer()
	base := s.agent.Provider().(*mockProvider)

	rec := postChatBody(s, chatThinking(t, 6)) // "high" on the ladder
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := *base.seenLevels; len(got) != 1 || got[0] != "high" {
		t.Fatalf("streamed levels = %v, want [high]", got)
	}
	if base.thinkingLevel != "" {
		t.Fatalf("base mutated: thinkingLevel = %q, want empty", base.thinkingLevel)
	}
}

// TestChatThinkingLevelDefault: index 0 ("none") and an omitted field
// both leave the provider untouched - no thinking parameter sent.
func TestChatThinkingLevelDefault(t *testing.T) {
	s := newTestServer()
	base := s.agent.Provider().(*mockProvider)

	rec := postChatBody(s, chatThinking(t, 0))
	if rec.Code != http.StatusOK {
		t.Fatalf("status(0) = %d, want 200", rec.Code)
	}
	rec = postChatBody(s, chatThinking(t, -1))
	if rec.Code != http.StatusOK {
		t.Fatalf("status(omitted) = %d, want 200", rec.Code)
	}
	if got := *base.seenLevels; len(got) != 2 || got[0] != "" || got[1] != "" {
		t.Fatalf("streamed levels = %v, want two empty (no override)", got)
	}
}

// TestChatThinkingLevelInvalid: out-of-ladder indexes are a 400.
func TestChatThinkingLevelInvalid(t *testing.T) {
	s := newTestServer()
	rec := postChatBody(s, chatThinking(t, len(llm.ThinkingLevels)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	rec = postChatBody(s, chatThinking(t, -3))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestChatThinkingLevelConcurrent is the concurrency law for the
// thinking level: two /chat runs with different levels at the same
// time each stream on their own copy, in order-indeed - and the base
// provider keeps no level.
func TestChatThinkingLevelConcurrent(t *testing.T) {
	s := newTestServer()
	base := s.agent.Provider().(*mockProvider)

	var wg sync.WaitGroup
	for _, level := range []int{2, 7} { // "on", "extra high"
		wg.Add(1)
		go func(level int) {
			defer wg.Done()
			if rec := postChatBody(s, chatThinking(t, level)); rec.Code != http.StatusOK {
				t.Errorf("status(%d) = %d, want 200", level, rec.Code)
			}
		}(level)
	}
	wg.Wait()

	levels := *base.seenLevels
	sort.Strings(levels)
	if !reflect.DeepEqual(levels, []string{"extra high", "on"}) {
		t.Fatalf("streamed levels = %v, want [extra high, on]", levels)
	}
	if base.thinkingLevel != "" {
		t.Fatalf("base mutated: thinkingLevel = %q", base.thinkingLevel)
	}
}

func TestChatBadRequest(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestChatUnknownRole(t *testing.T) {
	s := newTestServer()
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "wizard", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSessionsCRUD(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	h := s.Handler()

	// POST /sessions creates a session.
	body, _ := json.Marshal(map[string]string{"id": "s1"})
	req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", rec.Code)
	}
	var created core.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID != "s1" {
		t.Fatalf("created id = %q, want s1", created.ID)
	}

	// GET /sessions lists it (the sessions list rode the store capsule
	// out with the shell-ring shrink wave 2 - the list law lives in
	// harness/store/routes_test.go now).

	// GET /sessions/s1 returns session + empty messages.
	req = httptest.NewRequest(http.MethodGet, "/sessions/s1", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", rec.Code)
	}
	var detail struct {
		Session  core.Session   `json:"session"`
		Messages []core.Message `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if detail.Session.ID != "s1" || len(detail.Messages) != 0 {
		t.Fatalf("detail = %+v, want s1 with no messages", detail)
	}

	// DELETE /sessions/s1 removes it.
	req = httptest.NewRequest(http.MethodDelete, "/sessions/s1", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", rec.Code)
	}

	// GET now returns 404.
	req = httptest.NewRequest(http.MethodGet, "/sessions/s1", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d, want 404", rec.Code)
	}
}

func TestSessionMessagesCarryCreatedAt(t *testing.T) {
	s, store := newTestServerWithStore(t)
	ctx := context.Background()
	if err := store.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AppendMessage(ctx, "s1", core.NewUser("hi")); err != nil {
		t.Fatalf("append message: %v", err)
	}
	if err := store.AppendMessage(ctx, "s1", core.NewAssistant("hello")); err != nil {
		t.Fatalf("append message: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/sessions/s1", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Messages) != 2 {
		t.Fatalf("len = %d, want 2", len(body.Messages))
	}
	for i, m := range body.Messages {
		role, _ := m["role"].(string)
		createdAt, _ := m["created_at"].(string)
		if createdAt == "" {
			t.Fatalf("messages[%d] (role %q) has no created_at", i, role)
		}
		if _, err := time.Parse(time.RFC3339Nano, createdAt); err != nil {
			t.Fatalf("messages[%d].created_at = %q, not RFC3339: %v", i, createdAt, err)
		}
	}
	if body.Messages[0]["role"] != "user" || body.Messages[1]["role"] != "assistant" {
		t.Fatalf("roles = %v/%v, want user/assistant",
			body.Messages[0]["role"], body.Messages[1]["role"])
	}
}

func TestSessionsNoStore(t *testing.T) {
	s := newTestServer()
	// The create floor keeps its no-store gate: POST answers 501.
	req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"id":"x1"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestSessionTokensPersistAndAccumulate(t *testing.T) {
	// Provider scripts stream a response and report usage numbers; each
	// chat turn consumes exactly one script (the session's label is set
	// first so the detached title run never fires).
	provider := newMockProvider("mock",
		scripted(
			core.ResponseChunk{Type: "response_chunk", Content: "one"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop",
				PromptEvalCount: ptrInt(100), EvalCount: ptrInt(20)},
		),
		scripted(
			core.ResponseChunk{Type: "response_chunk", Content: "two"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop",
				PromptEvalCount: ptrInt(40), EvalCount: ptrInt(5)},
		),
	)
	a := agent.NewAgent(provider, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil, nil)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.store = st

	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", "labeled"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Turn 1: real usage lands on the session row.
	if code := postChat(s, "s1"); code != http.StatusOK {
		t.Fatalf("turn 1 status = %d, want 200", code)
	}
	sess, err := st.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if sess.PromptTokens != 100 || sess.CompletionTokens != 20 {
		t.Fatalf("turn 1 totals = %d/%d, want 100/20", sess.PromptTokens, sess.CompletionTokens)
	}

	// Turn 2: accumulate on top of turn 1.
	if code := postChat(s, "s1"); code != http.StatusOK {
		t.Fatalf("turn 2 status = %d, want 200", code)
	}
	sess, err = st.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("reget: %v", err)
	}
	if sess.PromptTokens != 140 || sess.CompletionTokens != 25 {
		t.Fatalf("turn 2 totals = %d/%d, want 140/25 (accumulated)", sess.PromptTokens, sess.CompletionTokens)
	}

	// The totals land on the row the session store serves (the list
	// payload rides the store capsule now - the row truth is here).
	sess, err = st.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("reget totals: %v", err)
	}
	if sess.PromptTokens != 140 || sess.CompletionTokens != 25 {
		t.Fatalf("row totals = %d/%d, want 140/25 (accumulated)", sess.PromptTokens, sess.CompletionTokens)
	}
}

func ptrInt(n int) *int { return &n }

func postChat(s *Server, sessionID string) int {
	body, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec.Code
}

func TestChatWithSessionPersists(t *testing.T) {
	s, store := newTestServerWithStore(t)
	ctx := context.Background()
	if err := store.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"session_id": "s1",
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	messages, err := store.GetMessages(ctx, "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len = %d, want 2 (user + assistant)", len(messages))
	}
	if _, ok := messages[0].(core.User); !ok {
		t.Fatalf("messages[0] = %T, want core.User", messages[0])
	}
	assistant, ok := messages[1].(core.Assistant)
	if !ok {
		t.Fatalf("messages[1] = %T, want core.Assistant", messages[1])
	}
	if assistant.Content != "hello" {
		t.Fatalf("assistant content = %q, want hello", assistant.Content)
	}
}

func TestChatWithSessionMissing(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	body, _ := json.Marshal(map[string]any{
		"session_id": "nope",
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestChatWithoutSessionAutoCreates pins the Endo 2026-10-18 contract:
// a /chat with no session_id on a store-backed server creates a session,
// persists normally, and tells the client the id via a leading `session`
// SSE frame. Without a store the run stays ephemeral (TestChatSSE pins
// that shape).
func TestChatWithoutSessionAutoCreates(t *testing.T) {
	s, store := newTestServerWithStore(t)
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// the leading frame names the created session
	events := parseSSE(t, rec.Body.String())
	if len(events) < 2 || events[0].event != "session" {
		t.Fatalf("first frames = %v, want a leading session frame", events)
	}
	var sessFrame struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(events[0].data, &sessFrame); err != nil {
		t.Fatalf("decode session frame: %v", err)
	}
	if sessFrame.ID == "" {
		t.Fatalf("session frame id empty")
	}

	// the session exists and carries the whole persisted exchange
	ctx := context.Background()
	messages, err := store.GetMessages(ctx, sessFrame.ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len = %d, want 2 (user + assistant)", len(messages))
	}
	if _, ok := messages[1].(core.Assistant); !ok {
		t.Fatalf("messages[1] = %T, want core.Assistant", messages[1])
	}

	// and it shows up in the list like any other session
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != sessFrame.ID {
		t.Fatalf("list = %+v, want exactly the created session %q", sessions, sessFrame.ID)
	}
}

// TestChatNoSessionConcurrent pins the no-collision property: N parallel
// session-less /chat calls each land in their OWN session - no shared
// id, no CreateSession clash, every exchange persisted intact.
func TestChatNoSessionConcurrent(t *testing.T) {
	s, store := newTestServerWithStore(t)
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})
	const n = 12
	ids := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				errs <- fmt.Errorf("status = %d", rec.Code)
				return
			}
			events := parseSSE(t, rec.Body.String())
			if len(events) == 0 || events[0].event != "session" {
				errs <- fmt.Errorf("no leading session frame")
				return
			}
			var frame struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(events[0].data, &frame); err != nil {
				errs <- fmt.Errorf("decode: %v", err)
				return
			}
			ids <- frame.ID
		}()
	}
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		select {
		case err := <-errs:
			t.Fatalf("concurrent chat: %v", err)
		case id := <-ids:
			if seen[id] {
				t.Fatalf("session id %q reused across concurrent chats", id)
			}
			seen[id] = true
		}
	}
	sessions, err := store.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != n {
		t.Fatalf("list has %d sessions, want %d", len(sessions), n)
	}
	for _, sess := range sessions {
		messages, err := store.GetMessages(context.Background(), sess.ID)
		if err != nil {
			t.Fatalf("get messages: %v", err)
		}
		if len(messages) != 2 {
			t.Fatalf("session %s has %d messages, want 2", sess.ID, len(messages))
		}
	}
}

// parseSSE parses "event: <type>\ndata: <json>\n\n" frames.
func parseSSE(t *testing.T, raw string) []sseFrame {
	t.Helper()
	var out []sseFrame
	scanner := bufio.NewScanner(strings.NewReader(raw))
	var current sseFrame
	haveEvent := false
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if haveEvent {
				out = append(out, current)
				current = sseFrame{}
				haveEvent = false
			}
		case strings.HasPrefix(line, "event: "):
			current.event = strings.TrimPrefix(line, "event: ")
			haveEvent = true
		case strings.HasPrefix(line, "data: "):
			current.data = []byte(strings.TrimPrefix(line, "data: "))
			haveEvent = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan SSE: %v", err)
	}
	return out
}

func TestModelsMethodNotAllowed(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/models", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestSessionsMethodNotAllowed(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	req := httptest.NewRequest(http.MethodPut, "/sessions", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestSessionsPostInvalidJSON(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSessionByIDNoStore(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/sessions/abc", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestSessionByIDMethodNotAllowed(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	req := httptest.NewRequest(http.MethodPost, "/sessions/abc", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestSessionPatchPinned covers the pin round-trip through the API: PATCH
// {"pinned": true} persists the flag across a fresh GET /sessions read,
// {"pinned": false} clears it, and an invalid body is rejected.
func TestSessionPatchPinned(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", "labeled"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	patch := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/sessions/s1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}

	rec := patch(t, `{"pinned": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var sess core.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !sess.Pinned {
		t.Fatalf("patch response pinned = false, want true")
	}

	// the flag survives the row read (the list payload rides the store
	// capsule now; the row truth is the store)
	if pinned, _ := st.GetSession(ctx, "s1"); !pinned.Pinned {
		t.Fatalf("row pinned = false after the pin PATCH, want true")
	}

	rec = patch(t, `{"pinned": false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unpin status = %d, want 200", rec.Code)
	}
	if after, _ := st.GetSession(ctx, "s1"); after.Pinned {
		t.Fatalf("pinned = true after unpin, want false")
	}

	rec = patch(t, `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d, want 400", rec.Code)
	}
	rec = patch(t, `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing pinned status = %d, want 400", rec.Code)
	}
}

// TestSessionPatchArchived covers the archive round-trip through the
// API: PATCH {"archived": true} persists the flag, the default GET
// /sessions excludes the row, archived=include returns it, unarchive
// brings it back, and updated_at never moves.
func TestSessionPatchArchived(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", "labeled"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	patch := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/sessions/s1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}

	rec := patch(t, `{"archived": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var sess core.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !sess.Archived {
		t.Fatalf("patch response archived = false, want true")
	}
	updatedAt := sess.UpdatedAt

	// the exclude/restore law rides the store capsule's list (ported to
	// harness/store/routes_test.go); here the row truth: the flag is on
	// and the PATCH is not activity - updated_at untouched
	if archived, _ := st.GetSession(ctx, "s1"); !archived.Archived {
		t.Fatalf("row archived = false after the archive PATCH, want true")
	}
	if got, _ := st.GetSession(ctx, "s1"); got.UpdatedAt != updatedAt {
		t.Fatalf("archive moved updated_at: %q -> %q", updatedAt, got.UpdatedAt)
	}

	rec = patch(t, `{"archived": false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive status = %d, want 200", rec.Code)
	}
	if after, _ := st.GetSession(ctx, "s1"); after.Archived {
		t.Fatalf("archived = true after unarchive, want false")
	}

	rec = patch(t, `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d, want 400", rec.Code)
	}
	rec = patch(t, `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing archived status = %d, want 400", rec.Code)
	}
}

// TestSessionPatchRead covers the mark-read round-trip through the API:
// output landing after the session's last read turns unread on in the
// list, PATCH {"read": true} stamps it off without touching updated_at,
// and the invalid-body cases are rejected.
func TestSessionPatchRead(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", "label"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// Output lands after birth - the row is unread before the mark-read,
	// and the detail endpoint exposes the same read-time state the
	// capsule's list carries.
	if err := st.AppendMessage(ctx, "s1", core.NewUser("hi")); err != nil {
		t.Fatalf("append message: %v", err)
	}

	patch := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/sessions/s1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}

	before, _ := st.GetSession(ctx, "s1")
	rec := patch(t, `{"read": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if read, _ := st.GetSession(ctx, "s1"); agent.SessionUnread(read) {
		t.Fatalf("row after mark-read unread = true, want false")
	}
	// Reading is not activity: updated_at is untouched.
	after, _ := st.GetSession(ctx, "s1")
	if after.UpdatedAt != before.UpdatedAt {
		t.Fatalf("read moved updated_at: %q -> %q", before.UpdatedAt, after.UpdatedAt)
	}
	if after.LastReadAt == "" {
		t.Fatalf("mark-read left last_read_at empty")
	}

	// The detail endpoint exposes the same read-time state.
	req := httptest.NewRequest(http.MethodGet, "/sessions/s1", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var detail map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if unread, ok := detail["unread"].(bool); !ok || unread {
		t.Fatalf("detail unread = %v, want false", detail["unread"])
	}

	rec = patch(t, `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d, want 400", rec.Code)
	}
	rec = patch(t, `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing flag status = %d, want 400", rec.Code)
	}
	rec = patch(t, `{"read": false}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("read-false status = %d, want 400", rec.Code)
	}
}

func TestSessionByIDPutLabel(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"id":"t-put"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPut, "/sessions/t-put", strings.NewReader(`{"label":"my title"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var sess core.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sess.Label != "my title" {
		t.Fatalf("label = %q, want %q", sess.Label, "my title")
	}
	req = httptest.NewRequest(http.MethodPut, "/sessions/t-put", strings.NewReader(`{"label":""}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty label status = %d, want 400", rec.Code)
	}
}

// TestMaybeTitleSessionTitleModelRouting pins the title_model knob:
// with the knob set, the title call streams through the title model;
// without it, it streams through the call's pinned provider (main).
func TestMaybeTitleSessionTitleModelRouting(t *testing.T) {
	ctx := context.Background()
	// With the knob.
	s, st, prov := newTitleTestServerWithStore(t)
	if err := st.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	s.titleModel = "gemcheap"
	s.maybeTitleSession(ctx, prov, titleChatRequest("s1"), "", false)
	if models := prov.mockProvider.servedModelNames(); len(models) != 1 || models[0] != "gemcheap" {
		t.Fatalf("provisional call served by %v, want [gemcheap]", models)
	}
	if sess, _ := st.GetSession(ctx, "s1"); sess.TitleState != 1 {
		t.Fatalf("title state = %d, want provisional (1)", sess.TitleState)
	}

	// Without the knob: the pinned provider (main) serves directly.
	s, st, prov = newTitleTestServerWithStore(t)
	if err := st.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	s.maybeTitleSession(ctx, prov, titleChatRequest("s1"), "", false)
	if models := prov.mockProvider.servedModelNames(); len(models) != 1 || models[0] != "mock" {
		t.Fatalf("no-knob call served by %v, want [mock]", models)
	}
}

// TestMaybeTitleSessionToolless pins the live defect t_381ae0d2: the title
// turn is metadata, not a conversation. Its first user message can invite
// tool use ("Use the delegate.task tool..."), and before agent.WithToolless
// the title turn ran the FULL merged tool table - the title model spawned
// REAL subagents (delegate:task-3/6, one parentless). The run reaching the
// provider must be marked toolless: no schema, no dispatch entry, present
// or future. The real conversation turn is untouched - it carries no
// WithToolless (the loop drops the registry only when the mark is present).
func TestMaybeTitleSessionToolless(t *testing.T) {
	ctx := context.Background()
	s, st, prov := newTitleTestServerWithStore(t)
	if err := st.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	req := chatRequest{
		SessionID: "s1",
		Messages:  []json.RawMessage{json.RawMessage(`{"role":"user","content":"Use the delegate.task tool to spawn a research subagent while you read the title."}`)},
	}
	s.maybeTitleSession(ctx, prov, req, "", false)

	prov.mu.Lock()
	defer prov.mu.Unlock()
	if len(prov.toollessRun) != 1 {
		t.Fatalf("title stream calls = %d, want exactly one title turn", len(prov.toollessRun))
	}
	if !prov.toollessRun[0] {
		t.Fatal("title turn reached the provider with a live tool table (agent.Toolless(ctx) = false)")
	}
}

func TestSessionByIDNotFound(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	req := httptest.NewRequest(http.MethodGet, "/sessions/nonexistent", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// captureProvider wraps mockProvider and records the user prompt sent
// to every Stream call, so the title prompt shape is directly checked.
type captureProvider struct {
	mockProvider
	mu          sync.Mutex
	prompts     []string
	toollessRun []bool // agent.Toolless(ctx) of each Stream call
}

func newCaptureProvider(scripts ...[]core.ProviderStreamEvent) *captureProvider {
	return &captureProvider{mockProvider: *newMockProvider("mock", scripts...)}
}

func (c *captureProvider) Stream(ctx context.Context, msgs []core.Message, tools []llm.ToolSchema) <-chan core.ProviderStreamEvent {
	c.mu.Lock()
	for _, m := range msgs {
		if core.MessageRole(m) == "user" {
			if u, ok := m.(core.User); ok {
				c.prompts = append(c.prompts, u.Content)
			}
		}
	}
	c.toollessRun = append(c.toollessRun, agent.Toolless(ctx))
	c.mu.Unlock()
	return c.mockProvider.Stream(ctx, msgs, tools)
}

func (c *captureProvider) streamPromptCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.prompts)
}

func (c *captureProvider) streamPrompt(i int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prompts[i]
}

// newTitleTestServerWithStore builds a server whose title calls stream
// "prov title" first, "final title" after.
func newTitleTestServerWithStore(t *testing.T) (*Server, *store.Store, *captureProvider) {
	t.Helper()
	prov := newCaptureProvider(
		scripted(
			core.ResponseChunk{Type: "response_chunk", Content: "prov title"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		),
		scripted(
			core.ResponseChunk{Type: "response_chunk", Content: "final title"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		),
	)
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil, nil)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.store = st
	return s, st, prov
}

func titleChatRequest(sessionID string) chatRequest {
	return chatRequest{
		SessionID: sessionID,
		Messages:  []json.RawMessage{json.RawMessage(`{"role":"user","content":"greetings friend"}`)},
	}
}

// TestMaybeTitleSessionStateGates walks the title state machine through
// maybeTitleSession synchronously: provisional only from 0, final from
// 0 or 1, both blocked at 2, and a manually set label never overwritten.
func TestMaybeTitleSessionStateGates(t *testing.T) {
	ctx := context.Background()
	s, st, prov := newTitleTestServerWithStore(t)
	if err := st.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	req := titleChatRequest("s1")

	// Provisional from 0 -> 1: prompt is user-message-only.
	s.maybeTitleSession(ctx, s.agent.Provider(), req, "", false)
	sess, err := st.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("get s1: %v", err)
	}
	if sess.Label != "prov title" || sess.TitleState != 1 {
		t.Fatalf("after provisional: %+v", sess)
	}
	if prov.streamPromptCount() != 1 {
		t.Fatalf("title calls = %d, want 1", prov.streamPromptCount())
	}
	if p := prov.streamPrompt(0); strings.Contains(p, "\nAssistant: ") || !strings.Contains(p, "User: greetings friend") {
		t.Fatalf("provisional prompt shape: %q", p)
	}

	// Provisional again at 1: blocked, no second title call.
	s.maybeTitleSession(ctx, s.agent.Provider(), req, "", false)
	if sess, _ := st.GetSession(ctx, "s1"); sess.Label != "prov title" || sess.TitleState != 1 {
		t.Fatalf("second provisional overwrote: %+v", sess)
	}
	if prov.streamPromptCount() != 1 {
		t.Fatalf("title calls = %d, want 1 (blocked at provisional)", prov.streamPromptCount())
	}

	// Final from 1 -> 2: prompt carries the reply context.
	s.maybeTitleSession(ctx, s.agent.Provider(), req, "assistant reply text", true)
	if sess, _ := st.GetSession(ctx, "s1"); sess.Label != "final title" || sess.TitleState != 2 {
		t.Fatalf("after final: %+v", sess)
	}
	if p := prov.streamPrompt(1); !strings.Contains(p, "\nAssistant: assistant reply text") {
		t.Fatalf("final prompt shape: %q", p)
	}

	// Both are blocked at 2; a second exchange never retitles.
	s.maybeTitleSession(ctx, s.agent.Provider(), req, "assistant reply text", true)
	s.maybeTitleSession(ctx, s.agent.Provider(), req, "", false)
	if sess, _ := st.GetSession(ctx, "s1"); sess.Label != "final title" || sess.TitleState != 2 {
		t.Fatalf("state-2 write overwrote: %+v", sess)
	}
	if prov.streamPromptCount() != 2 {
		t.Fatalf("title calls = %d, want 2 (blocked at final)", prov.streamPromptCount())
	}

	// A manually set label blocks generated titles even at state 0 -
	// the pre-title-state protection keeps working after migration.
	if err := st.CreateSession(ctx, "s2", "", ""); err != nil {
		t.Fatalf("create s2: %v", err)
	}
	if err := st.UpdateSession(ctx, "s2", "manual label"); err != nil {
		t.Fatalf("set manual label: %v", err)
	}
	req2 := titleChatRequest("s2")
	s.maybeTitleSession(ctx, s.agent.Provider(), req2, "", false)
	s.maybeTitleSession(ctx, s.agent.Provider(), req2, "reply", true)
	if sess, _ := st.GetSession(ctx, "s2"); sess.Label != "manual label" || sess.TitleState != 0 {
		t.Fatalf("manual label overwritten: %+v", sess)
	}

	// A provisionally-titled session is never re-provisioned at state 1,
	// but the final call still upgrades it from 1.
	if err := st.CreateSession(ctx, "s3", "", ""); err != nil {
		t.Fatalf("create s3: %v", err)
	}
	if err := st.SetSessionTitle(ctx, "s3", "early prov", false); err != nil {
		t.Fatalf("early provisional: %v", err)
	}
	req3 := titleChatRequest("s3")
	s.maybeTitleSession(ctx, s.agent.Provider(), req3, "", false)
	if sess, _ := st.GetSession(ctx, "s3"); sess.Label != "early prov" || sess.TitleState != 1 {
		t.Fatalf("state-1 reprovisioned: %+v", sess)
	}
	// Final still lands from 1.
	s.maybeTitleSession(ctx, s.agent.Provider(), req3, "reply", true)
	if sess, _ := st.GetSession(ctx, "s3"); sess.Label != "final title" || sess.TitleState != 2 {
		t.Fatalf("final from 1 failed: %+v", sess)
	}
}

// TestChatFiresProvisionalThenFinalOnce exercises the full /chat flow:
// a state-0 session gets a provisional title at request start and a
// final one at persist end, then a second exchange fires no title call.
func TestChatFiresProvisionalThenFinalOnce(t *testing.T) {
	ctx := context.Background()
	// Every stream returns the same content, so both title calls
	// produce "hello" regardless of call order.
	s, st := newTestServerWithStore(t)
	if err := st.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if code := postChat(s, "s1"); code != http.StatusOK {
		t.Fatalf("first chat status = %d, want 200", code)
	}
	settle := func(wantTitleState int) bool {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if sess, _ := st.GetSession(ctx, "s1"); sess.TitleState == wantTitleState {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	if !settle(2) {
		sess, _ := st.GetSession(ctx, "s1")
		t.Fatalf("session never reached final title state: %+v", sess)
	}
	sess, _ := st.GetSession(ctx, "s1")
	if sess.Label == "" {
		t.Fatalf("label empty after first exchange: %+v", sess)
	}

	label := sess.Label
	// Second exchange: no provisional at request start, no final at
	// persist end - the title never changes again.
	if code := postChat(s, "s1"); code != http.StatusOK {
		t.Fatalf("second chat status = %d, want 200", code)
	}
	time.Sleep(300 * time.Millisecond)
	if sess, _ := st.GetSession(ctx, "s1"); sess.Label != label || sess.TitleState != 2 {
		t.Fatalf("second exchange retitled: %+v (was label %q)", sess, label)
	}
}

// TestTools: GET /tools lists the registry as {name, description}, sorted
// by name; a bare server (nil registry) answers []; any other method is
// 405. The webui's module-owned tools panel renders this inventory.
func TestTools(t *testing.T) {
	a := agent.NewAgent(newMockProvider("mock"), nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, map[string]core.Tool{
		"shell.run":   {Description: "run a shell command"},
		"files.read":  {Description: "read a file"},
		"files.write": {Description: "write a file"},
	}, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []toolListing
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("tools = %+v, want 3 rows", got)
	}
	if got[0].Name != "files.read" || got[1].Name != "files.write" || got[2].Name != "shell.run" {
		t.Fatalf("tools = %+v, want sorted by name", got)
	}
	if got[0].Description != "read a file" {
		t.Fatalf("description = %q, want 'read a file'", got[0].Description)
	}

	// the real registry comes from the agent's ToolProviders (the loop
	// merges those with the server set) - /tools must show them too
	a.SetToolProviders([]core.ToolProvider{testToolProvider{tools: map[string]core.Tool{
		"web.search": {Description: "search the web"},
	}}})
	rec4 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec4, httptest.NewRequest(http.MethodGet, "/tools", nil))
	var got4 []toolListing
	if err := json.Unmarshal(rec4.Body.Bytes(), &got4); err != nil {
		t.Fatalf("decode provider body: %v", err)
	}
	if len(got4) != 4 {
		t.Fatalf("tools = %+v, want 4 rows (server set + provider set)", got4)
	}
	if got4[0].Name != "files.read" || got4[3].Name != "web.search" {
		t.Fatalf("tools = %+v, want provider tools in the sorted union", got4)
	}

	// domain tool with actions + alias: the folded row carries the
	// actions metadata; the alias shim is hidden from the inventory.
	a.SetToolProviders([]core.ToolProvider{testToolProvider{tools: map[string]core.Tool{
		"memory": {
			Description: "domain tool",
			Actions:     []core.ToolAction{{Name: "add", Description: "append"}, {Name: "remove", Description: "delete"}},
		},
		"files.read": {Description: "legacy shim", AliasOf: "files"},
	}}})
	rec6 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec6, httptest.NewRequest(http.MethodGet, "/tools", nil))
	var got6 []toolListing
	if err := json.Unmarshal(rec6.Body.Bytes(), &got6); err != nil {
		t.Fatalf("decode actions body: %v", err)
	}
	var mem, legacyFound *toolListing
	for i := range got6 {
		switch got6[i].Name {
		case "memory":
			mem = &got6[i]
		case "files.read":
			legacyFound = &got6[i]
		}
	}
	if legacyFound != nil {
		t.Fatalf("alias shim leaked into the inventory: %+v", got6)
	}
	if mem == nil || len(mem.Actions) != 2 || mem.Actions[0].Name != "add" || mem.Actions[0].Description != "append" {
		t.Fatalf("memory row missing actions metadata: %+v", mem)
	}

	// empty registry: [] on the wire, never null
	bare := newTestServer()
	rec2 := httptest.NewRecorder()
	bare.Handler().ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/tools", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("bare status = %d, want 200", rec2.Code)
	}
	if body := rec2.Body.String(); body != "[]\n" {
		t.Fatalf("bare body = %q, want []", body)
	}

	// method gate
	rec3 := httptest.NewRecorder()
	bare.Handler().ServeHTTP(rec3, httptest.NewRequest(http.MethodPost, "/tools", nil))
	if rec3.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", rec3.Code)
	}
}
