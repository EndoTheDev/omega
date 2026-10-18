package provider

// Live probes against the keypool gateway surface. Skipped unless
// KEYPOOL_CLIENT_KEY is set - no secrets in the tree, the key travels
// via env. Base URL defaults to the standard keypool port.
//
//	KEYPOOL_CLIENT_KEY=<key> KEYPOOL_HOST=http://127.0.0.1:11435 \
//	  go test ./modules/provider/ -run TestLive -v

import (
	"os"
	"testing"

	"github.com/EndoTheDev/omega/ai"
)

const keypoolModel = "glm-5.3"

func keypoolProvider(t *testing.T, typ string) (*Provider, string) {
	t.Helper()
	host := os.Getenv("KEYPOOL_HOST")
	if host == "" {
		host = "http://127.0.0.1:11435/v1"
	}
	return &Provider{typ: typ, baseURL: host, modelName: keypoolModel, apiKey: os.Getenv("KEYPOOL_CLIENT_KEY")}, host
}

// TestLiveKeypoolAnthropic drives the anthropic lane end to end: auth,
// streaming with thinking chunks, a full tool loop (tool_use -> folded
// tool_result -> final text), usage counters, and rotation across
// repeated requests (each request is a separate HTTP connection).
func TestLiveKeypoolAnthropic(t *testing.T) {
	if os.Getenv("KEYPOOL_CLIENT_KEY") == "" {
		t.Skip("KEYPOOL_CLIENT_KEY not set; live keypool probe skipped")
	}
	p, host := keypoolProvider(t, "anthropic")
	tools := []ai.ToolSchema{{
		Name:        "get_weather",
		Description: "Get current weather for a city",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []any{"city"}},
	}}

	// Turn 1: expect a get_weather tool call.
	got := collectEvents(t, p.Stream(testContext(), []ai.Message{ai.NewUser("Use the get_weather tool for Tokyo. Do not answer from your own knowledge.")}, tools))
	text, thinking := "", ""
	var callID string
	var end ai.StreamEnd
	for _, e := range got {
		switch ev := e.(type) {
		case ai.ResponseChunk:
			text += ev.Content
		case ai.ThinkingChunk:
			thinking += ev.Content
		case ai.ToolCallEvent:
			t.Logf("tool call: id=%s name=%s args=%v", ev.ToolCall.ID, ev.ToolCall.Name, ev.ToolCall.Arguments)
			if callID != "" {
				t.Logf("second tool call event; keeping first id")
			}
			callID = ev.ToolCall.ID
		case ai.StreamEnd:
			end = ev
		}
	}
	if end.Error != "" {
		t.Fatalf("turn 1 stream error: %s", end.Error)
	}
	if callID == "" {
		t.Fatalf("turn 1: no tool call event (finish_reason=%q)", end.FinishReason)
	}
	if end.FinishReason != "tool_use" {
		t.Errorf("turn 1 finish_reason = %q, want tool_use", end.FinishReason)
	}
	t.Logf("turn 1: thinking %d chars, stop=%s", len(thinking), end.FinishReason)

	// Turn 2: send assistant tool_use + folded tool_result back.
	messages := []ai.Message{
		ai.NewUser("Use the get_weather tool for Tokyo. Do not answer from your own knowledge."),
		ai.Assistant{Content: "", ToolCalls: []ai.ToolCall{{ID: callID, Name: "get_weather", Arguments: map[string]any{"city": "Tokyo"}}}},
		ai.NewToolResult("22C, sunny", callID, false),
	}
	p2, _ := keypoolProvider(t, "anthropic") // fresh provider: separate request, same client key (rotation)
	got2 := collectEvents(t, p2.Stream(testContext(), messages, tools))
	text2, end2 := "", ai.StreamEnd{}
	for _, e := range got2 {
		switch ev := e.(type) {
		case ai.ResponseChunk:
			text2 += ev.Content
		case ai.StreamEnd:
			end2 = ev
		}
	}
	if end2.Error != "" {
		t.Fatalf("turn 2 stream error: %s", end2.Error)
	}
	if end2.FinishReason != "end_turn" {
		t.Errorf("turn 2 finish_reason = %q, want end_turn", end2.FinishReason)
	}
	if text2 == "" {
		t.Error("turn 2: no response text")
	}
	t.Logf("turn 2: %d chars, usage in=%v out=%v cached=%v",
		len(text2), deref(end2.PromptEvalCount), deref(end2.EvalCount), deref(end2.CachedCount),
	)
	_ = host // logged below for the transcript
	t.Logf("lane=%s host=%s", "anthropic", host)
}

// TestLiveKeypoolOpenAI drives the openai lane end to end: auth,
// streaming, reasoning chunks, tool-call round trip, usage counters.
func TestLiveKeypoolOpenAI(t *testing.T) {
	if os.Getenv("KEYPOOL_CLIENT_KEY") == "" {
		t.Skip("KEYPOOL_CLIENT_KEY not set; live keypool probe skipped")
	}
	p, host := keypoolProvider(t, "openai")
	tools := []ai.ToolSchema{{
		Name:        "get_weather",
		Description: "Get current weather for a city",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []any{"city"}},
	}}

	got := collectEvents(t, p.Stream(testContext(), []ai.Message{ai.NewUser("Use the get_weather tool for Tokyo.")}, tools))
	text, thinking := "", ""
	var calls []ai.ToolCallEvent
	end := ai.StreamEnd{}
	for _, e := range got {
		switch ev := e.(type) {
		case ai.ResponseChunk:
			text += ev.Content
		case ai.ThinkingChunk:
			thinking += ev.Content
		case ai.ToolCallEvent:
			calls = append(calls, ev)
		case ai.StreamEnd:
			end = ev
		}
	}
	if end.Error != "" {
		t.Fatalf("stream error: %s", end.Error)
	}
	if len(calls) == 0 {
		t.Fatalf("no tool call event (finish_reason=%q)", end.FinishReason)
	}
	if end.FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", end.FinishReason)
	}
	if thinking == "" {
		t.Log("note: no reasoning deltas on this lane/model")
	}
	t.Logf("openai lane host=%s: %d tool call(s), thinking %d chars, usage in=%v out=%v cached=%v",
		host, len(calls), len(thinking), deref(end.PromptEvalCount), deref(end.EvalCount), deref(end.CachedCount))

	// Tool result round trip.
	messages := []ai.Message{
		ai.NewUser("Use the get_weather tool for Tokyo."),
		ai.Assistant{Content: "", ToolCalls: []ai.ToolCall{{ID: calls[0].ToolCall.ID, Name: "get_weather", Arguments: calls[0].ToolCall.Arguments}}},
		ai.NewToolResult("22C, sunny", calls[0].ToolCall.ID, false),
	}
	got2 := collectEvents(t, p.Stream(testContext(), messages, tools))
	text2, end2 := "", ai.StreamEnd{}
	for _, e := range got2 {
		switch ev := e.(type) {
		case ai.ResponseChunk:
			text2 += ev.Content
		case ai.StreamEnd:
			end2 = ev
		}
	}
	if end2.Error != "" {
		t.Fatalf("round-trip stream error: %s", end2.Error)
	}
	if text2 == "" {
		t.Error("round trip: no response text")
	}
}

func deref(n *int) any {
	if n == nil {
		return "nil"
	}
	return *n
}