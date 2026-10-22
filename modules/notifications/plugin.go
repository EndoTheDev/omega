package notifications

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// Config is the optional knob for the store path. Empty resolves to
// <home>/data/notifications.db; tests and embedders point it anywhere.
type Config struct {
	DBPath string
}

// Default returns the zero config (path resolved from the home).
func Default() Config { return Config{} }

// Plugin adapts the notifications Store into omega's plugin system. It
// owns the exclusive "notifications" seam; kanban, cron and delegate
// emit through ctx.Notifications, the HTTP channel serves the bell.
type Plugin struct {
	store *Store
}

var _ agent.Plugin = (*Plugin)(nil)

// NewPlugin returns a notifications Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "notifications" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "notifications: the finished-things store behind the bell (tasks, crons, chats)"
}
func (p *Plugin) Provides() []string { return []string{"notifications"} }
func (p *Plugin) Requires() []string { return nil }

// Mount opens the store and mounts the seam, then registers the
// read-only /notifications TUI command. Without a home AND a config
// path (exotic embeds) the plugin mounts nothing - every emitter is
// nil-safe and the HTTP endpoints answer 501, so notifications are a
// no-op rather than a startup failure.
func (p *Plugin) Mount(ctx *agent.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["notifications"].(Config); ok {
		cfg = c
	}
	path := cfg.DBPath
	if path == "" {
		home := datadir.Home()
		if home == "" {
			return nil
		}
		path = datadir.DBPath(home, "notifications.db")
	}
	s, err := Open(path)
	if err != nil {
		return fmt.Errorf("notifications store: %w", err)
	}
	p.store = s
	ctx.Notifications = s
	p.wireCommand(ctx)
	return nil
}

// wireCommand registers the read-only /notifications table command
// with the TUI command chain (the /board pattern: wrap the previous
// handler, fall through on a miss).
func (p *Plugin) wireCommand(ctx *agent.Context) {
	ctx.Commands = append(ctx.Commands,
		agent.ExtensionCommand{Name: "/notifications", Description: "list recent finished things (tasks, crons, chats)"},
	)
	prev := ctx.CommandHandler
	ctx.CommandHandler = func(c context.Context, name, args string) (agent.CommandResult, error) {
		if name == "/notifications" {
			return p.handleCommand(c, args)
		}
		if prev != nil {
			return prev(c, name, args)
		}
		return agent.CommandResult{}, fmt.Errorf("no handler for %s", name)
	}
}

// handleCommand renders the newest records as a fixed-width table.
// Read-only on purpose: reads happen in the webui, the terminal never
// interrupts (no TUI toasts).
func (p *Plugin) handleCommand(c context.Context, args string) (agent.CommandResult, error) {
	unreadOnly := strings.TrimSpace(args) == "unread"
	notes, unread, err := p.store.List(c, unreadOnly, 20, 0)
	if err != nil {
		return agent.CommandResult{}, err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "notifications: %d unread (latest %d)\n\n", unread, len(notes))
	if len(notes) == 0 {
		sb.WriteString("nothing finished yet\n")
		return agent.CommandResult{Text: sb.String()}, nil
	}
	for _, n := range notes {
		marker := " "
		if !n.Read {
			marker = "*"
		}
		fmt.Fprintf(&sb, "%s #%d  %-5s %-8s %-20s %s\n",
			marker, n.ID, n.Kind, "["+n.Verdict+"]", n.CreatedAt, n.Title)
	}
	sb.WriteString("\n* unread\n")
	return agent.CommandResult{Text: sb.String()}, nil
}