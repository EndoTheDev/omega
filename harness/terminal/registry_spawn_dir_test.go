package terminal

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// TestSpawnDirOverride pins the shell.run working-directory
// override (card t_ccf74d88): an explicit dir (absolute or
// workspace-relative) roots the spawned command. The probe lingers
// ~2s so the cwd assertion rides the log without racing the child's
// exit (post-t_304afb4e the finished log stays put, but the read
// while ALIVE is still the no-race shape).
func TestSpawnDirOverride(t *testing.T) {
	ws := t.TempDir()
	sub := filepath.Join(ws, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r := testRegistry(t)

	// The probe prints its cwd and lingers ~2s (sleep on unix, ping
	// on windows - cmd carries no sleep and rotates nothing while it
	// waits).
	probe := "pwd; sleep 2"
	if runtime.GOOS == "windows" {
		probe = "cd & ping -n 3 127.0.0.1 >NUL"
	}

	ctx := core.WithRunWorkspace(context.Background(), ws, "")
	// Absolute override wins over the run's workspace.
	if _, err := r.Spawn(ctx, probe, "abs-dir", sub); err != nil {
		t.Fatalf("spawn abs dir: %v", err)
	}
	waitFor(t, 10*time.Second, "the abs-dir log to name its dir", func() bool {
		log, _, _, err := r.Read("abs-dir", 100)
		return err == nil && strings.Contains(log, sub)
	})

	// Relative dir rides the session workspace root.
	if _, err := r.Spawn(ctx, probe, "rel-dir", "sub"); err != nil {
		t.Fatalf("spawn rel dir: %v", err)
	}
	waitFor(t, 10*time.Second, "the rel-dir log to name its dir", func() bool {
		log, _, _, err := r.Read("rel-dir", 100)
		return err == nil && strings.Contains(log, sub)
	})

	// No dir: the run's workspace stands (t_9de5b009).
	if _, err := r.Spawn(ctx, probe, "no-dir", ""); err != nil {
		t.Fatalf("spawn workspace: %v", err)
	}
	waitFor(t, 10*time.Second, "the no-dir log to name the workspace", func() bool {
		log, _, _, err := r.Read("no-dir", 100)
		return err == nil && strings.Contains(log, filepath.Clean(ws))
	})

	// The impossible dir is a spawn-side refusal (the shell.run door
	// resolves the dir through the same override helper).
	p := &ShellProvider{reg: r}
	if _, err := p.registry().Spawn(ctx, probe, "dead-dir", "/omega-definitely-not-here"); err == nil {
		t.Fatal("missing dir: want the spawn to be refused")
	}
	if _, err := p.RunShell(ctx, map[string]any{
		"command": probe, "dir": "/omega-definitely-not-here",
	}); err == nil || !strings.Contains(err.Error(), "omega-definitely-not-here") {
		t.Fatalf("missing dir: err = %v, want a dir-named error", err)
	}
}

// TestDirOverrideValidation pins the helper's own edges.
func TestDirOverrideValidation(t *testing.T) {
	ws := t.TempDir()
	ctx := core.WithRunWorkspace(context.Background(), ws, "")

	if got, err := dirOverride(ctx, map[string]any{}); err != nil || got != "" {
		t.Fatalf("no arg: got %q err %v, want empty", got, err)
	}
	if got, err := dirOverride(ctx, map[string]any{"dir": "  "}); err != nil || got != "" {
		t.Fatalf("blank arg: got %q err %v, want empty", got, err)
	}
	abs, err := dirOverride(ctx, map[string]any{"dir": ws})
	if err != nil || filepath.Clean(abs) != filepath.Clean(ws) {
		t.Fatalf("absolute pass-through: got %q err %v", abs, err)
	}
	rel, err := dirOverride(ctx, map[string]any{"dir": "does-not-exist"})
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("missing dir: err = %v, want a dir-named error", err)
	}
	_ = rel

	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := dirOverride(ctx, map[string]any{"dir": file}); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file dir: err = %v, want not-a-directory", err)
	}
}

