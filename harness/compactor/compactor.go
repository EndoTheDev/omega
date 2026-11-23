// Package compactor implements the compactor seam as an in-process plugin.
package compactor

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// The growth gate's production numbers: about 50K tokens of growth per compact
// (window-independent pacing - the source runs it on 1M windows), a floor of
// max(5000, 1% of the window) of recoverable context so a compact always pays for its
// summary, and a summary that costs at most 2K tokens (the source's corpus median is 185).
// A window too small to afford the production pace paces at half its budget instead.
const (
	DefaultGrowthTokens = 50_000
	MinBenefitTokens    = 5_000
	minBenefitFraction  = 0.01
	summaryBudgetTokens = 2_048
	baselineCap         = 4_096
)

// Compactor implements agent.CompactionProvider (the run's automatic arm) and
// agent.ContextCompactor (the host's growth-gated plan and on-demand summary).
type Compactor struct {
	provider agent.Provider
	config   agent.CompactionConfig
	// autoWindow is the context window probed from the provider once at Mount. The value
	// is cached for the process lifetime - SetConfig never touches it. 0 = no usable
	// window (probe failed, offline, or the provider cannot report one).
	autoWindow int

	// baseline is the growth gate's table: session id -> tokens the context held when the
	// last compact landed (or when the session was first seen). Memory only, like the
	// gate itself: a restart re-enters at first sight, which the mass bypass handles.
	mu       sync.Mutex
	baseline map[string]int
}

// New builds a compactor over a provider and a config. Mount builds one from the config
// section; a host (or a test) that already holds both uses this.
func New(provider agent.Provider, config agent.CompactionConfig) *Compactor {
	return &Compactor{provider: provider, config: config}
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

// keep resolves the retained tail: the request's forced value (-1 = the configured
// default), floored at zero.
func (c *Compactor) keep(forced int) int {
	keep := c.config.KeepLast
	if forced >= 0 {
		keep = forced
	}
	if keep < 0 {
		keep = 0
	}
	return keep
}

// materialEnd is where a compact's raw material stops: the retained tail, clamped at a
// pending turn (the never-compact law - a turn whose calls have not settled is never
// summarized away).
func (c *Compactor) materialEnd(ctx context.Context, wire []core.Message, keep int) int {
	end := len(wire) - keep
	if ledger := agent.YieldLedgerFromContext(ctx); ledger != nil {
		if sid := agent.SessionIDFromContext(ctx); sid != "" {
			if idx := agent.PendingAssistantIndex(wire, ledger.Pending(sid)); idx > 0 && idx < end {
				end = idx
			}
		}
	}
	return end
}

// summaryBudget is what a summary may cost: the doctrine's 2K cap, never more than a
// quarter of the material it replaces (a summary that large is not a compact).
func summaryBudget(material int) int {
	budget := material / 4
	if budget > summaryBudgetTokens {
		budget = summaryBudgetTokens
	}
	return budget
}

// PlanCompact is the growth gate: the nudge decides (the context is over its budget), the
// gate paces (enough growth since the last compact), and the benefit floor guarantees the
// summary pays for itself. A session first seen already over budget is the mass bypass -
// it compacts at once instead of waiting for growth it will never survive.
func (c *Compactor) PlanCompact(ctx context.Context, req agent.CompactRequest) agent.CompactPlan {
	keep := c.keep(req.Keep)
	end := c.materialEnd(ctx, req.Wire, keep)
	if end <= req.LiveStart {
		return agent.CompactPlan{Why: "this conversation is all head and retained tail"}
	}
	forced := agent.CompactForced(ctx)
	if !c.config.Enabled {
		return agent.CompactPlan{Why: "compaction is off"}
	}
	tokens := agent.EstimateTokens(req.Wire)
	window := c.window()
	budget := c.config.BudgetWith(window)
	floor := MinBenefitTokens
	if fraction := int(float64(window) * minBenefitFraction); fraction > floor {
		floor = fraction
	}
	if budget > 0 && floor > budget {
		// The floor guards a compact; it can never exceed what a compact could recover.
		floor = budget
	}
	material := agent.EstimateTokens(req.Wire[req.LiveStart:end])
	benefit := material - summaryBudget(material)
	if benefit < floor {
		return agent.CompactPlan{Why: fmt.Sprintf("the compact would recover %d tokens, under the %d it must pay for", benefit, floor)}
	}
	if !forced {
		if tokens <= budget {
			return agent.CompactPlan{Why: "the context is under the token budget"}
		}
		seen, base := c.note(ctx, tokens)
		if !seen {
			return agent.CompactPlan{Fire: true, Keep: keep}
		}
		pace := DefaultGrowthTokens
		if half := budget / 2; half > 0 && half < pace {
			pace = half
		}
		if tokens-base < pace {
			return agent.CompactPlan{Why: "growth since the last compact has not reached the pace"}
		}
	}
	return agent.CompactPlan{Fire: true, Keep: keep}
}

// CompactNow summarizes the material a request covers and answers the summary plus the
// tail it left verbatim. The user trigger runs through here with the gate out of the way:
// an explicit compact outranks the nudge's veto.
func (c *Compactor) CompactNow(ctx context.Context, req agent.CompactRequest) (agent.CompactAnswer, error) {
	if !c.config.Enabled {
		return agent.CompactAnswer{}, nil
	}
	start := req.LiveStart
	end := c.materialEnd(ctx, req.Wire, c.keep(req.Keep))
	if end <= start {
		return agent.CompactAnswer{}, nil
	}
	summary, err := c.summarize(ctx, req.Wire[start:end])
	if err != nil {
		return agent.CompactAnswer{}, fmt.Errorf("compactor: %w", err)
	}
	c.reset(ctx, agent.EstimateTokens(req.Wire[:start])+agent.SummaryTokens(summary)+agent.EstimateTokens(req.Wire[end:]))
	// The answer names the tail that is ACTUALLY retained: a pending turn can push it past
	// the configured keep, and the caller's span arithmetic must see the same number.
	return agent.CompactAnswer{Summary: summary, Keep: len(req.Wire) - end}, nil
}

// Compact is the loop's automatic arm and its overflow emergency. The gate decides whether
// the proactive compact runs; a forced context (the provider already answered "overflow")
// skips it. The returned wire carries the summary at the TAIL, so the next request shares
// every byte before the carrier with the one that came before it.
func (c *Compactor) Compact(ctx context.Context, messages []core.Message) ([]core.Message, error) {
	req := agent.CompactRequest{Wire: messages, LiveStart: agent.LiveStart(messages), Keep: -1}
	plan := c.PlanCompact(ctx, req)
	if !plan.Fire {
		return messages, nil
	}
	answer, err := c.CompactNow(ctx, agent.CompactRequest{Wire: messages, LiveStart: req.LiveStart, Keep: plan.Keep})
	if err != nil || answer.Summary == "" {
		return messages, err
	}
	// The store half: a block the run cannot write leaves the in-memory compact as the
	// emergency shape it is - no error into the model, the next run re-derives the truth.
	var from, to int64
	if rec := agent.CompactRecorderFromContext(ctx); rec != nil {
		if block, rerr := rec.RecordCompact(ctx, answer.Summary, answer.Keep); rerr == nil {
			from, to = block.From, block.To
		}
	}
	return agent.CarrierWire(messages, req.LiveStart, len(messages)-answer.Keep, answer.Summary, from, to), nil
}

// note records the session's first sight and answers whether it was seen before.
func (c *Compactor) note(ctx context.Context, tokens int) (seen bool, baseline int) {
	session := agent.SessionIDFromContext(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseline == nil {
		c.baseline = make(map[string]int, 8)
	}
	base, ok := c.baseline[session]
	if !ok && len(c.baseline) >= baselineCap {
		// The gate is pacing, not truth: dropping the table costs one first sight per
		// session, which is exactly the mass bypass.
		c.baseline = make(map[string]int, 8)
	}
	if !ok {
		c.baseline[session] = tokens
	}
	return ok, base
}

// reset moves the session's baseline to the compacted size: growth is counted from the
// context the compact left behind.
func (c *Compactor) reset(ctx context.Context, tokens int) {
	session := agent.SessionIDFromContext(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseline == nil {
		c.baseline = make(map[string]int, 8)
	}
	if len(c.baseline) >= baselineCap {
		c.baseline = make(map[string]int, 8)
	}
	c.baseline[session] = tokens
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
