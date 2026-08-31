package agent_loop

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// ---------------------------------------------------------------------------
// Mock implementations for PromptBuilder and CompactionProvider
// ---------------------------------------------------------------------------

// mockPromptBuilder implements agent.PromptBuilder, returning a fixed prompt
// and guidelines for testing the system-prompt prepend path.
type mockPromptBuilder struct {
	prompt     string
	guidelines []string
	called     bool
}

func (m *mockPromptBuilder) BuildPrompt(_ context.Context, _ agent.PromptBuildOptions) (string, bool) {
	m.called = true
	return m.prompt, true
}

func (m *mockPromptBuilder) Guidelines() []string {
	return m.guidelines
}

// mockCompactionProvider implements agent.CompactionProvider, recording
// calls and returning a compacted message set.
type mockCompactionProvider struct {
	called    bool
	callCount int
}

func (m *mockCompactionProvider) Compact(_ context.Context, messages []ai.Message) ([]ai.Message, error) {
	m.called = true
	m.callCount++
	// Replace the message history with a single system message so
	// len(compacted) < len(messages) and the log branch fires.
	return []ai.Message{ai.NewSystem("compacted")}, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRunContextCancellation covers lines 105-108: Run with a cancelled
// context. The loop should emit AgentEnd with FinishReason="cancelled" and
// an error containing "context canceled".
func TestRunContextCancellation(t *testing.T) {
	provider := ai.NewFakeProvider("test",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run starts

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(ctx, agent.LoopOptions{
			Provider: provider,
			Messages: []ai.Message{ai.NewUser("hi")},
			Events:   events,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "cancelled" {
		t.Errorf("FinishReason = %q, want %q", end.FinishReason, "cancelled")
	}
	if !strings.Contains(end.Error, "context canceled") {
		t.Errorf("Error = %q, want it to contain %q", end.Error, "context canceled")
	}
}

// TestRunToolProvidersMerging covers lines 39-57: passing ToolProviders
// (plural) with tools that get merged. The merged tool should be callable
// by the provider.
func TestRunToolProvidersMerging(t *testing.T) {
	provider := ai.NewFakeProviderScripts("test",
		// Turn 1: call a tool provided by ToolProviders.
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "merged_tool", Arguments: map[string]any{}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// Turn 2: respond and stop.
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "done"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	toolCalled := false
	toolProvider := agent.DefaultToolProvider{
		ToolsMap: map[string]agent.Tool{
			"merged_tool": {
				Description: "A tool from ToolProviders",
				Parameters:  map[string]any{"type": "object"},
				Run: func(_ context.Context, _ map[string]any) (string, error) {
					toolCalled = true
					return "merged_result", nil
				},
			},
		},
	}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider:      provider,
			Messages:      []ai.Message{ai.NewUser("go")},
			ToolProviders: []agent.ToolProvider{toolProvider},
			Events:        events,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", end.FinishReason)
	}
	if !toolCalled {
		t.Error("merged tool was not called; ToolProviders merging failed")
	}
}

// TestRunPromptBuilder covers lines 69-87: a PromptBuilder that returns a
// prompt and guidelines. The loop should prepend a system message containing
// the prompt and guidelines to the message history.
func TestRunPromptBuilder(t *testing.T) {
	provider := ai.NewFakeProvider("test",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)

	pb := &mockPromptBuilder{
		prompt:     "You are a test agent.",
		guidelines: []string{"Be concise.", "Use tools wisely."},
	}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider:     provider,
			Messages:     []ai.Message{ai.NewUser("hi")},
			PromptBuilder: pb,
			Events:        events,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", end.FinishReason)
	}
	if !pb.called {
		t.Error("PromptBuilder.BuildPrompt was not called")
	}
	// The FakeProvider records the messages passed to Stream in LastMessages.
	// The first message should be a System message containing the prompt and guidelines.
	if len(provider.LastMessages) == 0 {
		t.Fatal("no messages recorded by provider")
	}
	sysMsg, ok := provider.LastMessages[0].(ai.System)
	if !ok {
		t.Fatalf("first message = %T, want ai.System", provider.LastMessages[0])
	}
	if !strings.Contains(sysMsg.Content, "You are a test agent.") {
		t.Errorf("system message missing prompt: %q", sysMsg.Content)
	}
	if !strings.Contains(sysMsg.Content, "## Extension Guidelines") {
		t.Errorf("system message missing guidelines header: %q", sysMsg.Content)
	}
	if !strings.Contains(sysMsg.Content, "Be concise.") {
		t.Errorf("system message missing guideline 'Be concise.': %q", sysMsg.Content)
	}
}

// TestRunCompactionProvider covers lines 119-131: a CompactionProvider
// that compacts messages. It should be called each turn and the compacted
// messages should replace the original history.
func TestRunCompactionProvider(t *testing.T) {
	provider := ai.NewFakeProvider("test",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)

	compactor := &mockCompactionProvider{}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider:           provider,
			Messages:           []ai.Message{ai.NewUser("hi"), ai.NewUser("second"), ai.NewUser("third")},
			CompactionProvider: compactor,
			Events:             events,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", end.FinishReason)
	}
	if !compactor.called {
		t.Error("CompactionProvider.Compact was not called")
	}
	if compactor.callCount < 1 {
		t.Errorf("Compact call count = %d, want >= 1", compactor.callCount)
	}
	// After compaction, the provider should have received the compacted
	// messages (a single system message), not the original 3 messages.
	if len(provider.LastMessages) != 1 {
		t.Errorf("provider received %d messages, want 1 (compacted)", len(provider.LastMessages))
	}
}

// TestRunMaxToolOutputTruncation covers lines 232-234: setting MaxToolOutput
// to a small value so a tool's long output is truncated with the
// "... [truncated, N bytes total]" suffix.
func TestRunMaxToolOutputTruncation(t *testing.T) {
	longOutput := strings.Repeat("a", 100)

	provider := ai.NewFakeProviderScripts("test",
		// Turn 1: call the verbose tool.
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "verbose", Arguments: map[string]any{}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// Turn 2: stop.
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "done"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	tools := map[string]agent.Tool{
		"verbose": {
			Description: "Returns a long string",
			Parameters:  map[string]any{"type": "object"},
			Run: func(_ context.Context, _ map[string]any) (string, error) {
				return longOutput, nil
			},
		},
	}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider:      provider,
			Messages:      []ai.Message{ai.NewUser("go")},
			Tools:         tools,
			MaxToolOutput: 10,
			Events:        events,
		})
	}()

	var toolResult *ai.ToolResult
	for e := range events {
		if tre, ok := e.(agent.ToolResultEvent); ok {
			tr := tre.Message
			toolResult = &tr
		}
	}

	if toolResult == nil {
		t.Fatal("no ToolResultEvent received")
	}
	if !strings.Contains(toolResult.Content, "... [truncated") {
		t.Errorf("tool result = %q, want it to contain truncation suffix", toolResult.Content)
	}
	if !strings.Contains(toolResult.Content, fmt.Sprintf("%d bytes total", len(longOutput))) {
		t.Errorf("tool result = %q, want it to contain byte count %d", toolResult.Content, len(longOutput))
	}
	if toolResult.IsError {
		t.Error("tool result should not be an error")
	}
}

// TestRunUnknownTool covers line 219: the provider calls a tool that doesn't
// exist in the tools map. The result should contain "unknown tool: <name>"
// and have IsError=true.
func TestRunUnknownTool(t *testing.T) {
	provider := ai.NewFakeProviderScripts("test",
		// Turn 1: call a non-existent tool.
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "nonexistent", Arguments: map[string]any{}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// Turn 2: stop.
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "done"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []ai.Message{ai.NewUser("go")},
			Tools:    map[string]agent.Tool{}, // empty tools map, no "nonexistent" tool
			Events:   events,
		})
	}()

	var toolResult *ai.ToolResult
	for e := range events {
		if tre, ok := e.(agent.ToolResultEvent); ok {
			tr := tre.Message
			toolResult = &tr
		}
	}

	if toolResult == nil {
		t.Fatal("no ToolResultEvent received")
	}
	if !strings.Contains(toolResult.Content, "unknown tool: nonexistent") {
		t.Errorf("tool result = %q, want it to contain 'unknown tool: nonexistent'", toolResult.Content)
	}
	if !toolResult.IsError {
		t.Error("tool result should have IsError=true")
	}
}

// TestRunInjectedMessagesDraining covers lines 256-275: pass an
// InjectedMessages channel with buffered messages. After a turn, the loop
// should drain them, combine into a user message, and continue.
func TestRunInjectedMessagesDraining(t *testing.T) {
	provider := ai.NewFakeProviderScripts("test",
		// Turn 1: respond and stop (no tool calls).
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "first"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
		// Turn 2: after injection, respond and stop again.
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "second"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	injected := make(chan agent.InjectedMessage, 2)
	injected <- agent.InjectedMessage{Text: "injected message 1", Source: "test"}
	injected <- agent.InjectedMessage{Text: "injected message 2", Source: "test"}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider:         provider,
			Messages:         []ai.Message{ai.NewUser("go")},
			Events:           events,
			InjectedMessages: injected,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", end.FinishReason)
	}
	// The provider should have been called twice: once for the first turn,
	// once for the injected message turn.
	if provider.Calls() != 2 {
		t.Errorf("provider calls = %d, want 2", provider.Calls())
	}
	// The second call's messages should contain the injected content.
	if len(provider.LastMessages) == 0 {
		t.Fatal("no messages recorded by provider on second call")
	}
	// Find the user message with injected content in the second call's messages.
	var foundInjected bool
	for _, msg := range provider.LastMessages {
		if u, ok := msg.(ai.User); ok {
			if strings.Contains(u.Content, "injected message 1") && strings.Contains(u.Content, "injected message 2") {
				foundInjected = true
			}
		}
	}
	if !foundInjected {
		t.Error("injected messages were not combined into a user message for the second turn")
	}
}

