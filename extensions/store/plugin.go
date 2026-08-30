package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
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

// Mount opens the store from config and populates ctx.Store + a
// sessions.search tool provider + session slash commands.
func (p *Plugin) Mount(ctx *agent.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["store"].(Config); ok {
		cfg = c
	}
	dsn := cfg.DBPath
	if dsn == "" {
		dsn = "omega.db"
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
		agent.ExtensionCommand{Name: "/new", Description: "start a fresh conversation (--ephemeral for no persistence)"},
		agent.ExtensionCommand{Name: "/resume", Description: "resume a session by #, id, or label"},
		agent.ExtensionCommand{Name: "/branch", Description: "branch a new session from the current (or given) one"},
		agent.ExtensionCommand{Name: "/label", Description: "set a label on the current session (no text clears it)"},
		agent.ExtensionCommand{Name: "/export", Description: "export session messages to JSONL (default: <session_id>.jsonl)"},
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
		return agent.CommandResult{}, fmt.Errorf("unknown command: %s", name)
	}

	return nil
}

// NewPlugin returns a store Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }
