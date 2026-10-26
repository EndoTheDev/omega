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
type Plugin struct {
	// mountState carries what Dismount must reverse (the E1 dismount
	// proof): the provider, its appends and the prior command chain.
	sp          *SkillsProvider
	prevHandler commandHandler
}

// commandHandler is the CommandHandler stack shape the classic mounts
// chain onto.
type commandHandler = func(ctx context.Context, name, args string) (core.CommandResult, error)

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
	p.sp = sp
	p.prevHandler = ctx.CommandHandler
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

// Dismount unmounts the skills seam: the seam slot, the provider, its
// prompt section, its commands and the command chain (the E1
// dismount proof). The provider is removed from the additive slices
// by identity.
func (p *Plugin) Dismount(ctx *core.Context) error {
	ctx.Skills = nil
	tools := ctx.ToolProviders[:0]
	for _, tp := range ctx.ToolProviders {
		if tp == p.sp {
			continue
		}
		tools = append(tools, tp)
	}
	ctx.ToolProviders = tools
	sections := ctx.PromptSectioners[:0]
	for _, ps := range ctx.PromptSectioners {
		if ps == p.sp {
			continue
		}
		sections = append(sections, ps)
	}
	ctx.PromptSectioners = sections
	names := map[string]bool{}
	if p.sp != nil {
		for _, c := range p.sp.Commands() {
			names[c.Name] = true
		}
	}
	cmds := ctx.Commands[:0]
	for _, c := range ctx.Commands {
		if names[c.Name] {
			continue
		}
		cmds = append(cmds, c)
	}
	ctx.Commands = cmds
	ctx.CommandHandler = p.prevHandler
	return nil
}
