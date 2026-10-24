package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

func TestEstimateTokens(t *testing.T) {
	history := []core.Message{
		core.NewUser("hello world"),                           // 11 chars -> 2 tokens
		core.NewAssistant("a longer assistant response here"), // 32 chars -> 8 tokens
	}
	got := EstimateTokens(history)
	if got != 10 { // (11+32)/4 = 10
		t.Fatalf("estimateTokens = %d, want 10", got)
	}
}

func TestBuildCompactedMessages(t *testing.T) {
	history := []core.Message{
		core.NewSystem("sys"),
		core.NewUser("u1"),
		core.NewUser("u2"),
		core.NewUser("u3"),
		core.NewUser("u4"),
		core.NewUser("u5"),
	}
	got := BuildCompactedMessages(history, "summary text", 2, 2)
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5 (2 first + 1 summary + 2 last)", len(got))
	}
	// First two preserved verbatim.
	if got[0].(core.System).Content != "sys" || got[1].(core.User).Content != "u1" {
		t.Fatalf("first messages not preserved: %+v", got[:2])
	}
	// Middle replaced with a system summary.
	sys, ok := got[2].(core.System)
	if !ok || !strings.Contains(sys.Content, "summary text") {
		t.Fatalf("middle not replaced with summary: %+v", got[2])
	}
	// Last two preserved verbatim.
	if got[3].(core.User).Content != "u4" || got[4].(core.User).Content != "u5" {
		t.Fatalf("last messages not preserved: %+v", got[3:])
	}
}

func TestBuildCompactedMessagesNoOpWhenNothingToCompact(t *testing.T) {
	history := []core.Message{core.NewUser("a"), core.NewUser("b")}
	got := BuildCompactedMessages(history, "summary", 1, 1)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (1 first + 1 summary + 1 last)", len(got))
	}
}

// mockCompactionProvider is a minimal CompactionProvider for agent
// loop tests. It streams a one-line summary from the fake provider
// and wraps it with BuildCompactedMessages. Disabled or under-budget
// = no-op.
type mockCompactionProvider struct {
	Provider ai.Provider
	Config   *CompactionConfig
}

func TestBudgetDefaultConfig(t *testing.T) {
	// Zero-value config: window defaults to 8192, reserve to 16384.
	// effective = 8192 - 16384 < 0, so effective = 8192/2 = 4096.
	// Threshold is 0, so result = 0.
	c := CompactionConfig{}
	got := c.Budget()
	if got != 0 {
		t.Fatalf("Budget() zero config = %d, want 0", got)
	}
}

func TestBudgetExplicitWindowAndReserve(t *testing.T) {
	c := CompactionConfig{ContextWindow: 100000, ReserveTokens: 10000, Threshold: 0.6}
	got := c.Budget()
	// effective = 100000 - 10000 = 90000; 90000 * 0.6 = 54000
	if got != 54000 {
		t.Fatalf("Budget() = %d, want 54000", got)
	}
}

func TestBudgetReserveLargerThanHalfWindow(t *testing.T) {
	c := CompactionConfig{ContextWindow: 20000, ReserveTokens: 16384, Threshold: 0.6}
	got := c.Budget()
	// effective = 20000 - 16384 = 3616 < 10000 (half), so effective = 10000.
	// 10000 * 0.6 = 6000
	if got != 6000 {
		t.Fatalf("Budget() = %d, want 6000", got)
	}
}

func TestBudgetThresholdZero(t *testing.T) {
	c := CompactionConfig{ContextWindow: 100000, ReserveTokens: 10000, Threshold: 0.0}
	got := c.Budget()
	if got != 0 {
		t.Fatalf("Budget() threshold 0 = %d, want 0", got)
	}
}

func TestBudgetThresholdOne(t *testing.T) {
	c := CompactionConfig{ContextWindow: 100000, ReserveTokens: 10000, Threshold: 1.0}
	got := c.Budget()
	// effective = 90000; 90000 * 1.0 = 90000
	if got != 90000 {
		t.Fatalf("Budget() threshold 1 = %d, want 90000", got)
	}
}

func TestMessageTextSystem(t *testing.T) {
	got := MessageText(core.NewSystem("system prompt"))
	if got != "system prompt" {
		t.Fatalf("MessageText(System) = %q, want %q", got, "system prompt")
	}
}

func TestMessageTextUser(t *testing.T) {
	got := MessageText(core.NewUser("user text"))
	if got != "user text" {
		t.Fatalf("MessageText(User) = %q, want %q", got, "user text")
	}
}

func TestMessageTextAssistant(t *testing.T) {
	got := MessageText(core.NewAssistant("assistant reply"))
	if got != "assistant reply" {
		t.Fatalf("MessageText(Assistant) = %q, want %q", got, "assistant reply")
	}
}

func TestMessageTextToolResult(t *testing.T) {
	got := MessageText(core.NewToolResult("tool output", "call-1", false))
	if got != "tool output" {
		t.Fatalf("MessageText(ToolResult) = %q, want %q", got, "tool output")
	}
}

func TestMessageTextModelChange(t *testing.T) {
	got := MessageText(core.NewModelChange("model-name"))
	if got != "" {
		t.Fatalf("MessageText(ModelChange) = %q, want empty", got)
	}
}

func TestMessageTextThinkingLevelChange(t *testing.T) {
	got := MessageText(core.NewThinkingLevelChange("high"))
	if got != "" {
		t.Fatalf("MessageText(ThinkingLevelChange) = %q, want empty", got)
	}
}

func (m mockCompactionProvider) Compact(ctx context.Context, messages []core.Message) ([]core.Message, error) {
	if m.Config == nil || !m.Config.Enabled {
		return messages, nil
	}
	if m.Config.KeepFirst+m.Config.KeepLast >= len(messages) {
		return messages, nil
	}
	// Stream a summary from the fake provider.
	var summary strings.Builder
	for event := range m.Provider.Stream(ctx, []core.Message{core.NewUser("summarize")}, nil) {
		switch e := event.(type) {
		case core.ResponseChunk:
			summary.WriteString(e.Content)
		case core.StreamEnd:
			if e.FinishReason == "error" {
				return nil, fmt.Errorf("summarize: %s", e.Error)
			}
		}
	}
	if summary.Len() == 0 {
		return nil, fmt.Errorf("summarize: empty summary")
	}
	return BuildCompactedMessages(messages, strings.TrimSpace(summary.String()), m.Config.KeepFirst, m.Config.KeepLast), nil
}
