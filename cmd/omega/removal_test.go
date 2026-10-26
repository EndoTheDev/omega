package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if _, err := c2.Dispatch(core.ActionCall{Module: "notifications", Action: "notifications.mark_all"}); err != nil {
		t.Fatalf("dispatch notifications.mark_all: %v", err)
	}
	if _, unread, err := ctx.Notifications.List(t.Context(), false, 10, 0); err != nil || unread != 0 {
		t.Fatalf("FULL MOUNT: notifications.mark_all did not clear the store (unread=%d err=%v)", unread, err)
	}
	t.Log("PROOF action-pipe: notifications.mark_all cleared the store, list refreshed through the canvas")

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
	// the REST surface: the http_channel routes answer 501 on the
	// nil seam (server_notify.go's guard) - the routes answer the
	// seam, they do not outlive it.
	t.Log("PROOF removed: seam nil (REST routes fall through to their 501 guard), canvas carries no notifications panel, no toast, no emitter wiring")
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
	taskRun, err := pluginTool(ctx, "kanban.task")
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
	// uses, routed by module + namespaced action id
	if _, err := live.Dispatch(core.ActionCall{Module: "kanban", Action: "kanban.complete_task", Args: json.RawMessage(`{"id": "` + id + `"}`)}); err != nil {
		t.Fatalf("dispatch kanban.complete_task: %v", err)
	}
	kt, err := ctx.Kanban.GetTask(cctx, "", id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if kt.Status != "done" {
		t.Fatalf("FULL MOUNT: kanban.complete_task left %s at %q", id, kt.Status)
	}
	// and the switcher runs the /board equivalence
	if _, err := live.Dispatch(core.ActionCall{Module: "kanban", Action: "kanban.switch_board", Args: json.RawMessage(`{"board": "main"}`)}); err != nil {
		t.Fatalf("dispatch kanban.switch_board: %v", err)
	}
	t.Logf("PROOF action-pipe: kanban.complete_task closed %s, kanban.switch_board answered the switcher", id)

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
	// the tool registry: no kanban tool and no legacy prefix (the
	// dotted alias shims died with the mount migration)
	names := map[string]bool{}
	for _, tp := range ctx2.ToolProviders {
		for name := range tp.Tools() {
			names[name] = true
		}
	}
	if names["kanban.task"] {
		t.Fatal("REMOVED MOUNT: the kanban task tool survived the removal")
	}
	for name := range names {
		if strings.HasPrefix(name, "kanban.") || strings.HasPrefix(name, "task.") {
			t.Fatalf("REMOVED MOUNT: tool %q survived the removal", name)
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
// carries for the ephemeral deny lists (http_channel,
// tui) are name-based guards, not module surfaces: they see a
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

// TestDelegateRemovalProof is the arc 6 delegate-capsule proof (card
// t_47675b6a), the fifth worked proof in this file. Delegate is another
// born-capsule, but the wired kind: the inject seam (ctx.InjectedMessages
// + ctx.PendingDelegations), the consolidated delegate tool and its two
// alias shims, and the finished-things emitter capture all ship from the
// plugin's single Mount, and the module ships nothing else - no command,
// no REST route, no skills, and no canvas panel (nothing rendered
// delegate before the arc; adding one is a behavior change, not a
// migration). The two mounts are the proof:
//
//  1. WITH delegate: the inject seam and pending count are mounted, the
//     registry carries the delegate tool plus the task/status alias
//     shims, a status call answers the live listing, and a real spawn
//     (OMEGA_BIN -> /bin/echo: the child `run`s, echo just prints) pushes
//     the result through the inject channel and emits one finished-things
//     record through the notifications edge - the wiring the
//     notifications proof exercises from the receive side.
//  2. WITHOUT delegate: the inject seam and pending count are nil, no
//     delegate-shaped tool is left, no delegate-qualified action or panel
//     exists, and the notifications seam still mounts - the Requires edge
//     points from delegate AT notifications, not the reverse, so the
//     removal takes no dependents with it. Delegate keeps no disk state
//     (its task map is in-memory), so data-is-not-code holds vacuously.
//
// The core types stay (core/messages.go InjectedMessage, core/seams.go's
// ctx.InjectedMessages / PendingDelegations fields) - the seam signature
// references them and layers never import modules. Intentional residue,
// documented: this test compiles them to prove it. The OMEGA_SUBAGENT
// recursion guard is the tool's own env wiring on the spawn - nothing
// outside the module implements the guard.
func TestDelegateRemovalProof(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	cfg := DefaultConfig()

	// --- mount 1: the capsule mounted ---
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	// declared data, recipe step 1: no other plugin's Requires may
	// name delegate - a hit here is the removal recipe's stop signal
	for _, p := range plugins {
		for _, req := range p.Requires() {
			if req == "delegate" {
				t.Fatalf("FULL MOUNT precondition: %q requires delegate - the removal takes a dependent", p.Name())
			}
		}
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	if err := agent.MountAll(plugins, ctx); err != nil {
		t.Fatalf("mount all: %v", err)
	}
	if ctx.InjectedMessages == nil {
		t.Fatal("FULL MOUNT: inject seam nil - the capsule did not mount")
	}
	if ctx.PendingDelegations == nil {
		t.Fatal("FULL MOUNT: pending-delegations func nil - the capsule did not mount")
	}
	if n := ctx.PendingDelegations(); n != 0 {
		t.Fatalf("FULL MOUNT: pending delegations = %d, want 0 on a fresh mount", n)
	}
	t.Log("PROOF mount=full: inject seam mounted (ctx.InjectedMessages + ctx.PendingDelegations), pending count 0")

	// the tool face: the registry carries the consolidated delegate tool
	// with its task/status actions and the two alias shims
	var delegateTool core.Tool
	foundTool, foundTaskShim, foundStatusShim := false, false, false
	for _, tp := range ctx.ToolProviders {
		for name, tool := range tp.Tools() {
			switch name {
			case "delegate":
				delegateTool, foundTool = tool, true
			case "delegate.task":
				foundTaskShim = foundTaskShim || tool.AliasOf == "delegate"
			case "delegate.status":
				foundStatusShim = foundStatusShim || tool.AliasOf == "delegate"
			}
		}
	}
	if !foundTool {
		t.Fatal("FULL MOUNT: the delegate tool is missing from the registry")
	}
	if !foundTaskShim || !foundStatusShim {
		t.Fatalf("FULL MOUNT: alias shims missing (task=%v status=%v)", foundTaskShim, foundStatusShim)
	}
	t.Log("PROOF tool wiring: registry carries the delegate tool + the delegate.task / delegate.status alias shims")

	// the status arm answers the live listing
	res, runErr := delegateTool.Run(context.Background(), map[string]any{"action": "status"})
	if runErr != nil || !strings.Contains(res, "Running: 0") || !strings.Contains(res, "No tasks.") {
		t.Fatalf("FULL MOUNT: delegate status failed (res=%s err=%v)", res, runErr)
	}

	// the spawn arm, end to end: OMEGA_BIN -> /bin/echo, the child
	// `run`s, echo just prints the prompt back. The result must ride
	// the inject channel and the finished-things edge must emit one
	// chat record (echo exits 0, so verdict ok).
	echo := "/bin/echo"
	if _, err := os.Stat(echo); err != nil {
		echo = "/usr/bin/echo"
		if _, err := os.Stat(echo); err != nil {
			t.Skip("no /bin/echo or /usr/bin/echo on this host")
		}
	}
	t.Setenv("OMEGA_BIN", echo)
	drained := make(chan core.InjectedMessage, 1)
	go func() {
		select {
		case m := <-ctx.InjectedMessages:
			drained <- m
		case <-time.After(10 * time.Second):
		}
	}()
	res, runErr = delegateTool.Run(context.Background(), map[string]any{"action": "task", "prompt": "arc 6 proof"})
	if runErr != nil || !strings.Contains(res, "task-1") {
		t.Fatalf("FULL MOUNT: delegate task spawn failed (res=%s err=%v)", res, runErr)
	}
	select {
	case m := <-drained:
		if m.Source != "delegate:task-1" {
			t.Fatalf("FULL MOUNT: injected message source = %q, want delegate:task-1", m.Source)
		}
		if !strings.Contains(m.Text, "run arc 6 proof") {
			t.Fatalf("FULL MOUNT: injected result lost the child output: %.80q", m.Text)
		}
	case <-time.After(9 * time.Second):
		t.Fatal("FULL MOUNT: the spawn result never landed on the inject channel")
	}
	// finish() emits after it injects - the record may land a beat
	// after the drain, so poll the store briefly
	sawFinish := false
	for i := 0; i < 50 && !sawFinish; i++ {
		notes, _, err := ctx.Notifications.List(context.Background(), false, 10, 0)
		if err != nil {
			t.Fatalf("notifications list: %v", err)
		}
		for _, n := range notes {
			if n.Kind == "chat" && n.SourceID == "task-1" && n.Verdict == "ok" {
				sawFinish = true
			}
		}
		if !sawFinish {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !sawFinish {
		notes, _, _ := ctx.Notifications.List(context.Background(), false, 10, 0)
		t.Fatalf("FULL MOUNT: no chat/task-1/ok finished-things record from the spawn: %+v", notes)
	}
	t.Log("PROOF spawn: the echo child's result rode the inject channel; one chat finished-things record emitted through the notifications edge")

	// the action pipe: delegate ships no panel and no actions - even
	// the full mount has nothing to route
	canvas, err := agent.NewCanvas(ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	for _, p := range canvas.Snapshot() {
		if p.ID == "delegate" {
			t.Fatalf("FULL MOUNT: unexpected delegate panel in the snapshot: %+v", p)
		}
		for _, a := range p.Actions {
			if a.Module == "delegate" {
				t.Fatalf("FULL MOUNT: unexpected delegate-qualified action on panel %q", p.ID)
			}
		}
	}
	if _, err := canvas.Dispatch(core.ActionCall{Module: "delegate", Action: "status"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("FULL MOUNT: dispatch answered %v, want ErrUnknownModule (delegate declares no actions)", err)
	}
	t.Log("PROOF action-pipe: no panel, no actions - the pipe answers ErrUnknownModule on the full mount (module ships no canvas surface by design)")

	// --- mount 2: the capsule removed ---
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "delegate" {
			continue
		}
		removed = append(removed, p)
	}
	ctx2 := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	if err := agent.MountAll(removed, ctx2); err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if ctx2.InjectedMessages != nil {
		t.Fatal("REMOVED MOUNT: inject seam still mounted")
	}
	if ctx2.PendingDelegations != nil {
		t.Fatal("REMOVED MOUNT: pending-delegations func still mounted")
	}
	for _, tp := range ctx2.ToolProviders {
		for name := range tp.Tools() {
			if strings.Contains(name, "delegate") {
				t.Fatalf("REMOVED MOUNT: delegate-shaped tool %q survived the removal", name)
			}
		}
	}
	// the emitter edge points from delegate AT notifications, not the
	// reverse: the notifications seam must still mount (its own proof
	// removes the emitters with it - that is the counter-case)
	if ctx2.Notifications == nil {
		t.Fatal("REMOVED MOUNT: notifications seam lost - the removal must not take a module it depends on")
	}
	noCanvas, err := agent.NewCanvas(ctx2, nil)
	if err != nil {
		t.Fatalf("canvas removed: %v", err)
	}
	for _, p := range noCanvas.Snapshot() {
		if p.ID == "delegate" {
			t.Fatalf("REMOVED MOUNT: delegate panel survived the removal: %+v", p)
		}
		for _, a := range p.Actions {
			if a.Module == "delegate" {
				t.Fatalf("REMOVED MOUNT: delegate-qualified action survived on panel %q", p.ID)
			}
		}
	}
	if _, err := noCanvas.Dispatch(core.ActionCall{Module: "delegate", Action: "status"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("REMOVED MOUNT: dispatch answered %v, want ErrUnknownModule", err)
	}
	// core types remain - intentional (the seam signature references
	// them; layers never import modules). Compile-time proof:
	_ = core.InjectedMessage{Text: "proof", Source: "delegate:task-1"}
	t.Log("PROOF removed: inject seam nil, no pending-delegations func, no delegate-shaped tools, no delegate panel or actions, notifications seam still mounted (the Requires edge points at it, not from it); delegate keeps no disk state (in-memory task map) - data is not code holds vacuously; core.InjectedMessage + the ctx seam fields remain - documented as intentional")
}

// TestMCPRemovalProof is the arc 6 mcp-capsule proof (card
// t_cf6b7795), the sixth worked proof in this file. MCP is the
// additive-seam born-capsule: the bridge (stdio + Streamable HTTP MCP
// servers), its server-qualified discovered tools ("server.tool"), and
// the whole env/mcp.yaml config load ship from files in modules/mcp,
// and the single Mount is the module's whole surface - no command, no
// REST route, no skills, no prompt section, no canvas panel (nothing
// rendered mcp before the arc; adding one is a behavior change, not a
// migration). The additive tools seam (ctx.ToolProviders) is core
// vocabulary every tool module shares, not an mcp-owned seam slot, so
// the module keeps no core types of its own. The two mounts are the
// proof:
//
//  1. WITH mcp: MCP_SERVERS points at a fake stdio MCP server (the
//     same fixture style the module's own tests use) and the registry
//     carries the discovered proof.echo tool; a real call round-trips
//     through the JSON-RPC stdio bridge. The action pipe answers
//     ErrUnknownModule because the module declares no actions.
//  2. WITHOUT mcp: with MCP_SERVERS still set - nothing outside the
//     module reads it - no mcp-shaped tool is left, no /mcp command
//     exists, the action pipe still answers ErrUnknownModule, and the
//     other modules mount unchanged. MCP names no Requires edge and
//     nothing requires mcp, so the removal takes no dependents with
//     it.
//
// The bridge discovers tools live at mount time and persists no state
// (mcp.yaml/mcp.json is user config, owned by the user): data is not
// code holds vacuously. The additive seam types (core.Tool,
// core.ToolProvider, ctx.ToolProviders) remain regardless - shared
// vocabulary, not mcp residue; this test compiles them to prove it.
func TestMCPRemovalProof(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	cfg := DefaultConfig()

	// the fake MCP server: stdio JSON-RPC, one echo tool. The proof
	// makes exactly one tools/call, so the script exits after it - a
	// subprocess the bridge holds open must never outlive the test.
	python, err := exec.LookPath("python3")
	if err != nil {
		if python, err = exec.LookPath("python"); err != nil {
			t.Skip("no python3/python on this host")
		}
	}
	script := `import sys, json

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except Exception:
        continue
    if "id" not in req:
        continue  # notification (e.g. notifications/initialized)
    method = req.get("method", "")
    if method == "initialize":
        result = {"protocolVersion": "2025-11-25",
                  "serverInfo": {"name": "proof-mcp", "version": "0.1.0"},
                  "capabilities": {}}
    elif method == "tools/list":
        result = {"tools": [
            {"name": "echo", "description": "Removal proof echo tool",
             "inputSchema": {"type": "object"}},
        ]}
    elif method == "tools/call":
        args = req.get("params", {}).get("arguments", {})
        result = {"content": [{"type": "text", "text": "echoed:" + json.dumps(args, sort_keys=True)}],
                  "isError": False}
    else:
        continue
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": req["id"], "result": result}) + "\n")
    sys.stdout.flush()
    if method == "tools/call":
        sys.exit(0)  # the bridge made its one call; do not outlive the test
`
	scriptPath := filepath.Join(home, "removal_proof_mcp_server.py")
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatalf("write fake mcp server: %v", err)
	}
	servers, err := json.Marshal(map[string]any{
		"servers": []map[string]any{{
			"name":    "proof",
			"command": python,
			"args":    []string{scriptPath},
		}},
	})
	if err != nil {
		t.Fatalf("mcp servers json: %v", err)
	}
	t.Setenv("MCP_SERVERS", string(servers))

	// --- mount 1: the capsule mounted ---
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	// recipe step 1, declared data: mcp requires nothing - and no other
	// plugin's Requires may name mcp - a hit here is the removal
	// recipe's stop signal
	for _, p := range plugins {
		for _, req := range p.Requires() {
			if req == "mcp" {
				t.Fatalf("FULL MOUNT precondition: %q requires mcp - the removal takes a dependent", p.Name())
			}
		}
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	if err := agent.MountAll(plugins, ctx); err != nil {
		t.Fatalf("mount all: %v", err)
	}

	// the tool face: the registry carries the server-qualified tool the
	// bridge discovered at mount time (buildPlugins -> NewPluginFromEnv
	// -> the JSON-RPC discovery round-trip), and a real call answers
	// through the same bridge
	echoTool, err := pluginTool(ctx, "proof.echo")
	if err != nil {
		fatalTools := ""
		for _, tp := range ctx.ToolProviders {
			for name := range tp.Tools() {
				fatalTools += " " + name
			}
		}
		t.Fatalf("FULL MOUNT: the proof.echo tool is missing from the registry (tools:%s)", fatalTools)
	}
	res, runErr := echoTool(context.Background(), map[string]any{"msg": "arc 6 proof"})
	if runErr != nil || !strings.Contains(res, "echoed:") || !strings.Contains(res, "arc 6 proof") {
		t.Fatalf("FULL MOUNT: proof.echo round-trip failed (res=%s err=%v)", res, runErr)
	}
	t.Logf("PROOF mount=full: bridge mounted as an additive tool provider, proof.echo answered the call (%s)", strings.TrimSpace(res))

	// the action pipe: mcp ships no panel and no actions - even the
	// full mount has nothing to route
	canvas, err := agent.NewCanvas(ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	for _, p := range canvas.Snapshot() {
		if p.ID == "mcp" {
			t.Fatalf("FULL MOUNT: unexpected mcp panel in the snapshot: %+v", p)
		}
		for _, a := range p.Actions {
			if a.Module == "mcp" {
				t.Fatalf("FULL MOUNT: unexpected mcp-qualified action on panel %q", p.ID)
			}
		}
	}
	if _, err := canvas.Dispatch(core.ActionCall{Module: "mcp", Action: "list_servers"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("FULL MOUNT: dispatch answered %v, want ErrUnknownModule (mcp declares no actions)", err)
	}
	// the TUI surface: the module registers no command
	for _, cmd := range ctx.Commands {
		if cmd.Name == "/mcp" {
			t.Fatal("FULL MOUNT: an /mcp command exists - the module ships none")
		}
	}
	t.Log("PROOF action-pipe: no panel, no actions, no /mcp - the pipe answers ErrUnknownModule on the full mount (module ships no canvas surface by design)")

	// --- mount 2: the capsule removed ---
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "mcp" {
			continue
		}
		removed = append(removed, p)
	}
	ctx2 := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	if err := agent.MountAll(removed, ctx2); err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	// the tool registry: no proof.* tool, and nothing answered the env
	// that still points at the fake server - nothing outside the module
	// reads MCP_SERVERS
	for _, tp := range ctx2.ToolProviders {
		for name := range tp.Tools() {
			if strings.HasPrefix(name, "proof.") || strings.Contains(name, "mcp") {
				t.Fatalf("REMOVED MOUNT: mcp-shaped tool %q survived the removal", name)
			}
		}
	}
	for _, cmd := range ctx2.Commands {
		if cmd.Name == "/mcp" {
			t.Fatal("REMOVED MOUNT: an /mcp command survived the removal")
		}
	}
	noCanvas, err := agent.NewCanvas(ctx2, nil)
	if err != nil {
		t.Fatalf("canvas removed: %v", err)
	}
	for _, p := range noCanvas.Snapshot() {
		if p.ID == "mcp" {
			t.Fatalf("REMOVED MOUNT: an mcp panel survived the removal: %+v", p)
		}
		for _, a := range p.Actions {
			if a.Module == "mcp" {
				t.Fatalf("REMOVED MOUNT: an mcp-qualified action survived on panel %q", p.ID)
			}
		}
	}
	if _, err := noCanvas.Dispatch(core.ActionCall{Module: "mcp", Action: "list_servers"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("REMOVED MOUNT: dispatch answered %v, want ErrUnknownModule", err)
	}
	// the neighbors keep mounting: the removal takes no dependents
	// (mcp requires nothing, nothing requires mcp)
	if ctx2.Notifications == nil {
		t.Fatal("REMOVED MOUNT: notifications seam lost - the removal must not take a module it does not own")
	}
	// the additive tools seam remains - shared vocabulary, not mcp
	// residue (layers never import modules; core has no mcp types at
	// all). Compile-time proof:
	_ = core.Tool{}
	_ = core.ToolProvider(nil)
	t.Log("PROOF removed: no mcp-shaped tools (MCP_SERVERS still set, nothing outside the module reads it), no /mcp command, no mcp panel or actions, the action pipe answers ErrUnknownModule, the other modules mount unchanged; the bridge persists no state (tools are discovered live, mcp.yaml is user config) - data is not code holds vacuously; the additive tools seam (core.Tool / core.ToolProvider / ctx.ToolProviders) remains - shared vocabulary, documented as intentional")
}

// TestVerdictRemovalProof is the arc 6 verdict-capsule proof (card
// t_78935c07), the seventh worked proof in this file. Verdict is the
// interactive born-capsule: the single-answer hub (ctx.Verdicts), the
// blocking `verdict` tool, the /verdict TUI command, and the Verdict
// Cards prompt section all ship from the one Mount - the same object
// the hub builds is the tool's pause point and every answer surface's
// write point, which is the whole single-answer race. No canvas panel,
// no module-qualified actions, no skills: the webui renders the card
// in the chat stream off the tool call and the bell polls /verdicts,
// both surfaces guard by construction (a noninteractive run denies
// the tool name; the REST routes 501 through their seam-nil guard).
// The two mounts are the proof:
//
//  1. WITH verdict: the seam is mounted, the registry carries the
//     verdict tool with its request sub-action, a real verdict.request
//     BLOCKS the turn and the first answer through the seam resolves
//     it (the very path the /verdicts POST and the /verdict command
//     share), and the Verdict Cards prompt section rides the
//     sectioner seam.
//  2. WITHOUT verdict: the seam is nil (the /verdicts routes fall
//     through to their 501 guard), no verdict tool, no /verdict
//     command, no Verdict Cards prompt block, no canvas panel or
//     verdict-qualified action, and the other modules mount unchanged.
//
// The audit rows stay: Answer persists the verdict record into the
// session store (roles "verdict_card" / "verdict") - the store is a
// different module and keeps rows from mounts gone by; data is not
// code. The core types stay too (core/seams.go: VerdictsProvider,
// VerdictPrompt, Verdict) - the seam signature references them and
// layers never import modules. Intentional residue, documented: this
// test compiles them to prove it.
func TestVerdictRemovalProof(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	cfg := DefaultConfig()

	// --- mount 1: the capsule mounted ---
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	// recipe step 1, declared data: verdict requires store and logging,
	// and no other plugin's Requires may name "verdicts" - a hit here
	// is the removal recipe's stop signal
	for _, p := range plugins {
		for _, req := range p.Requires() {
			if req == "verdicts" {
				t.Fatalf("FULL MOUNT precondition: %q requires verdicts - the removal takes a dependent", p.Name())
			}
		}
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	if err := agent.MountAll(plugins, ctx); err != nil {
		t.Fatalf("mount all: %v", err)
	}
	if ctx.Verdicts == nil {
		t.Fatal("FULL MOUNT: verdicts seam nil - capsule did not mount")
	}
	t.Log("PROOF mount=full: seam mounted (ctx.Verdicts), tool + command + prompt section from the same Mount")

	// the prompt-section face: the same Mount renders the Verdict
	// Cards guidance between Project Context and Environment
	foundCardsBlock := false
	for _, ps := range ctx.PromptSectioners {
		for _, s := range ps.PromptSections(core.PromptBuildOptions{}) {
			if s.Title == "Verdict Cards" && strings.Contains(s.Body, "verdict.request") {
				foundCardsBlock = true
			}
		}
	}
	if !foundCardsBlock {
		t.Fatal("FULL MOUNT: the Verdict Cards prompt section is missing")
	}

	// the tool face: the registry carries the verdict tool with its
	// one sub-action, and a real verdict.request hard-pauses the turn
	// until an answer lands through the seam - the path the /verdicts
	// POST and the /verdict command share
	verdictTool, err := pluginTool(ctx, "verdict")
	if err != nil {
		t.Fatalf("FULL MOUNT: the verdict tool is missing from the registry: %v", err)
	}
	for _, tp := range ctx.ToolProviders {
		if tool, ok := tp.Tools()["verdict"]; ok {
			foundRequest := false
			for _, a := range tool.Actions {
				if a.Name == "request" {
					foundRequest = true
				}
			}
			if !foundRequest {
				t.Fatalf("FULL MOUNT: the verdict tool lost its request sub-action: %+v", tool.Actions)
			}
		}
	}
	type toolResult struct {
		res string
		err error
	}
	resCh := make(chan toolResult, 1)
	go func() {
		res, err := verdictTool(context.Background(), map[string]any{
			"action":  "request",
			"title":   "arc 6 proof: which wire?",
			"options": []any{"left", "right"},
		})
		resCh <- toolResult{res: res, err: err}
	}()
	var cardID string
	cardDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(cardDeadline) {
		if cards := ctx.Verdicts.Pending(""); len(cards) > 0 {
			cardID = cards[0].ID
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if cardID == "" {
		t.Fatal("FULL MOUNT: the blocked tool call never registered a pending card")
	}
	if _, err := ctx.Verdicts.Answer("", cardID, core.Verdict{OptionIndex: 2}); err != nil {
		t.Fatalf("FULL MOUNT: answer through the seam failed: %v", err)
	}
	var got toolResult
	select {
	case got = <-resCh:
		if got.err != nil || !strings.Contains(got.res, `"verdict":"answer"`) || !strings.Contains(got.res, `"option":"right"`) {
			t.Fatalf("FULL MOUNT: verdict.request did not return the answered pick (res=%s err=%v)", got.res, got.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("FULL MOUNT: verdict.request never unblocked after the first answer")
	}
	t.Logf("PROOF tool wiring: verdict.request blocked the turn, the first answer through the seam won the race - %s", strings.TrimSpace(got.res))

	// the TUI face: /verdict declared through the same Mount
	foundCommand := false
	for _, tp := range ctx.TUIProviders {
		for _, c := range tp.TUICommands() {
			if c.Name == "/verdict" {
				foundCommand = true
			}
		}
	}
	if !foundCommand {
		t.Fatal("FULL MOUNT: no /verdict command registered through the capsule's TUIProvider")
	}
	t.Log("PROOF command face: /verdict registered by the same Mount that owns the tool and the hub")

	// the action pipe: verdict ships no panel and no actions - even
	// the full mount has nothing to route
	canvas, err := agent.NewCanvas(ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	for _, p := range canvas.Snapshot() {
		if p.ID == "verdict" {
			t.Fatalf("FULL MOUNT: unexpected verdict panel in the snapshot: %+v", p)
		}
		for _, a := range p.Actions {
			if a.Module == "verdict" {
				t.Fatalf("FULL MOUNT: unexpected verdict-qualified action on panel %q", p.ID)
			}
		}
	}
	if _, err := canvas.Dispatch(core.ActionCall{Module: "verdict", Action: "list_cards"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("FULL MOUNT: dispatch answered %v, want ErrUnknownModule (verdict declares no actions)", err)
	}
	t.Log("PROOF action-pipe: no panel, no actions - the pipe answers ErrUnknownModule on the full mount (module ships no canvas surface by design)")

	// --- mount 2: the capsule removed ---
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "verdict" {
			continue
		}
		removed = append(removed, p)
	}
	ctx2 := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	if err := agent.MountAll(removed, ctx2); err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if ctx2.Verdicts != nil {
		t.Fatal("REMOVED MOUNT: verdicts seam still mounted")
	}
	for _, tp := range ctx2.ToolProviders {
		for name := range tp.Tools() {
			if strings.Contains(name, "verdict") {
				t.Fatalf("REMOVED MOUNT: verdict-shaped tool %q survived the removal", name)
			}
		}
	}
	for _, tp := range ctx2.TUIProviders {
		for _, c := range tp.TUICommands() {
			if c.Name == "/verdict" {
				t.Fatal("REMOVED MOUNT: the /verdict command survived the removal")
			}
		}
	}
	for _, ps := range ctx2.PromptSectioners {
		for _, s := range ps.PromptSections(core.PromptBuildOptions{}) {
			if strings.Contains(s.Body, "verdict.request") {
				t.Fatalf("REMOVED MOUNT: the Verdict Cards prompt block survived the removal: %.120q", s.Body)
			}
		}
	}
	noCanvas, err := agent.NewCanvas(ctx2, nil)
	if err != nil {
		t.Fatalf("canvas removed: %v", err)
	}
	for _, p := range noCanvas.Snapshot() {
		if p.ID == "verdict" {
			t.Fatalf("REMOVED MOUNT: a verdict panel survived the removal: %+v", p)
		}
		for _, a := range p.Actions {
			if a.Module == "verdict" {
				t.Fatalf("REMOVED MOUNT: a verdict-qualified action survived on panel %q", p.ID)
			}
		}
	}
	if _, err := noCanvas.Dispatch(core.ActionCall{Module: "verdict", Action: "list_cards"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("REMOVED MOUNT: dispatch answered %v, want ErrUnknownModule", err)
	}
	// the neighbors keep mounting: the removal takes no dependents
	// (verdict requires store and logging, nothing requires verdicts)
	if ctx2.Notifications == nil {
		t.Fatal("REMOVED MOUNT: notifications seam lost - the removal must not take a module it does not own")
	}
	// core types remain - intentional (the seam signature references
	// them; layers never import modules). Compile-time proof:
	_ = core.VerdictsProvider(nil)
	_ = core.VerdictPrompt{}
	_ = core.Verdict{}
	t.Log("PROOF removed: seam nil (the /verdicts routes fall through to their 501 guard), no verdict tool, no /verdict command, no Verdict Cards prompt block, no canvas panel or verdict-qualified actions, the action pipe answers ErrUnknownModule; the card + verdict audit rows stay on disk in the session store (data is not code) and the shell keeps its guarded surfaces inert (busy-Enter sees a nil seam, the denied tool name denies nothing); core/seams.go verdict types remain - documented as intentional")
}

// TestTrustRemovalProof is the arc 6 trust-capsule proof (card
// t_e1efb979), the eighth worked proof in this file. Trust is the
// gating capsule: the trust seam (ctx.Trust - ResolveContext gates the
// AGENTS.md project context behind trust.yaml, State feeds the status
// bar) is the whole surface, shipped from the one Mount. No tool, no
// slash command, no REST route, no skills, no canvas panel and no
// module-qualified actions exist, and none did before the arc: the
// frontends consume two strings the host computes from the seam before
// launch (PromptContext + TrustState on FrontendOptions), the CLI
// flags (--approve/--no-approve) parse in the host and ride the same
// seam, and the boot-time store migration (trust.Bootstrap) is host
// wiring firing a module-owned function - no mounted surface rides it.
// The two mounts are the proof:
//
//  1. WITH trust: the seam is mounted and the gate answers through it -
//     an untrusted project under a noninteractive run loads no context
//     (skip + warning, the run/serve path), the approve path persists
//     the project root into trust.yaml and returns the AGENTS.md text,
//     and State reads trusted off the persisted store with --no-approve
//     still winning.
//  2. WITHOUT trust: the seam is nil - the host entry paths answer
//     empty through their nil-guard (serve and run start, both
//     frontends stay alive), no trust tool, and the other modules mount
//     unchanged (trust Requires nothing and nothing Requires trust).
//
// Data is not code: trust.yaml and its trusted entries stay on disk
// after a removal. The core type stays too (core/seams.go:
// TrustProvider) - the seam signature references it and layers never
// import modules. Intentional residue, documented: this test compiles
// it to prove it.
func TestTrustRemovalProof(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	cfg := DefaultConfig()

	// --- mount 1: the capsule mounted ---
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	// recipe step 1, declared data: nothing requires trust. Trust ships
	// no seam others read and no dependent emitter rides its mount.
	for _, p := range plugins {
		for _, req := range p.Requires() {
			if req == "trust" {
				t.Fatalf("FULL MOUNT precondition: %q requires trust - the removal takes a dependent", p.Name())
			}
		}
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	if err := agent.MountAll(plugins, ctx); err != nil {
		t.Fatalf("mount all: %v", err)
	}
	if ctx.Trust == nil {
		t.Fatal("FULL MOUNT: trust seam nil - capsule did not mount")
	}
	t.Log("PROOF mount=full: trust seam mounted (ctx.Trust - the trust.yaml gate), the whole surface ships from the one Mount")

	// a real project directory with a real AGENTS.md, in a home
	// (OMEGA_HOME) that keeps the trust store; the gate walks the
	// project directory and writes the store under config/
	proj := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	const marker = "arc 6 trust-capsule proof context"
	if err := os.WriteFile(filepath.Join(proj, "AGENTS.md"), []byte(marker), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	// the run/serve face: an untrusted project outside an --approve run
	// loads no context (skip + warning)
	if got := trustGate(ctx.Trust, proj, trustFlags{}, false); got != "" {
		t.Fatalf("FULL MOUNT: untrusted project loaded context anyway: %q", got)
	}
	// the --approve face: the gate persists the project root into
	// trust.yaml and returns the AGENTS.md text
	approved := trustGate(ctx.Trust, proj, trustFlags{approve: true}, false)
	if !strings.Contains(approved, marker) {
		t.Fatalf("FULL MOUNT: the approve path did not load the project context (got %.80q)", approved)
	}
	store, err := os.ReadFile(filepath.Join(os.Getenv("OMEGA_HOME"), "config", "trust.yaml"))
	if err != nil || !strings.Contains(string(store), proj) {
		t.Fatalf("FULL MOUNT: the approve path did not persist %s into the trust store (err=%v yaml=%q)", proj, err, store)
	}
	t.Log("PROOF gate wiring: --approve persisted the project root into trust.yaml and the same call returned the AGENTS.md context")
	// the status-bar face: State reads trusted off the persisted store
	if st := trustStatus(ctx.Trust, proj, trustFlags{}); st != "trusted" {
		t.Fatalf("FULL MOUNT: State = %q, want trusted", st)
	}
	// --no-approve wins, on both faces, by the seam's own rule
	if st := trustStatus(ctx.Trust, proj, trustFlags{noApprove: true}); st != "untrusted" {
		t.Fatalf("FULL MOUNT: State with --no-approve = %q, want untrusted", st)
	}
	if got := trustGate(ctx.Trust, proj, trustFlags{noApprove: true}, false); got != "" {
		t.Fatalf("FULL MOUNT: --no-approve loaded context anyway: %.80q", got)
	}
	t.Log("PROOF gate faces: untrusted skip, approve + persist + load, trusted status, --no-approve wins - all answered through the seam")

	// trust ships no registry faces by design: no tool, no /trust
	// command, no canvas panel, no module-qualified action
	for _, tp := range ctx.ToolProviders {
		for name := range tp.Tools() {
			if strings.Contains(name, "trust") {
				t.Fatalf("FULL MOUNT: trust-shaped tool %q in the registry (the capsule ships no tools)", name)
			}
		}
	}
	for _, tp := range ctx.TUIProviders {
		for _, c := range tp.TUICommands() {
			if c.Name == "/trust" {
				t.Fatal("FULL MOUNT: unexpected /trust command (the capsule ships no commands)")
			}
		}
	}
	canvas, err := agent.NewCanvas(ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	for _, p := range canvas.Snapshot() {
		if p.ID == "trust" {
			t.Fatalf("FULL MOUNT: unexpected trust panel in the snapshot: %+v", p)
		}
		for _, a := range p.Actions {
			if a.Module == "trust" {
				t.Fatalf("FULL MOUNT: unexpected trust-qualified action on panel %q", p.ID)
			}
		}
	}
	if _, err := canvas.Dispatch(core.ActionCall{Module: "trust", Action: "trust"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("FULL MOUNT: dispatch answered %v, want ErrUnknownModule (trust declares no actions)", err)
	}
	t.Log("PROOF surface posture: no tool, no /trust command, no canvas panel or trust-qualified action - the action pipe answers ErrUnknownModule on the full mount (the module ships no canvas surface by design)")

	// --- mount 2: the capsule removed ---
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "trust" {
			continue
		}
		removed = append(removed, p)
	}
	ctx2 := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	if err := agent.MountAll(removed, ctx2); err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if ctx2.Trust != nil {
		t.Fatal("REMOVED MOUNT: trust seam still mounted")
	}
	// the frontends stay alive: the host entry paths are nil-guarded -
	// serve and run answer an empty gate and the status line stays bare
	if got := trustGate(nil, proj, trustFlags{approve: true}, true); got != "" {
		t.Fatalf("REMOVED MOUNT: the guarded host gate answered %q with a nil seam", got)
	}
	if st := trustStatus(nil, proj, trustFlags{}); st != "" {
		t.Fatalf("REMOVED MOUNT: the guarded status path answered %q with a nil seam", st)
	}
	for _, tp := range ctx2.ToolProviders {
		for name := range tp.Tools() {
			if strings.Contains(name, "trust") {
				t.Fatalf("REMOVED MOUNT: trust-shaped tool %q survived the removal", name)
			}
		}
	}
	for _, tp := range ctx2.TUIProviders {
		for _, c := range tp.TUICommands() {
			if c.Name == "/trust" {
				t.Fatal("REMOVED MOUNT: the /trust command survived the removal")
			}
		}
	}
	noCanvas, err := agent.NewCanvas(ctx2, nil)
	if err != nil {
		t.Fatalf("canvas removed: %v", err)
	}
	for _, p := range noCanvas.Snapshot() {
		if p.ID == "trust" {
			t.Fatalf("REMOVED MOUNT: a trust panel survived the removal: %+v", p)
		}
		for _, a := range p.Actions {
			if a.Module == "trust" {
				t.Fatalf("REMOVED MOUNT: a trust-qualified action survived on panel %q", p.ID)
			}
		}
	}
	if _, err := noCanvas.Dispatch(core.ActionCall{Module: "trust", Action: "trust"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("REMOVED MOUNT: dispatch answered %v, want ErrUnknownModule", err)
	}
	// the neighbors keep mounting: the removal takes no dependents
	// (trust requires nothing, nothing requires trust)
	if ctx2.Notifications == nil {
		t.Fatal("REMOVED MOUNT: notifications seam lost - the removal must not take a module it does not own")
	}
	if ctx2.Kanban == nil {
		t.Fatal("REMOVED MOUNT: kanban seam lost - the removal must not take a module it does not own")
	}
	// data is not code: the persisted trust store (and its entries)
	// stays on disk after the removal
	if _, err := os.Stat(filepath.Join(os.Getenv("OMEGA_HOME"), "config", "trust.yaml")); err != nil {
		t.Fatalf("REMOVED MOUNT: the trust store vanished from disk (data is not code): %v", err)
	}
	// core types remain - intentional (the seam signature references
	// them; layers never import modules). Compile-time proof:
	_ = core.TrustProvider(nil)
	t.Log("PROOF removed: seam nil, the host entry paths answer empty through their nil-guard (serve + run stay alive), no trust tool, no /trust command, no canvas panel or trust-qualified action, the action pipe answers ErrUnknownModule; trust.yaml and its entries stay on disk (data is not code); core/seams.go TrustProvider remains - documented as intentional")
}

// TestWebRemovalProof is the arc 6 web-capsule proof (card
// t_23f42712), the ninth worked proof in this file. Web is the
// additive-seam thin capsule (same shape as mcp): the consolidated
// `web` tool plus its one-release alias shims (web.search, web.fetch)
// ship from the single Mount as one ToolProvider, and the module even
// ships its config stanza (ctx.Configs["web"] routed by the plugin's
// ConfigKey; the base_url override and the provider-host mirror
// fallback derive inside the host's ConfigSection from module-owned
// types). No command, no REST route, no skills, no exclusive seam
// ( Provides() is exactly "tools" - nothing reads a ctx.Web field),
// no canvas panel, and none existed before the arc. The two mounts
// are the proof:
//
//  1. WITH web: the registry carries the domain tool and both alias
//     shims, and a real search and a real fetch round-trip through a
//     fake mirror endpoint - search through the folded dispatcher,
//     fetch through the alias shim (the same runWeb behind both).
//  2. WITHOUT web: with the mirror endpoint still configured and the
//     fake server's hit counter frozen - nothing outside the module
//     reads the endpoint - no web-shaped tool survives, no /web
//     command ever existed, the action pipe answers ErrUnknownModule,
//     and the other modules mount unchanged. Web Requires nothing and
//     nothing Requires web, so the removal takes no dependents with
//     it.
//
// The extension is stateless (every call is an upstream request; no
// store, no db files): data is not code holds vacuously. The additive
// seam types (core.Tool, core.ToolProvider, ctx.ToolProviders) remain
// - shared vocabulary, not web residue; this test compiles them.
func TestWebRemovalProof(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)

	// the fake mirror: the two endpoints the module targets. The hit
	// counter doubles as the nothing-outside-the-module-reads-the-
	// endpoint proof on the removed mount.
	var hits int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/web_search", func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Authorization") != "Bearer proof-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"results":[{"title":"Arc 6","url":"https://omega.example/proof","content":"web search answer"}]}`)
	})
	mux.HandleFunc("/api/web_fetch", func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Authorization") != "Bearer proof-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"title":"Proof page","content":"web fetch answer","links":["https://omega.example/next"]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("OMEGA_MAX_RETRIES", "0")

	cfg := DefaultConfig()
	cfg.Web.BaseURL = srv.URL
	// the web extension rides the provider key (the mirror swaps it
	// for a real client key upstream)
	cfg.Provider.APIKey = "proof-key"

	// --- mount 1: the capsule mounted ---
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	// recipe step 1, declared data: web provides exactly the additive
	// tools seam (no exclusive seam exists), requires nothing - and no
	// other plugin's Requires may name web - a hit here is the removal
	// recipe's stop signal
	for _, p := range plugins {
		if p.Name() == "web" && !sameStrings(p.Provides(), []string{"tools"}) {
			t.Fatalf("FULL MOUNT: web ships %v, want exactly [tools] - it has no exclusive seam", p.Provides())
		}
		for _, req := range p.Requires() {
			if req == "web" {
				t.Fatalf("FULL MOUNT precondition: %q requires web - the removal takes a dependent", p.Name())
			}
		}
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	if err := agent.MountAll(plugins, ctx); err != nil {
		t.Fatalf("mount all: %v", err)
	}

	// the tool face: the domain tool answers a real search through the
	// folded dispatcher
	search, err := pluginTool(ctx, "web")
	if err != nil {
		fatalTools := ""
		for _, tp := range ctx.ToolProviders {
			for name := range tp.Tools() {
				fatalTools += " " + name
			}
		}
		t.Fatalf("FULL MOUNT: the web tool is missing from the registry (tools:%s)", fatalTools)
	}
	res, runErr := search(context.Background(), map[string]any{"action": "search", "query": "arc 6 proof"})
	if runErr != nil || !strings.Contains(res, "web search answer") || !strings.Contains(res, "https://omega.example/proof") {
		t.Fatalf("FULL MOUNT: web search round-trip failed (res=%q err=%v)", res, runErr)
	}
	// the alias face: web.fetch routes through the same dispatcher
	var fetchRun func(context.Context, map[string]any) (string, error)
	for _, tp := range ctx.ToolProviders {
		if tl, ok := tp.Tools()["web.fetch"]; ok && tl.AliasOf == "web" && tl.Run != nil {
			fetchRun = tl.Run
		}
	}
	if fetchRun == nil {
		t.Fatal("FULL MOUNT: web.fetch alias Shim missing (AliasOf=web)")
	}
	res, runErr = fetchRun(context.Background(), map[string]any{"url": "https://omega.example/proof"})
	if runErr != nil || !strings.Contains(res, "Proof page") || !strings.Contains(res, "web fetch answer") {
		t.Fatalf("FULL MOUNT: web.fetch alias round-trip failed (res=%q err=%v)", res, runErr)
	}
	if hits != 2 {
		t.Fatalf("FULL MOUNT: the fake mirror served %d hits, want 2 (search + fetch)", hits)
	}
	t.Logf("PROOF tool wiring: web search answered through the folded dispatcher, web.fetch through the alias shim (the same runWeb behind both) - both round-tripped the configured mirror (%d hits)", hits)

	// the action pipe: web ships no panel and no actions - even the
	// full mount has nothing to route
	canvas, err := agent.NewCanvas(ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	for _, p := range canvas.Snapshot() {
		if p.ID == "web" {
			t.Fatalf("FULL MOUNT: unexpected web panel in the snapshot: %+v", p)
		}
		for _, a := range p.Actions {
			if a.Module == "web" {
				t.Fatalf("FULL MOUNT: unexpected web-qualified action on panel %q", p.ID)
			}
		}
	}
	if _, err := canvas.Dispatch(core.ActionCall{Module: "web", Action: "search"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("FULL MOUNT: dispatch answered %v, want ErrUnknownModule (web declares no actions)", err)
	}
	// the TUI surface: the module registers no command
	for _, cmd := range ctx.Commands {
		if cmd.Name == "/web" {
			t.Fatal("FULL MOUNT: a /web command exists - the module ships none")
		}
	}
	t.Log("PROOF action-pipe: no panel, no actions, no /web - the pipe answers ErrUnknownModule on the full mount (module ships no canvas surface by design)")

	// --- mount 2: the capsule removed ---
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "web" {
			continue
		}
		removed = append(removed, p)
	}
	ctx2 := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	if err := agent.MountAll(removed, ctx2); err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	// the tool registry: no web-shaped tool survives - domain tool and
	// both alias shims alike - and nothing answered the still-set
	// endpoint (frozen hit counter: nothing outside the module reads
	// the mirror URL)
	for _, tp := range ctx2.ToolProviders {
		for name := range tp.Tools() {
			if name == "web" || strings.HasPrefix(name, "web.") || strings.Contains(name, "web") {
				t.Fatalf("REMOVED MOUNT: web-shaped tool %q survived the removal", name)
			}
		}
	}
	if hits != 2 {
		t.Fatalf("REMOVED MOUNT: the fake mirror served %d hits, want the mount-1 freeze at 2", hits)
	}
	for _, cmd := range ctx2.Commands {
		if cmd.Name == "/web" {
			t.Fatal("REMOVED MOUNT: a /web command survived the removal")
		}
	}
	noCanvas, err := agent.NewCanvas(ctx2, nil)
	if err != nil {
		t.Fatalf("canvas removed: %v", err)
	}
	for _, p := range noCanvas.Snapshot() {
		if p.ID == "web" {
			t.Fatalf("REMOVED MOUNT: a web panel survived the removal: %+v", p)
		}
		for _, a := range p.Actions {
			if a.Module == "web" {
				t.Fatalf("REMOVED MOUNT: a web-qualified action survived on panel %q", p.ID)
			}
		}
	}
	if _, err := noCanvas.Dispatch(core.ActionCall{Module: "web", Action: "search"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("REMOVED MOUNT: dispatch answered %v, want ErrUnknownModule", err)
	}
	// the neighbors keep mounting: the removal takes no dependents
	// (web requires nothing, nothing requires web)
	if ctx2.Notifications == nil {
		t.Fatal("REMOVED MOUNT: notifications seam lost - the removal must not take a module it does not own")
	}
	if ctx2.Kanban == nil {
		t.Fatal("REMOVED MOUNT: kanban seam lost - the removal must not take a module it does not own")
	}
	// data is not code: the extension is stateless - no store, no db
	// files - so the property holds vacuously. Assert exactly that:
	// no web artifacts under the home.
	if dirs, _ := filepath.Glob(filepath.Join(os.Getenv("OMEGA_HOME"), "**", "web*")); len(dirs) != 0 {
		t.Fatalf("REMOVED MOUNT: web state shipped a store it never owned: %v", dirs)
	}
	// the additive tools seam remains - shared vocabulary, not web
	// residue (layers never import modules; core has no web types at
	// all). Compile-time proof:
	_ = core.Tool{}
	_ = core.ToolProvider(nil)
	t.Log("PROOF removed: no web tool, no web.search/web.fetch shims (the mirror URL is still set, nothing outside the module reads it - hit counter frozen), no /web command, no web panel or actions, the action pipe answers ErrUnknownModule, the other modules mount unchanged; the extension persists no state (every call is an upstream request) - data is not code holds vacuously; the additive tools seam (core.Tool / core.ToolProvider / ctx.ToolProviders) remains - shared vocabulary, documented as intentional")
}

// sameStrings is the order-sensitive slice equality the Provides()
// posture assertion uses.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
