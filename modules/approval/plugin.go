package approval

import (
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// Plugin mounts the approval capsule (card t_c2b52cb5): the
// smart-approval seam, fed by the one gate object that owns the
// detector, the reviewer client, the ladder, the cache and the audit
// log. Mode off mounts NOTHING (Q10: the gate is fully absent - no
// seam, no detection, no calls, no logging); manual asks the human
// (Q5 default); smart reviews with the decision model first.
type Plugin struct {
	gate *Gate
}

var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string { return "approval" }

func (p *Plugin) Description() string {
	return "approval: the smart approval gate - risky shell commands reach a decision model + deterministic ladder, escalations ride verdict cards"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer).
func (p *Plugin) ConfigKey() string { return "approvals" }

// Provides approvals (exclusive). Requires verdicts: an escalation
// HARD-PAUSES inside the gate on a verdict card (Q13) - a host
// without verdict cards cannot block for a human. Read-only use of
// the Context's notifications emitter (Q11) is nil-safe; no Requires
// edge (kanban's emitter pattern - a disabled notifications module
// still gates, it just does not ring).
func (p *Plugin) Provides() []string { return []string{"approvals"} }
func (p *Plugin) Requires() []string { return []string{"verdicts"} }

// MountWithAPI registers the seam; nothing else exists (no tools - the
// gate rides the loop's tool-exec hook - no commands, no routes, no
// panels, no skills). Mode off registers nothing at all (Q10).
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["approvals"].(Config); ok {
		cfg = c
	}
	if cfg.NormalizedMode() == ModeOff {
		return nil
	}
	// One hub per Context is the single-answer race; the gate reads it
	// live so a seam rewire is visible at ask time (verdicts mount
	// before approvals by the Requires edge).
	verdicts := func() core.VerdictsProvider { return ctx.Verdicts }
	notes := func() core.NotificationsProvider { return ctx.Notifications }
	p.gate = NewGate(cfg, verdicts, notes, ctx.Logger)
	if err := api.RegisterApprovals(p.gate); err != nil {
		return err
	}
	// the prompt guidance rides the same Mount (every module self-
	// registers its sectioner)
	return api.RegisterPromptSectioner(p)
}

const promptSectionOrder = 36 // after verdict cards (35)

// PromptSections tells the agent (both origins) how the gate behaves:
// which commands flag, what an escalation looks like in its context,
// and that an escalated command never executed. Pure information for
// routing around refusals; the gate itself is loop-side, invisible.
func (p *Plugin) PromptSections(opts core.PromptBuildOptions) []core.PromptSection {
	body := "- approval gate: shell commands with write, network or system effects pass an approval check before they run. Read-only and inspecting commands are unaffected.\n" +
		"- In smart mode a decision model reviews flagged commands: APPROVE/DENY decide automatically, ESCALATE rises as a verdict card (interactive) or a tool error + notification (cron runs). A DENY or escalation means the command never executed - do not retry the identical command expecting a different answer; change it, or ask the user.\n" +
		"- In manual mode every flagged command asks you to approve via a card. Keep ordinary operations in files tools and routine commands; batch related shell work into fewer commands.\n"
	return []core.PromptSection{{Title: "Approval Gate", Body: body, Order: promptSectionOrder}}
}

// Toggleable marks this capsule as a module capsule (v0.12 E1, the
// tier law): the config.yaml disabled: list may name it.
func (p *Plugin) Toggleable() bool { return true }

// NewPlugin returns the approval Plugin.
func NewPlugin() *Plugin { return &Plugin{} }
