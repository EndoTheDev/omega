package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newOpenAITestServer returns an httptest server that serves a canned
// SSE chat completions stream and records the request body for asserts.
func newOpenAITestServer(t *testing.T, sse string) (*httptest.Server, *strings.Builder) {
	t.Helper()
	var body strings.Builder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Swallow the body into a builder for later inspection.
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			body.Write(buf[:n])
			if err != nil {
				break
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sse))
	}))
	t.Cleanup(srv.Close)
	return srv, &body
}

// drain collects all stream events into slices for asserts.
func drain(t *testing.T, events <-chan StreamEvent) (content string, toolCalls []ToolCall, end StreamEnd) {
	t.Helper()
	for ev := range events {
		switch e := ev.(type) {
		case ResponseChunk:
			content += e.Content
		case ToolCallEvent:
			toolCalls = append(toolCalls, e.ToolCall)
		case StreamEnd:
			end = e
		}
	}
	return content, toolCalls, end
}

func TestOpenAIStreamContent(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	srv, _ := newOpenAITestServer(t, sse)
	p := NewOpenAIProvider("gpt-4o", srv.URL, "test-key")

	content, _, end := drain(t, p.Stream(context.Background(), []Message{
		NewSystem("be terse"),
		NewUser("hi"),
	}, nil))

	if content != "Hello" {
		t.Fatalf("content = %q, want Hello", content)
	}
	if end.FinishReason != "stop" {
		t.Fatalf("finish_reason = %q, want stop", end.FinishReason)
	}
	if end.Error != "" {
		t.Fatalf("unexpected error: %s", end.Error)
	}
}

func TestOpenAIStreamToolCall(t *testing.T) {
	// Arguments arrive split across two deltas; the JSON string must be
	// reassembled and unmarshalled into a map.
	sse := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"add\",\"arguments\":\"{\\\"a\\\":\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"1,\\\"b\\\":2}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	srv, _ := newOpenAITestServer(t, sse)
	p := NewOpenAIProvider("gpt-4o", srv.URL, "test-key")

	_, toolCalls, end := drain(t, p.Stream(context.Background(), nil, []ToolSchema{
		{Name: "add", Description: "add two", Parameters: map[string]any{"type": "object"}},
	}))

	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls = %d, want 1", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.ID != "call_1" || tc.Name != "add" {
		t.Fatalf("tool call = %+v, want id call_1 name add", tc)
	}
	if tc.Arguments["a"] != float64(1) || tc.Arguments["b"] != float64(2) {
		t.Fatalf("arguments = %v, want {a:1 b:2}", tc.Arguments)
	}
	if end.FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", end.FinishReason)
	}
}

func TestOpenAIMessagesToAPI(t *testing.T) {
	p := NewOpenAIProvider("gpt-4o", "", "k")
	msgs := []Message{
		NewSystem("sys"),
		NewUser("hello"),
		NewAssistant("calling").withToolCalls(),
		NewToolResult("42", "call_1", false),
	}
	api := p.messagesToAPI(msgs)
	if len(api) != 4 {
		t.Fatalf("len = %d, want 4", len(api))
	}
	if api[0]["role"] != "system" || api[0]["content"] != "sys" {
		t.Errorf("system = %v", api[0])
	}
	if api[1]["role"] != "user" || api[1]["content"] != "hello" {
		t.Errorf("user = %v", api[1])
	}
	if api[2]["role"] != "assistant" {
		t.Errorf("assistant role = %v", api[2])
	}
	if tc, ok := api[2]["tool_calls"].([]map[string]any); !ok || len(tc) != 1 {
		t.Errorf("assistant tool_calls = %v", api[2]["tool_calls"])
	}
	if api[3]["role"] != "tool" || api[3]["tool_call_id"] != "call_1" {
		t.Errorf("tool = %v", api[3])
	}
}

// withToolCalls builds an Assistant with one tool call for message-shape
// tests. Arguments marshal to a JSON string in the OpenAI API message.
func (Assistant) withToolCalls() Assistant {
	return Assistant{
		Content: "calling",
		ToolCalls: []ToolCall{
			{ID: "call_1", Name: "add", Arguments: map[string]any{"a": float64(1)}},
		},
	}
}
