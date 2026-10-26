package logging

import (
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin implements agent.Plugin for the logging seam. It creates
// a FileLogger (or NopLogger when disabled) and sets ctx.Logger.
type Plugin struct {
	// renderer is the /logs TUI provider Mount appended - the value
	// Dismount removes by identity (the E1 dismount proof).
	renderer *tui
}

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

// Mount creates the logger from config and sets ctx.Logger.
func (p *Plugin) Mount(ctx *core.Context) error {
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
	p.renderer = &tui{}
	ctx.TUIProviders = append(ctx.TUIProviders, p.renderer)
	return nil
}

// NewPlugin creates a logging plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }

// Dismount unmounts the logging seam and the /logs renderer (the E1
// dismount proof). The log file stays: disable never deletes data.
func (p *Plugin) Dismount(ctx *core.Context) error {
	ctx.Logger = nil
	tuis := ctx.TUIProviders[:0]
	for _, tp := range ctx.TUIProviders {
		if tp == p.renderer {
			continue
		}
		tuis = append(tuis, tp)
	}
	ctx.TUIProviders = tuis
	return nil
}
