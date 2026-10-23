package verdict

import (
	"github.com/EndoTheDev/omega/agent"
)

// Plugin mounts the verdicts seam and its surfaces: the verdict tool,
// the /verdict TUI command, and a short prompt section so the
// model knows when to ask. The hub is built once and shared by the
// tool (the blocking Ask), the HTTP channel (the answer POSTs), and the
// TUI (the answer command) - that one object is the single-answer race.
type Plugin struct {
	hub *Hub
}

var _ agent.Plugin = (*Plugin)(nil)

// Name implements agent.Plugin.
func (p *Plugin) Name() string { return "verdict" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "verdict cards: the verdict.request sub-action hard-pauses a turn until the user answers by click or type"
}

// Provides: verdicts (exclusive - one hub on the Context), tools and
// prompt_sections (additive). Requires store (the hub captures it at
// Mount to persist card + verdict audit rows) and logging (the hub
// captures ctx.Logger at Mount) - any host mounting this plugin must
// provide both seams; buildPlugins always does. Card t_13a2af8a:
// mount order is declared data, not list position.
// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"verdicts", "tools", "prompt_sections"} }
func (p *Plugin) Requires() []string { return []string{"store", "logging"} }

// Mount builds the hub, publishes it on the Context, and appends the
// tool provider, TUI provider, and prompt-section contributor.
func (p *Plugin) Mount(ctx *agent.Context) error {
	p.hub = NewHub(ctx.Store, ctx.Logger)
	ctx.Verdicts = p.hub
	ctx.ToolProviders = append(ctx.ToolProviders, &verdictToolProvider{hub: p.hub})
	ctx.TUIProviders = append(ctx.TUIProviders, &tuiProvider{hub: p.hub})
	ctx.PromptSectioners = append(ctx.PromptSectioners, p)
	return nil
}

// NewPlugin returns a verdict Plugin.
func NewPlugin() *Plugin { return &Plugin{} }

const promptSectionOrder = 35 // after memory (10), skills (20), tools (30)

// PromptSections renders the model guidance for verdict.request: when
// to ask (the agent decides, no gate or config) and the block/answer
// contract. The JSON tool schema already reaches the model; this is
// the "use it well" nudge.
func (p *Plugin) PromptSections(opts agent.PromptBuildOptions) []agent.PromptSection {
	body := "- verdict.request: ask the user when a decision materially affects them and you cannot proceed without their input - approval (two options, \"yes\" / \"no\"), a pick between options, a multi-select, or free text (allow_text). It BLOCKS the turn until they answer, the card expires (20 minutes default), or the run is cancelled - so ask only when you cannot proceed without them, and decide autonomously for everything else.\n- Prefer concrete options over open questions; give each option a one-line description when the choice needs explaining. Options render as a list, one per row.\n- The verdict returns as JSON: {verdict:\"answer\",option,option_index} | {verdict:\"answer\",options,option_indexes} (multi) | {verdict:\"skip\"} | {verdict:\"expired\"}. Act on it through your normal tools; the card itself executes nothing.\n"
	return []agent.PromptSection{
		{Title: "Verdict Cards", Body: body, Order: promptSectionOrder},
	}
}
