package http_channel

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
	"github.com/EndoTheDev/omega/modules/store"
)

// mockProvider is a scripted Provider for http_channel tests. Each call
// returns the next scripted stream; the final script repeats so loops
// that must keep going stay alive. The call counter lives behind a
// mutex - title generation streams concurrently with the main run.
type mockProvider struct {
	modelName string
	scripts   [][]ai.StreamEvent
	mu        *sync.Mutex
	calls     *int
}

func newMockProvider(modelName string, scripts ...[]ai.StreamEvent) *mockProvider {
	calls := 0
	return &mockProvider{
		modelName: modelName,
		scripts:   scripts,
		mu:        &sync.Mutex{},
		calls:     &calls,
	}
}

func (m *mockProvider) ModelName() string { return m.modelName }

func (m *mockProvider) SetThinkingLevel(level string) {}

func (m *mockProvider) WithModel(model string) ai.Provider {
	cp := *m
	cp.modelName = model
	return &cp
}

func (m *mockProvider) ListModels() ([]string, error) {
	return []string{"test-model", "other-model"}, nil
}

func (m *mockProvider) ModelInfo() (ai.ModelInfo, error) {
	return ai.ModelInfo{}, nil
}

func (m *mockProvider) Stream(_ context.Context, _ []ai.Message, _ []ai.ToolSchema) <-chan ai.StreamEvent {
	events := make(chan ai.StreamEvent)
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

// streamCalls reports how many Stream calls were made. Concurrent with
// in-flight streams it is advisory; callers poll for quiescence.
func (m *mockProvider) streamCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.calls
}

func scripted(events ...ai.StreamEvent) []ai.StreamEvent { return events }

func newTestServer() *Server {
	provider := newMockProvider("mock",
		scripted(
			ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		),
	)
	a := agent.NewAgent(provider, nil, 0)
	a.SetLoopProvider(testLoop{})
	return NewServer(a, nil, nil, nil)
}

// testLoop is a minimal loop for http_channel tests. It mirrors the
// real loop's event sequence for simple one-turn conversations.
// The real loop lives in modules/agent_loop/.
type testLoop struct{}

// testToolProvider is a stub ToolProvider for the /tools inventory test.
type testToolProvider struct{ tools map[string]agent.Tool }

func (p testToolProvider) Tools() map[string]agent.Tool { return p.tools }

func (testLoop) Run(ctx context.Context, opts agent.LoopOptions) error {
	start := agent.AgentStart{Type: "agent_start", ModelName: ""}
	if opts.Provider != nil {
		start.ModelName = opts.Provider.ModelName()
	}
	opts.Events <- start
	opts.Events <- agent.TurnStart{Type: "turn_start", Turn: 1}
	if opts.Provider == nil {
		opts.Events <- agent.AgentEnd{Type: "agent_end", FinishReason: "error", Error: "no provider configured"}
		return nil
	}
	for event := range opts.Provider.Stream(ctx, opts.Messages, nil) {
		opts.Events <- agent.StreamEvent{Event: event}
		if end, ok := event.(ai.StreamEnd); ok && end.FinishReason == "stop" {
			assistant := ai.NewAssistant("")
			opts.Events <- agent.AssistantMessageEvent{Type: "assistant_message", Message: assistant}
			opts.Events <- agent.TurnEnd{Type: "turn_end", Turn: 1, ToolCalls: 0}
			opts.Events <- agent.AgentEnd{Type: "agent_end", Turns: 1, FinishReason: "stop", Message: assistant}
		}
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

func TestHealth(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Status        string `json:"status"`
		Version       string `json:"version"`
		UptimeSeconds int    `json:"uptime_seconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("status field = %q, want ok", body.Status)
	}
	// uptime is anchored at server construction - non-negative, present
	if body.UptimeSeconds < 0 {
		t.Fatalf("uptime_seconds = %d, want >= 0", body.UptimeSeconds)
	}
	if body.Version != "" {
		t.Fatalf("version = %q, want absent without a host build tag", body.Version)
	}
}

// TestHealthVersion: the host build tag rides the health payload. The
// version field must match exactly what the host supplied.
func TestHealthVersion(t *testing.T) {
	s := newTestServer()
	s.version = "v0.5.0-test"
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["version"] != "v0.5.0-test" {
		t.Fatalf("version = %v, want v0.5.0-test", body["version"])
	}
	if body["uptime_seconds"] == nil {
		t.Fatalf("uptime_seconds missing")
	}
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
		Current string   `json:"current"`
		Models  []string `json:"models"`
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
	var chunk ai.ResponseChunk
	if err := json.Unmarshal(events[2].data, &chunk); err != nil {
		t.Fatalf("decode response_chunk: %v", err)
	}
	if chunk.Content != "hello" {
		t.Fatalf("content = %q, want hello", chunk.Content)
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

func TestStaticIndex(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// works for the committed placeholder AND a synced real build
	if !strings.Contains(rec.Body.String(), "<!DOCTYPE html") {
		t.Fatalf("index body missing doctype")
	}
}

// TestSPAFallback pins the history-mode fallback: deep route paths serve
// the SPA shell, asset-like misses under /_nuxt/ still 404.
func TestSPAFallback(t *testing.T) {
	s := newTestServer()
	h := s.Handler()

	// deep route path -> 200 + SPA shell
	req := httptest.NewRequest(http.MethodGet, "/chat/anything", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/chat/anything status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<!DOCTYPE html") {
		t.Fatalf("/chat/anything body missing SPA shell doctype")
	}

	// asset miss under /_nuxt/ -> 404, never the shell
	req = httptest.NewRequest(http.MethodGet, "/_nuxt/missing.js", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/_nuxt/missing.js status = %d, want 404", rec.Code)
	}

	// real assets keep winning: FileServer canonically redirects
	// /index.html to / (301), not the shell path
	req = httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("/index.html status = %d, want 301", rec.Code)
	}
}

// sseFrame is one parsed SSE frame.
type sseFrame struct {
	event string
	data  []byte
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
	var created agent.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID != "s1" {
		t.Fatalf("created id = %q, want s1", created.ID)
	}

	// GET /sessions lists it.
	req = httptest.NewRequest(http.MethodGet, "/sessions", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	var list []agent.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].ID != "s1" {
		t.Fatalf("list = %+v, want [s1]", list)
	}

	// GET /sessions/s1 returns session + empty messages.
	req = httptest.NewRequest(http.MethodGet, "/sessions/s1", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", rec.Code)
	}
	var detail struct {
		Session  agent.Session `json:"session"`
		Messages []ai.Message  `json:"messages"`
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
	if err := store.AppendMessage(ctx, "s1", ai.NewUser("hi")); err != nil {
		t.Fatalf("append message: %v", err)
	}
	if err := store.AppendMessage(ctx, "s1", ai.NewAssistant("hello")); err != nil {
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
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestSessionsListCounts(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if err := st.CreateSession(ctx, "s2", "", ""); err != nil {
		t.Fatalf("create s2: %v", err)
	}
	// s1 carries messages; the tool calls ride the assistant, the
	// results are separate tool-role rows.
	s1 := []ai.Message{
		ai.NewUser("hi"),
		ai.NewAssistant("working"),
		ai.ToolResult{Content: "r1"},
		ai.ToolResult{Content: "r2"},
		ai.NewUser("again"),
		ai.NewAssistant("done"),
	}
	for _, m := range s1 {
		if err := st.AppendMessage(ctx, "s1", m); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var list []sessionPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	byID := map[string]sessionPayload{}
	for _, row := range list {
		byID[row.ID] = row
	}
	if got := byID["s1"]; got.MessageCount != 6 || got.ToolCount != 2 {
		t.Fatalf("s1 = %+v, want 6 messages / 2 tools", got)
	}
	if got := byID["s2"]; got.MessageCount != 0 || got.ToolCount != 0 {
		t.Fatalf("s2 = %+v, want 0/0", got)
	}
}

func TestSessionTokensPersistAndAccumulate(t *testing.T) {
	// Provider scripts stream a response and report usage numbers; each
	// chat turn consumes exactly one script (the session's label is set
	// first so the detached title run never fires).
	provider := newMockProvider("mock",
		scripted(
			ai.ResponseChunk{Type: "response_chunk", Content: "one"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop",
				PromptEvalCount: ptrInt(100), EvalCount: ptrInt(20)},
		),
		scripted(
			ai.ResponseChunk{Type: "response_chunk", Content: "two"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop",
				PromptEvalCount: ptrInt(40), EvalCount: ptrInt(5)},
		),
	)
	a := agent.NewAgent(provider, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil)
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

	// The totals flow through GET /sessions.
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var list []sessionPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].PromptTokens != 140 || list[0].CompletionTokens != 25 {
		t.Fatalf("sessions payload = %+v, want totals 140/25", list)
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
	if _, ok := messages[0].(ai.User); !ok {
		t.Fatalf("messages[0] = %T, want ai.User", messages[0])
	}
	assistant, ok := messages[1].(ai.Assistant)
	if !ok {
		t.Fatalf("messages[1] = %T, want ai.Assistant", messages[1])
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

func TestChatWithoutSessionStateless(t *testing.T) {
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
	sessions, err := store.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("stateless chat should persist nothing, got %d sessions", len(sessions))
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

func TestHealthMethodNotAllowed(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
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

func TestSessionSearch(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	for _, id := range []string{"s1", "s2", "s3"} {
		if err := st.CreateSession(ctx, id, "", ""); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	// s4 matches by LABEL only - the unified search must surface it as
	// a title hit (titleMatch set, snippet = the label itself).
	if err := st.CreateSession(ctx, "s4", "", "Harbor patrol log"); err != nil {
		t.Fatalf("create s4: %v", err)
	}
	// s1 matches twice - the response must dedupe to one hit; s2 has a
	// title-independent content match; s3 stays silent.
	for _, m := range []struct {
		id string
		m  ai.Message
	}{
		{"s1", ai.NewUser("the harbor hides at midnight")},
		{"s1", ai.NewAssistant("harbor confirmed")},
		{"s2", ai.NewUser("weather bulletin: harbor closed")},
		{"s3", ai.NewUser("nothing to see here")},
	} {
		if err := st.AppendMessage(ctx, m.id, m.m); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	h := s.Handler()

	// content term matches s1 and s2, one row each
	req := httptest.NewRequest(http.MethodGet, "/sessions/search?q=harbor", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var hits []sessionSearchHit
	if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil {
		t.Fatalf("decode hits: %v", err)
	}
	ids := map[string]string{}
	titled := map[string]bool{}
	for _, ht := range hits {
		if _, dup := ids[ht.ID]; dup {
			t.Fatalf("duplicate hit for %s", ht.ID)
		}
		ids[ht.ID] = ht.Snippet
		titled[ht.ID] = ht.TitleMatch
	}
	if len(ids) != 3 || ids["s1"] == "" || ids["s2"] == "" {
		t.Fatalf("hits = %+v, want one snippet each for s1, s2 and s4", hits)
	}
	// s4 hit by label: titleMatch set, snippet is the label itself, and
	// it leads (title hits rank ahead of content hits).
	if !titled["s4"] || hits[0].ID != "s4" || hits[0].Snippet != "Harbor patrol log" {
		t.Fatalf("s4 must be the first, title-marked hit; got %+v", hits)
	}

	// Renaming s4's label away from the term drops its title hit; s4
	// has no harbor content, so any leftover s4 row must not be
	// title-marked (the fresh label never contains 'harbor').
	if err := st.UpdateSession(ctx, "s4", "Fishing journal"); err != nil {
		t.Fatalf("rename s4: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/sessions/search?q=harbor", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post-rename: status = %d; body=%s", rec.Code, rec.Body.String())
	}
	hits = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil {
		t.Fatalf("decode post-rename hits: %v", err)
	}
	for _, ht := range hits {
		if ht.ID == "s4" && ht.TitleMatch {
			t.Fatalf("stale label must not be a title hit after rename: %+v", hits)
		}
	}

	// empty q searches nothing, 200 []
	req = httptest.NewRequest(http.MethodGet, "/sessions/search?q=", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty q: status = %d body = %q, want 200 []", rec.Code, rec.Body.String())
	}

	// SearchMessages phrase-quotes the term, so even FTS-breaking
	// input ("unbalanced quote) becomes a valid literal phrase and
	// degrades to a no-hit search, not an error. The 400 backstop
	// for FTS syntax errors stays in the handler for store-level
	// faults, but user input can no longer reach it.
	req = httptest.NewRequest(http.MethodGet, `/sessions/search?q=%22unbalanced`, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf(`unbalanced quote term: status = %d body = %q, want 200 []`, rec.Code, rec.Body.String())
	}

	// wrong method and no store
	req = httptest.NewRequest(http.MethodPost, "/sessions/search?q=harbor", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post: status = %d, want 405", rec.Code)
	}
	nn := NewServer(nil, nil, nil, nil)
	req = httptest.NewRequest(http.MethodGet, "/sessions/search?q=harbor", nil)
	rec = httptest.NewRecorder()
	nn.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("no store: status = %d, want 501", rec.Code)
	}
}

func TestSessionSearchBeforeSessions(t *testing.T) {
	// /sessions/search must win over /sessions/{id} even though it
	// sits inside the {id} segment space.
	s, _ := newTestServerWithStore(t)
	req := httptest.NewRequest(http.MethodGet, "/sessions/search?q=x", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("/sessions/search fell through to the {id} handler")
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
	var sess agent.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !sess.Pinned {
		t.Fatalf("patch response pinned = false, want true")
	}

	// the flag survives a fresh list (the webui reload case)
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	listRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(listRec, req)
	var list []sessionPayload
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || !list[0].Pinned {
		t.Fatalf("list = %+v, want one pinned session", list)
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
	var sess agent.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !sess.Archived {
		t.Fatalf("patch response archived = false, want true")
	}
	updatedAt := sess.UpdatedAt

	// default list excludes archived; include brings it back
	listDec := func(t *testing.T, url string) []sessionPayload {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		listRec := httptest.NewRecorder()
		s.Handler().ServeHTTP(listRec, req)
		if listRec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", url, listRec.Code)
		}
		var list []sessionPayload
		if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return list
	}
	if list := listDec(t, "/sessions"); len(list) != 0 {
		t.Fatalf("default list = %+v, want archived session excluded", list)
	}
	if list := listDec(t, "/sessions?archived=include"); len(list) != 1 || !list[0].Archived {
		t.Fatalf("include list = %+v, want one archived session", list)
	}

	// the PATCH is not activity: updated_at untouched
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
	if list := listDec(t, "/sessions"); len(list) != 1 {
		t.Fatalf("default list after unarchive = %+v, want the session back", list)
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

// TestSessionSearchArchivedFilter covers the search-side archived
// filter: hits whose session is archived are excluded by default and
// restored by archived=include.
func TestSessionSearchArchivedFilter(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ctx := context.Background()
	for _, id := range []string{"s1", "s2"} {
		if err := st.CreateSession(ctx, id, "", ""); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if err := st.AppendMessage(ctx, id, ai.NewUser("quiet harbor town")); err != nil {
			t.Fatalf("append %s: %v", id, err)
		}
	}
	if err := st.SetSessionArchived(ctx, "s1", true); err != nil {
		t.Fatalf("archive s1: %v", err)
	}

	searchDec := func(t *testing.T, url string) map[string]string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d; body=%s", url, rec.Code, rec.Body.String())
		}
		var hits []sessionSearchHit
		if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil {
			t.Fatalf("decode: %v", err)
		}
		out := map[string]string{}
		for _, h := range hits {
			out[h.ID] = h.Snippet
		}
		return out
	}

	if hits := searchDec(t, "/sessions/search?q=harbor"); len(hits) != 1 || hits["s2"] == "" {
		t.Fatalf("default hits = %+v, want only the unarchived s2", hits)
	}
	hits := searchDec(t, "/sessions/search?q=harbor&archived=include")
	if len(hits) != 2 || hits["s1"] == "" || hits["s2"] == "" {
		t.Fatalf("include hits = %+v, want s1 and s2", hits)
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
	// Output lands after birth - the session reads unread at the API.
	if err := st.AppendMessage(ctx, "s1", ai.NewUser("hi")); err != nil {
		t.Fatalf("append message: %v", err)
	}

	list := func(t *testing.T) sessionPayload {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		var rows []sessionPayload
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("list = %d rows, want 1", len(rows))
		}
		return rows[0]
	}

	if row := list(t); !row.Unread {
		t.Fatalf("row after new output unread = false, want true")
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
	if row := list(t); row.Unread {
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
	var sess agent.Session
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
	mu       sync.Mutex
	prompts  []string
}

func newCaptureProvider(scripts ...[]ai.StreamEvent) *captureProvider {
	return &captureProvider{mockProvider: *newMockProvider("mock", scripts...)}
}

func (c *captureProvider) Stream(ctx context.Context, msgs []ai.Message, tools []ai.ToolSchema) <-chan ai.StreamEvent {
	c.mu.Lock()
	for _, m := range msgs {
		if ai.MessageRole(m) == "user" {
			if u, ok := m.(ai.User); ok {
				c.prompts = append(c.prompts, u.Content)
			}
		}
	}
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
			ai.ResponseChunk{Type: "response_chunk", Content: "prov title"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		),
		scripted(
			ai.ResponseChunk{Type: "response_chunk", Content: "final title"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		),
	)
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil)
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
	s := NewServer(a, map[string]agent.Tool{
		"shell.run":   {Description: "run a shell command"},
		"files.read":  {Description: "read a file"},
		"files.write": {Description: "write a file"},
	}, nil, nil)

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
	a.SetToolProviders([]agent.ToolProvider{testToolProvider{tools: map[string]agent.Tool{
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
