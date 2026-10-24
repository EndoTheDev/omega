package notifications

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// TestMountWiresSeamAndCommand: a home-backed mount opens the store,
// fills ctx.Notifications, and answers the read-only /notifications
// TUI command (card t_5d9032d3).
func TestMountWiresSeamAndCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	ctx := &core.Context{Configs: map[string]any{}}
	bg := context.Background()
	p := NewPlugin()
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("mount: %v", err)
	}
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
	p := NewPlugin()
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if ctx.Notifications == nil {
		t.Fatalf("seam not mounted on a config path")
	}
	e, ok := ctx.Notifications.(*emittingStore)
	if !ok || e.store.db == nil {
		t.Fatalf("unexpected seam implementation: %T", ctx.Notifications)
	}
}
