// Package compactor implements the compactor seam as an in-process
// plugin. It keeps the first N and last M messages, LLM-summarizes the
// middle via the mounted provider, and returns the compacted list.
//
// Seam: compactor (exclusive). Requires: provider.
package compactor

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// Compactor implements agent.CompactionProvider. It holds a reference
// to the provider (set at Mount time) and the compaction config (read
// from ctx.Configs at Mount time).
type Compactor struct {
	provider agent.Provider
	config   agent.CompactionConfig
	// autoWindow is the context window probed from the provider once
	// at Mount (server start). The value is cached for the process
	// lifetime - SetConfig never touches it. 0 = no usable window
	// (probe failed, offline, or the provider cannot report one).
	autoWindow int
}

// SetConfig replaces the compaction config at runtime. The host's
// settings hot-apply path (WatchConfig / settings save) calls this so
// an edited config.yaml takes effect on the running agent without a
// restart — Compact reads the config on every call.
func (c *Compactor) SetConfig(cfg agent.CompactionConfig) {
	c.config = cfg
}

// AutoWindow returns the context window probed at startup, or 0 when
// the probe produced nothing usable.
func (c *Compactor) AutoWindow() int { return c.autoWindow }

// window resolves the effective context window: a manual override
// (>0) wins exactly as before; 0 means auto - the startup-probed
// window. Both 0 -> Budget's DefaultContextWindow fallback.
func (c *Compactor) window() int {
	if c.config.ContextWindow > 0 {
		return c.config.ContextWindow
	}
	return c.autoWindow
}

// Compact compacts the message history. If disabled or under budget,
// returns the original messages unchanged.
func (c *Compactor) Compact(ctx context.Context, messages []core.Message) ([]core.Message, error) {
	if !c.config.Enabled {
		return messages, nil
	}

	estTokens := agent.EstimateTokens(messages)
	budget := c.config.BudgetWith(c.window())

	keepFirst := c.config.KeepFirst
	keepLast := c.config.KeepLast

	if estTokens <= budget || keepFirst+keepLast >= len(messages) {
		return messages, nil
	}

	middle := messages[keepFirst : len(messages)-keepLast]
	summary, err := c.summarize(ctx, middle)
	if err != nil {
		return nil, fmt.Errorf("compactor: %w", err)
	}

	return agent.BuildCompactedMessages(messages, summary, keepFirst, keepLast), nil
}

// summarize calls the provider to summarize a slice of messages. It
// streams the response, collects text chunks, and returns the trimmed
// summary.
func (c *Compactor) summarize(ctx context.Context, middle []core.Message) (string, error) {
	var b strings.Builder
	// The summarization prompt carries the billion-context doctrine
	// (idea-level extraction; github.com/ranxianglei/billion-context -
	// their PROVENANCE framing marks the ideas license-clean, credit
	// theirs): in their controlled evidence the doctrine + tags bundle
	// delivered ~62% of the re-fetch reduction, more than the
	// compression machinery itself. The summary is the only thing that
	// survives a fold, so the classes below tell the model what must
	// not be lost. No fill-percentage disclosure or question anywhere:
	// a model told it is only part-way full refuses to compress.
	b.WriteString("Summarize the conversation below for a context fold. " +
		"The summary is the only thing that survives the fold; the first and last few messages around it are kept separately.\n" +
		"\n" +
		"KEEP VERBATIM: file paths with line numbers; function/type signatures and load-bearing code lines; " +
		"exact error strings; decisions WITH their rationale; exact values (names, ids, versions, flags); " +
		"references to earlier turns by their position so a later human can find the original.\n" +
		"DROP: verbose tool output once its result is captured; duplicate reads of the same content; " +
		"dead-end exploration - but keep the LESSON of the dead end (what was tried and why it failed).\n" +
		"STRUCTURE: order the summary to match the conversation's chronology; " +
		"no commentary about the summarization itself; output only the summary.\n\n")
	for _, m := range middle {
		b.WriteString("- [")
		switch m.(type) {
		case core.System:
			b.WriteString("system")
		case core.User:
			b.WriteString("user")
		case core.Assistant:
			b.WriteString("assistant")
		case core.ToolResult:
			b.WriteString("tool")
		}
		b.WriteString("] ")
		b.WriteString(agent.MessageText(m))
		b.WriteString("\n")
	}

	// The consult resolves the per-subsystem model slot (card
	// t_fc0b2f2d): an "instance:model" value rides the named provider
	// instance, a bare value serves through the default instance, and
	// an empty model keeps the instance's own default. Resolved per
	// call - the model knob is hot (compaction config is re-read on
	// every Compact).
	prov, model := agent.ResolveModelSlot(c.provider, c.config.Model)
	if model != "" {
		prov = prov.WithModel(model)
	}

	events := prov.Stream(ctx, []core.Message{core.NewUser(b.String())}, nil)
	var summary strings.Builder
	for ev := range events {
		switch e := ev.(type) {
		case core.ResponseChunk:
			summary.WriteString(e.Content)
		case core.StreamEnd:
			if e.FinishReason == "error" {
				return "", fmt.Errorf("summarize: %s", e.Error)
			}
		}
	}

	s := strings.TrimSpace(summary.String())
	if s == "" {
		return "", fmt.Errorf("summarize: empty summary")
	}
	return s, nil
}
