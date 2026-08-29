package store

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
)

// Plugin implements agent.Plugin for the store seam. It opens the
// SQLite database from config and mounts it as the StoreProvider,
// plus contributes the sessions.search tool and session commands.
type Plugin struct{}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string       { return "store" }
func (p *Plugin) Provides() []string { return []string{"store", "tools"} }
func (p *Plugin) Requires() []string { return nil }

// Mount opens the store from config (gateway.Config.Store.DBPath,
// defaulting to "omega.db") and populates ctx.Store + a sessions.search
// tool provider + session slash commands.
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

	// Register session commands.
	ctx.Commands = append(ctx.Commands,
		agent.ExtensionCommand{Name: "/sessions", Description: "list, resume, branch, label, or delete sessions"},
		agent.ExtensionCommand{Name: "/tree", Description: "show session tree with nesting and message counts"},
		agent.ExtensionCommand{Name: "/search", Description: "search past session messages by keyword"},
		agent.ExtensionCommand{Name: "/insights", Description: "show cross-session usage analytics"},
	)

	// Wire command handler, chaining after any previous handler.
	prev := ctx.CommandHandler
	ctx.CommandHandler = func(c context.Context, name, args string) (agent.CommandResult, error) {
		switch name {
		case "/sessions":
			return HandleSessionsCommand(c, s, args)
		case "/tree":
			return HandleTreeCommand(c, s)
		case "/search":
			return HandleSearchCommand(c, s, args)
		case "/insights":
			return HandleInsightsCommand(c, s, args)
		}
		if prev != nil {
			return prev(c, name, args)
		}
		return agent.CommandResult{}, fmt.Errorf("unknown command: %s", name)
	}

	return nil
}

// NewPlugin returns a store Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }