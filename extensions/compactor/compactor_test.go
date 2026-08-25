package compactor

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// Compile-time interface checks.
var _ agent.CompactionProvider = (*Compactor)(nil)
var _ agent.Plugin = (*Plugin)(nil)

func TestCompactDisabled(t *testing.T) {
	c := &Compactor{
		provider: ai.NewFakeProvider("test"),
		config:   agent.CompactionConfig{Enabled: false},
	}
	msgs := []ai.Message{ai.NewUser("hello")}
	out, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 message unchanged, got %d", len(out))
	}
}

func TestCompactUnderBudget(t *testing.T) {
	c := &Compactor{
		provider: ai.NewFakeProvider("test"),
		config: agent.CompactionConfig{
			Enabled:       true,
			Threshold:     0.6,
			ContextWindow: 32768,
			KeepFirst:     1,
			KeepLast:      1,
		},
	}
	msgs := []ai.Message{ai.NewUser("short")}
	out, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 message (under budget), got %d", len(out))
	}
}

func TestCompactSummarizes(t *testing.T) {
	summary := "This is a summary."
	provider := ai.NewFakeProvider("test",
		ai.ResponseChunk{Type: "response_chunk", Content: summary},
		ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	c := &Compactor{
		provider: provider,
		config: agent.CompactionConfig{
			Enabled:       true,
			Threshold:     0.6,
			ContextWindow: 10,
			KeepFirst:     1,
			KeepLast:      1,
			ReserveTokens: 1,
		},
	}
	msgs := []ai.Message{
		ai.NewUser("first message long enough to exceed tiny budget"),
		ai.NewUser("middle message one"),
		ai.NewUser("middle message two"),
		ai.NewUser("last message also long enough"),
	}
	out, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	// Expect: [first] + [system: summary] + [last] = 3
	if len(out) != 3 {
		t.Fatalf("expected 3 messages (first+summary+last), got %d", len(out))
	}
	sys, ok := out[1].(ai.System)
	if !ok {
		t.Fatalf("expected system message at index 1, got %T", out[1])
	}
	want := "[compacted: " + summary + "]"
	if sys.Content != want {
		t.Fatalf("expected %q, got %q", want, sys.Content)
	}
}

func TestPluginInterface(t *testing.T) {
	p := NewPlugin()
	if p.Name() != "compactor" {
		t.Fatalf("expected name %q, got %q", "compactor", p.Name())
	}
	if len(p.Provides()) != 1 || p.Provides()[0] != "compactor" {
		t.Fatalf("unexpected provides: %v", p.Provides())
	}
	if len(p.Requires()) != 1 || p.Requires()[0] != "provider" {
		t.Fatalf("unexpected requires: %v", p.Requires())
	}
}