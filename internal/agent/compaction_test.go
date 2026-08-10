package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega-dev/internal/ai"
)

func TestEstimateTokens(t *testing.T) {
	history := []ai.Message{
		ai.NewUser("hello world"), // 11 chars -> 2 tokens
		ai.NewAssistant("a longer assistant response here"), // 32 chars -> 8 tokens
	}
	got := estimateTokens(history)
	if got != 10 { // (11+32)/4 = 10
		t.Fatalf("estimateTokens = %d, want 10", got)
	}
}

func TestCompactReplacesMiddle(t *testing.T) {
	provider := &mockProvider{
		modelName: "mock",
		scripts: [][]ai.StreamEvent{
			scripted(
				ai.ResponseChunk{Type: "response_chunk", Content: "summary text"},
				ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
			),
		},
	}
	history := []ai.Message{
		ai.NewSystem("sys"),
		ai.NewUser("u1"),
		ai.NewUser("u2"),
		ai.NewUser("u3"),
		ai.NewUser("u4"),
		ai.NewUser("u5"),
	}
	got, err := compact(context.Background(), provider, history, 2, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
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

func TestCompactNoOpWhenNothingToCompact(t *testing.T) {
	provider := &mockProvider{modelName: "mock"}
	history := []ai.Message{ai.NewUser("a"), ai.NewUser("b")}
	got, err := compact(context.Background(), provider, history, 1, 1)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (unchanged)", len(got))
	}
}

func TestCompactPropagatesSummaryError(t *testing.T) {
	provider := &mockProvider{
		modelName: "mock",
		scripts: [][]ai.StreamEvent{
			scripted(ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "boom"}),
		},
	}
	history := []ai.Message{ai.NewUser("a"), ai.NewUser("b"), ai.NewUser("c")}
	if _, err := compact(context.Background(), provider, history, 1, 1); err == nil {
		t.Fatal("expected error from summarize, got nil")
	}
}
