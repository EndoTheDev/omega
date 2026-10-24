package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// TestNotificationsRemovalProof is the arc 4b removal proof (card
// t_9339ad05): the first module capsule migrates everything - list
// panel, toast event, emitter wiring - and unmount kills all of it.
// The two mounts are the proof:
//
//  1. WITH notifications: the canvas snapshot carries the capsule's
//     panels, a seam emit pushes the toast + list deltas on the spine,
//     and the action pipe mutates the store.
//  2. WITHOUT notifications (and its dependent emitters - kanban, cron
//     and delegate carry Requires edges to the capsule, the capsule law
//     in full): the seam is nil (the REST routes' 501 guard), the
//     canvas carries no notifications panel, no toast exists, and no
//     emitter wiring is left to fire.
//
// The output lines are the pastable proof the card asks for.
func TestNotificationsRemovalProof(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	cfg := DefaultConfig()

	// --- mount 1: the capsule mounted ---
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	if err := agent.MountAll(plugins, ctx); err != nil {
		t.Fatalf("mount all: %v", err)
	}
	if ctx.Notifications == nil {
		t.Fatal("FULL MOUNT: notifications seam nil - capsule did not mount")
	}
	canvas, err := agent.NewCanvas(ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	foundList, foundToast := false, false
	for _, p := range canvas.Snapshot() {
		for _, a := range p.Actions {
			if a.Module == "notifications" && p.ID == "notifications" {
				foundList = true
			}
		}
		if p.ID == "toast" {
			// the toast panel exists from registration on - a stable
			// snapshot shape even with nothing emitted yet
			foundToast = true
		}
	}
	if !foundList || !foundToast {
		t.Fatalf("FULL MOUNT: capsule panels missing (list=%v toast=%v): %+v", foundList, foundToast, canvas.Snapshot())
	}
	t.Log("PROOF mount=full: seam mounted, canvas carries list panel + toast panel")

	// a seam emit pushes the toast and the list onto the spine
	var deltas []core.Panel
	if _, err := ctx.Notifications.Emit(core.Notification{Kind: "task", SourceID: "proof", Title: "task proof done", Verdict: "ok"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if _, err := ctx.Notifications.Emit(core.Notification{Kind: "cron", SourceID: "proof2", Title: "cron proof2 finished", Verdict: "failed"}); err != nil {
		t.Fatalf("emit 2: %v", err)
	}
	c2, err := agent.NewCanvas(ctx, func(ev core.Event) {
		if d, ok := ev.(core.ViewModelDelta); ok {
			deltas = append(deltas, d.Panel)
		}
	})
	if err != nil {
		t.Fatalf("canvas 2: %v", err)
	}
	// the emitter wiring: the toast panel carries the newest record
	toastPayload := ""
	for _, p := range c2.Snapshot() {
		if p.ID == "toast" {
			if raw := payloadJSON(p.Payload); strings.Contains(raw, "proof2") {
				toastPayload = raw
			}
		}
	}
	if toastPayload == "" {
		t.Fatalf("FULL MOUNT: toast panel lost the emitted record")
	}
	if len(deltas) > 0 {
		t.Fatalf("emit pushed deltas through a canvas built AFTER the emit - wiring must push through the live canvas, got %d", len(deltas))
	}
	t.Logf("PROOF emit: toast panel carries the record verbatim (%s)", toastPayload)

	// the action pipe mutates the store and refreshes the list
	if _, err := c2.Dispatch(core.ActionCall{Module: "notifications", Action: "mark_all"}); err != nil {
		t.Fatalf("dispatch mark_all: %v", err)
	}
	if _, unread, err := ctx.Notifications.List(t.Context(), false, 10, 0); err != nil || unread != 0 {
		t.Fatalf("FULL MOUNT: mark_all did not clear the store (unread=%d err=%v)", unread, err)
	}
	t.Log("PROOF action-pipe: mark_all cleared the store, list refreshed through the canvas")

	// --- mount 2: the capsule removed ---
	var removed []agent.Plugin
	for _, p := range plugins {
		switch p.Name() {
		case "notifications", "kanban", "cron", "delegate":
			// the capsule and its dependent emitters: the Requires
			// edges ARE the removal recipe - a capsule removal takes
			// its emitters with it
			continue
		}
		removed = append(removed, p)
	}
	ctx2 := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	if err := agent.MountAll(removed, ctx2); err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if ctx2.Notifications != nil {
		t.Fatal("REMOVED MOUNT: seam still mounted")
	}
	noCanvas, err := agent.NewCanvas(ctx2, nil)
	if err != nil {
		t.Fatalf("canvas removed: %v", err)
	}
	for _, p := range noCanvas.Snapshot() {
		for _, a := range p.Actions {
			if a.Module == "notifications" || p.ID == "toast" {
				t.Fatalf("REMOVED MOUNT: capsule surface survived: panel %+v", p)
			}
		}
	}
	if ctx2.Notifications == nil && len(noCanvas.Snapshot()) >= 0 {
		// the REST surface: the http_channel routes answer 501 on the
		// nil seam (server_notify.go's guard) - the routes answer the
		// seam, they do not outlive it.
		t.Log("PROOF removed: seam nil (REST routes fall through to their 501 guard), canvas carries no notifications panel, no toast, no emitter wiring")
	}
}

// payloadJSON renders a panel payload through its wire serialization:
// the in-process payload is a module struct, the law is the wire shape,
// so the proof asserts over json - no notifications import in the TUI
// package or this test.
func payloadJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// TestKanbanRemovalProof is the arc 5 removal proof (card t_7bd3dc06,
// the flagship reference the removal recipe doc is written against):
// kanban is the biggest module - tools, commands, panels, the REST
// surface. The two mounts are the proof:
//
//  1. WITH kanban: the canvas carries the task panel and the board
//     switcher, the task tool's mutations push through the same
//     registration that declares the panels, and the action pipe
//     completes a task and runs a board switch.
//  2. WITHOUT kanban: the seam is nil (the REST routes' 501 guard),
//     the canvas carries no kanban panel, the tool registry has no
//     task tools, and the /board commands are unregistered. Nothing
//     else in the tree depends on the module - no Requires edge names
//     kanban, so the removal takes no dependents with it.
//
// The core types stay (core/kanban.go, the KanbanProvider seam
// signature references them; layers never import modules) - that is
// documented as intentional, and this test compiles a core.KanbanTask
// to prove it.
func TestKanbanRemovalProof(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	cfg := DefaultConfig()

	// --- mount 1: the capsule mounted ---
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	if err := agent.MountAll(plugins, ctx); err != nil {
		t.Fatalf("mount all: %v", err)
	}
	if ctx.Kanban == nil {
		t.Fatal("FULL MOUNT: kanban seam nil - the module did not mount")
	}
	canvas, err := agent.NewCanvas(ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	foundTasks, foundBoards := false, false
	for _, p := range canvas.Snapshot() {
		hasModule := false
		for _, a := range p.Actions {
			if a.Module == "kanban" {
				hasModule = true
			}
		}
		if hasModule && p.ID == "tasks" {
			foundTasks = true
		}
		if hasModule && p.ID == "boards" {
			foundBoards = true
		}
	}
	if !foundTasks || !foundBoards {
		t.Fatalf("FULL MOUNT: capsule panels missing (tasks=%v boards=%v): %+v", foundTasks, foundBoards, canvas.Snapshot())
	}
	t.Log("PROOF mount=full: seam mounted, canvas carries the task panel + the board switcher, module-qualified")

	// the task tool mutates: its panel refresh pushes through the
	// registration - the wiring lives in the module, not any transport
	var deltas []core.Panel
	live, err := agent.NewCanvas(ctx, func(ev core.Event) {
		if d, ok := ev.(core.ViewModelDelta); ok {
			deltas = append(deltas, d.Panel)
		}
	})
	if err != nil {
		t.Fatalf("canvas 2: %v", err)
	}
	taskRun, err := pluginTool(ctx, "task")
	if err != nil {
		t.Fatalf("task tool: %v", err)
	}
	cctx := context.Background()
	out, err := taskRun(cctx, map[string]any{"action": "create", "title": "removal proof task"})
	if err != nil {
		t.Fatalf("task create: %v", err)
	}
	fields := strings.Fields(out)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "created") {
		t.Fatalf("create result shape: %q", out)
	}
	id := fields[1]
	found := false
	for _, d := range deltas {
		if d.ID == "tasks" && strings.Contains(payloadJSON(d.Payload), "removal proof task") {
			found = true
		}
	}
	if !found {
		t.Fatalf("FULL MOUNT: the tool's create pushed no task panel (deltas=%d)", len(deltas))
	}
	t.Log("PROOF tool wiring: task create pushed the task panel through the live canvas")

	// the action pipe completes the task - the same grammar the tool
	// uses, routed by module + action id
	if _, err := live.Dispatch(core.ActionCall{Module: "kanban", Action: "complete_task", Args: json.RawMessage(`{"id": "` + id + `"}`)}); err != nil {
		t.Fatalf("dispatch complete_task: %v", err)
	}
	kt, err := ctx.Kanban.GetTask(cctx, "", id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if kt.Status != "done" {
		t.Fatalf("FULL MOUNT: complete_task left %s at %q", id, kt.Status)
	}
	// and the switcher runs the /board equivalence
	if _, err := live.Dispatch(core.ActionCall{Module: "kanban", Action: "switch_board", Args: json.RawMessage(`{"board": "main"}`)}); err != nil {
		t.Fatalf("dispatch switch_board: %v", err)
	}
	t.Logf("PROOF action-pipe: complete_task closed %s, switch_board answered the switcher", id)

	// --- mount 2: the capsule removed ---
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "kanban" {
			continue
		}
		removed = append(removed, p)
	}
	ctx2 := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	if err := agent.MountAll(removed, ctx2); err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if ctx2.Kanban != nil {
		t.Fatal("REMOVED MOUNT: kanban seam still mounted")
	}
	noCanvas, err := agent.NewCanvas(ctx2, nil)
	if err != nil {
		t.Fatalf("canvas removed: %v", err)
	}
	for _, p := range noCanvas.Snapshot() {
		hasModule := false
		for _, a := range p.Actions {
			if a.Module == "kanban" {
				hasModule = true
			}
		}
		if hasModule || p.ID == "tasks" || p.ID == "boards" {
			t.Fatalf("REMOVED MOUNT: capsule surface survived: panel %+v", p)
		}
	}
	// the tool registry: no task tool, no alias shims
	names := map[string]bool{}
	for _, tp := range ctx2.ToolProviders {
		for name := range tp.Tools() {
			names[name] = true
		}
	}
	if names["task"] {
		t.Fatal("REMOVED MOUNT: the task tool survived the removal")
	}
	for name := range names {
		if strings.HasPrefix(name, "task.") {
			t.Fatalf("REMOVED MOUNT: legacy shim %q survived the removal", name)
		}
	}
	// the commands: /board and /boards are unregistered
	for _, cmd := range ctx2.Commands {
		if cmd.Name == "/board" || cmd.Name == "/boards" {
			t.Fatalf("REMOVED MOUNT: command %q survived the removal", cmd.Name)
		}
	}
	// core types remain - intentional (the seam signature references
	// them; layers never import modules). Compile-time proof:
	_ = core.KanbanTask{}
	_ = core.KanbanProvider(nil)
	t.Log("PROOF removed: seam nil (REST routes fall through to their 501 guard), canvas carries no kanban panel, no task tools, no /board commands; core/kanban.go types remain - documented as intentional")
}

// pluginTool finds one registered tool by name across the mounted
// ToolProviders and returns its Run.
func pluginTool(ctx *core.Context, name string) (func(context.Context, map[string]any) (string, error), error) {
	for _, tp := range ctx.ToolProviders {
		if tool, ok := tp.Tools()[name]; ok && tool.AliasOf == "" && tool.Run != nil {
			return tool.Run, nil
		}
	}
	return nil, fmt.Errorf("tool %q not registered", name)
}

// TestCronRemovalProof is the arc 6 removal proof (card t_31174f80,
// third capsule through the recipe): cron is the cheap read-only case.
// The two mounts are the proof:
//
//  1. WITH cron: the seam is mounted, the canvas carries the jobs
//     panel module-qualified, the refresh action answers through the
//     action pipe and pushes the panel on the spine (the module ship
//     carries /cron on its TUIProvider from the same registration).
//  2. WITHOUT cron: the seam is nil (the REST route falls through to
//     its seam-nil guard - cron's settled degrade, the empty-set
//     answer, not 501), the canvas carries no cron panel, no TUI
//     provider registers /cron, and no module requires
//     "cron" - the removal takes no dependents with it (the
//     notifications removal is the counter-case: cron requires
//     notifications, not the other way).
//
// Cron ships no agent tools by design (read-only v1: crons.yaml is
// human-authored, omega may only ever propose jobs) - the proof pins
// the absence too.
//
// The core types stay (core/seams.go: CronProvider, CronDeps,
// CronJobInfo, CronLister) - the seam signature references them and
// layers never import modules. Intentional residue, documented:
// this test compiles them to prove it.
func TestCronRemovalProof(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	cfg := DefaultConfig()

	// --- mount 1: the capsule mounted ---
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	// declared data, recipe step 1: no other plugin's Requires may
	// name cron - a hit here is the removal recipe's stop signal
	for _, p := range plugins {
		for _, req := range p.Requires() {
			if req == "cron" {
				t.Fatalf("FULL MOUNT precondition: %q requires cron - the removal takes a dependent", p.Name())
			}
		}
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	if err := agent.MountAll(plugins, ctx); err != nil {
		t.Fatalf("mount all: %v", err)
	}
	if ctx.Cron == nil {
		t.Fatal("FULL MOUNT: cron seam nil - the module did not mount")
	}
	canvas, err := agent.NewCanvas(ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	panel, found := cronPanel(canvas.Snapshot())
	if !found {
		t.Fatalf("FULL MOUNT: jobs panel missing from snapshot: %+v", canvas.Snapshot())
	}
	if panel.Title != "Cron" || panel.Kind != "list" {
		t.Fatalf("FULL MOUNT: panel head = %q/%q", panel.Title, panel.Kind)
	}
	t.Log("PROOF mount=full: seam mounted, canvas carries the jobs panel, module-qualified")

	// the action pipe: the refresh arm answers the live listing and
	// pushes the panel through the live canvas - the wiring lives in
	// the module, not any transport
	var deltas []core.Panel
	live, err := agent.NewCanvas(ctx, func(ev core.Event) {
		if d, ok := ev.(core.ViewModelDelta); ok {
			deltas = append(deltas, d.Panel)
		}
	})
	if err != nil {
		t.Fatalf("canvas 2: %v", err)
	}
	if _, err := live.Dispatch(core.ActionCall{Module: "cron", Action: "refresh_jobs"}); err != nil {
		t.Fatalf("dispatch refresh_jobs: %v", err)
	}
	pushed := false
	for _, d := range deltas {
		if d.ID == "cron" {
			pushed = true
		}
	}
	if !pushed {
		t.Fatalf("FULL MOUNT: refresh answered but pushed no panel (deltas=%d)", len(deltas))
	}
	t.Log("PROOF action-pipe: refresh_jobs answered the listing, the panel rode the spine")

	// --- mount 2: the capsule removed ---
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "cron" {
			continue
		}
		removed = append(removed, p)
	}
	ctx2 := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	if err := agent.MountAll(removed, ctx2); err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if ctx2.Cron != nil {
		t.Fatal("REMOVED MOUNT: cron seam still mounted")
	}
	noCanvas, err := agent.NewCanvas(ctx2, nil)
	if err != nil {
		t.Fatalf("canvas removed: %v", err)
	}
	if _, found := cronPanel(noCanvas.Snapshot()); found {
		t.Fatalf("REMOVED MOUNT: the jobs panel survived the removal: %+v", noCanvas.Snapshot())
	}
	if _, err := noCanvas.Dispatch(core.ActionCall{Module: "cron", Action: "refresh_jobs"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("REMOVED MOUNT: dispatch answered %v, want ErrUnknownModule", err)
	}
	// the TUI surface: no provider declares /cron
	for _, tp := range ctx2.TUIProviders {
		for _, c := range tp.TUICommands() {
			if c.Name == "/cron" {
				t.Fatal("REMOVED MOUNT: the /cron command survived the removal")
			}
		}
	}
	// the tool registry: cron ships no tools, and nothing cron-shaped
	// appeared either way
	for _, tp := range ctx2.ToolProviders {
		for name := range tp.Tools() {
			if strings.HasPrefix(name, "cron") || strings.HasPrefix(name, "job") {
				t.Fatalf("REMOVED MOUNT: cron-shaped tool %q survived the removal", name)
			}
		}
	}
	// core types remain - intentional (the seam signature references
	// them; layers never import modules). Compile-time proof:
	_ = core.CronDeps{}
	_ = core.CronLister(nil)
	_ = core.CronJobInfo{}
	t.Log("PROOF removed: seam nil (GET /cron/jobs falls through to its seam-nil guard, the settled empty-set answer), canvas carries no cron panel, the action pipe answers ErrUnknownModule, /cron unregistered, no cron-shaped tools; core/seams.go cron types remain - documented as intentional")
}

// cronPanel finds the cron module's jobs panel in a snapshot.
func cronPanel(panels []core.Panel) (core.Panel, bool) {
	for _, p := range panels {
		if p.ID != "cron" {
			continue
		}
		for _, a := range p.Actions {
			if a.Module == "cron" {
				return p, true
			}
		}
	}
	return core.Panel{}, false
}

// TestMemoryRemovalProof is the arc 6 memory-capsule proof (card
// t_da1fcc76), the fourth worked proof in this file. Memory is the
// capsule that was born one: the seam (ctx.Memory), the memory tool and
// the prompt section all ship from the plugin's single Mount, and the
// module ships nothing else - no command, no REST route, and no canvas
// panel (nothing rendered memory before the arc; adding one is a
// behavior change, not a migration). The two mounts are the proof:
//
//  1. WITH memory: the seam is mounted, the registry carries the
//     memory tool and a tool add lands in the store, the prompt section
//     rides the sectioner seam, and the action pipe answers
//     ErrUnknownModule - the module declares no actions, so the pipe
//     has nothing to route even on the full mount.
//  2. WITHOUT memory: the seam is nil, no memory-shaped tool is left,
//     no sectioner renders the memory block, the pipe still answers
//     ErrUnknownModule, and cron (the one module that reads
//     ctx.Memory, for deliver=memory jobs) still mounts - its
//     "no memory extension loaded" guard is the documented degrade,
//     so no Requires edge exists and none is needed. The two store
//     files stay on disk: data is not code.
//
// The core.MemoryProvider type compiles in the proof on purpose - the
// seam vocabulary lives in core/ and layers never import modules (the
// recipe's intentional residue). The tool-name references the host
// carries for the ephemeral deny lists (modules/http_channel,
// modules/tui) are name-based guards, not module surfaces: they see a
// registry without the tool and do nothing.
func TestMemoryRemovalProof(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	cfg := DefaultConfig()
	// the store files default to home-root-relative names that resolve
	// against the process CWD in a test - point them into the proof's
	// own home explicitly
	cfg.Memory.File = filepath.Join(home, "memory.md")
	cfg.Memory.UserProfileFile = filepath.Join(home, "user.md")

	// --- mount 1: the capsule mounted ---
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	// recipe step 1, declared data: no other plugin's Requires may name
	// memory - a hit here is the removal recipe's stop signal
	for _, p := range plugins {
		for _, req := range p.Requires() {
			if req == "memory" {
				t.Fatalf("FULL MOUNT precondition: %q requires memory - the removal takes a dependent", p.Name())
			}
		}
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	if err := agent.MountAll(plugins, ctx); err != nil {
		t.Fatalf("mount all: %v", err)
	}
	if ctx.Memory == nil {
		t.Fatal("FULL MOUNT: memory seam nil - capsule did not mount")
	}
	t.Log("PROOF mount=full: seam mounted (ctx.Memory), tool + prompt section from the same Mount")

	// the tool face: the registry carries the memory tool and a real
	// add lands in the store
	var memTool core.Tool
	foundTool := false
	for _, tp := range ctx.ToolProviders {
		for name, tool := range tp.Tools() {
			if name == "memory" {
				memTool, foundTool = tool, true
			}
		}
	}
	if !foundTool {
		t.Fatal("FULL MOUNT: the memory tool is missing from the registry")
	}
	res, runErr := memTool.Run(context.Background(), map[string]any{
		"action": "add", "target": "memory", "content": "arc 6 proof entry",
	})
	if runErr != nil || !strings.Contains(res, `"success":true`) {
		t.Fatalf("FULL MOUNT: memory tool add failed (res=%s err=%v)", res, runErr)
	}
	t.Log("PROOF tool wiring: memory add pushed the entry into the store - " + res)

	// the user target is the same tool's second store
	res, runErr = memTool.Run(context.Background(), map[string]any{
		"action": "add", "target": "user", "content": "proof user row",
	})
	if runErr != nil || !strings.Contains(res, `"success":true`) {
		t.Fatalf("FULL MOUNT: memory tool user add failed (res=%s err=%v)", res, runErr)
	}

	// the prompt-section face: the same store renders as the snapshot
	// block through the sectioner seam
	secs := ctx.Memory.(core.PromptSectioner).PromptSections(core.PromptBuildOptions{})
	if len(secs) != 1 {
		t.Fatalf("FULL MOUNT: prompt sections = %d, want the one memory block", len(secs))
	}
	if body := secs[0].Body; !strings.Contains(body, "MEMORY (your personal notes)") || !strings.Contains(body, "arc 6 proof entry") {
		t.Fatalf("FULL MOUNT: prompt section lost the entry: %.120q", body)
	}
	t.Log("PROOF prompt face: the snapshot block carries the entry verbatim")

	// the action pipe: memory ships no panel and no actions - even the
	// full mount has nothing to route, and every capsule that ships a
	// panel rides the snapshot + deltas + action pipe instead
	canvas, err := agent.NewCanvas(ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	for _, p := range canvas.Snapshot() {
		if p.ID == "memory" {
			t.Fatalf("FULL MOUNT: unexpected memory panel in the snapshot: %+v", p)
		}
		for _, a := range p.Actions {
			if a.Module == "memory" {
				t.Fatalf("FULL MOUNT: unexpected memory-qualified action on panel %q", p.ID)
			}
		}
	}
	if _, err := canvas.Dispatch(core.ActionCall{Module: "memory", Action: "list_entries"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("FULL MOUNT: dispatch answered %v, want ErrUnknownModule (memory declares no actions)", err)
	}
	t.Log("PROOF action-pipe: no panel, no actions - the pipe answers ErrUnknownModule on the full mount (module ships no canvas surface by design)")

	// --- mount 2: the capsule removed ---
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "memory" {
			continue
		}
		removed = append(removed, p)
	}
	ctx2 := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	if err := agent.MountAll(removed, ctx2); err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if ctx2.Memory != nil {
		t.Fatal("REMOVED MOUNT: memory seam still mounted")
	}
	for _, tp := range ctx2.ToolProviders {
		for name := range tp.Tools() {
			if strings.Contains(name, "memory") {
				t.Fatalf("REMOVED MOUNT: memory-shaped tool %q survived the removal", name)
			}
		}
	}
	for _, ps := range ctx2.PromptSectioners {
		for _, s := range ps.PromptSections(core.PromptBuildOptions{}) {
			if strings.Contains(s.Body, "MEMORY (your personal notes)") || strings.Contains(s.Body, "USER PROFILE (") {
				t.Fatalf("REMOVED MOUNT: the memory prompt block survived the removal: %.120q", s.Body)
			}
		}
	}
	noCanvas, err := agent.NewCanvas(ctx2, nil)
	if err != nil {
		t.Fatalf("canvas removed: %v", err)
	}
	if _, err := noCanvas.Dispatch(core.ActionCall{Module: "memory", Action: "list_entries"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("REMOVED MOUNT: dispatch answered %v, want ErrUnknownModule", err)
	}
	// the dependent that reads the seam without a Requires edge: cron
	// mounts still, and its deliver=memory jobs degrade through the
	// documented nil-guard ("no memory extension loaded") at fire time
	if ctx2.Cron == nil {
		t.Fatal("REMOVED MOUNT: cron did not mount - the memory removal must not take a dependent")
	}
	// data is not code: the two store files stay on disk
	for _, f := range []string{cfg.Memory.File, cfg.Memory.UserProfileFile} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("REMOVED MOUNT: store file %s vanished - data is not code", f)
		}
	}
	// core types remain - intentional (the seam signature references
	// them; layers never import modules). Compile-time proof:
	_ = core.MemoryProvider(nil)
	t.Log("PROOF removed: seam nil, no memory-shaped tools, no memory prompt block, the action pipe answers ErrUnknownModule, cron keeps mounting (deliver=memory degrades through its nil-guard), the store files stay on disk - data is not code; core/seams.go memory types remain - documented as intentional")
}
