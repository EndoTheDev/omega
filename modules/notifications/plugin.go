package notifications

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
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
// emit through ctx.Notifications, the HTTP channel serves the bell,
// and the capsule's canvas surface carries the list panel and the
// toast event (arc 4b).
type Plugin struct {
	store   *Store
	capsule *capsule
}

var _ agent.Plugin = (*Plugin)(nil)

// NewPlugin returns a notifications Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "notifications" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "notifications: the finished-things store behind the bell (tasks, crons, chats)"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "notifications" }

func (p *Plugin) Provides() []string { return []string{"notifications"} }
func (p *Plugin) Requires() []string { return nil }

// Mount satisfies agent.Plugin (the interface keeps its classic shape
// until phase F). It is never dispatched: MountWithAPI exists, and
// APIPlugin wins (the strangler rule). If ever reached it fails loud -
// notifications does not mount the old way, which is the proof of
// migration.
func (p *Plugin) Mount(ctx *core.Context) error {
	return fmt.Errorf("notifications: classic mount retired - the capsule mounts through the ExtensionAPI (v0.12 phase C2, docs/migrating-a-module.md)")
}

// MountWithAPI implements agent.APIPlugin. Same resolution the classic
// body ran; without a home AND a config path (exotic embeds) the mount
// registers nothing and returns nil - every emitter is nil-safe and
// the HTTP endpoints answer 501, so notifications are a no-op rather
// than a startup failure. Every face goes through the api: the seam
// (the capsule-backed emittingStore), the canvas panels, and the
// read-only /notifications command.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
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
	// Capsule law (arc 4b, t_9339ad05): the module ships its view-model
	// with its store. The seam the emitters see is the capsule-backed
	// wrapper - every emit records through the store AND pushes the
	// canvas panels (toast event + list refresh), so the toast is a
	// canvas event and the emitter wiring lives in this module. The
	// provider rides the same registration: an unregistered plugin
	// ships no store, no panels, no toasts, no wiring - removal kills
	// all of it together.
	p.capsule = newCapsule(s)
	if err := api.RegisterNotifications(&emittingStore{store: s, capsule: p.capsule}); err != nil {
		return err
	}
	if err := api.RegisterPanelProvider(p.capsule); err != nil {
		return err
	}
	// The read-only /notifications table command (the /board pattern:
	// one registration per command, the wrapper falls through on a
	// miss).
	return api.RegisterCommand("/notifications", "list recent finished things (tasks, crons, chats)", nil,
		func(c context.Context, name, args string) (core.CommandResult, error) {
			if name != "/notifications" {
				return core.CommandResult{}, fmt.Errorf("no handler for %s", name)
			}
			return p.handleCommand(c, args)
		})
}

// handleCommand renders the newest records as a fixed-width table.
// Read-only on purpose: reads happen in the webui, the terminal never
// interrupts (no TUI toasts).
func (p *Plugin) handleCommand(c context.Context, args string) (core.CommandResult, error) {
	unreadOnly := strings.TrimSpace(args) == "unread"
	notes, unread, err := p.store.List(c, unreadOnly, 20, 0)
	if err != nil {
		return core.CommandResult{}, err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "notifications: %d unread (latest %d)\n\n", unread, len(notes))
	if len(notes) == 0 {
		sb.WriteString("nothing finished yet\n")
		return core.CommandResult{Text: sb.String()}, nil
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
	return core.CommandResult{Text: sb.String()}, nil
}

// Toggleable marks this capsule as a module capsule (v0.12 E1, the
// tier law): the omega.json disable list may name it.
func (p *Plugin) Toggleable() bool { return true }
