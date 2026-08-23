package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
)

func TestEstimateTokens(t *testing.T) {
	history := []ai.Message{
		ai.NewUser("hello world"), // 11 chars -> 2 tokens
		ai.NewAssistant("a longer assistant response here"), // 32 chars -> 8 tokens
	}
	got := EstimateTokens(history)
	if got != 10 { // (11+32)/4 = 10
		t.Fatalf("estimateTokens = %d, want 10", got)
	}
}

func TestBuildCompactedMessages(t *testing.T) {
	history := []ai.Message{
		ai.NewSystem("sys"),
		ai.NewUser("u1"),
		ai.NewUser("u2"),
		ai.NewUser("u3"),
		ai.NewUser("u4"),
		ai.NewUser("u5"),
	}
	got := BuildCompactedMessages(history, "summary text", 2, 2)
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5 (2 first + 1 summary + 2 last)", len(got))
	}
	// First two preserved verbatim.
	if got[0].(ai.System).Content != "sys" || got[1].(ai.User).Content != "u1" {
		t.Fatalf("first messages not preserved: %+v", got[:2])
	}
	// Middle replaced with a system summary.
	sys, ok := got[2].(ai.System)
	if !ok || !strings.Contains(sys.Content, "summary text") {
		t.Fatalf("middle not replaced with summary: %+v", got[2])
	}
	// Last two preserved verbatim.
	if got[3].(ai.User).Content != "u4" || got[4].(ai.User).Content != "u5" {
		t.Fatalf("last messages not preserved: %+v", got[3:])
	}
}

func TestBuildCompactedMessagesNoOpWhenNothingToCompact(t *testing.T) {
	history := []ai.Message{ai.NewUser("a"), ai.NewUser("b")}
	got := BuildCompactedMessages(history, "summary", 1, 1)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (1 first + 1 summary + 1 last)", len(got))
	}
}

// mockCompactor is a minimal Compactor for agent loop tests. It
// streams a one-line summary from the fake provider and wraps it
// with BuildCompactedMessages. Disabled or under-budget = no-op.
type mockCompactor struct {
	Provider ai.Provider
	Config   *CompactionConfig
}

func (m mockCompactor) Compact(ctx context.Context, messages []ai.Message) ([]ai.Message, error) {
	if m.Config == nil || !m.Config.Enabled {
		return messages, nil
	}
	if m.Config.KeepFirst+m.Config.KeepLast >= len(messages) {
		return messages, nil
	}
	// Stream a summary from the fake provider.
	var summary strings.Builder
	for event := range m.Provider.Stream(ctx, []ai.Message{ai.NewUser("summarize")}, nil) {
		switch e := event.(type) {
		case ai.ResponseChunk:
			summary.WriteString(e.Content)
		case ai.StreamEnd:
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
