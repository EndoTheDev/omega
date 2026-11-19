package verdict

import (
	"net/http"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin mounts the verdicts seam and its surfaces. The hub is built once and shared by the
// tool (the blocking Ask), the HTTP channel (the answer POSTs) and the /verdict command -
// that one object is the single-answer race.
type Plugin struct {
	hub    *Hub
	routes *Routes
}

var _ agent.Plugin = (*Plugin)(nil)

// Name implements agent.Plugin.
func (p *Plugin) Name() string { return "verdict" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "verdict cards: the request action hard-pauses a turn until the user answers by click or type"
}

func (p *Plugin) ConfigKey() string { return "" }

// Provides: verdicts (exclusive) plus the tools and prompt sections this capsule ships.
// Requires store and logger (the hub captures the logger offer at Mount).
func (p *Plugin) Provides() []string { return []string{"verdicts", "tools", "prompt_sections"} }
func (p *Plugin) Requires() []string { return []string{"store", "logger"} }

// MountWithAPI builds the hub from the seams the mount reads in place. Capsule posture:
// this registration is the module's whole surface - the seam, the blocking verdict tool,
// the /verdict command, the /verdicts REST routes and the prompt section. The CLI loop's
// busy-time answer path reads the same seam and is nil-guarded: no mount, no answering.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	p.hub = NewHub(seam[agent.StoreProvider](ctx, "store"), seam[agent.LoggerProvider](ctx, "logger"))
	p.routes = newRoutes(p.hub)
	if err := api.Offer("verdicts", p.hub); err != nil {
		return err
	}
	// The answer surface is the capsule's REST face: the bell's pending listing and the
	// answering POST mount and die with the hub.
	for _, r := range []struct {
		pattern string
		h       func(http.ResponseWriter, *http.Request)
	}{
		{"/verdicts", p.routes.handleVerdicts},
		{"/sessions/{id}/verdicts", p.routes.handleVerdicts},
	} {
		if err := api.RegisterRoute(r.pattern, r.h); err != nil {
			return err
		}
	}
	for name, tool := range (&verdictToolProvider{hub: p.hub}).Tools() {
		if err := api.RegisterTool(name, tool); err != nil {
			return err
		}
	}
	if err := api.RegisterCommand("/verdict",
		"list pending verdict cards / answer one: /verdict <N> <num|1,3|y|n|skip|text>",
		[]string{}, verdictHandler(p)); err != nil {
		return err
	}
	return api.RegisterPromptSectioner(p)
}

// NewPlugin returns a verdict Plugin.
func NewPlugin() *Plugin { return &Plugin{} }

const promptSectionOrder = 35 // after memory (10), skills (20), tools (30)

// PromptSections renders the model guidance for the verdict tool: when to ask (the
// agent decides, no gate or config) and the block/answer contract.
func (p *Plugin) PromptSections(opts core.PromptBuildOptions) []core.PromptSection {
	body := "- verdict: ask the user when a decision materially affects them and you cannot proceed without their input - approval (two options, \"yes\" / \"no\"), a pick between options, a multi-select, or free text (allow_text). It BLOCKS the turn until they answer, the card expires (20 minutes default), or the run is cancelled - so ask only when you cannot proceed without them, and decide autonomously for everything else.\n- Prefer concrete options over open questions; give each option a one-line description when the choice needs explaining. Options render as a list, one per row.\n- The verdict returns as JSON: {verdict:\"answer\",option,option_index} | {verdict:\"answer\",options,option_indexes} (multi) | {verdict:\"skip\"} | {verdict:\"expired\"}. Act on it through your normal tools; the card itself executes nothing.\n"
	return []core.PromptSection{
		{Title: "Verdict Cards", Body: body, Order: promptSectionOrder},
	}
}

// Toggleable marks this capsule as a module capsule.
func (p *Plugin) Toggleable() bool { return true }