// TestRunThinkingChunk covers lines 153-154: the provider sends
// ThinkingChunk events. They should appear in the assistant message's
// Thinking field.
func TestRunThinkingChunk(t *testing.T) {
	provider := ai.NewFakeProvider("test",
		ai.ThinkingChunk{Type: "thinking_chunk", Content: "Let me think..."},
		ai.ThinkingChunk{Type: "thinking_chunk", Content: " about this."},
		ai.ResponseChunk{Type: "response_chunk", Content: "Here is my answer."},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []ai.Message{ai.NewUser("hi")},
			Events:   events,
		})
	}()

	var assistantMsg *ai.Assistant
	for e := range events {
		if am, ok := e.(agent.AssistantMessageEvent); ok {
			msg := am.Message
			assistantMsg = &msg
		}
	}

	if assistantMsg == nil {
		t.Fatal("no AssistantMessageEvent received")
	}
	if assistantMsg.Thinking == nil {
		t.Fatal("Thinking field is nil, want non-nil")
	}
	expected := "Let me think... about this."
	if *assistantMsg.Thinking != expected {
		t.Errorf("Thinking = %q, want %q", *assistantMsg.Thinking, expected)
	}
	if assistantMsg.Content != "Here is my answer." {
		t.Errorf("Content = %q, want %q", assistantMsg.Content, "Here is my answer.")
	}
}

