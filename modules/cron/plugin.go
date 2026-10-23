package cron

import (
	"os"
	"path/filepath"

	"github.com/EndoTheDev/omega/agent"
)

// Plugin adapts the cron Runner into omega's plugin system. It
// provides the "cron" seam (exclusive) - the serve host starts the
// runner after MountAll with the store open.
type Plugin struct {
	cfg    *Config
	home   string
	runner agent.CronProvider // the mounted scheduler; nil = not mounted
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

// Requires lists seams that must be mounted before this plugin. The
// cron runner receives agent, store, memory and logger through
// CronDeps at Start, and it captures ctx.Notifications there too -
// declared as the notifications edge (card t_13a2af8a: mount order is
// declared data, not a comment law about list position).
func (p *Plugin) Requires() []string { return []string{"notifications"} }

// Mount loads crons.yaml and mounts the Runner into ctx.Cron. A load
// failure (invalid yaml) is a mount error - the operator sees the
// offending job name and line at startup, not a silent skip.
func (p *Plugin) Mount(ctx *agent.Context) error {
	home := p.home
	if home == "" {
		home = cronHome()
	}
	cfg := p.cfg
	if cfg == nil {
		var err error
		cfg, err = LoadConfig(homeFile(home))
		if err != nil {
			return err
		}
	}
	p.cfg = cfg
	p.home = home
	p.runner = NewRunner(cfg, home)
	ctx.Cron = p.runner
	// The TUI command rides the module seam: registering here means a
	// cron plugin left out of buildPlugins takes /cron out of the
	// command table with it (card t_88bb7dcf).
	ctx.TUIProviders = append(ctx.TUIProviders, &tui{p: p})
	return nil
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