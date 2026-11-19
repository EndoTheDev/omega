package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// Plugin implements agent.Plugin for the store seam.
type Plugin struct {
	store *Store // nil until MountWithAPI; Dismount releases it
}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string { return "store" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "session persistence: SQLite-backed message and session store"
}

// ConfigKey names the config section this plugin reads from ctx.Configs.
func (p *Plugin) ConfigKey() string { return "store" }

func (p *Plugin) Provides() []string { return []string{"store", "tools"} }
func (p *Plugin) Requires() []string { return nil }

// Toggleable opts the store into the toggleable tier.
func (p *Plugin) Toggleable() bool { return true }

// Dismount releases the mounted store (the open sqlite handle): beyond-Context state
// the journal cannot see. Nil-safe: never mounted (or already dismounted) closes
// nothing.
func (p *Plugin) Dismount(ctx *core.Context) error {
	if p.store == nil {
		return nil
	}
	err := p.store.Close()
	p.store = nil
	return err
}

// MountWithAPI implements agent.APIPlugin. The store connection is host-owned at
// dismount - the journal unmounts the registrations, never the data (disable never
// deletes data).
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["store"].(Config); ok {
		cfg = c
	}
	dsn := cfg.DBPath
	if dsn == "" || dsn == "omega.db" {
		// <home>/data/omega.db - the host (cmd/omega resolveHomePaths) passes an absolute.
		home, aerr := datadir.AgentHome()
		if aerr != nil {
			return fmt.Errorf("store data home: %w", aerr)
		}
		if home != "" {
			dsn = datadir.DBPath(home, "omega.db")
		} else {
			dsn = "omega.db"
		}
	}
	s, err := Open(dsn)
	if err != nil {
		return err
	}
	s.SetHome(cfg.Home)
	if p.store != nil {
		_ = p.store.Close()
	}
	p.store = s
	search := &searchToolProvider{store: s}
	hyg := DefaultHygiene()
	if c, ok := ctx.Configs["sessions"].(HygieneConfig); ok {
		hyg = c
	}
	s.SetHygieneConfig(hyg)
	hygiene := &hygieneToolProvider{store: s, providerFn: func() agent.Provider { return seam[agent.Provider](ctx, "provider") }}
	routes := newRoutes(ctx, s)
	steps := []func() error{
		func() error { return api.Offer("store", s) },
		func() error {
			for name, t := range search.Tools() {
				if err := api.RegisterBuiltinTool(name, t); err != nil {
					return err
				}
			}
			for name, t := range hygiene.Tools() {
				if err := api.RegisterBuiltinTool(name, t); err != nil {
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
		// The management REST face registers last so a half-mount failure below never.
		func() error {
			for _, row := range routes.registrationRows() {
				if err := api.RegisterRoute(row.pattern, row.handler); err != nil {
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

// sessionCommands returns the eleven session commands in registration order.
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

// handleSessionCommand dispatches one session command.
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
		// /branch [parent-id]: the given id wins.
		parentID, rest := sessionTarget(c, args)
		return HandleBranchCommand(c, s, parentID, rest)
	case "/label":
		sessionID, label := sessionPayload(c, args)
		return HandleLabelCommand(c, s, sessionID, label)
	case "/export":
		sessionID, path := sessionPayload(c, args)
		return HandleExportCommand(c, s, sessionID, path)
	}
	return core.CommandResult{}, core.CommandMiss(name)
}

// sessionTarget resolves the session a command acts.
func sessionTarget(c context.Context, args string) (sessionID, rest string) {
	head, tail, _ := strings.Cut(strings.TrimSpace(args), " ")
	if head != "" {
		return head, strings.TrimSpace(tail)
	}
	return core.CommandSession(c), ""
}

// sessionPayload is sessionTarget's sibling for commands whose args are.
// A caller that binds no session (the terminal loop) keeps the legacy shape, where the
// first word names the session: "/label <session-id> <text>", "/export <session-id>
// [path]".
func sessionPayload(c context.Context, args string) (sessionID, payload string) {
	if id := core.CommandSession(c); id != "" {
		return id, strings.TrimSpace(args)
	}
	id, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
	return id, strings.TrimSpace(rest)
}

// NewPlugin returns a store Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }
