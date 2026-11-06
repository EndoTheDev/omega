// Package store provides the session persistence seam (agent.StoreProvider)
// via the SQLite implementation in store_impl.go, plus the sessions.search
// tool that lets the agent search past conversations via FTS5.
//
// Seam: store (exclusive), tools (additive).
package store

import (
	"github.com/EndoTheDev/omega/agent"
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// searchToolProvider provides the sessions.search tool.
type searchToolProvider struct {
	store agent.StoreProvider
}

// Tools returns the sessions.search tool definition.
func (p *searchToolProvider) Tools() map[string]core.Tool {
	return map[string]core.Tool{
		"sessions.search": {
			Description: "Search past session messages by keyword. Returns matching sessions with snippets of the matching content.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query (FTS5 syntax: words, phrases, OR, NOT)",
					},
				},
				"required": []string{"query"},
			},
			Run: p.runSearch,
		},
	}
}

// runSearch executes the sessions.search tool: queries the FTS5 index
// and formats results as "[label] snippet" lines, using the session
// label when available (falling back to the session ID).
func (p *searchToolProvider) runSearch(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	results, err := p.store.SearchMessages(ctx, query)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "no results", nil
	}
	var out string
	for _, r := range results {
		label := r.SessionID
		if sess, err := p.store.GetSession(ctx, r.SessionID); err == nil && sess.Label != "" {
			label = sess.Label
		}
		out += fmt.Sprintf("[%s] %s\n", label, r.Snippet)
	}
	return out, nil
}

// hygieneToolProvider provides the sessions.hygiene tool: the tool the
// async cron's hygiene fire (an agent turn) calls to run the pass -
// the same shape the skills curator rides. The cron module stays
// store-agnostic (no module imports a module): the wiring is yaml job
// definition -> agent turn -> THIS tool.
type hygieneToolProvider struct {
	store *Store
	// providerFn is the live provider reader, wired at mount. The
	// model slot is NOT baked here (t_f78acfd6): hygiene() re-reads
	// the store's live HygieneConfig at call time, so a
	// summarizer_model flip reaches the next sweep without a mount.
	providerFn func() agent.Provider
}

// provider resolves the live provider seam at call time (nil-safe,
// mirroring the skills capsule's providerFn discipline).
func (p *hygieneToolProvider) provider() agent.Provider {
	if p.providerFn == nil {
		return nil
	}
	return p.providerFn()
}

// Tools returns the sessions.hygiene tool definition.
func (p *hygieneToolProvider) Tools() map[string]core.Tool {
	return map[string]core.Tool{
		"sessions.hygiene": {
			Description: "Run the session hygiene pass: summarize settled sessions that lack a summary " +
				"(one LLM call each, at most 5 per run), prune sessions past the retention window " +
				"(pinned sessions are exempt), and vacuum the SQLite database (WAL checkpoint + " +
				"incremental vacuum). Returns a counts report.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"run", "summarize", "prune", "vacuum"},
						"description": "Pass step to run. Default (empty) = run, the whole pass in order.",
					},
				},
			},
			Run: p.runHygiene,
		},
	}
}

// runHygiene executes one tool call. The full pass summarizes BEFORE
// pruning (the consult reads history that is going nowhere before
// anything is deleted) and vacuums last, so the report's file sizes
// state the post-maintenance shape.
func (p *hygieneToolProvider) runHygiene(ctx context.Context, args map[string]any) (string, error) {
	if p.store == nil {
		return "", fmt.Errorf("sessions.hygiene: no store")
	}
	// One live snapshot per call (card t_f78acfd6): a settings save
	// that lands mid-sweep is the NEXT sweep's config.
	cfg := p.hygiene()
	action, _ := args["action"].(string)
	action = strings.TrimSpace(strings.ToLower(action))
	switch action {
	case "", "run":
		rep, err := p.store.RunHygiene(ctx, p.provider(), cfg)
		if err != nil {
			return formatHygieneReport(rep), err
		}
		return formatHygieneReport(rep) + p.consultLine(cfg), nil
	case "summarize":
		rep, err := p.store.RunSummarizer(ctx, p.provider(), cfg.SummarizerModel, cfg.SummarizeAfterHours)
		if err != nil {
			return formatHygieneReport(rep), err
		}
		return formatHygieneReport(rep) + p.consultLine(cfg), nil
	case "prune":
		ids, err := p.store.PruneSessions(ctx, cfg.RetentionDays)
		if err != nil {
			return "", err
		}
		return formatHygieneReport(HygieneReport{Pruned: len(ids), PrunedIDs: ids}), nil
	case "vacuum":
		beforeDB, beforeWAL := p.store.DBFileSizes()
		if err := p.store.RunVacuum(ctx); err != nil {
			return "", err
		}
		afterDB, afterWAL := p.store.DBFileSizes()
		return fmt.Sprintf("vacuum: db %d -> %d bytes, WAL %d -> %d bytes",
			beforeDB, afterDB, beforeWAL, afterWAL), nil
	default:
		return "", fmt.Errorf("unknown action %q - run | summarize | prune | vacuum", action)
	}
}

// hygiene returns the store's live hygiene config snapshot (card
// t_f78acfd6): the tool never holds a mount-time copy.
func (p *hygieneToolProvider) hygiene() HygieneConfig {
	return p.store.HygieneConfig()
}

// consultLine names the model slot the summarizer consulted - the
// configured slot, or the provider's session model when the slot is
// empty. The acceptance proof (card t_f78acfd6): change
// provider.summarizer_model in settings on a running lane and the
// next sweep's tool output names the new slot, no restart.
func (p *hygieneToolProvider) consultLine(cfg HygieneConfig) string {
	slot := cfg.SummarizerModel
	if slot == "" {
		prov := p.provider()
		if prov != nil {
			slot = prov.ModelName()
		}
	}
	if slot == "" {
		return "\nsummarizer model: (none - no provider mounted)"
	}
	return "\nsummarizer model: " + slot
}

// formatHygieneReport renders the counts the agent turn relays.
func formatHygieneReport(rep HygieneReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "hygiene: %d summarized, %d pending, %d pruned, vacuum %v",
		rep.Summarized, rep.Pending, rep.Pruned, rep.Vacuumed)
	if rep.DBSizeBytes > 0 {
		fmt.Fprintf(&sb, "\ndb: %d bytes, WAL %d bytes", rep.DBSizeBytes, rep.WALSizeBytes)
	}
	for _, id := range rep.PrunedIDs {
		sb.WriteString("\npruned: " + id)
	}
	for _, s := range rep.Skipped {
		sb.WriteString("\nskipped: " + s)
	}
	return sb.String()
}
