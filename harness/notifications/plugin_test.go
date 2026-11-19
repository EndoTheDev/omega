package notifications

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// mountCtx mounts a fresh plugin through agent.MountAllAPI.
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

// seamBoard is the test's own consumer face (part 3).
type seamBoard interface {
	Emit(n core.Notice) (core.Notice, error)
	List(ctx context.Context, unreadOnly bool, limit, offset int) ([]core.Notice, int, error)
	MarkRead(ctx context.Context, id int64) error
	MarkAllRead(ctx context.Context) error
}

func offeredSeam(t *testing.T, ctx *core.Context) seamBoard {
	t.Helper()
	svc, _ := ctx.Services().Resolve("notifications").(seamBoard)
	if svc == nil {
		t.Fatal("seam mismatch: the notifications offer is nil after the api mount")
	}
	return svc
}

// TestMountWiresSeamAndCommand: a home-backed mount opens the store, offers.
func TestMountWiresSeamAndCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	ctx := &core.Context{Configs: map[string]any{}}
	bg := context.Background()
	_, ctx = mountCtx(t, ctx)
	seam := offeredSeam(t, ctx)
	if _, err := seam.Emit(core.Notice{Kind: "cron", SourceID: "nightly", Title: "cron nightly finished", Verdict: "failed"}); err != nil {
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

	seam.MarkAllRead(bg)
	res, err = ctx.CommandHandler(bg, "/notifications", "unread")
	if err != nil {
		t.Fatalf("command unread: %v", err)
	}
	if strings.Contains(res.Text, "nightly") {
		t.Fatalf("unread table still shows a read record: %q", res.Text)
	}
}

// TestMountWithAPIShape pins the migrated mount's registrations.
func TestMountWithAPIShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	ctx := &core.Context{Configs: map[string]any{}}
	_, ctx = mountCtx(t, ctx)

	offeredSeam(t, ctx) // the loud mismatch naming the face
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

// TestMountWithConfigPathOverride: a config path mounts the store at that exact file.
func TestMountWithConfigPathOverride(t *testing.T) {
	dbDir := t.TempDir() + "/custom"
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := dbDir + "/notifications.db"
	t.Setenv("OMEGA_HOME", t.TempDir())
	ctx := &core.Context{Configs: map[string]any{"notifications": Config{DBPath: dbPath}}}
	_, ctx = mountCtx(t, ctx)
	e, ok := ctx.Services().Resolve("notifications").(*emittingStore)
	if !ok || e.store.db == nil {
		t.Fatalf("unexpected seam implementation")
	}
}
