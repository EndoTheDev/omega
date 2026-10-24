package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// sseAnthropicServer serves a canned anthropic SSE stream shaped like
// the live keypool /v1/messages surface: message_start, content blocks,
// message_delta with usage, message_stop.
func sseAnthropicServer(t *testing.T, events ...string) *httptest.Server {
	t.Helper()
	var sb strings.Builder
	for _, e := range events {
		sb.WriteString("data: " + e + "\n\n")
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sb.String())
	}))
}

// TestStreamAnthropicThinking verifies thinking_delta deltas surface as
// ThinkingChunk events alongside text deltas (gap found against the
// live keypool surface, which streams thinking as thinking_delta).
func TestStreamAnthropicThinking(t *testing.T) {
	srv := sseAnthropicServer(t,
		`{"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Two plus two"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" is four"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"The answer is 4."}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		`{"type":"message_stop"}`,
	)
	defer srv.Close()

	p := &Provider{typ: "anthropic", baseURL: srv.URL, modelName: "glm-5.3", apiKey: "k"}
	got := collectEvents(t, p.Stream(testContext(), []core.Message{core.NewUser("2+2?")}, nil))

	var thinking, response string
	for _, e := range got {
		switch ev := e.(type) {
		case core.ThinkingChunk:
			thinking += ev.Content
		case core.ResponseChunk:
			response += ev.Content
		}
	}
	if thinking != "Two plus two is four" {
		t.Errorf("thinking = %q, want %q", thinking, "Two plus two is four")
	}
	if response != "The answer is 4." {
		t.Errorf("response = %q, want %q", response, "The answer is 4.")
	}
}

// TestStreamAnthropicUsage verifies usage from message_start (input
// side) and message_delta (output side) lands on the StreamEnd
// counters, and that omitted fields stay nil instead of fake zeros.
func TestStreamAnthropicUsage(t *testing.T) {
	srv := sseAnthropicServer(t,
		`{"type":"message_start","message":{"usage":{"input_tokens":41,"cache_read_input_tokens":128,"output_tokens":0}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hi"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":41,"cache_read_input_tokens":128,"output_tokens":32}}`,
		`{"type":"message_stop"}`,
	)
	defer srv.Close()

	p := &Provider{typ: "anthropic", baseURL: srv.URL, modelName: "glm-5.3", apiKey: "k"}
	got := collectEvents(t, p.Stream(testContext(), []core.Message{core.NewUser("hi")}, nil))
	end := got[len(got)-1].(core.StreamEnd)
	if end.FinishReason != "end_turn" {
		t.Errorf("FinishReason = %q, want %q", end.FinishReason, "end_turn")
	}
	if end.PromptEvalCount == nil || *end.PromptEvalCount != 41 {
		t.Errorf("PromptEvalCount = %v, want 41", end.PromptEvalCount)
	}
	if end.EvalCount == nil || *end.EvalCount != 32 {
		t.Errorf("EvalCount = %v, want 32", end.EvalCount)
	}
	if end.CachedCount == nil || *end.CachedCount != 128 {
		t.Errorf("CachedCount = %v, want 128", end.CachedCount)
	}

	// message_start-only usage: output side stays nil.
	srv2 := sseAnthropicServer(t,
		`{"type":"message_start","message":{"usage":{"input_tokens":7}}}`,
		`{"type":"message_stop"}`,
	)
	defer srv2.Close()
	p2 := &Provider{typ: "anthropic", baseURL: srv2.URL, modelName: "glm-5.3", apiKey: "k"}
	got2 := collectEvents(t, p2.Stream(testContext(), []core.Message{core.NewUser("hi")}, nil))
	end2 := got2[len(got2)-1].(core.StreamEnd)
	if end2.PromptEvalCount == nil || *end2.PromptEvalCount != 7 {
		t.Errorf("PromptEvalCount = %v, want 7", end2.PromptEvalCount)
	}
	if end2.EvalCount != nil {
		t.Errorf("EvalCount = %v, want nil", *end2.EvalCount)
	}
	if end2.CachedCount != nil {
		t.Errorf("CachedCount = %v, want nil", *end2.CachedCount)
	}
}

// TestStreamAnthropicMaxTokensWithBudget verifies a thinking budget
// that exceeds the default max_tokens grows max_tokens (budget_tokens
// must be less than max_tokens on the real Anthropic API).
func TestStreamAnthropicMaxTokensWithBudget(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, "data: "+`{"type":"message_stop"}`+"\n\n")
	}))
	defer srv.Close()

	p := &Provider{typ: "anthropic", baseURL: srv.URL, modelName: "glm-5.3", apiKey: "k", thinkingLevel: "ultra"}
	collectEvents(t, p.Stream(testContext(), []core.Message{core.NewUser("hi")}, nil))

	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	maxTokens, _ := sent["max_tokens"].(float64)
	budget, _ := sent["thinking"].(map[string]any)["budget_tokens"].(float64)
	if int(budget) != 32768 {
		t.Errorf("budget_tokens = %v, want 32768", budget)
	}
	if int(maxTokens) <= int(budget) {
		t.Errorf("max_tokens = %v, want > budget_tokens %v", maxTokens, budget)
	}

	// No thinking level: plain 4096.
	p2 := &Provider{typ: "anthropic", baseURL: srv.URL, modelName: "glm-5.3", apiKey: "k"}
	collectEvents(t, p2.Stream(testContext(), []core.Message{core.NewUser("hi")}, nil))
	sent = nil
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if sent["max_tokens"].(float64) != 4096 {
		t.Errorf("max_tokens = %v, want 4096", sent["max_tokens"])
	}
	if _, ok := sent["thinking"]; ok {
		t.Errorf("thinking key present without a thinking level")
	}
}

// TestAnthropicConvertMessagesIsError verifies errored tool results get
// the is_error flag on the resulting tool_result block.
func TestAnthropicConvertMessagesIsError(t *testing.T) {
	messages := []map[string]any{
		{"role": "tool", "tool_call_id": "call_ok", "content": "fine"},
		{"role": "tool", "tool_call_id": "call_fail", "content": "boom", "is_error": true},
	}
	_, result := anthropicConvertMessages(messages)
	if len(result) != 1 {
		t.Fatalf("got %d messages, want 1 folded user message", len(result))
	}
	blocks, ok := result[0]["content"].([]map[string]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("content = %v, want 2 tool_result blocks", result[0]["content"])
	}
	if _, has := blocks[0]["is_error"]; has {
		t.Errorf("first block should not carry is_error: %v", blocks[0])
	}
	if blocks[1]["is_error"] != true {
		t.Errorf("second block is_error = %v, want true", blocks[1]["is_error"])
	}
}

// openaiSSEServer serves a canned openai chat.completions SSE stream.
func openaiSSEServer(t *testing.T, chunks ...string) *httptest.Server {
	t.Helper()
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString("data: " + c + "\n\n")
	}
	sb.WriteString("data: [DONE]\n\n")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sb.String())
	}))
}

// TestStreamOpenAIReasoningContent verifies the deepseek-style
// reasoning_content field maps to ThinkingChunk when the `reasoning`
// field is absent (parity with the ollama native thinking path).
func TestStreamOpenAIReasoningContent(t *testing.T) {
	srv := openaiSSEServer(t,
		`{"choices":[{"index":0,"delta":{"reasoning_content":"Working it out"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{"content":"Done."},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)
	defer srv.Close()

	p := &Provider{typ: "openai", baseURL: srv.URL, modelName: "glm-5.3", apiKey: "k"}
	got := collectEvents(t, p.Stream(testContext(), []core.Message{core.NewUser("hi")}, nil))

	var thinking, response string
	for _, e := range got {
		switch ev := e.(type) {
		case core.ThinkingChunk:
			thinking += ev.Content
		case core.ResponseChunk:
			response += ev.Content
		}
	}
	if thinking != "Working it out" {
		t.Errorf("thinking = %q, want %q", thinking, "Working it out")
	}
	if response != "Done." {
		t.Errorf("response = %q, want %q", response, "Done.")
	}
}

// TestStreamOpenAISynthesizesToolCallIDs verifies tool-call fragments
// without a wire id get stable synthetic ids (call_N), and a wire id
// on a later fragment still wins.
func TestStreamOpenAISynthesizesToolCallIDs(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		// index 0: no id ever; index 1: id arrives on the fragment.
		// Chunks are built with json.Marshal so the embedded argument
		// fragments stay valid JSON without hand-typed escapes.
		chunk := func(calls []map[string]any) string {
			b, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]any{"tool_calls": calls},
					"finish_reason": nil,
				}},
			})
			return string(b)
		}
		fmt.Fprintln(w, "data: "+chunk([]map[string]any{
			{"index": 0, "function": map[string]any{"name": "get_weather", "arguments": `{"city":`}},
			{"index": 1, "function": map[string]any{"name": "get_time", "arguments": ""}},
		}))
		fmt.Fprintln(w, "data: "+chunk([]map[string]any{
			{"index": 0, "function": map[string]any{"arguments": `"Tokyo"}`}},
			{"index": 1, "id": "call_wire", "function": map[string]any{"arguments": `{"tz":"JST"}`}},
		}))
		fmt.Fprintln(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	tools := []ai.ToolSchema{{Name: "get_weather", Description: "d", Parameters: map[string]any{"type": "object"}}}
	p := &Provider{typ: "openai", baseURL: srv.URL, modelName: "glm-5.3", apiKey: "k"}
	got := collectEvents(t, p.Stream(testContext(), []core.Message{core.NewUser("hi")}, tools))

	var calls []core.ToolCallEvent
	for _, e := range got {
		if ev, ok := e.(core.ToolCallEvent); ok {
			calls = append(calls, ev)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("got %d tool call events, want 2", len(calls))
	}
	if calls[0].ToolCall.ID != "call_1" {
		t.Errorf("call 0 id = %q, want call_1", calls[0].ToolCall.ID)
	}
	if calls[0].ToolCall.Arguments["city"] != "Tokyo" {
		t.Errorf("call 0 args = %v, want city=Tokyo", calls[0].ToolCall.Arguments)
	}
	if calls[1].ToolCall.ID != "call_wire" {
		t.Errorf("call 1 id = %q, want call_wire (wire id wins)", calls[1].ToolCall.ID)
	}
	if calls[1].ToolCall.Arguments["tz"] != "JST" {
		t.Errorf("call 1 args = %v, want tz=JST", calls[1].ToolCall.Arguments)
	}
	if len(gotBody) == 0 {
		t.Fatal("request body not captured")
	}
}

// TestStreamOpenAIAbsentUsage verifies a stream with no usage chunk
// reports absent counters (nil) on StreamEnd, matching the ollama
// native path instead of emitting fake zeros.
func TestStreamOpenAIAbsentUsage(t *testing.T) {
	srv := openaiSSEServer(t,
		`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)
	defer srv.Close()

	p := &Provider{typ: "openai", baseURL: srv.URL, modelName: "glm-5.3", apiKey: "k"}
	got := collectEvents(t, p.Stream(testContext(), []core.Message{core.NewUser("hi")}, nil))
	end := got[len(got)-1].(core.StreamEnd)
	if end.PromptEvalCount != nil {
		t.Errorf("PromptEvalCount = %v, want nil", *end.PromptEvalCount)
	}
	if end.EvalCount != nil {
		t.Errorf("EvalCount = %v, want nil", *end.EvalCount)
	}
	if end.CachedCount != nil {
		t.Errorf("CachedCount = %v, want nil", *end.CachedCount)
	}
}

// TestStreamOpenAIUsageReported verifies the usage chunk (opt-in via
// stream_options.include_usage) lands on the StreamEnd counters.
func TestStreamOpenAIUsageReported(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`)
		fmt.Fprintln(w, `data: {"choices":[],"usage":{"prompt_tokens":25,"completion_tokens":148,"prompt_tokens_details":{"cached_tokens":9}}}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := &Provider{typ: "openai", baseURL: srv.URL, modelName: "glm-5.3", apiKey: "k"}
	got := collectEvents(t, p.Stream(testContext(), []core.Message{core.NewUser("hi")}, nil))
	end := got[len(got)-1].(core.StreamEnd)
	if end.PromptEvalCount == nil || *end.PromptEvalCount != 25 {
		t.Errorf("PromptEvalCount = %v, want 25", end.PromptEvalCount)
	}
	if end.EvalCount == nil || *end.EvalCount != 148 {
		t.Errorf("EvalCount = %v, want 148", end.EvalCount)
	}
	if end.CachedCount == nil || *end.CachedCount != 9 {
		t.Errorf("CachedCount = %v, want 9", end.CachedCount)
	}

	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	opts, _ := sent["stream_options"].(map[string]any)
	if opts == nil || opts["include_usage"] != true {
		t.Errorf("stream_options = %v, want include_usage true", sent["stream_options"])
	}
}
