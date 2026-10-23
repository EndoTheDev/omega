package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// Plugin implements agent.Plugin for the store seam. It opens the
// SQLite database from config and mounts it as the StoreProvider,
// plus contributes the sessions.search tool and session commands.
type Plugin struct{}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string       { return "store" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "session persistence: SQLite-backed message and session store"
}
// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "store" }

func (p *Plugin) Provides() []string { return []string{"store", "tools"} }
func (p *Plugin) Requires() []string { return nil }

// Mount opens the store from config and populates ctx.Store + a
// sessions.search tool provider + session slash commands.
func (p *Plugin) Mount(ctx *core.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["store"].(Config); ok {
		cfg = c
	}
	dsn := cfg.DBPath
	if dsn == "" || dsn == "omega.db" {
		// <home>/data/omega.db - the host (cmd/omega resolveHomePaths)
		// passes an absolute path for the exe-dir home case.
		if home := datadir.Home(); home != "" {
			dsn = datadir.DBPath(home, "omega.db")
		} else {
			dsn = "omega.db"
		}
	}
	s, err := Open(dsn)
	if err != nil {
		return err
	}
	ctx.Store = s
	ctx.ToolProviders = append(ctx.ToolProviders, &searchToolProvider{store: s})

	// Register session commands.
	ctx.Commands = append(ctx.Commands,
		core.ExtensionCommand{Name: "/sessions", Description: "list, resume, branch, label, or delete sessions"},
		core.ExtensionCommand{Name: "/agents", Description: "list registered agent profiles"},
		core.ExtensionCommand{Name: "/agent", Description: "show one agent profile's detail (/agent <name>)"},
		core.ExtensionCommand{Name: "/tree", Description: "show session tree with nesting and message counts"},
		core.ExtensionCommand{Name: "/search", Description: "search past session messages by keyword"},
		core.ExtensionCommand{Name: "/insights", Description: "show cross-session usage analytics"},
		core.ExtensionCommand{Name: "/new", Description: "start a fresh conversation (--ephemeral for no persistence, --agent <name> to bind it)"},
		core.ExtensionCommand{Name: "/resume", Description: "resume a session by #, id, or label"},
		core.ExtensionCommand{Name: "/branch", Description: "branch a new session from the current (or given) one"},
		core.ExtensionCommand{Name: "/label", Description: "set a label on the current session (no text clears it)"},
		core.ExtensionCommand{Name: "/export", Description: "export session messages to JSONL (default: <session_id>.jsonl)"},
	)

	// Wire command handler, chaining after any previous handler.
	prev := ctx.CommandHandler
	ctx.CommandHandler = func(c context.Context, name, args string) (core.CommandResult, error) {
		switch name {
		case "/sessions":
			return HandleSessionsCommand(c, s, args)
		case "/agents":
			return HandleAgentsCommand(c, s)
		case "/agent":
			// TUI passes the raw args; the agent name is the first word.
			return HandleAgentDetailCommand(c, s, args)
		case "/tree":
			return HandleTreeCommand(c, s)
		case "/search":
			return HandleSearchCommand(c, s, args)
		case "/insights":
			return HandleInsightsCommand(c, s, args)
		case "/new":
			return HandleNewCommand(args)
		case "/resume":
			return HandleResumeCommand(c, s, args)
		case "/branch":
			// TUI passes "parentID rest" — extract the parent ID.
			parts := strings.SplitN(args, " ", 2)
			parentID := parts[0]
			rest := ""
			if len(parts) > 1 {
				rest = parts[1]
			}
			return HandleBranchCommand(c, s, parentID, rest)
		case "/label":
			// TUI passes "sessionID label" — extract the session ID.
			parts := strings.SplitN(args, " ", 2)
			sessionID := parts[0]
			rest := ""
			if len(parts) > 1 {
				rest = parts[1]
			}
			return HandleLabelCommand(c, s, sessionID, rest)
		case "/export":
			// TUI passes "sessionID path" — extract the session ID.
			parts := strings.SplitN(args, " ", 2)
			sessionID := parts[0]
			rest := ""
			if len(parts) > 1 {
				rest = parts[1]
			}
			return HandleExportCommand(c, s, sessionID, rest)
		}
		if prev != nil {
			return prev(c, name, args)
		}
		return core.CommandResult{}, fmt.Errorf("unknown command: %s", name)
	}

	return nil
}

// NewPlugin returns a store Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }
