package skills

import (
	"context"

	"github.com/EndoTheDev/omega/core"
)

// NewSkillsProvider creates a SkillsProvider configured to scan dir.
func NewSkillsProvider(dir string) *SkillsProvider {
	return &SkillsProvider{Dir: dir}
}

// Plugin is the agent.Plugin adapter for the skills extension.
type Plugin struct{}

// NewPlugin returns a skills Plugin.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "skills" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "skills: reusable procedure files agents can invoke in a sub-agent"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "skills" }

func (p *Plugin) Provides() []string { return []string{"skills", "tools", "prompt_sections"} }
// Requires names the logging seam: Mount runs WarnLegacyShape, which
// reads ctx.Logger to mirror the legacy-format boot warning into the
// log file. Logging must mount first or the edge is meaningless.
func (p *Plugin) Requires() []string { return []string{"logging"} }

// Mount populates ctx.Skills, appends the tool provider, and sets
// commands + handler. The skills directory is read from ctx.Configs.
func (p *Plugin) Mount(ctx *core.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["skills"].(Config); ok {
		cfg = c
	}
	dir := cfg.Dir
	if dir == "" {
		dir = "skills"
	}

	sp := NewSkillsProvider(dir)
	sp.SharedDir = cfg.SharedDir
	ctx.Skills = sp
	ctx.ToolProviders = append(ctx.ToolProviders, sp)
	ctx.PromptSectioners = append(ctx.PromptSectioners, sp)
	ctx.Commands = append(ctx.Commands, sp.Commands()...)

	// Legacy-format visibility (card t_5baad951): a folder holding
	// <name>.md without SKILL.md is invisible to discovery - one boot
	// warning per hit, never a silent skip. Reading the logging seam
	// here is why Requires names it.
	sp.WarnLegacyShape(ctx.Logger)

	// Wire command handler if not already set. Multiple plugins could
	// provide commands; the last one wins — ponytail: acceptable for now
	// since each command name is unique. Upgrade: dispatch table.
	prev := ctx.CommandHandler
	ctx.CommandHandler = func(c context.Context, name, args string) (core.CommandResult, error) {
		out, err := sp.HandleCommand(c, name, args)
		if err == nil {
			return core.CommandResult{Text: out}, nil
		}
		if prev != nil {
			return prev(c, name, args)
		}
		return core.CommandResult{Text: out}, err
	}

	return nil
}
