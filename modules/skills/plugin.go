package skills

import (
	"context"

	"github.com/EndoTheDev/omega/agent"
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
// commands + handler. The skills directory is read from ctx.Configs.
func (p *Plugin) Mount(ctx *agent.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["skills"].(Config); ok {
		cfg = c
	}
	dir := cfg.Dir
	if dir == "" {
		dir = "skills"
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
