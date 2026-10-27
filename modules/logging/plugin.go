package logging

import (
	"fmt"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// Plugin implements agent.Plugin for the logging seam. It creates
// a FileLogger (or NopLogger when disabled) and mounts it through the
// ExtensionAPI exclusively (v0.12 phase F).
type Plugin struct{}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string { return "logging" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "file logging: every run, tool call, and error appended to OMEGA_HOME/omega.log"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "logging" }

func (p *Plugin) Provides() []string { return []string{"logging"} }
func (p *Plugin) Requires() []string { return nil }

// MountWithAPI implements agent.APIPlugin. Creates the logger from
// config, registers the logging seam, and registers the /logs TUI
// renderer (a logging plugin left out of buildPlugins takes /logs out
// of the command table with it - card t_88bb7dcf). The log file stays
// at dismount: disable never deletes data.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["logging"].(Config); ok {
		cfg = c
	}
	logger, err := NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("logging: %w", err)
	}
	if err := api.RegisterLogger(logger); err != nil {
		return err
	}
	return api.RegisterRenderer(&tui{})
}

// NewPlugin creates a logging plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }
