package web

import (
	"github.com/EndoTheDev/omega/core"
)

// Plugin implements agent.Plugin for the web extension.
// It provides the additive "tools" seam: web.search and web.fetch.
type Plugin struct{}

// NewPlugin returns a web Plugin.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string       { return "web" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "web extension: browser-driven page fetching and extraction"
}
// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "web" }

func (p *Plugin) Provides() []string { return []string{"tools"} }
func (p *Plugin) Requires() []string { return nil }

// Mount creates the Extension from config and appends it to the
// additive ToolProviders slice.
//
// This Mount is the module's whole surface (arc 6 capsule, card
// t_23f42712): the consolidated web tool with its one-release alias
// shims (web.search, web.fetch) and the web config stanza (routed to
// ctx.Configs["web"] via ConfigKey; the base_url override and the
// provider-host mirror fallback derive in the host's ConfigSection)
// all ship from here, and the module ships nothing else - no command,
// no REST route, no skills, no exclusive seam, no canvas panel (nothing
// rendered web before the arc). Web Requires nothing and nothing
// Requires web. Removal proof: TestWebRemovalProof
// (cmd/omega/removal_test.go).
func (p *Plugin) Mount(ctx *core.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["web"].(Config); ok {
		cfg = c
	}
	ext := New(cfg)
	ctx.ToolProviders = append(ctx.ToolProviders, ext)
	return nil
}
