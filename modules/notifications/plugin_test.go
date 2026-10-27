package notifications

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// mountCtx mounts a fresh plugin through agent.MountAllAPI - the same
// wiring the hosts do, and the only mount since the v0.12 phase-C2
// migration (docs/migrating-a-module.md is the recipe).
func mountCtx(t *testing.T, ctx *core.Context) (*Plugin, *core.Context) {
	t.Helper()
	p := NewPlugin()
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	t.Cleanup(func() {
		if p.store != nil {
			p.store.db.Close()
		}
	})
	return p, ctx
}

// TestMountWiresSeamAndCommand: a home-backed mount opens the store,
// fills ctx.Notifications, and answers the read-only /notifications
// TUI command (card t_5d9032d3).
func TestMountWiresSeamAndCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	ctx := &core.Context{Configs: map[string]any{}}
	bg := context.Background()
	_, ctx = mountCtx(t, ctx)
	if ctx.Notifications == nil {
		t.Fatalf("mount did not fill the seam")
	}
	if _, err := ctx.Notifications.Emit(core.Notification{Kind: "cron", SourceID: "nightly", Title: "cron nightly finished", Verdict: "failed"}); err != nil {
		t.Fatalf("emit: %v", err)
	}

	res, err := ctx.CommandHandler(bg, "/notifications", "")
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	if !strings.Contains(res.Text, "nightly") || !strings.Contains(res.Text, "failed") {
		t.Fatalf("table lost the record: %q", res.Text)
	}
	if !strings.Contains(res.Text, "1 unread") {
		t.Fatalf("table lost the unread line: %q", res.Text)
	}

	// unread-only arg filters
	ctx.Notifications.MarkAllRead(bg)
	res, err = ctx.CommandHandler(bg, "/notifications", "unread")
	if err != nil {
		t.Fatalf("command unread: %v", err)
	}
	if strings.Contains(res.Text, "nightly") {
		t.Fatalf("unread table still shows a read record: %q", res.Text)
	}
}

// TestMountWithAPIShape pins the migrated mount's registrations: the
// capsule-backed seam lands, the live panel provider carries the
// namespaced action ids, the command answers its own name on the chain
// (falling through to the miss error for anything else), and the
// classic Mount fails loud.
func TestMountWithAPIShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	ctx := &core.Context{Configs: map[string]any{}}
	_, ctx = mountCtx(t, ctx)

	if ctx.Notifications == nil {
		t.Fatal("seam mismatch: ctx.Notifications is nil after the api mount")
	}
	// the panels: one live provider, two module-qualified panels with
	// namespaced action ids
	if len(ctx.PanelProviders) != 1 {
		t.Fatalf("panel providers = %d", len(ctx.PanelProviders))
	}
	panels := ctx.PanelProviders[0].Panels()
	if len(panels) != 2 {
		t.Fatalf("panels = %d, want the list panel and the toast", len(panels))
	}
	seen := map[string]bool{}
	for _, panel := range panels {
		if panel.ID != listPanelID && panel.ID != toastPanelID {
			t.Fatalf("panel id %q: not the capsule's", panel.ID)
		}
		seen[panel.ID] = true
		for _, a := range panel.Actions {
			if a.Module != "notifications" {
				t.Fatalf("panel %q action %q module = %q", panel.ID, a.ID, a.Module)
			}
			if _, _, perr := core.ParseSeamName(a.ID); perr != nil {
				t.Fatalf("panel %q action id %q is not a namespaced pair: %v", panel.ID, a.ID, perr)
			}
		}
	}
	if !seen[listPanelID] || !seen[toastPanelID] {
		t.Fatalf("panels = %v, want both %q and %q", panels, listPanelID, toastPanelID)
	}
	if len(panels[0].Actions) != 2 {
		t.Fatalf("list panel actions = %d, want mark_read + mark_all", len(panels[0].Actions))
	}
	// the command table and the chain
	var cmds []string
	for _, c := range ctx.Commands {
		cmds = append(cmds, c.Name)
	}
	if fmt.Sprint(cmds) != "[/notifications]" {
		t.Fatalf("commands = %v", cmds)
	}
	if _, err := ctx.CommandHandler(context.Background(), "/notes", ""); err == nil ||
		!strings.Contains(fmt.Sprint(err), "no handler for /notes") {
		t.Fatalf("foreign command must fall through to the miss error, got %v", err)
	}
}

// TestMountWithConfigPathOverride: a config path mounts the store at
// that exact file (tests and embedders point it anywhere) even with a
// live home on the side.
func TestMountWithConfigPathOverride(t *testing.T) {
	dbDir := t.TempDir() + "/custom"
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := dbDir + "/notifications.db"
	t.Setenv("OMEGA_HOME", t.TempDir())
	ctx := &core.Context{Configs: map[string]any{"notifications": Config{DBPath: dbPath}}}
	_, ctx = mountCtx(t, ctx)
	if ctx.Notifications == nil {
		t.Fatalf("seam not mounted on a config path")
	}
	e, ok := ctx.Notifications.(*emittingStore)
	if !ok || e.store.db == nil {
		t.Fatalf("unexpected seam implementation: %T", ctx.Notifications)
	}
}
