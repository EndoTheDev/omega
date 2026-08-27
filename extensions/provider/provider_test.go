package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// testContext returns a background context for tests.
func testContext() context.Context { return context.Background() }

// drainEvents reads all events from a stream channel and returns
// the final StreamEnd. Fails the test if the last event is not a
// StreamEnd.
func drainEvents(events <-chan ai.StreamEvent) ai.StreamEnd {
	var end ai.StreamEnd
	for e := range events {
		if se, ok := e.(ai.StreamEnd); ok {
			end = se
		}
	}
	if end.Type == "" {
		panic("no StreamEnd event received")
	}
	return end
}

// TestProviderImplementsAIProvider verifies Provider satisfies the
// ai.Provider interface at compile time.
func TestProviderImplementsAIProvider(t *testing.T) {
	var _ ai.Provider = (*Provider)(nil)
}

// TestNewProviderDefaults verifies defaults are applied: empty type
// becomes "ollama", empty baseURL gets the Ollama default.
func TestNewProviderDefaults(t *testing.T) {
	p := NewProvider("", "", "", "")
	if p.typ != "ollama" {
		t.Fatalf("typ = %q, want %q", p.typ, "ollama")
	}
	if p.baseURL != "http://localhost:11434" {
		t.Fatalf("baseURL = %q, want %q", p.baseURL, "http://localhost:11434")
	}
}

// TestSetModel verifies SetModel updates the model name.
func TestSetModel(t *testing.T) {
	p := NewProvider("ollama", "old", "", "")
	p.SetModel("new")
	if p.ModelName() != "new" {
		t.Fatalf("ModelName() = %q, want %q", p.ModelName(), "new")
	}
}

// TestSetThinkingLevel verifies SetThinkingLevel stores the level.
func TestSetThinkingLevel(t *testing.T) {
	p := NewProvider("ollama", "m", "", "")
	p.SetThinkingLevel("high")
	if p.thinkingLevel != "high" {
		t.Fatalf("thinkingLevel = %q, want %q", p.thinkingLevel, "high")
	}
}

// TestUnknownProviderStreamError verifies Stream returns an error
// StreamEnd for an unknown provider type without panicking.
func TestUnknownProviderStreamError(t *testing.T) {
	p := &Provider{typ: "bogus", baseURL: "http://localhost"}
	events := p.Stream(testContext(), nil, nil)
	end := drainEvents(events)
	if end.FinishReason != "error" {
		t.Fatalf("FinishReason = %q, want %q", end.FinishReason, "error")
	}
	if end.Error == "" {
		t.Fatal("expected non-empty error message")
	}
}

// TestMessagesToJSON verifies role fields are added and timestamps
// are stripped for each message type.
func TestMessagesToJSON(t *testing.T) {
	msgs := []ai.Message{
		ai.NewSystem("sys"),
		ai.NewUser("hello"),
		ai.NewAssistant("hi"),
		ai.NewToolResult("result", "t1", false),
	}
	result := messagesToJSON(msgs)
	if len(result) != 4 {
		t.Fatalf("got %d maps, want 4", len(result))
	}
	wantRoles := []string{"system", "user", "assistant", "tool"}
	for i, want := range wantRoles {
		role, _ := result[i]["role"].(string)
		if role != want {
			t.Errorf("msg %d role = %q, want %q", i, role, want)
		}
		if _, hasTS := result[i]["timestamp"]; hasTS {
			t.Errorf("msg %d should not have timestamp", i)
		}
	}
}

// TestThinkingMappers verifies the three thinking-level mappers.
func TestThinkingMappers(t *testing.T) {
	// ollamaThinkValue
	if ollamaThinkValue("off") != false {
		t.Fatal("ollama off should be false")
	}
	if ollamaThinkValue("on") != true {
		t.Fatal("ollama on should be true")
	}
	if ollamaThinkValue("medium") != "medium" {
		t.Fatal("ollama medium should be medium")
	}
	if ollamaThinkValue("unknown") != nil {
		t.Fatal("ollama unknown should be nil")
	}

	// openaiReasoningEffort
	if openaiReasoningEffort("low") != "low" {
		t.Fatal("openai low should be low")
	}
	if openaiReasoningEffort("high") != "high" {
		t.Fatal("openai high should be high")
	}
	if openaiReasoningEffort("none") != "" {
		t.Fatal("openai none should be empty")
	}

	// anthropicBudgetTokens
	if anthropicBudgetTokens("low") != 2048 {
		t.Fatal("anthropic low should be 2048")
	}
	if anthropicBudgetTokens("ultra") != 32768 {
		t.Fatal("anthropic ultra should be 32768")
	}
	if anthropicBudgetTokens("none") != 0 {
		t.Fatal("anthropic none should be 0")
	}
}

// TestAnthropicConvertMessages verifies system prompt is lifted,
// tool results are folded, and assistant tool calls are converted.
func TestAnthropicConvertMessages(t *testing.T) {
	messages := []map[string]any{
		{"role": "system", "content": "be helpful"},
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello", "tool_calls": []any{
			map[string]any{
				"id": "t1",
				"function": map[string]any{
					"name":      "search",
					"arguments": map[string]any{"q": "test"},
				},
			},
		}},
		{"role": "tool", "content": "found", "tool_call_id": "t1"},
	}
	system, result := anthropicConvertMessages(messages)
	if system != "be helpful" {
		t.Fatalf("system = %q, want %q", system, "be helpful")
	}
	if len(result) != 3 {
		t.Fatalf("got %d messages, want 3", len(result))
	}
	// user message stays as-is
	if result[0]["role"] != "user" {
		t.Fatalf("msg 0 role = %v, want user", result[0]["role"])
	}
	// assistant message has content blocks with tool_use
	asstContent, _ := result[1]["content"].([]map[string]any)
	if len(asstContent) != 2 {
		t.Fatalf("assistant blocks = %d, want 2", len(asstContent))
	}
	if asstContent[1]["type"] != "tool_use" {
		t.Fatalf("block 1 type = %v, want tool_use", asstContent[1]["type"])
	}
	// tool result folded into user message
	if result[2]["role"] != "user" {
		t.Fatalf("msg 2 role = %v, want user", result[2]["role"])
	}
	toolBlocks, _ := result[2]["content"].([]map[string]any)
	if len(toolBlocks) != 1 {
		t.Fatalf("tool blocks = %d, want 1", len(toolBlocks))
	}
	if toolBlocks[0]["type"] != "tool_result" {
		t.Fatalf("tool block type = %v, want tool_result", toolBlocks[0]["type"])
	}
}

// TestPluginInterface verifies the Plugin adapter returns the right
// metadata.
func TestPluginInterface(t *testing.T) {
	p := Plugin{}
	if p.Name() != "provider" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "provider")
	}
	provides := p.Provides()
	if len(provides) != 1 || provides[0] != "provider" {
		t.Fatalf("Provides() = %v, want [provider]", provides)
	}
	if len(p.Requires()) != 0 {
		t.Fatalf("Requires() = %v, want []", p.Requires())
	}
}

// TestProviderModelCommand verifies /model <name> sets the model
// via Provider.SetModel and returns set_model + refresh_title +
// fetch_model_info CmdActions.
func TestProviderModelCommand(t *testing.T) {
	p := &Plugin{}
	ctx := &agent.Context{}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	result, err := ctx.CommandHandler(context.Background(), "/model", "qwen2.5")
	if err != nil {
		t.Fatalf("CommandHandler: %v", err)
	}
	if !strings.Contains(result.Text, "switched to qwen2.5") {
		t.Fatalf("expected confirmation text, got %q", result.Text)
	}
	if len(result.Actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(result.Actions))
	}
	if result.Actions[0].Type != "set_model" || result.Actions[0].Value != "qwen2.5" {
		t.Fatalf("expected set_model action, got %+v", result.Actions[0])
	}
	if result.Actions[1].Type != "refresh_title" {
		t.Fatalf("expected refresh_title action, got %+v", result.Actions[1])
	}
	if result.Actions[2].Type != "fetch_model_info" {
		t.Fatalf("expected fetch_model_info action, got %+v", result.Actions[2])
	}
}

// TestProviderModelNoArgs verifies /model with no args returns usage.
func TestProviderModelNoArgs(t *testing.T) {
	p := &Plugin{}
	ctx := &agent.Context{}
	p.Mount(ctx)

	result, err := ctx.CommandHandler(context.Background(), "/model", "")
	if err != nil {
		t.Fatalf("CommandHandler: %v", err)
	}
	if !strings.Contains(result.Text, "usage") {
		t.Fatalf("expected usage text, got %q", result.Text)
	}
}

// TestProviderInfoCommand verifies /provider returns provider + model info.
func TestProviderInfoCommand(t *testing.T) {
	p := &Plugin{}
	ctx := &agent.Context{}
	p.Mount(ctx)

	result, err := ctx.CommandHandler(context.Background(), "/provider", "")
	if err != nil {
		t.Fatalf("CommandHandler: %v", err)
	}
	if !strings.Contains(result.Text, "provider:") {
		t.Fatalf("expected provider info, got %q", result.Text)
	}
	if !strings.Contains(result.Text, "model:") {
		t.Fatalf("expected model info, got %q", result.Text)
	}
}

// TestListModels verifies listModels against fake HTTP servers for
// each provider type: endpoint paths, model name extraction, sorting,
// unknown types, and HTTP error statuses.
func TestListModels(t *testing.T) {
	// ollama: GET /api/tags, "models" array with "name" fields.
	var ollamaPath string
	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ollamaPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"models":[{"name":"qwen"},{"name":"llama3"}]}`)
	}))
	defer ollamaSrv.Close()

	p := &Provider{typ: "ollama", baseURL: ollamaSrv.URL}
	names, err := p.listModels()
	if err != nil {
		t.Fatalf("listModels (ollama): %v", err)
	}
	if ollamaPath != "/api/tags" {
		t.Errorf("ollama request path = %q, want /api/tags", ollamaPath)
	}
	if !reflect.DeepEqual(names, []string{"llama3", "qwen"}) {
		t.Errorf("ollama names = %v, want [llama3 qwen] (sorted)", names)
	}

	// openai: GET /models, "data" array with "id" fields.
	var openaiPath string
	openaiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openaiPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"gpt-x"}]}`)
	}))
	defer openaiSrv.Close()

	p = &Provider{typ: "openai", baseURL: openaiSrv.URL, apiKey: "sk-test"}
	names, err = p.listModels()
	if err != nil {
		t.Fatalf("listModels (openai): %v", err)
	}
	if openaiPath != "/models" {
		t.Errorf("openai request path = %q, want /models", openaiPath)
	}
	if !reflect.DeepEqual(names, []string{"gpt-x"}) {
		t.Errorf("openai names = %v, want [gpt-x]", names)
	}

	// anthropic: GET /models, "data" array with "id" fields.
	var anthropicPath string
	anthropicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"claude-x"}]}`)
	}))
	defer anthropicSrv.Close()

	p = &Provider{typ: "anthropic", baseURL: anthropicSrv.URL, apiKey: "ak-test"}
	names, err = p.listModels()
	if err != nil {
		t.Fatalf("listModels (anthropic): %v", err)
	}
	if anthropicPath != "/models" {
		t.Errorf("anthropic request path = %q, want /models", anthropicPath)
	}
	if !reflect.DeepEqual(names, []string{"claude-x"}) {
		t.Errorf("anthropic names = %v, want [claude-x]", names)
	}

	// unknown provider type errors out before any HTTP request.
	p = &Provider{typ: "bogus", baseURL: ollamaSrv.URL}
	if names, err := p.listModels(); err == nil {
		t.Errorf("listModels (unknown type) = %v, want error", names)
	}

	// HTTP 500 surfaces as an error.
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer errSrv.Close()

	p = &Provider{typ: "ollama", baseURL: errSrv.URL}
	if names, err := p.listModels(); err == nil {
		t.Errorf("listModels (HTTP 500) = %v, want error", names)
	}
}

// TestModelInfo verifies modelInfo: ollama /api/show context_length
// extraction, non-200 error, and 0/nil for other provider types.
func TestModelInfo(t *testing.T) {
	var gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model_info":{"gemma4.context_length": 131072}}`)
	}))
	defer srv.Close()

	// ollama: returns the context length from model_info.
	p := &Provider{typ: "ollama", baseURL: srv.URL, modelName: "gemma4"}
	info, err := p.modelInfo()
	if err != nil {
		t.Fatalf("modelInfo (ollama): %v", err)
	}
	if info != 131072 {
		t.Errorf("modelInfo (ollama) = %d, want 131072", info)
	}
	if gotPath != "/api/show" {
		t.Errorf("request path = %q, want /api/show", gotPath)
	}
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if sent["model"] != "gemma4" {
		t.Errorf("request body model = %v, want gemma4", sent["model"])
	}

	// non-200 response is an error.
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer errSrv.Close()

	p = &Provider{typ: "ollama", baseURL: errSrv.URL, modelName: "gemma4"}
	if info, err := p.modelInfo(); err == nil {
		t.Errorf("modelInfo (HTTP 500) = %d, want error", info)
	}

	// openai/anthropic: not exposed, returns 0, nil without HTTP.
	p = &Provider{typ: "openai", baseURL: srv.URL}
	if info, err := p.modelInfo(); err != nil || info != 0 {
		t.Errorf("modelInfo (openai) = %d, %v; want 0, nil", info, err)
	}
	p = &Provider{typ: "anthropic", baseURL: srv.URL}
	if info, err := p.modelInfo(); err != nil || info != 0 {
		t.Errorf("modelInfo (anthropic) = %d, %v; want 0, nil", info, err)
	}
}

