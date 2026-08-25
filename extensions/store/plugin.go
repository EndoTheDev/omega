package store

import (
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
)

// Plugin implements agent.Plugin for the store seam. It opens the
// SQLite database from config and mounts it as the StoreProvider,
// plus contributes the sessions.search tool.
type Plugin struct{}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string       { return "store" }
func (p *Plugin) Provides() []string { return []string{"store", "tools"} }
func (p *Plugin) Requires() []string { return nil }

// Mount opens the store from config (gateway.Config.Store.DBPath,
// defaulting to "omega.db") and populates ctx.Store + a sessions.search
// tool provider.
func (p *Plugin) Mount(ctx *agent.Context) error {
	dsn := "omega.db"
	if cfg, ok := ctx.Config.(gateway.Config); ok && cfg.Store.DBPath != "" {
		dsn = cfg.Store.DBPath
	}
	s, err := NewStore(dsn)
	if err != nil {
		return err
	}
	ctx.Store = s
	ctx.ToolProviders = append(ctx.ToolProviders, &searchToolProvider{store: s})
	return nil
}

// NewPlugin returns a store Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }