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
// SQLite database from config and mounts it through the ExtensionAPI
// exclusively (v0.12 phase F) - the StoreProvider seam, the
// sessions.search tool and the session commands.
type Plugin struct{}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string { return "store" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "session persistence: SQLite-backed message and session store"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "store" }

func (p *Plugin) Provides() []string { return []string{"store", "tools"} }
func (p *Plugin) Requires() []string { return nil }

// Mount is the retired classic body (v0.12 phase F). Fail loud.
func (p *Plugin) Mount(ctx *core.Context) error {
	return fmt.Errorf("store: classic mount retired - the capsule mounts through the ExtensionAPI (v0.12 phase F)")
}

// MountWithAPI implements agent.APIPlugin. Opens the store from
// config, registers the store seam, the sessions.search tool provider
// and the eleven session commands. The store connection is host-owned
// at dismount - the journal unmounts the registrations, never the
// data (disable never deletes data).
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
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
	search := &searchToolProvider{store: s}
	steps := []func() error{
		func() error { return api.RegisterStore(s) },
		func() error {
			for name, t := range search.Tools() {
				if err := api.RegisterTool(name, t); err != nil {
					return err
				}
			}
			return nil
		},
		func() error {
			for _, c := range sessionCommands() {
				cmd := c
				if err := api.RegisterCommand(cmd.Name, cmd.Description, cmd.Args,
					func(c context.Context, name, args string) (core.CommandResult, error) {
						return p.handleSessionCommand(s, c, name, args)
					}); err != nil {
					return err
				}
			}
			return nil
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			_ = s.Close() // a half-registered store would leak the db file
			return err
		}
	}
	return nil
}

// sessionCommands returns the eleven session commands in registration
// order.
func sessionCommands() []core.ExtensionCommand {
	return []core.ExtensionCommand{
		{Name: "/sessions", Description: "list, resume, branch, label, or delete sessions"},
		{Name: "/agents", Description: "list registered agent profiles"},
		{Name: "/agent", Description: "show one agent profile's detail (/agent <name>)"},
		{Name: "/tree", Description: "show session tree with nesting and message counts"},
		{Name: "/search", Description: "search past session messages by keyword"},
		{Name: "/insights", Description: "show cross-session usage analytics"},
		{Name: "/new", Description: "start a fresh conversation (--ephemeral for no persistence, --agent <name> to bind it)"},
		{Name: "/resume", Description: "resume a session by #, id, or label"},
		{Name: "/branch", Description: "branch a new session from the current (or given) one"},
		{Name: "/label", Description: "set a label on the current session (no text clears it)"},
		{Name: "/export", Description: "export session messages to JSONL (default: <session_id>.jsonl)"},
	}
}

// handleSessionCommand dispatches one session command (the classic
// wrapper's switch, one command at a time - the api chain-on-miss
// wrapper produces the same fall-through).
func (p *Plugin) handleSessionCommand(s *Store, c context.Context, name, args string) (core.CommandResult, error) {
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
	return core.CommandResult{}, fmt.Errorf("no handler for %s", name)
}

// NewPlugin returns a store Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }