package http_channel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/extensions/store"
)

// mockProvider is a scripted Provider for http_channel tests. Each call
// returns the next scripted stream; the final script repeats so loops
// that must keep going stay alive.
type mockProvider struct {
	modelName string
	scripts   [][]ai.StreamEvent
	calls     int
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
		index := m.calls
		if index >= len(m.scripts) {
			index = len(m.scripts) - 1
		}
		if index >= 0 {
			for _, e := range m.scripts[index] {
				events <- e
			}
		}
		m.calls++
	}()
	return events
}

func scripted(events ...ai.StreamEvent) []ai.StreamEvent { return events }

func newTestServer() *Server {
	provider := &mockProvider{
		modelName: "mock",
		scripts: [][]ai.StreamEvent{
			scripted(
				ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
				ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
			),
		},
	}
	a := agent.NewAgent(provider, nil, 0)
	a.SetLoopProvider(testLoop{})
	return NewServer(a, nil, nil)
}

// testLoop is a minimal loop for http_channel tests. It mirrors the
// real loop's event sequence for simple one-turn conversations.
// The real loop lives in extensions/agent_loop/.
type testLoop struct{}

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
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status field = %q, want ok", body["status"])
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
	provider := &mockProvider{
		modelName: "mock",
		scripts: [][]ai.StreamEvent{
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
		},
	}
	a := agent.NewAgent(provider, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil)
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
	req := httptest.NewRequest(http.MethodPatch, "/sessions/abc", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
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
