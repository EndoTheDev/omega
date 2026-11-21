package skills

import (
	"context"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

func NewSkillsProvider(dir string) *SkillsProvider {
	return &SkillsProvider{Dir: dir}
}

type Plugin struct {
	routes *Routes
}

func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "skills" }

func (p *Plugin) Description() string {
	return "skills: reusable procedure files agents can invoke in a sub-agent"
}

func (p *Plugin) ConfigKey() string { return "skills" }

func (p *Plugin) Provides() []string { return []string{"skills", "tools", "prompt_sections", "nudger"} }

// Requires names the logger seam: Mount runs WarnLegacyShape, which
// resolves the logging offer to mirror the legacy-format boot warning
// into the log file. Logging must mount first or the edge is meaningless.
func (p *Plugin) Requires() []string { return []string{"logger"} }

func (p *Plugin) Toggleable() bool { return true }

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
	sp.Allow, sp.AllowSet = cfg.Allow, cfg.AllowSet
	// The curator reads the provider seam LIVE at consult time.
	sp.SetProviderFn(func() agent.Provider { return seam[agent.Provider](ctx, "provider") })
	sp.SetCuratorModel(cfg.CuratorModel)
	// Legacy-format visibility: a folder holding <name>.md without SKILL.md is invisible
	// to discovery - one boot warning per hit, never a silent skip.
	sp.WarnLegacyShape(seam[agent.LoggerProvider](ctx, "logger"))

	interval := cfg.NudgeInterval
	if interval < 0 {
		interval = 0
	}

	steps := []func() error{
		func() error { return api.Offer("skills", sp) },
		func() error {
			p.routes = newRoutes(sp)
			return api.RegisterRoute("/skills/content", p.routes.handleSkillsContent)
		},
		func() error { return api.RegisterRoute("POST /skills/save", p.routes.handleSkillsSave) },
		func() error { return api.RegisterRoute("POST /skills/delete", p.routes.handleSkillsDelete) },
		func() error { return p.registerCuratorCLI(api, sp) },
		func() error { return p.registerSkillsCLI(api, sp) },
		func() error { return api.Offer("nudger", NewTurnNudger(interval)) },
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
							return core.CommandResult{}, core.CommandMiss(name)
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
