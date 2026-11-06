package compactor

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// The per-subsystem model slot (card t_fc0b2f2d): the compaction
// consult routes through the named provider instance the slot names;
// the summary text identifies which provider served the call.

// slotMessages builds a history whose tiny window forces one summarize
// call.
func slotMessages() []core.Message {
	return []core.Message{
		core.NewUser("first message long enough to exceed the tiny budget"),
		core.NewUser("middle message one"),
		core.NewUser("middle message two"),
		core.NewUser("last message also long enough"),
	}
}

func slotConfig(model string) agent.CompactionConfig {
	return agent.CompactionConfig{
		Enabled:       true,
		Threshold:     0.6,
		ContextWindow: 10,
		KeepFirst:     1,
		KeepLast:      1,
		ReserveTokens: 1,
		Model:         model,
	}
}

// slotFixtures builds the default provider with one named instance;
// the "big:<model>" slot routes there, everything else stays.
func slotFixtures() *Compactor {
	base := agent.NewFakeProvider("base-model",
		core.ResponseChunk{Type: "response_chunk", Content: "base-summary"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	base.SetInstances(map[string]agent.Provider{
		"big": agent.NewFakeProvider("big-model",
			core.ResponseChunk{Type: "response_chunk", Content: "big-summary"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"}),
	})
	return &Compactor{provider: base}
}

// compactedSummary runs one Compact and returns the summary text the
// consult produced (head/tail messages drop out of the result).
func compactedSummary(t *testing.T, c *Compactor) string {
	t.Helper()
	out, err := c.Compact(context.Background(), slotMessages())
	if err != nil {
		t.Fatal(err)
	}
	sys, ok := out[1].(core.System)
	if !ok {
		t.Fatalf("expected system message at index 1, got %T", out[1])
	}
	return strings.TrimPrefix(sys.Content, "[compacted: ")
}

func TestCompactSlotRoutesInstance(t *testing.T) {
	c := slotFixtures()
	c.config = slotConfig("big:big-model")
	if got := compactedSummary(t, c); got != "big-summary]" {
		t.Errorf("slot %q routed wrong provider: summary = %q", "big:big-model", got)
	}
}

// An instance prefix with no model part serves the instance's own
// default model (the base fake never says "big").
func TestCompactSlotInstanceDefaultModel(t *testing.T) {
	c := slotFixtures()
	c.config = slotConfig("big:")
	if got := compactedSummary(t, c); got != "big-summary]" {
		t.Errorf("instance default model not served: summary = %q", got)
	}
}

// TestCompactSlotUnknownPrefixIsBare pins the ONE slot rule: an
// instance-shaped prefix without a configured instance is still a bare
// model on the default provider - the consult never silently blocks.
func TestCompactSlotUnknownPrefixIsBare(t *testing.T) {
	c := slotFixtures()
	c.config = slotConfig("unknown:whatever")
	if got := compactedSummary(t, c); got != "base-summary]" {
		t.Errorf("unconfigured prefix must stay a bare model: summary = %q", got)
	}
}

// An empty slot keeps today's consult (the default instance, its main
// model).
func TestCompactSlotEmpty(t *testing.T) {
	c := slotFixtures()
	c.config = slotConfig("")
	if got := compactedSummary(t, c); got != "base-summary]" {
		t.Errorf("empty slot: summary = %q", got)
	}
}
