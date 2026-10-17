package cron

import (
	"path/filepath"

	"github.com/EndoTheDev/omega/agent"
)

// Plugin adapts the cron Runner into omega's plugin system. It
// provides the "cron" seam (exclusive) - the serve host starts the
// runner after MountAll with the store open.
type Plugin struct {
	cfg  *Config
	home string
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

// Provides lists the seams this plugin mounts.
func (p *Plugin) Provides() []string { return []string{"cron"} }

// Requires lists seams that must be mounted before this plugin. The
// cron runner receives the agent, store, memory and logger through
// CronDeps at Start - no mount-time dependencies.
func (p *Plugin) Requires() []string { return nil }

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
	ctx.Cron = NewRunner(cfg, home)
	return nil
}

// homeFile returns the crons.yaml path inside home. Kept separate so
// tests and the watcher share one path constant.
func homeFile(home string) string {
	return filepath.Join(home, stateFileName)
}