// TestSendEvent verifies sendEvent delivers on a ready channel and
// drops the event instead of blocking when the context is cancelled
// and the channel is full.
func TestSendEvent(t *testing.T) {
	// buffered channel: event is delivered.
	events := make(chan ai.StreamEvent, 1)
	sendEvent(testContext(), events, ai.StreamEnd{FinishReason: "stop"})
	ev := <-events
	end, ok := ev.(ai.StreamEnd)
	if !ok {
		t.Fatalf("received event type %T, want ai.StreamEnd", ev)
	}
	if end.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", end.FinishReason, "stop")
	}

	// unbuffered channel + cancelled context: returns without blocking.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		sendEvent(ctx, make(chan ai.StreamEvent), ai.StreamEnd{FinishReason: "stop"})
	}()
	select {
	case <-done:
		// returned without blocking.
	case <-time.After(2 * time.Second):
		t.Fatal("sendEvent blocked on cancelled context with full channel")
	}
}

// testToolCall is the generic parameter for flushToolCalls tests.
type testToolCall struct {
	ID   string
	Name string
	JSON string
}

// TestFlushToolCalls verifies flushToolCalls: empty pending sends
// nothing, and out-of-order pending entries are flushed in index
// order with their argument JSON unmarshalled.
func TestFlushToolCalls(t *testing.T) {
	// empty pending: no events.
	events := make(chan ai.StreamEvent, 4)
	flushToolCalls(testContext(), events, map[int]*testToolCall{},
		func(tc *testToolCall) string { return tc.ID },
		func(tc *testToolCall) string { return tc.Name },
		func(tc *testToolCall) string { return tc.JSON })
	if len(events) != 0 {
		t.Fatalf("empty pending sent %d events, want 0", len(events))
	}

	// out-of-index-order entries flush in sorted index order.
	pending := map[int]*testToolCall{
		2: {ID: "c3", Name: "third", JSON: `{"n":3}`},
		0: {ID: "c1", Name: "first", JSON: `{"n":1}`},
		1: {ID: "c2", Name: "second", JSON: `{"n":2}`},
	}
	flushToolCalls(testContext(), events, pending,
		func(tc *testToolCall) string { return tc.ID },
		func(tc *testToolCall) string { return tc.Name },
		func(tc *testToolCall) string { return tc.JSON })

	wantOrder := []struct{ id, name string; n float64 }{
		{"c1", "first", 1},
		{"c2", "second", 2},
		{"c3", "third", 3},
	}
	for i, want := range wantOrder {
		if len(events) == 0 {
			t.Fatalf("event %d: channel empty, want ToolCallEvent", i)
		}
		ev := <-events
		tce, ok := ev.(ai.ToolCallEvent)
		if !ok {
			t.Fatalf("event %d: got type %T, want ai.ToolCallEvent", i, ev)
		}
		if tce.Type != "tool_call" {
			t.Errorf("event %d: Type = %q, want %q", i, tce.Type, "tool_call")
		}
		if tce.ToolCall.ID != want.id {
			t.Errorf("event %d: ID = %q, want %q (index order)", i, tce.ToolCall.ID, want.id)
		}
		if tce.ToolCall.Name != want.name {
			t.Errorf("event %d: Name = %q, want %q", i, tce.ToolCall.Name, want.name)
		}
		if n, _ := tce.ToolCall.Arguments["n"].(float64); n != want.n {
			t.Errorf("event %d: args n = %v, want %v", i, tce.ToolCall.Arguments["n"], want.n)
		}
	}
	if len(events) != 0 {
		t.Fatalf("got %d extra events after draining", len(events))
	}
}

// collectEvents reads all events from a stream channel and returns
// them in order. Fails the test if no events arrive or the last
// event is not a StreamEnd.
func collectEvents(t *testing.T, events <-chan ai.StreamEvent) []ai.StreamEvent {
	t.Helper()
	var out []ai.StreamEvent
	for e := range events {
		out = append(out, e)
	}
	if len(out) == 0 {
		t.Fatal("no events received")
	}
	if _, ok := out[len(out)-1].(ai.StreamEnd); !ok {
		t.Fatalf("last event type %T, want ai.StreamEnd", out[len(out)-1])
	}
	return out
}

