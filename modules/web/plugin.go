package web

import (
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin implements agent.Plugin for the web extension.
// It provides the additive "tools" seam: web.search and web.fetch.
type Plugin struct{}

// Compile-time assertion that *Plugin is an agent.Plugin and an
// agent.APIPlugin.
var (
	_ agent.Plugin    = (*Plugin)(nil)
	_ agent.Plugin = (*Plugin)(nil)
)

// NewPlugin returns a web Plugin.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "web" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "web extension: browser-driven page fetching and extraction"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "web" }

func (p *Plugin) Provides() []string { return []string{"tools"} }
func (p *Plugin) Requires() []string { return nil }

// Mount satisfies agent.Plugin (the interface keeps its classic shape
// until phase F). It is never dispatched: MountWithAPI exists, and
// APIPlugin wins (the strangler rule). If ever reached it fails loud -
// web does not mount the old way, which is the proof of migration.
func (p *Plugin) Mount(ctx *core.Context) error {
	return fmt.Errorf("web: classic mount retired - the capsule mounts through the ExtensionAPI (v0.12 phase C2, docs/migrating-a-module.md)")
}

// MountWithAPI implements agent.APIPlugin. This registration is the
// module's whole surface (arc 6 capsule, card t_23f42712): the two
// namespaced tools (web.search, web.fetch - the dotted alias shims
// promoted when the consolidation lost its namespace, Q10) and the web
// config stanza (routed to ctx.Configs["web"] via ConfigKey; the
// base_url override and the provider-host mirror fallback derive in
// the host's ConfigSection) all ship from here, and the module ships
// nothing else - no command, no REST route, no skills, no exclusive
// seam, no canvas panel (nothing rendered web before the arc). Web
// Requires nothing and nothing Requires web. Removal proof:
// TestWebRemovalProof (cmd/omega/removal_test.go).
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["web"].(Config); ok {
		cfg = c
	}
	ext := New(cfg)
	for name, tool := range ext.Tools() {
		if err := api.RegisterTool(name, tool); err != nil {
			return err
		}
	}
	return nil
}
// Toggleable marks this capsule as a module capsule (v0.12 E1, the
// tier law): the omega.json disable list may name it.
func (p *Plugin) Toggleable() bool { return true }
