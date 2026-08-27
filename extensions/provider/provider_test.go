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