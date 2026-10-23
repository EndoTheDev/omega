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

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// Compactor implements core.CompactionProvider. It holds a reference
// to the provider (set at Mount time) and the compaction config (read
// from ctx.Configs at Mount time).
type Compactor struct {
	provider ai.Provider
	config   core.CompactionConfig
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
func (c *Compactor) SetConfig(cfg core.CompactionConfig) {
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
func (c *Compactor) Compact(ctx context.Context, messages []ai.Message) ([]ai.Message, error) {
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
func (c *Compactor) summarize(ctx context.Context, middle []ai.Message) (string, error) {
	var b strings.Builder
	b.WriteString("Summarize the following conversation concisely, preserving key facts, decisions, and context. Output only the summary.\n\n")
	for _, m := range middle {
		b.WriteString("- [")
		switch m.(type) {
		case ai.System:
			b.WriteString("system")
		case ai.User:
			b.WriteString("user")
		case ai.Assistant:
			b.WriteString("assistant")
		case ai.ToolResult:
			b.WriteString("tool")
		}
		b.WriteString("] ")
		b.WriteString(agent.MessageText(m))
		b.WriteString("\n")
	}

	events := c.provider.Stream(ctx, []ai.Message{ai.NewUser(b.String())}, nil)
	var summary strings.Builder
	for ev := range events {
		switch e := ev.(type) {
		case ai.ResponseChunk:
			summary.WriteString(e.Content)
		case ai.StreamEnd:
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
