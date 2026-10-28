package skills

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// NewSkillsProvider creates a SkillsProvider configured to scan dir.
func NewSkillsProvider(dir string) *SkillsProvider {
	return &SkillsProvider{Dir: dir}
}

// Plugin is the agent.Plugin adapter for the skills extension. It
// mounts through the ExtensionAPI exclusively (v0.12 phase F).
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

// MountWithAPI implements agent.APIPlugin. Populates the skills seam,
// the skills.read tool, the prompt section, and the /skills command.
// The skills directory is read from ctx.Configs.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
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
	sp.SetDisabled(cfg.Disabled)
	// Legacy-format visibility (card t_5baad951): a folder holding
	// <name>.md without SKILL.md is invisible to discovery - one boot
	// warning per hit, never a silent skip. Reading the logging seam
	// here is why Requires names it.
	sp.WarnLegacyShape(ctx.Logger)

	steps := []func() error{
		func() error { return api.RegisterSkills(sp) },
		func() error {
			for name, t := range sp.Tools() {
				if err := api.RegisterTool(name, t); err != nil {
					return err
				}
			}
			return nil
		},
		func() error { return api.RegisterPromptSectioner(sp) },
		func() error {
			for _, c := range sp.Commands() {
				cmd := c
				if err := api.RegisterCommand(cmd.Name, cmd.Description, cmd.Args,
					func(c context.Context, name, args string) (core.CommandResult, error) {
						if name != cmd.Name {
							return core.CommandResult{}, fmt.Errorf("no handler for %s", name)
						}
						out, err := sp.HandleCommand(c, name, args)
						if err == nil {
							return core.CommandResult{Text: out}, nil
						}
						return core.CommandResult{Text: out}, err
					}); err != nil {
					return err
				}
			}
			return nil
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}
