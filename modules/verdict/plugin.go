package verdict

import (
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin mounts the verdicts seam and its surfaces: the
// verdict.request tool, the /verdict TUI command, and a short prompt
// section so the model knows when to ask. The hub is built once and
// shared by the tool (the blocking Ask), the HTTP channel (the answer
// POSTs), and the TUI (the answer command) - that one object is the
// single-answer race. It mounts through the ExtensionAPI exclusively
// (v0.12 phase C2; the classic Mount body is retired to the fail-loud
// stub below).
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

// Mount satisfies agent.Plugin (the interface keeps its classic shape
// until phase F). It is never dispatched: MountWithAPI exists, and
// APIPlugin wins (the strangler rule). If ever reached it fails loud -
// verdict does not mount the old way, which is the proof of migration.
func (p *Plugin) Mount(ctx *core.Context) error {
	return fmt.Errorf("verdict: classic mount retired - the capsule mounts through the ExtensionAPI (v0.12 phase C2, docs/migrating-a-module.md)")
}

// MountWithAPI builds the hub from the seams the mount reads in place
// (store, logging - the Requires edges stay, recipe law 4) and
// registers every face: the verdicts seam, the verdict.request tool,
// the TUI renderer, and the Verdict Cards prompt section.
//
// Capsule posture (arc 6, card t_78935c07): this registration is the
// module's whole surface - the seam, the blocking verdict tool, the
// /verdict command, and the Verdict Cards prompt section all ship from
// here, and the module ships nothing else. No canvas panel and no
// module-qualified actions exist (the card renders in the chat stream
// off the tool call; nothing rendered a verdict panel before the arc).
// The /verdicts REST routes live in http_channel behind the
// verdicts seam's nil guard - unmount answers 501 by construction.
// The shell's busy-Enter answer path (tui/tui_verdict.go)
// reads the same seam and is nil-guarded: no mount, no intercept.
// Take verdict.NewPlugin() out of buildPlugins() and every face above
// vanishes; the proof is TestVerdictRemovalProof
// (cmd/omega/removal_test.go, seventh).
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	p.hub = NewHub(ctx.Store, ctx.Logger)
	if err := api.RegisterVerdicts(p.hub); err != nil {
		return err
	}
	for name, tool := range (&verdictToolProvider{hub: p.hub}).Tools() {
		if err := api.RegisterTool(name, tool); err != nil {
			return err
		}
	}
	if err := api.RegisterRenderer(&tuiProvider{hub: p.hub}); err != nil {
		return err
	}
	return api.RegisterPromptSectioner(p)
}

// NewPlugin returns a verdict Plugin.
func NewPlugin() *Plugin { return &Plugin{} }

const promptSectionOrder = 35 // after memory (10), skills (20), tools (30)

// PromptSections renders the model guidance for verdict.request: when
// to ask (the agent decides, no gate or config) and the block/answer
// contract. The JSON tool schema already reaches the model; this is
// the "use it well" nudge.
func (p *Plugin) PromptSections(opts core.PromptBuildOptions) []core.PromptSection {
	body := "- verdict.request: ask the user when a decision materially affects them and you cannot proceed without their input - approval (two options, \"yes\" / \"no\"), a pick between options, a multi-select, or free text (allow_text). It BLOCKS the turn until they answer, the card expires (20 minutes default), or the run is cancelled - so ask only when you cannot proceed without them, and decide autonomously for everything else.\n- Prefer concrete options over open questions; give each option a one-line description when the choice needs explaining. Options render as a list, one per row.\n- The verdict returns as JSON: {verdict:\"answer\",option,option_index} | {verdict:\"answer\",options,option_indexes} (multi) | {verdict:\"skip\"} | {verdict:\"expired\"}. Act on it through your normal tools; the card itself executes nothing.\n"
	return []core.PromptSection{
		{Title: "Verdict Cards", Body: body, Order: promptSectionOrder},
	}
}

// Toggleable marks this capsule as a module capsule (v0.12 E1, the
// tier law): the omega.json disable list may name it.
func (p *Plugin) Toggleable() bool { return true }