// TestStreamOllama verifies Stream against a fake Ollama NDJSON
// endpoint: content chunks, done-line counters, request path, and
// the thinking-level "think" field in the request body.
func TestStreamOllama(t *testing.T) {
	var gotPath string
	bodies := make(chan []byte, 8) // ponytail: handler pushes each request body in request order; channel beats a mutex+racy shared var
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		bodies <- body
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"Hello "}}`)
		fmt.Fprintln(w, `{"message":{"content":"world"}}`)
		fmt.Fprintln(w, `{"done":true,"prompt_eval_count":12,"eval_count":34}`)
	}))
	defer srv.Close()

	p := &Provider{typ: "ollama", baseURL: srv.URL, modelName: "llama3"}
	got := collectEvents(t, p.Stream(testContext(), []ai.Message{ai.NewUser("hi")}, nil))
	<-bodies // drain the default-level request body

	if gotPath != "/api/chat" {
		t.Errorf("request path = %q, want /api/chat", gotPath)
	}

	wantContents := []string{"Hello ", "world"}
	chunks := 0
	for _, e := range got {
		rc, ok := e.(ai.ResponseChunk)
		if !ok {
			continue
		}
		if chunks >= len(wantContents) {
			t.Fatalf("got more than %d ResponseChunk events", len(wantContents))
		}
		if rc.Content != wantContents[chunks] {
			t.Errorf("chunk %d content = %q, want %q", chunks, rc.Content, wantContents[chunks])
		}
		chunks++
	}
	if chunks != len(wantContents) {
		t.Fatalf("got %d ResponseChunk events, want %d", chunks, len(wantContents))
	}

	end := got[len(got)-1].(ai.StreamEnd)
	if end.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", end.FinishReason, "stop")
	}
	if end.PromptEvalCount == nil || *end.PromptEvalCount != 12 {
		t.Errorf("PromptEvalCount = %v, want 12", end.PromptEvalCount)
	}
	if end.EvalCount == nil || *end.EvalCount != 34 {
		t.Errorf("EvalCount = %v, want 34", end.EvalCount)
	}

	// thinking level "high" maps to "think":"high" in the request body.
	p = &Provider{typ: "ollama", baseURL: srv.URL, modelName: "llama3", thinkingLevel: "high"}
	collectEvents(t, p.Stream(testContext(), []ai.Message{ai.NewUser("hi")}, nil))
	var sent map[string]any
	if err := json.Unmarshal(<-bodies, &sent); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if sent["think"] != "high" {
		t.Errorf("request body think = %v, want %q", sent["think"], "high")
	}

	// thinking level "none" omits the "think" key entirely.
	p = &Provider{typ: "ollama", baseURL: srv.URL, modelName: "llama3", thinkingLevel: "none"}
	collectEvents(t, p.Stream(testContext(), []ai.Message{ai.NewUser("hi")}, nil))
	sent = nil
	if err := json.Unmarshal(<-bodies, &sent); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if _, ok := sent["think"]; ok {
		t.Errorf("request body should not contain a think key, got %v", sent["think"])
	}
}

// TestStreamOpenAI verifies Stream against a fake OpenAI SSE endpoint:
// content chunks, fragmented tool-call accumulation, finish_reason,
// Authorization header, and the in-stream error event.
func TestStreamOpenAI(t *testing.T) {
	var gotPath string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call1\",\"function\":{\"name\":\"shell\",\"arguments\":\"{\\\"cmd\\\":\"}},{\"index\":0,\"id\":\"\",\"function\":{\"name\":null,\"arguments\":\"\\\"ls\\\"}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := &Provider{typ: "openai", baseURL: srv.URL, modelName: "gpt-x", apiKey: "test-key"}
	got := collectEvents(t, p.Stream(testContext(), []ai.Message{ai.NewUser("hi")}, nil))

	if gotPath != "/chat/completions" {
		t.Errorf("request path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
	}

	chunks := 0
	var toolCalls []ai.ToolCallEvent
	for _, e := range got {
		switch ev := e.(type) {
		case ai.ResponseChunk:
			if ev.Content != "Hi" {
				t.Errorf("chunk content = %q, want %q", ev.Content, "Hi")
			}
			chunks++
		case ai.ToolCallEvent:
			toolCalls = append(toolCalls, ev)
		}
	}
	if chunks != 1 {
		t.Errorf("got %d ResponseChunk events, want 1", chunks)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("got %d ToolCallEvents, want 1", len(toolCalls))
	}
	tc := toolCalls[0].ToolCall
	if tc.ID != "call1" {
		t.Errorf("tool call ID = %q, want %q", tc.ID, "call1")
	}
	if tc.Name != "shell" {
		t.Errorf("tool call Name = %q, want %q", tc.Name, "shell")
	}
	if args, _ := tc.Arguments["cmd"].(string); args != "ls" {
		t.Errorf("tool call args cmd = %v, want %q (fragments accumulated)", tc.Arguments["cmd"], "ls")
	}

	end := got[len(got)-1].(ai.StreamEnd)
	if end.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want %q", end.FinishReason, "tool_calls")
	}

	// error path: in-stream error JSON surfaces as an error StreamEnd.
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"error\":{\"message\":\"bad\"}}\n\n")
	}))
	defer errSrv.Close()

	p = &Provider{typ: "openai", baseURL: errSrv.URL, apiKey: "test-key"}
	end = drainEvents(p.Stream(testContext(), []ai.Message{ai.NewUser("hi")}, nil))
	if end.FinishReason != "error" {
		t.Errorf("error path FinishReason = %q, want %q", end.FinishReason, "error")
	}
	if !strings.Contains(end.Error, "bad") {
		t.Errorf("error path Error = %q, want it to contain %q", end.Error, "bad")
	}
}

// TestStreamAnthropic verifies Stream against a fake Anthropic SSE
// endpoint: text deltas, fragmented input_json_delta accumulation,
// stop_reason, auth headers, and the in-stream error event.
func TestStreamAnthropic(t *testing.T) {
	var gotPath string
	var gotAPIKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tu1\",\"name\":\"files.read\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"x.go\\\"}\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"hey\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\n")
	}))
	defer srv.Close()

	p := &Provider{typ: "anthropic", baseURL: srv.URL, modelName: "claude-x", apiKey: "test-key"}
	got := collectEvents(t, p.Stream(testContext(), []ai.Message{ai.NewUser("hi")}, nil))

	if gotPath != "/messages" {
		t.Errorf("request path = %q, want /messages", gotPath)
	}
	if gotAPIKey != "test-key" {
		t.Errorf("x-api-key = %q, want %q", gotAPIKey, "test-key")
	}
	if gotVersion != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want %q", gotVersion, "2023-06-01")
	}

	chunks := 0
	var toolCalls []ai.ToolCallEvent
	for _, e := range got {
		switch ev := e.(type) {
		case ai.ResponseChunk:
			if ev.Content != "hey" {
				t.Errorf("chunk content = %q, want %q", ev.Content, "hey")
			}
			chunks++
		case ai.ToolCallEvent:
			toolCalls = append(toolCalls, ev)
		}
	}
	if chunks != 1 {
		t.Errorf("got %d ResponseChunk events, want 1", chunks)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("got %d ToolCallEvents, want 1", len(toolCalls))
	}
	tc := toolCalls[0].ToolCall
	if tc.ID != "tu1" {
		t.Errorf("tool call ID = %q, want %q", tc.ID, "tu1")
	}
	if tc.Name != "files.read" {
		t.Errorf("tool call Name = %q, want %q", tc.Name, "files.read")
	}
	if args, _ := tc.Arguments["path"].(string); args != "x.go" {
		t.Errorf("tool call args path = %v, want %q (fragments accumulated)", tc.Arguments["path"], "x.go")
	}

	end := got[len(got)-1].(ai.StreamEnd)
	if end.FinishReason != "tool_use" {
		t.Errorf("FinishReason = %q, want %q", end.FinishReason, "tool_use")
	}

	// error path: in-stream error event surfaces as an error StreamEnd.
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"type\":\"error\",\"error\":{\"message\":\"overloaded\"}}\n\n")
	}))
	defer errSrv.Close()

	p = &Provider{typ: "anthropic", baseURL: errSrv.URL, apiKey: "test-key"}
	end = drainEvents(p.Stream(testContext(), []ai.Message{ai.NewUser("hi")}, nil))
	if end.FinishReason != "error" {
		t.Errorf("error path FinishReason = %q, want %q", end.FinishReason, "error")
	}
	if !strings.Contains(end.Error, "overloaded") {
		t.Errorf("error path Error = %q, want it to contain %q", end.Error, "overloaded")
	}
}