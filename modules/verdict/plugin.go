package verdict

import (
	"github.com/EndoTheDev/omega/agent"
)

// Plugin mounts the verdicts seam and its surfaces: the request_verdict
// tool, the /verdict TUI command, and a short prompt section so the
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
	return "verdict cards: the request_verdict tool hard-pauses a turn until the user answers by click or type"
}

// Provides: verdicts (exclusive - one hub on the Context), tools and
// prompt_sections (additive). Requires store so the hub can capture
// ctx.Store at Mount and persist card + verdict audit rows; a host
// without the store plugin still mounts (topo sort skips the dep only
// when it is unsatisfiable - store is always present in buildPlugins).
func (p *Plugin) Provides() []string { return []string{"verdicts", "tools", "prompt_sections"} }
func (p *Plugin) Requires() []string { return []string{"store"} }

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

// Hub returns the mounted hub, so the host can Close it on shutdown
// (release every still-blocked Ask). Safe to call only after Mount.
func (p *Plugin) Hub() *Hub { return p.hub }

// Close releases blocked verdict asks - the host calls this on shutdown.
func (p *Plugin) Close() {
	if p.hub != nil {
		p.hub.Close()
	}
}

const promptSectionOrder = 35 // after memory (10), skills (20), tools (30)

// PromptSections renders the model guidance for request_verdict: when
// to ask and the block/answer contract. The JSON tool schema already
// reaches the model; this is the "use it well" nudge.
func (p *Plugin) PromptSections(opts agent.PromptBuildOptions) []agent.PromptSection {
	body := "- request_verdict: ask the user when a decision needs human input (approve, pick between options, or free text). It BLOCKS the turn until they answer, the card expires (20 minutes default), or the run is cancelled - so ask only when you cannot proceed without them.\n- Prefer concrete options over open questions. Approval is two options (\"yes\" / \"no\"). A skip is always available and is recorded, never ignored.\n- The verdict returns as JSON: {verdict:\"answer\",option,option_index} | {verdict:\"skip\"} | {verdict:\"expired\"}. Act on it through your normal tools; the card itself executes nothing.\n"
	return []agent.PromptSection{
		{Title: "Verdict Cards", Body: body, Order: promptSectionOrder},
	}
}

// NewPlugin returns a verdict Plugin.
func NewPlugin() *Plugin { return &Plugin{} }