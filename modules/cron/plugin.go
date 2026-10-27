package cron

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin adapts the cron Runner into omega's plugin system. It
// provides the "cron" seam (exclusive) - the serve host starts the
// runner after MountAllAPI with the store open.
type Plugin struct {
	cfg     *Config
	home    string
	runner  core.CronProvider // the mounted scheduler; nil = not mounted
	capsule *capsule          // the canvas surface; nil = not mounted
}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

// NewPlugin creates the cron plugin. Mount resolves OMEGA_HOME and
// loads crons.yaml - a missing file is an empty job set.
func NewPlugin() *Plugin { return &Plugin{} }

// NewPluginWithConfig creates the plugin from an explicit config and
// home (tests, embedders).
func NewPluginWithConfig(cfg *Config, home string) *Plugin {
	return &Plugin{cfg: cfg, home: home}
}

// Name returns the plugin name shown in the /extensions listing.
func (p *Plugin) Name() string { return "cron" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "cron: schedules omega prompts on cron expressions, one agent turn per fire"
}

// Provides lists the seams this plugin mounts.
// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"cron"} }

// Requires: notifications. The runner's notifier arrives through
// CronDeps.Ctx at Start (host wiring, post-mount) - the edge is the
// presence declaration: with notifications toggled off, cron must fail
// the boot (Q3), not silently drop its finished-things records. No
// mount-time capture exists (recipe law 4; the capture is at Start,
// after MountAllAPI returns).
func (p *Plugin) Requires() []string { return []string{"notifications"} }

// MountWithAPI loads crons.yaml and registers every face: the cron
// seam, the /cron TUI commands (the module-owns-its-surface law, card
// t_88bb7dcf: a cron plugin left out of buildPlugins takes /cron out
// of the command table with it), and the jobs panel. A load failure
// (invalid yaml) is a mount error - the operator sees the offending
// job name and line at startup, not a silent skip.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	home := p.home
	if home == "" {
		home = cronHome()
	}
	cfg := p.cfg
	if cfg == nil {
		var err error
		// One cron module for every home (card t_72ecca67, ruling 12):
		// the main crons.yaml AND each agent home's config/crons.yaml.
		cfg, err = LoadAllConfigs(home)
		if err != nil {
			return err
		}
	}
	p.cfg = cfg
	p.home = home
	r := NewRunner(cfg, home)
	p.runner = r
	if err := api.RegisterCron(r); err != nil {
		return err
	}
	// The goroutine fence (v0.12 E1): the runner comes live only when
	// the host calls StartAll after the mount commit - Mount spawns
	// nothing. The handle wraps the cron seam's Start/Stop. Runner.Start
	// blocks until cancellation or Stop, so the handle arms it on its
	// own goroutine (the fence Start law: Start returns promptly) and
	// Stop ends it.
	if err := api.RegisterLifecycle(
		"runner",
		func(ctx context.Context, deps core.StartDeps) error {
			go func() {
				if err := r.Start(ctx, core.CronDeps{Ctx: deps.Ctx, Agent: deps.Agent, Store: deps.Store}); err != nil {
					fmt.Fprintf(os.Stderr, "omega: cron runner: %v\n", err)
				}
			}()
			return nil
		},
		func() error { return r.Stop() },
	); err != nil {
		return err
	}
	if err := api.RegisterRenderer(&tui{p: p}); err != nil {
		return err
	}
	// Capsule law (arc 6, t_31174f80): the module ships its view-model
	// with its scheduler. The jobs panel rides the same mount - an
	// unregistered plugin ships no seam, no commands, no panels. The
	// runner pushes the refreshed panel through this registration on
	// every hot reload (the module's one internal mutation).
	p.capsule = newCapsule(r)
	r.panels = p.capsule
	return api.RegisterPanelProvider(p.capsule)
}

// homeFile returns the crons.yaml path inside home. The legacy home-root
// location wins if it exists (existing installs keep working unchanged);
// otherwise the config/ subdirectory (Endo 2026-10-18 layout) is used.
// Kept separate so tests and the watcher share one resolution rule.
func homeFile(home string) string {
	root := filepath.Join(home, stateFileName)
	if _, err := os.Stat(root); err == nil {
		return root
	}
	return filepath.Join(home, "config", stateFileName)
}

// Toggleable marks this capsule as a module capsule (v0.12 E1, the
// tier law): the omega.json disable list may name it.
func (p *Plugin) Toggleable() bool { return true }
