package web

import (
	"github.com/EndoTheDev/omega/agent"
)

// Plugin implements agent.Plugin for the web extension.
// It provides the additive "tools" seam: web.search and web.fetch.
type Plugin struct{}

// NewPlugin returns a web Plugin.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string       { return "web" }
func (p *Plugin) Provides() []string { return []string{"tools"} }
func (p *Plugin) Requires() []string { return nil }

// Mount creates the Extension from config and appends it to the
// additive ToolProviders slice.
func (p *Plugin) Mount(ctx *agent.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["web"].(Config); ok {
		cfg = c
	}
	ext := New(cfg)
	ctx.ToolProviders = append(ctx.ToolProviders, ext)
	return nil
}
