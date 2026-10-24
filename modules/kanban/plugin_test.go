package kanban

// Plugin-level multi-board tests: Mount resolves the pointer, /board
// switches the session's active board, the task tools follow it, and
// a board arg in the tool call overrides everything (t_d7a7beaa).

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// mountAt mounts a fresh plugin on an existing home (OMEGA_HOME must
// point there) - the same wiring the hosts do.
func mountAt(t *testing.T, home string) *Plugin {
	t.Helper()
	t.Setenv("OMEGA_HOME", home)
	p := NewPlugin()
	ctx := &core.Context{Configs: map[string]any{"kanban": Config{DBPath: "kanban.db"}}}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("mount: %v", err)
	}
	t.Cleanup(func() { p.store.Close() })
	return p
}

// mountPlugin makes a fresh home and mounts on it.
func mountPlugin(t *testing.T) (*Plugin, *core.Context, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	ctx := &core.Context{Configs: map[string]any{"kanban": Config{DBPath: "kanban.db"}}}
	p := NewPlugin()
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("mount: %v", err)
	}
	t.Cleanup(func() { p.store.Close() })
	return p, ctx, home
}

func TestMountResolvesPointerBoard(t *testing.T) {
	home := t.TempDir()
	p := mountAt(t, home)
	if err := CreateBoard(home, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := WritePointer(home, "alpha"); err != nil {
		t.Fatal(err)
	}

	// second mount on the switched pointer: the default board is alpha.
	// t.Setenv restores between; set it again for the remount.
	t.Setenv("OMEGA_HOME", home)
	p2 := NewPlugin()
	ctx := &core.Context{Configs: map[string]any{"kanban": Config{DBPath: "kanban.db"}}}
	if err := p2.Mount(ctx); err != nil {
		t.Fatalf("remount: %v", err)
	}
	t.Cleanup(func() { p2.store.Close() })
	if p2.store.defaultName != "alpha" {
		t.Fatalf("defaultName = %q, want alpha", p2.store.defaultName)
	}
	out, err := p2.tools.runTaskOp(context.Background(), map[string]any{"action": "list"})
	if err != nil || !strings.Contains(out, "no tasks") {
		t.Fatalf("list on alpha: %q %v", out, err)
	}

	// the tools on the FIRST mount still see main as default (the
	// pointer file was written after that mount)
	if tests, err := p.store.ListTasks(context.Background(), "", ""); err != nil || len(tests) != 0 {
		t.Fatalf("main tasks = %d %v", len(tests), err)
	}
	if tests, err := p.store.ListTasks(context.Background(), "alpha", ""); err != nil || len(tests) != 0 {
		t.Fatalf("alpha tasks = %d %v", len(tests), err)
	}
}

func TestBoardCommandSwitchesSessionBoard(t *testing.T) {
	p, _, home := mountPlugin(t)
	if err := CreateBoard(home, "alpha"); err != nil {
		t.Fatal(err)
	}

	// /boards shows main as pointer target
	res, err := p.handleCommand("/boards", "")
	if err != nil || !strings.Contains(res.Text, "main") {
		t.Fatalf("/boards: %q %v", res.Text, err)
	}

	// /board alpha switches; the tools follow
	if res, err := p.handleCommand("/board", "alpha"); err != nil || res.Text != "board: alpha" {
		t.Fatalf("/board alpha: %q %v", res.Text, err)
	}
	if got := p.sessionBoard(); got != "alpha" {
		t.Fatalf("session board = %q", got)
	}
	out, err := p.tools.runTaskOp(context.Background(), map[string]any{
		"action": "create", "title": "flows through",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "created t_") {
		t.Fatalf("create: %q", out)
	}
	// the card landed on alpha, not the default board
	tasks, err := p.store.ListTasks(context.Background(), "alpha", "")
	if err != nil || len(tasks) != 1 {
		t.Fatalf("alpha tasks = %d %v", len(tasks), err)
	}
	if tasks, err = p.store.ListTasks(context.Background(), "", ""); err != nil || len(tasks) != 0 {
		t.Fatalf("default leak: %d cards %v", len(tasks), err)
	}

	// explicit board arg overrides the session board
	if _, err := p.tools.runTaskOp(context.Background(), map[string]any{
		"action": "create", "title": "explicit", "board": "main",
	}); err != nil {
		t.Fatal(err)
	}
	if tasks, err = p.store.ListTasks(context.Background(), "", ""); err != nil || len(tasks) != 1 {
		t.Fatalf("main override leak: %d %v", len(tasks), err)
	}

	// /board resets to the default
	if _, err := p.handleCommand("/board", DefaultBoardName); err != nil {
		t.Fatal(err)
	}
	if got := p.sessionBoard(); got != DefaultBoardName {
		t.Fatalf("reset board = %q", got)
	}

	// unknown and illegal names refuse
	if _, err := p.handleCommand("/board", "gamma"); err == nil {
		t.Fatal("unknown board accepted")
	}
	if _, err := p.handleCommand("/board", "A B"); err == nil {
		t.Fatal("illegal name accepted")
	}
}

func TestCommandChainDelegates(t *testing.T) {
	p, ctx, _ := mountPlugin(t)
	if ctx == nil {
		t.Fatal("nil context from mount")
	}
	ctx.CommandHandler = func(c context.Context, name, args string) (core.CommandResult, error) {
		return core.CommandResult{}, fmt.Errorf("unreachable: %s", name)
	}
	// rewrite the chain the way a later extension would: kanban's own
	// /board passes through p.handleCommand, anything else falls back
	kanbanOnly := func(c context.Context, name, args string) (core.CommandResult, error) {
		if name == "/board" || name == "/boards" {
			return p.handleCommand(name, args)
		}
		if prev := ctx.CommandHandler; prev != nil {
			return prev(c, name, args)
		}
		return core.CommandResult{}, fmt.Errorf("no handler for %s", name)
	}
	if _, err := kanbanOnly(context.Background(), "/other", ""); err == nil {
		t.Fatal("prev fallback did not run")
	}
	res, err := kanbanOnly(context.Background(), "/board", "")
	if err != nil || !strings.HasPrefix(res.Text, "board:") {
		t.Fatalf("/board on chain: %q %v", res.Text, err)
	}
}
