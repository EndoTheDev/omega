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
)

// Compactor implements agent.CompactionProvider. It holds a reference
// to the provider (set at Mount time) and the compaction config (read
// from gateway.Config at Mount time).
type Compactor struct {
	provider ai.Provider
	config   agent.CompactionConfig
}

// Compact compacts the message history. If disabled or under budget,
// returns the original messages unchanged.
func (c *Compactor) Compact(ctx context.Context, messages []ai.Message) ([]ai.Message, error) {
	if !c.config.Enabled {
		return messages, nil
	}

	estTokens := agent.EstimateTokens(messages)
	budget := c.budget()

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

// budget replicates agent.CompactionConfig.budget() which is unexported.
// ponytail: duplicates the unexported method instead of exporting it.
// Upgrade path: export agent.CompactionConfig.Budget() and call it.
func (c *Compactor) budget() int {
	window := c.config.ContextWindow
	if window <= 0 {
		window = agent.DefaultContextWindow
	}
	reserve := c.config.ReserveTokens
	if reserve <= 0 {
		reserve = 16384 // agent.defaultReserveTokens
	}
	effective := window - reserve
	if effective < window/2 {
		effective = window / 2
	}
	return int(float64(effective) * c.config.Threshold)
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