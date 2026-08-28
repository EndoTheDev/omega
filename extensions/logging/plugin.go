package logging

import (
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
)

// Plugin implements agent.Plugin for the logging seam. It creates
// a FileLogger (or NopLogger when disabled) and sets ctx.Logger.
type Plugin struct{}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string       { return "logging" }
func (p *Plugin) Provides() []string { return []string{"logging"} }
func (p *Plugin) Requires() []string { return nil }

// Mount creates the logger from config and sets ctx.Logger.
func (p *Plugin) Mount(ctx *agent.Context) error {
	cfg, ok := ctx.Config.(gateway.Config)
	if !ok {
		cfg = gateway.DefaultConfig()
	}
	logger, err := NewLogger(cfg.Logging)
	if err != nil {
		return fmt.Errorf("logging: %w", err)
	}
	ctx.Logger = logger
	return nil
}

// NewPlugin returns a logging Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }