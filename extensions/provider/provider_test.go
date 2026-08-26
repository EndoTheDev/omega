package provider

import (
	"context"
	"strings"
	"testing"

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