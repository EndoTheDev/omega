package skills

import (
	"context"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// NewSkillsProvider creates a SkillsProvider configured to scan dir.
func NewSkillsProvider(dir string) *SkillsProvider {
	return &SkillsProvider{Dir: dir}
}

// Plugin is the agent.Plugin adapter for the skills extension.
type Plugin struct {
	routes *Routes
}

// NewPlugin returns a skills Plugin.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "skills" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "skills: reusable procedure files agents can invoke in a sub-agent"
}

// ConfigKey names the config section this plugin reads from ctx.Configs (see.
func (p *Plugin) ConfigKey() string { return "skills" }

func (p *Plugin) Provides() []string { return []string{"skills", "tools", "prompt_sections", "nudger"} }

// Requires names the logger seam: Mount runs WarnLegacyShape, which
// resolves the logging offer to mirror the legacy-format boot warning
// into the log file. Logging must mount first or the edge is meaningless.
func (p *Plugin) Requires() []string { return []string{"logger"} }

// Toggleable opts skills into the toggleable tier.
func (p *Plugin) Toggleable() bool { return true }

// MountWithAPI implements agent.APIPlugin.
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
	// The explicit allowlist (0.17.3 make-over).
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
		// The skill viewer's content route rides the capsule too.
		func() error {
			p.routes = newRoutes(sp)
			return api.RegisterRoute("/skills/content", p.routes.handleSkillsContent)
		},
		// The tab editor's write face.
		func() error { return api.RegisterRoute("POST /skills/save", p.routes.handleSkillsSave) },
		func() error { return api.RegisterRoute("POST /skills/delete", p.routes.handleSkillsDelete) },
		// The CLI face of the curator pass and of the registry editor.
		func() error { return p.registerCuratorCLI(api, sp) },
		func() error { return p.registerSkillsCLI(api, sp) },
		// The learning-loop reminder seam.
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
