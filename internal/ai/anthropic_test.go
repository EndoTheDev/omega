package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newAnthropicTestServer serves a canned SSE messages stream and records
// the request body.
func newAnthropicTestServer(t *testing.T, sse string) (*httptest.Server, *strings.Builder) {
	t.Helper()
	var body strings.Builder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestAnthropicStreamContent(t *testing.T) {
	sse := "event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	srv, _ := newAnthropicTestServer(t, sse)
	p := NewAnthropicProvider("claude-3-5-sonnet", srv.URL, "test-key")

	content, _, end := drain(t, p.Stream(context.Background(), []Message{
		NewSystem("be terse"),
		NewUser("hi"),
	}, nil))

	if content != "Hello" {
		t.Fatalf("content = %q, want Hello", content)
	}
	if end.FinishReason != "end_turn" {
		t.Fatalf("finish_reason = %q, want end_turn", end.FinishReason)
	}
	if end.Error != "" {
		t.Fatalf("unexpected error: %s", end.Error)
	}
}

func TestAnthropicStreamToolCall(t *testing.T) {
	// input_json_delta fragments reassemble into the tool input map.
	sse := "event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"add\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"input_json_delta\":\"{\\\"a\\\":\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"input_json_delta\":\"1,\\\"b\\\":2}\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	srv, _ := newAnthropicTestServer(t, sse)
	p := NewAnthropicProvider("claude-3-5-sonnet", srv.URL, "test-key")

	_, toolCalls, end := drain(t, p.Stream(context.Background(), nil, []ToolSchema{
		{Name: "add", Description: "add two", Parameters: map[string]any{"type": "object"}},
	}))

	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls = %d, want 1", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.ID != "toolu_1" || tc.Name != "add" {
		t.Fatalf("tool call = %+v, want id toolu_1 name add", tc)
	}
	if tc.Arguments["a"] != float64(1) || tc.Arguments["b"] != float64(2) {
		t.Fatalf("arguments = %v, want {a:1 b:2}", tc.Arguments)
	}
	if end.FinishReason != "tool_use" {
		t.Fatalf("finish_reason = %q, want tool_use", end.FinishReason)
	}
}

func TestAnthropicMessagesToAPI(t *testing.T) {
	p := NewAnthropicProvider("claude-3-5-sonnet", "", "k")
	msgs := []Message{
		NewSystem("sys"),
		NewUser("hello"),
		NewAssistant("calling").withToolCalls(),
		NewToolResult("42", "call_1", false),
		NewToolResult("7", "call_2", false),
	}
	system, api := p.messagesToAPI(msgs)
	if system != "sys" {
		t.Fatalf("system = %q, want sys", system)
	}
	if len(api) != 3 {
		t.Fatalf("len = %d, want 3 (user, assistant, grouped tool results)", len(api))
	}
	if api[0]["role"] != "user" || api[0]["content"] != "hello" {
		t.Errorf("user = %v", api[0])
	}
	// Assistant with tool_use blocks.
	blocks, ok := api[1]["content"].([]map[string]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("assistant content = %v, want 2 blocks (text + tool_use)", api[1]["content"])
	}
	if blocks[0]["type"] != "text" || blocks[1]["type"] != "tool_use" {
		t.Errorf("assistant blocks = %v", blocks)
	}
	// Two consecutive tool results fold into one user message.
	trBlocks, ok := api[2]["content"].([]map[string]any)
	if !ok || len(trBlocks) != 2 {
		t.Fatalf("tool result content = %v, want 2 blocks", api[2]["content"])
	}
	if trBlocks[0]["tool_use_id"] != "call_1" || trBlocks[1]["tool_use_id"] != "call_2" {
		t.Errorf("tool result ids = %v", trBlocks)
	}
}
