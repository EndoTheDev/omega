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

// TestCompactNowIgnoresBudget pins the manual compact's trigger. The token gate exists for
// the LOOP's automatic compact; a human typing /compact is the trigger, so the same short
// conversation the budget leaves alone still compacts - and the rows it keeps verbatim are
// the tail the caller's context boundary has to land on.
func TestCompactNowIgnoresBudget(t *testing.T) {
	provider := agent.NewFakeProvider("test",
		core.ResponseChunk{Type: "response_chunk", Content: "manual summary"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	c := &Compactor{
		provider: provider,
		config: agent.CompactionConfig{
			Enabled:       true,
			Threshold:     0.6,
			ContextWindow: 32768,
			KeepFirst:     1,
			KeepLast:      2,
		},
	}
	msgs := []core.Message{
		core.NewUser("one"), core.NewAssistant("two"), core.NewUser("three"),
		core.NewAssistant("four"), core.NewUser("five"),
	}
	auto, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(auto) != len(msgs) {
		t.Fatalf("automatic compact = %d rows, want the budget to leave all %d alone", len(auto), len(msgs))
	}

	out, err := c.CompactNow(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("manual compact = %d rows, want 3 (the summary + the 2 kept verbatim)", len(out))
	}
	if got := agent.MessageText(out[0]); !strings.Contains(got, "manual summary") {
		t.Fatalf("compact head = %q, want the summary the provider answered", got)
	}
	if _, ok := out[0].(core.System); !ok {
		t.Fatalf("compact head is %T, want the system summary row", out[0])
	}
	for i, want := range []string{"four", "five"} {
		if got := agent.MessageText(out[i+1]); got != want {
			t.Fatalf("kept row %d = %q, want %q - the tail rides verbatim", i, got, want)
		}
	}
}

// TestCompactNowCompactsTheHead pins what the manual compact hands the summarizer: the
// boundary a session store holds is a summary plus ONE tail, so every row before the tail
// must be compacted - a keep-first head kept nowhere is context lost while the reply counts
// it.
func TestCompactNowCompactsTheHead(t *testing.T) {
	provider := agent.NewFakeProvider("test",
		core.ResponseChunk{Type: "response_chunk", Content: "manual summary"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	c := &Compactor{
		provider: provider,
		config:   agent.CompactionConfig{Enabled: true, KeepFirst: 2, KeepLast: 2},
	}
	msgs := []core.Message{
		core.NewUser("one"), core.NewAssistant("two"), core.NewUser("three"),
		core.NewAssistant("four"), core.NewUser("five"), core.NewUser("six"),
	}
	out, err := c.CompactNow(context.Background(), msgs)
	if err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("manual compact = %d rows, want 3 (the summary + the 2 kept verbatim)", len(out))
	}
	prompt := summarizerPrompt(t, provider)
	for _, want := range []string{"- [user] one", "- [assistant] two", "- [user] three", "- [assistant] four"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the compact left %q out of the summary\nprompt:\n%s", want, prompt)
		}
	}
	for _, banned := range []string{"- [user] five", "- [user] six"} {
		if strings.Contains(prompt, banned) {
			t.Errorf("%q is kept verbatim, so it must not be summarized\nprompt:\n%s", banned, prompt)
		}
	}
}

// summarizerPrompt returns the single user prompt the compactor handed its provider.
func summarizerPrompt(t *testing.T, provider *agent.FakeProvider) string {
	t.Helper()
	if len(provider.LastMessages) != 1 {
		t.Fatalf("expected 1 message to the summarizer, got %d", len(provider.LastMessages))
	}
	user, ok := provider.LastMessages[0].(core.User)
	if !ok {
		t.Fatalf("expected user message to the summarizer, got %T", provider.LastMessages[0])
	}
	return user.Content
}

// TestCompactNowFloor pins the refusal: a conversation whose whole length is the keep-first/
// keep-last floor has no middle, so there is no summary to be had and no compact to write.
func TestCompactNowFloor(t *testing.T) {
	c := &Compactor{
		provider: agent.NewFakeProvider("test"),
		config: agent.CompactionConfig{
			Enabled: true, KeepFirst: 1, KeepLast: 2,
		},
	}
	out, err := c.CompactNow(context.Background(), []core.Message{
		core.NewUser("one"), core.NewAssistant("two"), core.NewUser("three"),
	})
	if err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("compact = %d rows, want nothing to compact (1+2 keeps the whole 3-row conversation)", len(out))
	}
}

// TestCompactNowDisabled pins the switched-off compactor: no summary, so no compact.
func TestCompactNowDisabled(t *testing.T) {
	c := &Compactor{
		provider: agent.NewFakeProvider("test"),
		config:   agent.CompactionConfig{Enabled: false, KeepFirst: 1, KeepLast: 1},
	}
	out, err := c.CompactNow(context.Background(), []core.Message{
		core.NewUser("one"), core.NewAssistant("two"), core.NewUser("three"),
	})
	if err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("compact = %d rows, want nothing (compaction is off)", len(out))
	}
}

// TestSummarizeDoctrinePrompt pins the billion-context KEEP/DROP doctrine as the. The
// doctrine is the deliverable - the controlled evidence credited it with ~62% of the
// re-fetch reduction - so a rewrite that silently drops a KEEP/DROP class or leaks the
// fill percentage must go red HERE, not in production summaries.
func TestSummarizeDoctrinePrompt(t *testing.T) {
	provider := agent.NewFakeProvider("test",
		core.ResponseChunk{Type: "response_chunk", Content: "compacted."},
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

	// summarize sends the provider exactly one user message.
	prompt := summarizerPrompt(t, provider)

	idx := strings.Index(prompt, "- [user]")
	if idx < 0 {
		t.Fatalf("prompt carries no compacted message lines: %q", prompt)
	}
	header := prompt[:idx]

	// Every KEEP class and every DROP class of the doctrine.
	for _, want := range []string{
		"Summarize the conversation below for a context compaction",
		"the only thing that survives a compaction",
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

	// The no-fill-percentage rule: the prompt must never disclose how full the context.
	if strings.ContainsRune(header, '%') {
		t.Errorf("doctrine prompt discloses a percentage\nheader:\n%s", header)
	}
	for _, banned := range []string{"fill", "context window", "tokens used"} {
		if strings.Contains(strings.ToLower(header), banned) {
			t.Errorf("doctrine prompt mentions %q\nheader:\n%s", banned, header)
		}
	}

	for _, want := range []string{"- [user] middle alpha", "- [user] middle beta"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lost summarized line %q\nprompt:\n%s", want, prompt)
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

// TestCompactCommand verifies /compact is registered and returns a run_compact.
func TestCompactCommand(t *testing.T) {
	p := NewPlugin()
	ctx := &core.Context{}
	ctx.ApplyOffer("provider", "test", agent.NewFakeProvider("test"))
	if _, err := agent.MountAllAPI([]agent.Plugin{stubProvides{plugin: p, seam: "provider"}, p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}

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

// stubProvides satisfies a Requires edge in the mount set without registering anything.
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
