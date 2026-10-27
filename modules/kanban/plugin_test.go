package kanban

// Plugin-level multi-board tests: Mount resolves the pointer, /board
// switches the session's active board, the task tools follow it, and
// a board arg in the tool call overrides everything (t_d7a7beaa).

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// mountAt mounts a fresh plugin on an existing home (OMEGA_HOME must
// point there) - the same wiring the hosts do. Mount dispatches
// through agent.MountAllAPI (the ExtensionAPI path is kanban's only
// mount since the v0.12 phase-C1 migration).
func mountAt(t *testing.T, home string) *Plugin {
	t.Helper()
	t.Setenv("OMEGA_HOME", home)
	p := NewPlugin()
	ctx := &core.Context{Configs: map[string]any{"kanban": Config{DBPath: "kanban.db"}}}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
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
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	t.Cleanup(func() { p.store.Close() })
	return p, ctx, home
}

// TestMountWithAPIShape pins the migrated mount's registrations: the
// REST routes land on ctx.ModuleRoutes, the one namespaced tool
// registers, the commands take their table order with a chain that
// falls through to the miss error, and the live panel provider lands
// with module-qualified, namespaced action ids.
func TestMountWithAPIShape(t *testing.T) {
	_, ctx, _ := mountPlugin(t)
	patterns := map[string]bool{}
	for _, rt := range ctx.ModuleRoutes {
		if rt.Module != "kanban" {
			t.Fatalf("route %q foreign-tenanted: %q", rt.Pattern, rt.Module)
		}
		patterns[rt.Pattern] = true
	}
	for _, want := range []string{"GET /tasks", "POST /tasks", "GET /tasks/{id}", "PATCH /tasks/{id}", "POST /tasks/{id}/comments", "GET /boards"} {
		if !patterns[want] {
			t.Fatalf("route %q not registered: %v", want, patterns)
		}
	}
	// the tool registry: exactly the folded namespaced tool
	names := map[string]int{}
	for _, tp := range ctx.ToolProviders {
		for name := range tp.Tools() {
			names[name]++
		}
	}
	if len(names) != 1 || names["kanban.task"] != 1 {
		t.Fatalf("tool registry = %v, want {kanban.task: 1}", names)
	}
	// the command table and the chain
	var cmds []string
	for _, c := range ctx.Commands {
		cmds = append(cmds, c.Name)
	}
	if fmt.Sprint(cmds) != "[/board /boards]" {
		t.Fatalf("commands = %v", cmds)
	}
	res, err := ctx.CommandHandler(context.Background(), "/board", "")
	if err != nil || !strings.HasPrefix(res.Text, "board:") {
		t.Fatalf("/board via chain: %q %v", res.Text, err)
	}
	if _, err := ctx.CommandHandler(context.Background(), "/boards", ""); err != nil {
		t.Fatalf("/boards via chain: %v", err)
	}
	if _, err := ctx.CommandHandler(context.Background(), "/notes", ""); err == nil ||
		!strings.Contains(fmt.Sprint(err), "no handler for /notes") {
		t.Fatalf("foreign command must fall through to the miss error, got %v", err)
	}
	// the panels: one live provider, two module-qualified panels with
	// namespaced action ids
	if len(ctx.PanelProviders) != 1 {
		t.Fatalf("panel providers = %d", len(ctx.PanelProviders))
	}
	panels := ctx.PanelProviders[0].Panels()
	if len(panels) != 2 {
		t.Fatalf("panels = %d, want the task list and the switcher", len(panels))
	}
	for _, panel := range panels {
		if panel.ID != listPanelID && panel.ID != boardsPanelID {
			t.Fatalf("panel id %q: not the capsule's", panel.ID)
		}
		for _, a := range panel.Actions {
			if a.Module != "kanban" {
				t.Fatalf("panel %q action %q module = %q", panel.ID, a.ID, a.Module)
			}
			if _, _, perr := core.ParseSeamName(a.ID); perr != nil {
				t.Fatalf("panel %q action id %q is not a namespaced pair: %v", panel.ID, a.ID, perr)
			}
		}
	}
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
	if _, err := agent.MountAllAPI([]agent.Plugin{p2}, ctx, nil, nil); err != nil {
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
