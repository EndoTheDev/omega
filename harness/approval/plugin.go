package approval

import (
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin mounts the approval capsule.
type Plugin struct {
	gate *Gate
}

var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string { return "approval" }

func (p *Plugin) Description() string {
	return "approval: the smart approval gate - risky shell commands reach a decision model + deterministic ladder, escalations ride verdict cards"
}

// ConfigKey names the config section this plugin reads from ctx.Configs (see.
func (p *Plugin) ConfigKey() string { return "approvals" }

// Provides approvals (exclusive). Requires verdicts: an escalation HARD-PAUSES inside
// the gate on a verdict card (Q13) - a host without verdict cards cannot block for a
// human. Read-only use of the Context's notifications emitter (Q11) is nil-safe; no
// Requires edge (kanban's emitter pattern - a disabled notifications module still
// gates, it just does not ring).
func (p *Plugin) Provides() []string { return []string{"approvals"} }
func (p *Plugin) Requires() []string { return []string{"verdicts"} }

// MountWithAPI registers the seam; nothing else exists (no tools.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["approvals"].(Config); ok {
		cfg = c
	}
	if cfg.NormalizedMode() == ModeOff {
		return nil
	}
	// One hub per Context is the single-answer race.
	verdicts := func() verdictHub {
		h, _ := ctx.Services().Resolve("verdicts").(verdictHub)
		return h
	}
	notes := func() finishedThings {
		n, _ := ctx.Services().Resolve("notifications").(finishedThings)
		return n
	}
	p.gate = NewGate(cfg, verdicts, notes, seam[agent.LoggerProvider](ctx, "logger"))
	if err := api.Offer("approvals", p.gate); err != nil {
		return err
	}
	return api.RegisterPromptSectioner(p)
}

const promptSectionOrder = 36 // after verdict cards (35)

// PromptSections tells the agent (both origins) how the gate behaves: which commands
// flag, what an escalation looks like in its context, and that an escalated command
// never executed.
func (p *Plugin) PromptSections(opts core.PromptBuildOptions) []core.PromptSection {
	body := "- approval gate: shell commands with write, network or system effects pass an approval check before they run. Read-only and inspecting commands are unaffected.\n" +
		"- In smart mode a decision model reviews flagged commands: APPROVE/DENY decide automatically, ESCALATE rises as a verdict card (interactive) or a tool error + notification (cron runs). A DENY or escalation means the command never executed - do not retry the identical command expecting a different answer; change it, or ask the user.\n" +
		"- In manual mode every flagged command asks you to approve via a card. Keep ordinary operations in files tools and routine commands; batch related shell work into fewer commands.\n"
	return []core.PromptSection{{Title: "Approval Gate", Body: body, Order: promptSectionOrder}}
}

// Toggleable marks this capsule as a module capsule (v0.12 E1, the tier law).
func (p *Plugin) Toggleable() bool { return true }

// NewPlugin returns the approval Plugin.
func NewPlugin() *Plugin { return &Plugin{} }
