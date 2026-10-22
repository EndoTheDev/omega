package logging

import (
	"fmt"

	"github.com/EndoTheDev/omega/agent"
)

// Plugin implements agent.Plugin for the logging seam. It creates
// a FileLogger (or NopLogger when disabled) and sets ctx.Logger.
type Plugin struct{}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string       { return "logging" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "file logging: every run, tool call, and error appended to OMEGA_HOME/omega.log"
}
func (p *Plugin) Provides() []string { return []string{"logging"} }
func (p *Plugin) Requires() []string { return nil }

// Mount creates the logger from config and sets ctx.Logger.
func (p *Plugin) Mount(ctx *agent.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["logging"].(Config); ok {
		cfg = c
	}
	logger, err := NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("logging: %w", err)
	}
	ctx.Logger = logger
	// The TUI command rides the module seam: a logging plugin left out
	// of buildPlugins takes /logs out of the command table with it
	// (card t_88bb7dcf).
	ctx.TUIProviders = append(ctx.TUIProviders, &tui{})
	return nil
}

// NewPlugin creates a logging plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }
