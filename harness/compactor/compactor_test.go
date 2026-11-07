package compactor

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Compile-time interface checks.
var _ agent.CompactionProvider = (*Compactor)(nil)
var _ agent.Plugin = (*Plugin)(nil)

func TestCompactDisabled(t *testing.T) {
	c := &Compactor{
		provider: agent.NewFakeProvider("test"),
		config:   agent.CompactionConfig{Enabled: false},
	}
	msgs := []core.Message{core.NewUser("hello")}
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
		provider: agent.NewFakeProvider("test"),
		config: agent.CompactionConfig{
			Enabled:       true,
			Threshold:     0.6,
			ContextWindow: 32768,
			KeepFirst:     1,
			KeepLast:      1,
		},
	}
	msgs := []core.Message{core.NewUser("short")}
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
	provider := agent.NewFakeProvider("test",
		core.ResponseChunk{Type: "response_chunk", Content: summary},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
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
	msgs := []core.Message{
		core.NewUser("first message long enough to exceed tiny budget"),
		core.NewUser("middle message one"),
		core.NewUser("middle message two"),
		core.NewUser("last message also long enough"),
	}
	out, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	// Expect: [first] + [system: summary] + [last] = 3
	if len(out) != 3 {
		t.Fatalf("expected 3 messages (first+summary+last), got %d", len(out))
	}
	sys, ok := out[1].(core.System)
	if !ok {
		t.Fatalf("expected system message at index 1, got %T", out[1])
	}
	want := "[compacted: " + summary + "]"
	if sys.Content != want {
		t.Fatalf("expected %q, got %q", want, sys.Content)
	}
}

// TestSummarizeDoctrinePrompt pins the billion-context KEEP/DROP doctrine
// (card t_5e39d5ca) as the prompt the summarizer actually receives. The
// doctrine is the deliverable - the controlled evidence credited it with
// ~62% of the re-fetch reduction - so a rewrite that silently drops a
// KEEP/DROP class or leaks the fill percentage must go red HERE, not in
// production summaries. It pins the prompt shape handed to the provider;
// the model output is irrelevant to this test.
func TestSummarizeDoctrinePrompt(t *testing.T) {
	provider := agent.NewFakeProvider("test",
		core.ResponseChunk{Type: "response_chunk", Content: "folded."},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
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
	msgs := []core.Message{
		core.NewUser("first message long enough to exceed tiny budget"),
		core.NewUser("middle alpha"),
		core.NewUser("middle beta"),
		core.NewUser("last message also long enough"),
	}
	if _, err := c.Compact(context.Background(), msgs); err != nil {
		t.Fatal(err)
	}

	// summarize() sends the provider exactly one user message: the
	// doctrine header, then the folded middle as tagged lines. The
	// empty Model slot keeps ResolveModelSlot on the original fake, so
	// LastMessages is the recording of what the summarizer received.
	if len(provider.LastMessages) != 1 {
		t.Fatalf("expected 1 message to the summarizer, got %d", len(provider.LastMessages))
	}
	user, ok := provider.LastMessages[0].(core.User)
	if !ok {
		t.Fatalf("expected user message to the summarizer, got %T", provider.LastMessages[0])
	}
	prompt := user.Content

	idx := strings.Index(prompt, "- [user]")
	if idx < 0 {
		t.Fatalf("prompt carries no folded message lines: %q", prompt)
	}
	header := prompt[:idx]

	// Every KEEP class and every DROP class of the doctrine.
	for _, want := range []string{
		"Summarize the conversation below for a context fold",
		"the only thing that survives the fold",
		"KEEP VERBATIM:",
		"file paths with line numbers",
		"signatures and load-bearing code lines",
		"exact error strings",
		"decisions WITH their rationale",
		"exact values (names, ids, versions, flags)",
		"references to earlier turns by their position",
		"DROP:",
		"verbose tool output once its result is captured",
		"duplicate reads of the same content",
		"dead-end exploration",
		"LESSON of the dead end",
		"STRUCTURE:",
		"order the summary to match the conversation's chronology",
		"output only the summary",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("doctrine prompt lost %q\nheader:\n%s", want, header)
		}
	}

	// The no-fill-percentage rule: the prompt must never disclose how
	// full the context window is - a model told it is only part-way
	// full refuses to compress.
	if strings.ContainsRune(header, '%') {
		t.Errorf("doctrine prompt discloses a percentage\nheader:\n%s", header)
	}
	for _, banned := range []string{"fill", "context window", "tokens used"} {
		if strings.Contains(strings.ToLower(header), banned) {
			t.Errorf("doctrine prompt mentions %q\nheader:\n%s", banned, header)
		}
	}

	// The folded middle rides along as tagged lines after the header.
	for _, want := range []string{"- [user] middle alpha", "- [user] middle beta"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lost folded line %q\nprompt:\n%s", want, prompt)
		}
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
	ctx := &core.Context{}
	// Compactor requires provider seam — mount a fake provider.
	ctx.ApplyOffer("provider", "test", agent.NewFakeProvider("test"))
	if _, err := agent.MountAllAPI([]agent.Plugin{stubProvides{plugin: p, seam: "provider"}, p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
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

// stubProvides satisfies a Requires edge in the mount set without
// registering anything: the seam it names is already populated on the
// Context the test built (a fake provider, a logging stub).
type stubProvides struct {
	plugin agent.Plugin
	seam   string
}

func (s stubProvides) Name() string       { return "stub-" + s.seam }
func (s stubProvides) Provides() []string { return []string{s.seam} }
func (s stubProvides) Requires() []string { return nil }
func (s stubProvides) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	_ = s.plugin
	return nil
}

// Mount satisfies the interface until phase F retires it; the stub
// never registers and is never dispatched (the API mount wins).
func (s stubProvides) Mount(ctx *core.Context) error { return nil }
