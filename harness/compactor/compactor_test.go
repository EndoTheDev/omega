package compactor

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

var _ agent.CompactionProvider = (*Compactor)(nil)
var _ agent.ContextCompactor = (*Compactor)(nil)
var _ agent.Plugin = (*Plugin)(nil)

// sessionCtx is a run context with a session id: the growth gate keys its baseline by it.
func sessionCtx(id string) context.Context {
	return agent.WithSessionID(context.Background(), id)
}

// longWire builds a wire a compact is worth running on: a first user message, then n
// messages of size characters each. 4 chars = 1 token (agent.EstimateTokens).
func longWire(n, size int) []core.Message {
	out := []core.Message{core.NewUser("the opening request")}
	for i := 0; i < n; i++ {
		out = append(out, core.NewUser(strings.Repeat("x", size)))
	}
	return out
}

func summarizer(summary string) *agent.FakeProvider {
	return agent.NewFakeProvider("test",
		core.ResponseChunk{Type: "response_chunk", Content: summary},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
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

func TestCompactDisabled(t *testing.T) {
	c := &Compactor{provider: summarizer("a summary"), config: agent.CompactionConfig{Enabled: false}}
	msgs := longWire(10, 6000)
	out, err := c.Compact(sessionCtx("s"), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(msgs) {
		t.Fatalf("disabled compact = %d messages, want the %d untouched", len(out), len(msgs))
	}
}

func TestCompactUnderBudget(t *testing.T) {
	c := &Compactor{
		provider: summarizer("a summary"),
		config: agent.CompactionConfig{
			Enabled: true, Threshold: 0.6, ContextWindow: 32768, KeepLast: 1,
		},
	}
	msgs := []core.Message{core.NewUser("short")}
	out, err := c.Compact(sessionCtx("s"), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 message (under budget), got %d", len(out))
	}
}

// TestCompactCarriesTheSummaryAtTheTail is the carrier law: the compact answers the head, a
// compress call + its result, then the retained tail - never a rewritten head.
func TestCompactCarriesTheSummaryAtTheTail(t *testing.T) {
	c := &Compactor{
		provider: summarizer("This is a summary."),
		config: agent.CompactionConfig{
			Enabled: true, Threshold: 0.6, ContextWindow: 32768, KeepLast: 2,
		},
	}
	msgs := longWire(10, 6000)
	out, err := c.Compact(sessionCtx("s"), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 5 {
		t.Fatalf("compact = %d messages, want 5 (head + carrier pair + kept tail)", len(out))
	}
	if !agent.IsCarrierCall(out[1]) {
		t.Fatalf("out[1] = %T, want the compress call", out[1])
	}
	if !agent.IsCarrierResult(out[2]) {
		t.Fatalf("out[2] = %T, want the summary carrier", out[2])
	}
	if got := agent.MessageText(out[2]); got != "This is a summary." {
		t.Fatalf("summary = %q, want the provider's answer", got)
	}
	if got := agent.MessageText(out[4]); got != agent.MessageText(msgs[len(msgs)-1]) {
		t.Fatalf("tail = %q, want the last message verbatim", got)
	}
}

// TestGrowthGatePaces pins the pacing: a context that did not grow since the last compact
// is not compacted again, and a grown one is.
func TestGrowthGatePaces(t *testing.T) {
	c := &Compactor{
		provider: summarizer("a summary"),
		config: agent.CompactionConfig{
			Enabled: true, Threshold: 0.6, ContextWindow: 32768, KeepLast: 2,
		},
	}
	ctx := sessionCtx("pace")
	msgs := longWire(10, 6000)
	if out, err := c.Compact(ctx, msgs); err != nil || len(out) >= len(msgs) {
		t.Fatalf("first sight did not compact: %d messages, err %v", len(out), err)
	}
	// Pace on a 32K window: budget/2 = 4915 tokens of growth. A context over budget whose
	// baseline is close enough that the growth stays under the pace is left alone.
	wire := longWire(10, 6000)
	tokens := agent.EstimateTokens(wire)
	c.mu.Lock()
	c.baseline = map[string]int{"pace": tokens - 3000}
	c.mu.Unlock()
	if plan := c.PlanCompact(ctx, agent.CompactRequest{Wire: wire, LiveStart: 1, Keep: -1}); plan.Fire {
		t.Fatalf("a 3000-token growth earned a compact: %+v", plan)
	}
	c.mu.Lock()
	c.baseline = map[string]int{"pace": tokens - 6000}
	c.mu.Unlock()
	if plan := c.PlanCompact(ctx, agent.CompactRequest{Wire: wire, LiveStart: 1, Keep: -1}); !plan.Fire {
		t.Fatalf("a 6000-token growth did not earn a compact: %s", plan.Why)
	}
}

// TestFirstSightMassBypass pins the bypass: a session never seen before that is already
// over budget compacts on the first decision instead of waiting for growth.
func TestFirstSightMassBypass(t *testing.T) {
	c := &Compactor{
		provider: summarizer("a summary"),
		config: agent.CompactionConfig{
			Enabled: true, Threshold: 0.6, ContextWindow: 32768, KeepLast: 2,
		},
	}
	plan := c.PlanCompact(sessionCtx("fresh"), agent.CompactRequest{Wire: longWire(10, 6000), LiveStart: 1, Keep: -1})
	if !plan.Fire {
		t.Fatalf("first sight over budget refused: %s", plan.Why)
	}
}

// TestBenefitFloor pins the zero-yield guard: a context over budget whose compactable
// middle is too small to pay for its summary never compacts - it would churn the cache and
// save nothing.
func TestBenefitFloor(t *testing.T) {
	c := &Compactor{
		provider: summarizer("a summary"),
		config: agent.CompactionConfig{
			Enabled: true, Threshold: 0.01, ContextWindow: 100, KeepLast: 1, ReserveTokens: 1,
		},
	}
	// 3 messages of 4 tokens each: the middle is 4 tokens, far under the 5000 floor.
	msgs := []core.Message{core.NewUser("aaaa"), core.NewUser("bbbb"), core.NewUser("cccc")}
	plan := c.PlanCompact(sessionCtx("tiny"), agent.CompactRequest{Wire: msgs, LiveStart: 1, Keep: -1})
	if plan.Fire {
		t.Fatalf("a 4-token middle earned a compact: %+v", plan)
	}
	if !strings.Contains(plan.Why, "under the") {
		t.Fatalf("refusal = %q, want the benefit floor named", plan.Why)
	}
}

// TestCompactNowIgnoresTheGate pins the user trigger: an explicit compact outranks the
// nudge's veto, so the same short conversation the gate leaves alone still compacts.
func TestCompactNowIgnoresTheGate(t *testing.T) {
	provider := summarizer("manual summary")
	c := &Compactor{
		provider: provider,
		config:   agent.CompactionConfig{Enabled: true, Threshold: 0.6, ContextWindow: 32768, KeepLast: 2},
	}
	msgs := []core.Message{
		core.NewUser("one"), core.NewAssistant("two"), core.NewUser("three"),
		core.NewAssistant("four"), core.NewUser("five"),
	}
	auto, err := c.Compact(sessionCtx("m"), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(auto) != len(msgs) {
		t.Fatalf("automatic compact = %d rows, want the budget to leave all %d alone", len(auto), len(msgs))
	}

	answer, err := c.CompactNow(context.Background(), agent.CompactRequest{Wire: msgs, LiveStart: 1, Keep: -1})
	if err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if answer.Summary != "manual summary" || answer.Keep != 2 {
		t.Fatalf("answer = %+v, want the summary and the 2 kept rows", answer)
	}
	prompt := summarizerPrompt(t, provider)
	for _, want := range []string{"- [assistant] two", "- [user] three"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the compact left %q out of the summary\nprompt:\n%s", want, prompt)
		}
	}
	for _, banned := range []string{"- [user] one", "- [assistant] four", "- [user] five"} {
		if strings.Contains(prompt, banned) {
			t.Errorf("%q is head or kept verbatim, so it must not be summarized\nprompt:\n%s", banned, prompt)
		}
	}
}

// TestCompactNowFloor pins the refusal: a conversation that is all head and retained tail
// has no middle, so there is no summary to be had.
func TestCompactNowFloor(t *testing.T) {
	c := &Compactor{
		provider: summarizer("a summary"),
		config:   agent.CompactionConfig{Enabled: true, KeepLast: 2},
	}
	answer, err := c.CompactNow(context.Background(), agent.CompactRequest{
		Wire: []core.Message{core.NewUser("one"), core.NewAssistant("two"), core.NewUser("three")},
		LiveStart: 1, Keep: -1,
	})
	if err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if answer.Summary != "" {
		t.Fatalf("summary = %q, want nothing to summarize (1 head + 2 kept)", answer.Summary)
	}
}

// TestCompactNowDisabled pins the switched-off compactor: no summary, so no compact.
func TestCompactNowDisabled(t *testing.T) {
	c := &Compactor{provider: summarizer("a summary"), config: agent.CompactionConfig{Enabled: false, KeepLast: 1}}
	answer, err := c.CompactNow(context.Background(), agent.CompactRequest{
		Wire: []core.Message{core.NewUser("one"), core.NewAssistant("two"), core.NewUser("three")},
		LiveStart: 1, Keep: -1,
	})
	if err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if answer.Summary != "" {
		t.Fatalf("summary = %q, want nothing (compaction is off)", answer.Summary)
	}
}

// fakeRecorder stands in for the store half: it records what the run hands it.
type fakeRecorder struct {
	summary string
	keep    int
	calls   int
	block   core.ContextBlock
	err     error
}

func (f *fakeRecorder) RecordCompact(_ context.Context, summary string, keep int) (core.ContextBlock, error) {
	f.calls++
	f.summary, f.keep = summary, keep
	return f.block, f.err
}

// TestCompactRecordsThroughTheRecorder pins the durable half of an in-run compact: the
// block lands in the store, and the in-memory carrier carries its real span.
func TestCompactRecordsThroughTheRecorder(t *testing.T) {
	rec := &fakeRecorder{block: core.ContextBlock{ID: "blk-9", From: 2, To: 9}}
	ctx := agent.WithCompactRecorder(sessionCtx("rec"), rec)
	c := &Compactor{
		provider: summarizer("a summary"),
		config:   agent.CompactionConfig{Enabled: true, Threshold: 0.6, ContextWindow: 32768, KeepLast: 2},
	}
	out, err := c.Compact(ctx, longWire(10, 6000))
	if err != nil {
		t.Fatal(err)
	}
	if rec.calls != 1 || rec.summary != "a summary" || rec.keep != 2 {
		t.Fatalf("recorder saw %d calls (summary %q, keep %d), want one compact with keep 2", rec.calls, rec.summary, rec.keep)
	}
	call := out[1].(core.Assistant)
	if got := call.ToolCalls[0].Arguments["from"]; got != core.MsgID(2) {
		t.Fatalf("carrier from = %v, want the block's real span", got)
	}
}

// TestSummarizeDoctrinePrompt pins the KEEP/DROP doctrine. The doctrine is the
// deliverable - the controlled evidence credited it with ~62% of the re-fetch reduction -
// so a rewrite that silently drops a KEEP/DROP class or leaks the fill percentage must go
// red HERE, not in production summaries.
func TestSummarizeDoctrinePrompt(t *testing.T) {
	provider := summarizer("compacted.")
	c := &Compactor{
		provider: provider,
		config: agent.CompactionConfig{
			Enabled: true, Threshold: 0.6, ContextWindow: 32768, KeepLast: 1, ReserveTokens: 1,
		},
	}
	msgs := []core.Message{
		core.NewUser("first message long enough to exceed tiny budget"),
		core.NewUser("middle alpha"),
		core.NewUser("middle beta"),
		core.NewUser("last message also long enough"),
	}
	if _, err := c.CompactNow(sessionCtx("doctrine"), agent.CompactRequest{Wire: msgs, LiveStart: 1, Keep: -1}); err != nil {
		t.Fatal(err)
	}

	prompt := summarizerPrompt(t, provider)
	idx := strings.Index(prompt, "- [user]")
	if idx < 0 {
		t.Fatalf("prompt carries no compacted message lines: %q", prompt)
	}
	header := prompt[:idx]

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

	// The no-fill-percentage rule: the prompt must never disclose how full the context is.
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

// Mount satisfies the interface and registers nothing: the stub is never dispatched
// (the API mount wins).
func (s stubProvides) Mount(ctx *core.Context) error { return nil }
