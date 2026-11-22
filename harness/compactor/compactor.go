// Package compactor implements the compactor seam as an in-process plugin.
package compactor

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Compactor implements agent.CompactionProvider.
type Compactor struct {
	provider agent.Provider
	config   agent.CompactionConfig
	// autoWindow is the context window probed from the provider once at Mount. The value
	// is cached for the process lifetime - SetConfig never touches it. 0 = no usable
	// window (probe failed, offline, or the provider cannot report one).
	autoWindow int
}

// SetConfig replaces the compaction config at runtime.
func (c *Compactor) SetConfig(cfg agent.CompactionConfig) {
	c.config = cfg
}

// AutoWindow returns the context window probed at startup, or 0 when the probe found none.
func (c *Compactor) AutoWindow() int { return c.autoWindow }

// window resolves the effective context window: a manual override (>0) wins exactly.
func (c *Compactor) window() int {
	if c.config.ContextWindow > 0 {
		return c.config.ContextWindow
	}
	return c.autoWindow
}

// Compact compacts the message history. The never-compact law: a turn whose calls are
// still pending is NEVER compacted - the compact boundary skips past it. The run context
// carries the pending-calls table (agent.WithYieldLedger, threaded by the loop); the
// earliest pending call's assistant message anchors the boundary, so middle never
// contains it and everything from that turn on stays verbatim.
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

	end := len(messages) - keepLast
	if ledger := agent.YieldLedgerFromContext(ctx); ledger != nil {
		if sid := agent.SessionIDFromContext(ctx); sid != "" {
			if idx := agent.PendingAssistantIndex(messages, ledger.Pending(sid)); idx > keepFirst && idx < end {
				end = idx
			}
		}
	}
	if end <= keepFirst {
		return messages, nil
	}
	middle := messages[keepFirst:end]
	summary, err := c.summarize(ctx, middle)
	if err != nil {
		return nil, fmt.Errorf("compactor: %w", err)
	}

	result := make([]core.Message, 0, keepFirst+len(messages)-end+1)
	result = append(result, messages[:keepFirst]...)
	result = append(result, core.NewSystem(core.ContextCompactPrefix+summary+core.ContextCompactSuffix))
	result = append(result, messages[end:]...)
	return result, nil
}

// CompactNow compacts because a human asked, not because the budget ran out: the token gate
// is the automatic trigger and does not apply here. It answers the messages the NEW
// context should carry - the summary first, then the tail the compact keeps verbatim - so
// a caller holding a session can move its context boundary to the last len(out)-1 rows.
// The boundary a session store holds is a summary plus ONE tail, so everything before
// that tail is summarized: a head row kept nowhere would leave the live context with the
// reply still counting it as compacted. nil means there was nothing to summarize (a
// conversation no longer than the keep-first/keep-last floor, a disabled compactor): the
// caller must not move its boundary for a compact that kept nothing.
func (c *Compactor) CompactNow(ctx context.Context, messages []core.Message) ([]core.Message, error) {
	if !c.config.Enabled {
		return nil, nil
	}
	// keep_first floors the compact here; it is not a kept head, because the boundary cannot
	// represent one - a conversation this short is all floor and has no compact to make.
	keepFirst := c.config.KeepFirst
	keepLast := c.config.KeepLast
	start := len(messages) - keepLast
	if keepLast <= 0 || start <= keepFirst {
		return nil, nil
	}
	summary, err := c.summarize(ctx, messages[:start])
	if err != nil {
		return nil, fmt.Errorf("compactor: %w", err)
	}
	out := make([]core.Message, 0, len(messages)-start+1)
	out = append(out, core.NewSystem(core.ContextCompactPrefix+summary+core.ContextCompactSuffix))
	return append(out, messages[start:]...), nil
}

func (c *Compactor) summarize(ctx context.Context, middle []core.Message) (string, error) {
	var b strings.Builder
	// The summary is the only thing that survives a compact, so the classes below tell the
	// model what must not be lost.
	b.WriteString("Summarize the conversation below for a context compaction. " +
		"The summary is the only thing that survives a compaction; messages outside the ones below ride along verbatim, so everything below must be represented in the summary.\n" +
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