// TestRunToolProviderSingular covers passing a single ToolProvider
// via ToolProviders (plural) should merge its tools.
func TestRunToolProviderSingular(t *testing.T) {
	provider := ai.NewFakeProviderScripts("test",
		[]ai.StreamEvent{
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "c1", Name: "singular_tool", Arguments: map[string]any{}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "done"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	toolCalled := false
	toolProvider := agent.DefaultToolProvider{
		ToolsMap: map[string]agent.Tool{
			"singular_tool": {
				Description: "A tool from singular ToolProvider",
				Parameters:  map[string]any{"type": "object"},
				Run: func(_ context.Context, _ map[string]any) (string, error) {
					toolCalled = true
					return "singular_result", nil
				},
			},
		},
	}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider:      provider,
			Messages:      []ai.Message{ai.NewUser("go")},
			ToolProviders: []agent.ToolProvider{toolProvider},
			Events:        events,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", end.FinishReason)
	}
	if !toolCalled {
		t.Error("singular ToolProvider's tool was not called")
	}
}

// TestRunCompactionError covers lines 121-124: CompactionProvider.Compact
// returns an error. The loop should emit AgentEnd with FinishReason="error".
func TestRunCompactionError(t *testing.T) {
	provider := ai.NewFakeProvider("test",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)

	compactor := &errorCompactor{}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider:           provider,
			Messages:           []ai.Message{ai.NewUser("hi")},
			CompactionProvider: compactor,
			Events:             events,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "error" {
		t.Errorf("FinishReason = %q, want error", end.FinishReason)
	}
	if !strings.Contains(end.Error, "compaction failed") {
		t.Errorf("Error = %q, want it to contain 'compaction failed'", end.Error)
	}
}

type errorCompactor struct{}

func (errorCompactor) Compact(_ context.Context, _ []ai.Message) ([]ai.Message, error) {
	return nil, fmt.Errorf("compaction failed")
}

// TestRunOverflowRetryWithCompactor covers lines 174-187: an overflow error
// with a CompactionProvider triggers one auto-compaction and retry.
func TestRunOverflowRetryWithCompactor(t *testing.T) {
	provider := ai.NewFakeProviderScripts("test",
		// Turn 1: overflow error with no content streamed.
		[]ai.StreamEvent{
			ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "context length exceeded"},
		},
		// Turn 2 (retry): respond normally.
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "recovered"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	compactor := &mockCompactionProvider{}

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider:           provider,
			Messages:           []ai.Message{ai.NewUser("hi")},
			CompactionProvider: compactor,
			Events:             events,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", end.FinishReason)
	}
	// The compactor should have been called at least once for the retry.
	if compactor.callCount < 1 {
		t.Errorf("compactor call count = %d, want >= 1", compactor.callCount)
	}
	// The provider should have been called twice (original + retry).
	if provider.Calls() != 2 {
		t.Errorf("provider calls = %d, want 2", provider.Calls())
	}
}

// TestRunNilToolProviderEntry covers line 43: a nil entry in ToolProviders
// should be skipped without panic.
func TestRunNilToolProviderEntry(t *testing.T) {
	provider := ai.NewFakeProvider("test",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []ai.Message{ai.NewUser("hi")},
			// Include a nil entry in ToolProviders.
			ToolProviders: []agent.ToolProvider{nil, nil},
			Events:        events,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", end.FinishReason)
	}
}

// TestRunNilToolsMap covers lines 32-35: nil Tools map should be initialized
// to an empty map (the loop should not panic).
func TestRunNilToolsMap(t *testing.T) {
	provider := ai.NewFakeProvider("test",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []ai.Message{ai.NewUser("hi")},
			Tools:    nil, // nil tools map
			Events:   events,
		})
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", end.FinishReason)
	}
}

// TestRunBlockingInjectionCancellation covers lines 281-292: in one-shot
// mode with pending delegations, a cancelled context during the blocking
// select should emit AgentEnd with FinishReason="cancelled".
func TestRunBlockingInjectionCancellation(t *testing.T) {
	// Provider that always stops without tool calls, so we reach the
	// blocking-injection path on every turn.
	provider := ai.NewFakeProvider("test",
		ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)

	ctx, cancel := context.WithCancel(context.Background())
	injected := make(chan agent.InjectedMessage)

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(ctx, agent.LoopOptions{
			Provider:         provider,
			Messages:         []ai.Message{ai.NewUser("hi")},
			Events:           events,
			InjectedMessages: injected,
			// One-shot mode: UserInput is nil.
			UserInput: nil,
			PendingDelegations: func() int {
				return 1 // always report 1 pending delegation
			},
		})
	}()

	// Cancel the context shortly after the loop starts, so the blocking
	// select on <-ctx.Done() fires.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	end := drainEvents(t, events)
	if end.FinishReason != "cancelled" {
		t.Errorf("FinishReason = %q, want cancelled", end.FinishReason)
	}
}