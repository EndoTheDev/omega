package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega-agent/internal/agent"
	"github.com/EndoTheDev/omega-agent/internal/ai"
)

// mockProvider is a scripted Provider for gateway tests. Each call
// returns the next scripted stream; the final script repeats so loops
// that must keep going stay alive.
type mockProvider struct {
	modelName string
	scripts   [][]ai.StreamEvent
	calls     int
}

func (m *mockProvider) ModelName() string { return m.modelName }

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
	return NewServer(a, nil)
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
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["model"] != "mock" {
		t.Fatalf("model = %q, want mock", body["model"])
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
	want := []string{"agent_start", "turn_start", "response_chunk", "stream_end", "turn_end", "agent_end"}
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
	req := httptest.NewRequest(http.MethodGet, "/static/", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "omega-agent") {
		t.Fatalf("index body missing title")
	}
}

// sseFrame is one parsed SSE frame.
type sseFrame struct {
	event string
	data  []byte
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
