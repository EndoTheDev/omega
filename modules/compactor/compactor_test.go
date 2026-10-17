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

// TestCompactCommand verifies /compact is registered and returns
// a run_compact CmdAction (the TUI interprets it).
func TestCompactCommand(t *testing.T) {
	p := NewPlugin()
	ctx := &agent.Context{}
	// Compactor requires provider seam — mount a fake provider.
	ctx.Provider = ai.NewFakeProvider("test")
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	// Verify /compact command was registered.
	found := false
	for _, c := range ctx.Commands {
		if c.Name == "/compact" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("/compact command not registered")
	}

	// Verify handler returns run_compact action.
	result, err := ctx.CommandHandler(context.Background(), "/compact", "")
	if err != nil {
		t.Fatalf("CommandHandler: %v", err)
	}
	if len(result.Actions) != 1 || result.Actions[0].Type != "run_compact" {
		t.Fatalf("expected run_compact action, got %+v", result.Actions)
	}
}