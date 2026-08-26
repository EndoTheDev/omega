package skills

import (
	"context"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
)

// NewSkillsProvider creates a SkillsProvider configured to scan dir.
func NewSkillsProvider(dir string) *SkillsProvider {
	return &SkillsProvider{Dir: dir}
}

// Plugin is the agent.Plugin adapter for the skills extension.
type Plugin struct{}

// NewPlugin returns a skills Plugin.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string       { return "skills" }
func (p *Plugin) Provides() []string { return []string{"skills", "tools"} }
func (p *Plugin) Requires() []string { return nil }

// Mount populates ctx.Skills, appends the tool provider, and sets
// commands + handler. The skills directory is read from ctx.Config
// (type-asserted to gateway.Config).
func (p *Plugin) Mount(ctx *agent.Context) error {
	dir := "skills" // default
	if cfg, ok := ctx.Config.(gateway.Config); ok {
		if cfg.Skills.Dir != "" {
			dir = cfg.Skills.Dir
		}
	}

	sp := NewSkillsProvider(dir)
	ctx.Skills = sp
	ctx.ToolProviders = append(ctx.ToolProviders, sp)
	ctx.Commands = append(ctx.Commands, sp.Commands()...)

	// Wire command handler if not already set. Multiple plugins could
	// provide commands; the last one wins — ponytail: acceptable for now
	// since each command name is unique. Upgrade: dispatch table.
	prev := ctx.CommandHandler
	ctx.CommandHandler = func(c context.Context, name, args string) (agent.CommandResult, error) {
		out, err := sp.HandleCommand(c, name, args)
		if err == nil {
			return agent.CommandResult{Text: out}, nil
		}
		if prev != nil {
			return prev(c, name, args)
		}
		return agent.CommandResult{Text: out}, err
	}

	return nil
}