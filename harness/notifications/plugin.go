package notifications

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/datadir"
)

type Config struct {
	DBPath string
}

// Default returns the zero config (path resolved from the home).
func Default() Config { return Config{} }

type Plugin struct {
	store   *Store
	capsule *capsule
	routes  *Routes
}

var _ agent.Plugin = (*Plugin)(nil)

func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "notifications" }

func (p *Plugin) Description() string {
	return "notifications: the finished-things store behind the dock's Notifications tab chip (tasks, crons, chats)"
}

func (p *Plugin) ConfigKey() string { return "notifications" }

func (p *Plugin) Provides() []string { return []string{"notifications"} }
func (p *Plugin) Requires() []string { return nil }

// MountWithAPI implements agent.APIPlugin. Same resolution the classic body ran;
// without a home AND a config path (exotic embeds) the mount registers nothing and
// returns nil - every emitter is nil-safe and the HTTP endpoints answer 501, so
// notifications are a no-op rather than a startup failure.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["notifications"].(Config); ok {
		cfg = c
	}
	path := cfg.DBPath
	if path == "" {
		home, aerr := datadir.AgentHome()
		if aerr != nil {
			return fmt.Errorf("notifications data home: %w", aerr)
		}
		if home == "" {
			return nil
		}
		path = datadir.DBPath(home, "notifications.db")
	}
	s, err := Open(path)
	if err != nil {
		return fmt.Errorf("notifications store: %w", err)
	}
	if p.store != nil {
		_ = p.store.Close()
	}
	p.store = s
	p.capsule = newCapsule(s)
	seam := &emittingStore{store: s, capsule: p.capsule}
	p.routes = newRoutes(seam)
	if err := api.Offer("notifications", seam); err != nil {
		return err
	}
	if err := api.RegisterPanelProvider(p.capsule); err != nil {
		return err
	}
	// The REST face over the same service handle: the tab chip's list/read + the toast SSE
	// ride the capsule - they register and die with the mount, exactly like the panel
	// surface above them.
	for _, r := range []struct {
		pattern string
		h       func(http.ResponseWriter, *http.Request)
	}{
		{"/notifications", p.routes.handleNotifications},
		{"/notifications/read", p.routes.handleNotificationsRead},
		{"/notifications/events", p.routes.handleNotificationEvents},
	} {
		if err := api.RegisterRoute(r.pattern, r.h); err != nil {
			return err
		}
	}
	return api.RegisterCommand("/notifications", "list recent finished things (tasks, crons, chats)", nil,
		func(c context.Context, name, args string) (core.CommandResult, error) {
			if name != "/notifications" {
				return core.CommandResult{}, core.CommandMiss(name)
			}
			return p.handleCommand(c, args)
		})
}

// handleCommand renders the newest records as a fixed-width table. Read-only on
// purpose: reads happen in the webui, the terminal never interrupts.
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

func (p *Plugin) Toggleable() bool { return true }
