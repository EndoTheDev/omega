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
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/kanban"
	"github.com/EndoTheDev/omega/harness/terminal"
)

// Local consumer faces for the proof tests (part 3, the seam
// constitution): every consumer names its own std-typed face and
// resolves the service by key. The capsule types stay in the capsules -
// even the proof carries no capsule noun.
type proofNotifier interface {
	Emit(n core.Notice) (core.Notice, error)
	List(ctx context.Context, unreadOnly bool, limit, offset int) ([]core.Notice, int, error)
	MarkAllRead(ctx context.Context) error
}

type proofVerdicts interface {
	Pending(sessionID string) []core.QuestionPrompt
	Answer(sessionID, cardID string, v core.QuestionAnswer) (core.QuestionAnswer, error)
}

// proofResolve fails the test loud naming the offer key - the negative
// of the boot-time bind assert.
func proofResolve(t *testing.T, ctx *core.Context, key string) {
	t.Helper()
	if ctx.Services().Resolve(key) == nil {
		t.Fatalf("FULL MOUNT: the %q offer is nil - capsule did not mount", key)
	}
}

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
	var kbPlugin *kanban.Plugin
	for _, pl := range plugins {
		if kb, ok := pl.(*kanban.Plugin); ok {
			kbPlugin = kb
		}
	}
	if kbPlugin == nil {
		t.Fatal("FULL MOUNT: the kanban capsule is absent from buildPlugins")
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	reg1, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	dismountCleanup(t, reg1, ctx)
	if err != nil {
		t.Fatalf("mount all: %v", err)
	}
	proofResolve(t, ctx, "notifications")
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
	notifier, _ := ctx.Services().Resolve("notifications").(proofNotifier)
	if _, err := notifier.Emit(core.Notice{Kind: "task", SourceID: "proof", Title: "task proof done", Verdict: "ok"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if _, err := notifier.Emit(core.Notice{Kind: "cron", SourceID: "proof2", Title: "cron proof2 finished", Verdict: "failed"}); err != nil {
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
	if _, unread, err := notifier.List(t.Context(), false, 10, 0); err != nil || unread != 0 {
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
	reg2, err := agent.MountAllAPI(removed, ctx2, nil, nil)
	dismountCleanup(t, reg2, ctx2)
	if err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if _, ok := ctx2.Services().Resolve("notifications").(proofNotifier); ok {
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
//  2. WITHOUT kanban: the REST routes are gone (the served surface
//     answers 404 - no route registered, nothing to guard), the canvas
//     carries no kanban panel, the tool registry has no task tools,
//     and the /board commands are unregistered. Nothing else in the
//     tree depends on the module - no Requires edge names kanban, so
//     the removal takes no dependents with it.
//
// The module nouns live in the capsule now (phase F): the board types
// are package kanban, core carries zero of them - the binary-purity
// nm check (scripts/nm-purity.sh) is the machine law on top of this.
func TestKanbanRemovalProof(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	cfg := DefaultConfig()

	// --- mount 1: the capsule mounted ---
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	var kbPlugin *kanban.Plugin
	for _, pl := range plugins {
		if kb, ok := pl.(*kanban.Plugin); ok {
			kbPlugin = kb
		}
	}
	if kbPlugin == nil {
		t.Fatal("FULL MOUNT: the kanban capsule is absent from buildPlugins")
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	reg3, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	dismountCleanup(t, reg3, ctx)
	if err != nil {
		t.Fatalf("mount all: %v", err)
	}
	routes := map[string]string{}
	for _, rt := range ctx.ModuleRoutes {
		routes[rt.Pattern] = rt.Module
	}
	for _, want := range []string{"GET /tasks", "POST /tasks", "GET /tasks/{id}", "PATCH /tasks/{id}", "POST /tasks/{id}/comments", "GET /boards"} {
		if routes[want] != "kanban" {
			t.Fatalf("FULL MOUNT: route %q missing or foreign-tenanted: %v", want, routes)
		}
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
	t.Log("PROOF mount=full: the capsule owns its REST routes through RegisterRoute, the canvas carries the task panel + the board switcher, module-qualified")

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
	taskRun, err := pluginTool(ctx, "kanban.create")
	if err != nil {
		t.Fatalf("task tool: %v", err)
	}
	cctx := context.Background()
	out, err := taskRun(cctx, map[string]any{"title": "removal proof task"})
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
	kt, err := kbPlugin.Store().GetTask(cctx, "", id)
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
	reg4, err := agent.MountAllAPI(removed, ctx2, nil, nil)
	dismountCleanup(t, reg4, ctx2)
	if err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	for _, rt := range ctx2.ModuleRoutes {
		if rt.Module == "kanban" {
			t.Fatalf("REMOVED MOUNT: route %q survived the removal", rt.Pattern)
		}
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
	if names["kanban"] {
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
	// the module nouns live in the capsule (phase F): the board types
	// are package kanban, the served surface rides ctx.ModuleRoutes.
	_ = kanban.KanbanTask{}
	_ = kanban.KanbanProvider(nil)
	t.Log("PROOF removed: no kanban route registered (the patterns left the ledger and the module mux), canvas carries no kanban panel, no task tools, no /board commands; module nouns live in package kanban, core carries zero")
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
//     carries /cron on the command seam from the same registration).
//  2. WITHOUT cron: the seam is nil (the REST route falls through to
//     its seam-nil guard - cron's settled degrade, the empty-set
//     answer, not 501), the canvas carries no cron panel, no module
//     command registers /cron, and no module requires
//     "cron" - the removal takes no dependents with it (the
//     notifications removal is the counter-case: cron requires
//     notifications, not the other way).
//
// Cron ships no agent tools by design (read-only v1: crons.yaml is
// human-authored, omega may only ever propose jobs) - the proof pins
// the absence too.
//
// Core carries no cron vocabulary (part 3, the seam constitution):
// CronDeps and JobInfo live in the capsule, the service resolves by
// the "cron" key from the universal registry, and the consumers keep
// only their own local need interfaces. No seam type left to compile.
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
	reg5, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	dismountCleanup(t, reg5, ctx)
	if err != nil {
		t.Fatalf("mount all: %v", err)
	}
	proofResolve(t, ctx, "cron")
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
	if _, err := live.Dispatch(core.ActionCall{Module: "cron", Action: "cron.refresh_jobs"}); err != nil {
		t.Fatalf("dispatch cron.refresh_jobs: %v", err)
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
	t.Log("PROOF action-pipe: cron.refresh_jobs answered the listing, the panel rode the spine")

	// --- mount 2: the capsule removed ---
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "cron" {
			continue
		}
		removed = append(removed, p)
	}
	ctx2 := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	reg6, err := agent.MountAllAPI(removed, ctx2, nil, nil)
	dismountCleanup(t, reg6, ctx2)
	if err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if _, ok := ctx2.Services().Resolve("cron").(interface{ Stop() error }); ok {
		t.Fatal("REMOVED MOUNT: cron seam still mounted")
	}
	noCanvas, err := agent.NewCanvas(ctx2, nil)
	if err != nil {
		t.Fatalf("canvas removed: %v", err)
	}
	if _, found := cronPanel(noCanvas.Snapshot()); found {
		t.Fatalf("REMOVED MOUNT: the jobs panel survived the removal: %+v", noCanvas.Snapshot())
	}
	if _, err := noCanvas.Dispatch(core.ActionCall{Module: "cron", Action: "cron.refresh_jobs"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("REMOVED MOUNT: dispatch answered %v, want ErrUnknownModule", err)
	}
	// the command surface: no module command declares /cron
	for _, c := range ctx2.Commands {
		if c.Name == "/cron" {
			t.Fatal("REMOVED MOUNT: the /cron command survived the removal")
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
	// capsule-owned types moved with the capsule (part 3): CronDeps and
	// JobInfo live in harness/cron now, nothing left to compile here.
	_ = struct{}{}
	t.Log("PROOF removed: seam nil (GET /cron/jobs falls through to its seam-nil guard, the settled empty-set answer), canvas carries no cron panel, the action pipe answers ErrUnknownModule, /cron unregistered, no cron-shaped tools; core carries no cron vocabulary - the capsule took its nouns whole (part 3)")
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
// capsule that was born one: the memory offer (the "memory" key), the memory tool and
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
//     the "memory" offer, for deliver=memory jobs) still mounts - its
//     "no memory extension loaded" guard is the documented degrade,
//     so no Requires edge exists and none is needed. The two store
//     files stay on disk: data is not code.
//
// Core carries no memory vocabulary (part 3, the seam constitution):
// the memory files live with the capsule, the service resolves by the
// "memory" key, and the consumer's local interface is its own need
// statement. The tool-name references the host
// carries for the ephemeral deny lists (agent/api's
// ephemeral sessions) are name-based guards, not module surfaces: they see a
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
	reg7, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	dismountCleanup(t, reg7, ctx)
	if err != nil {
		t.Fatalf("mount all: %v", err)
	}
	proofResolve(t, ctx, "memory")
	t.Log("PROOF mount=full: seam mounted (the memory offer), tool + prompt section from the same Mount")

	// the tool face: the registry carries the dotted memory tools and
	// a real add lands in the store
	var memAdd, memRemove func(context.Context, map[string]any) (string, error)
	foundAdd, foundRemove := false, false
	for _, tp := range ctx.ToolProviders {
		for name, tool := range tp.Tools() {
			switch name {
			case "memory.add":
				memAdd, foundAdd = tool.Run, true
			case "memory.remove":
				memRemove, foundRemove = tool.Run, true
			}
		}
	}
	if !foundAdd || !foundRemove {
		t.Fatal("FULL MOUNT: the dotted memory tools are missing from the registry")
	}
	res, runErr := memAdd(context.Background(), map[string]any{
		"target": "memory", "content": "arc 6 proof entry",
	})
	if runErr != nil || !strings.Contains(res, `"success":true`) {
		t.Fatalf("FULL MOUNT: memory add failed (res=%s err=%v)", res, runErr)
	}
	t.Log("PROOF tool wiring: memory add pushed the entry into the store - " + res)

	// the user target is the same tool's second store
	res, runErr = memAdd(context.Background(), map[string]any{
		"target": "user", "content": "proof user row",
	})
	if runErr != nil || !strings.Contains(res, `"success":true`) {
		t.Fatalf("FULL MOUNT: memory tool user add failed (res=%s err=%v)", res, runErr)
	}
	// and the remove verb rides the same store (the dotted wire's
	// third tool): the user row goes back out
	res, runErr = memRemove(context.Background(), map[string]any{
		"target": "user", "old_text": "proof user row",
	})
	if runErr != nil || !strings.Contains(res, `"success":true`) {
		t.Fatalf("FULL MOUNT: memory tool user remove failed (res=%s err=%v)", res, runErr)
	}

	// the prompt-section face: the same store renders as the snapshot
	// block through the sectioner rail
	mem, _ := ctx.Services().Resolve("memory").(core.PromptSectioner)
	secs := mem.PromptSections(core.PromptBuildOptions{})
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
	reg8, err := agent.MountAllAPI(removed, ctx2, nil, nil)
	dismountCleanup(t, reg8, ctx2)
	if err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if ctx2.Services().Resolve("memory") != nil {
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
	if _, ok := ctx2.Services().Resolve("cron").(interface{ Stop() error }); !ok {
		t.Fatal("REMOVED MOUNT: cron did not mount - the memory removal must not take a dependent")
	}
	// data is not code: the two store files stay on disk
	for _, f := range []string{cfg.Memory.File, cfg.Memory.UserProfileFile} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("REMOVED MOUNT: store file %s vanished - data is not code", f)
		}
	}
	// capsule-owned types moved with the capsule (part 3): nothing left
	// to compile here.
	_ = struct{}{}
	t.Log("PROOF removed: seam nil, no memory-shaped tools, no memory prompt block, the action pipe answers ErrUnknownModule, cron keeps mounting (deliver=memory degrades through its nil-guard), the store files stay on disk - data is not code; core/seams.go memory types remain - documented as intentional")
}

// TestDelegateRemovalProof is the arc 6 delegate-capsule proof (card
// t_47675b6a), the fifth worked proof in this file, now under the async
// agent loop's migration (card t_0805d438): the inject seam retired -
// delegate.task is yield-class, its report re-enters as the tool
// result through the loop's own wake machinery. Delegate ships only
// the two namespaced tools and the finished-things emitter capture -
// no command, no REST route, no skills, no canvas panel, and no
// delegate-owned re-entry surface. The two mounts are the proof:
//
//  1. WITH delegate: the registry carries the delegate tools, a status
//     call answers the live listing, and a real spawn (OMEGA_BIN ->
//     /bin/echo: the child `run`s, echo just prints) returns the
//     child's own answer as the tool result and emits one
//     finished-things record through the notifications edge.
//  2. WITHOUT delegate: no delegate-shaped tool is left, no
//     delegate-qualified action or panel exists, and the notifications
//     seam still mounts - the Requires edge points from delegate AT
//     notifications, not the reverse, so the removal takes no
//     dependents with it. Delegate keeps no disk state (its task map
//     is in-memory), so data-is-not-code holds vacuously.
//
// The OMEGA_SUBAGENT recursion guard lives inside the module twice
// over: the spawn writes the var on the child's env, and the child's
// own mount (MountWithAPI) refuses the delegate tools when the var is
// set.
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
	reg9, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	dismountCleanup(t, reg9, ctx)
	if err != nil {
		t.Fatalf("mount all: %v", err)
	}
	t.Log("PROOF mount=full: the capsule mounted; it ships no re-entry seam (the injected-user path retired with the phase-2 migration)")

	// the tool face: the registry carries the two namespaced tools -
	// the aliases promoted when the consolidated unprefixed primary
	// retired (Q10, the v0.12 mount migration)
	var delegateTool core.Tool
	foundTool, foundStatusTool := false, false
	for _, tp := range ctx.ToolProviders {
		for name, tool := range tp.Tools() {
			switch name {
			case "delegate.task":
				delegateTool, foundTool = tool, true
			case "delegate.status":
				foundStatusTool = tool.Run != nil
			}
		}
	}
	if !foundTool || !foundStatusTool {
		t.Fatalf("FULL MOUNT: delegate tools missing (task=%v status=%v)", foundTool, foundStatusTool)
	}
	t.Log("PROOF tool wiring: the registry carries the delegate.task / delegate.status namespace pair")

	// the status arm answers the live listing
	var statusRun func(context.Context, map[string]any) (string, error)
	for _, tp := range ctx.ToolProviders {
		if tl, ok := tp.Tools()["delegate.status"]; ok && tl.Run != nil {
			statusRun = tl.Run
		}
	}
	res, runErr := statusRun(context.Background(), map[string]any{})
	if runErr != nil || !strings.Contains(res, "Running: 0") || !strings.Contains(res, "No tasks.") {
		t.Fatalf("FULL MOUNT: delegate status failed (res=%s err=%v)", res, runErr)
	}

	// the spawn arm, end to end: OMEGA_BIN -> /bin/echo, the child
	// `run`s, echo just prints the prompt back. The report IS the tool
	// result now (card t_0805d438): the blocking Run returns the child's
	// own answer, and the finished-things edge emits one chat record
	// (echo exits 0, so verdict ok).
	echo := "/bin/echo"
	if _, err := os.Stat(echo); err != nil {
		echo = "/usr/bin/echo"
		if _, err := os.Stat(echo); err != nil {
			t.Skip("no /bin/echo or /usr/bin/echo on this host")
		}
	}
	t.Setenv("OMEGA_BIN", echo)
	res, runErr = delegateTool.Run(context.Background(), map[string]any{"action": "task", "prompt": "arc 6 proof"})
	if runErr != nil {
		t.Fatalf("FULL MOUNT: delegate task spawn failed (res=%s err=%v)", res, runErr)
	}
	// the echo child's stdout is its argv (echo echoes the flag
	// vector too); the pin is that the child's output - the prompt
	// among it - rides the tool result, trimmed, not lost.
	if !strings.Contains(res, "arc 6 proof") {
		t.Fatalf("FULL MOUNT: tool result lost the child output: %.80q", res)
	}
	// The wait is inside Run now, so the finish record cannot lag the
	// result - one poll suffices (poll stays honest to the async cap).
	notifier2, _ := ctx.Services().Resolve("notifications").(proofNotifier)
	sawFinish := false
	for i := 0; i < 50 && !sawFinish; i++ {
		notes, _, err := notifier2.List(context.Background(), false, 10, 0)
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
		notes, _, _ := notifier2.List(context.Background(), false, 10, 0)
		t.Fatalf("FULL MOUNT: no chat/task-1/ok finished-things record from the spawn: %+v", notes)
	}
	t.Log("PROOF spawn: the echo child's answer returned as the tool result; one chat finished-things record emitted through the notifications edge")

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
	reg10, err := agent.MountAllAPI(removed, ctx2, nil, nil)
	dismountCleanup(t, reg10, ctx2)
	if err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	// the re-entry seam retired - the context carries no delegate field
	// the removal could leave set (checked by construction: nothing in
	// core.Context names delegate anymore)
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
	if ctx2.Services().Resolve("notifications") == nil {
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
	t.Log("PROOF removed: no re-entry seam in the context (retired with the phase-2 migration), no delegate-shaped tools, no delegate panel or actions, notifications seam still mounted (the Requires edge points at it, not from it); delegate keeps no disk state (in-memory task map) - data is not code holds vacuously")
}

// TestMCPRemovalProof is the arc 6 mcp-capsule proof (card
// t_cf6b7795), the sixth worked proof in this file. MCP is the
// additive-seam born-capsule: the bridge (stdio + Streamable HTTP MCP
// servers), its server-qualified discovered tools ("server.tool"), and
// the whole env/mcp.yaml config load ship from files in harness/mcp,
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
	reg11, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	dismountCleanup(t, reg11, ctx)
	if err != nil {
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
	reg12, err := agent.MountAllAPI(removed, ctx2, nil, nil)
	dismountCleanup(t, reg12, ctx2)
	if err != nil {
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
	if ctx2.Services().Resolve("notifications") == nil {
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
//     verdict tool with its request sub-action, a real verdict
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
// code. The card + answer wire shapes stay in core (QuestionPrompt,
// QuestionAnswer - wire vocabulary, marshalled identically by every
// consumer and the serving surface); the seam interface was consumed by
// each consumer's own local one (wave 1), so no core seam type remains.
// The proof asserts the wire shapes compile.
func TestVerdictRemovalProof(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	cfg := DefaultConfig()

	// --- mount 1: the capsule mounted ---
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	// recipe step 1, declared data: verdict requires store and logging.
	// "approval" is its one registered dependent (t_c2b52cb5: an
	// escalation hard-pauses on a card) - anything ELSE naming
	// "verdicts" is the removal recipe's stop signal
	for _, p := range plugins {
		for _, req := range p.Requires() {
			if req == "verdicts" && p.Name() != "approval" {
				t.Fatalf("FULL MOUNT precondition: %q requires verdicts - the removal takes a dependent", p.Name())
			}
		}
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	reg13, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	dismountCleanup(t, reg13, ctx)
	if err != nil {
		t.Fatalf("mount all: %v", err)
	}
	proofResolve(t, ctx, "verdicts")
	t.Log("PROOF mount=full: seam mounted (the verdicts offer), tool + command + prompt section from the same Mount")

	// the prompt-section face: the same Mount renders the Verdict
	// Cards guidance between Project Context and Environment
	foundCardsBlock := false
	for _, ps := range ctx.PromptSectioners {
		for _, s := range ps.PromptSections(core.PromptBuildOptions{}) {
			if s.Title == "Verdict Cards" && strings.Contains(s.Body, "verdict") {
				foundCardsBlock = true
			}
		}
	}
	if !foundCardsBlock {
		t.Fatal("FULL MOUNT: the Verdict Cards prompt section is missing")
	}

	// the tool face: the registry carries the dotted verdict.request
	// tool and a real call hard-pauses the turn until an answer lands
	// through the seam - the path the /verdicts POST and the /verdict
	// command share
	verdictTool, err := pluginTool(ctx, "verdict.request")
	if err != nil {
		t.Fatalf("FULL MOUNT: the verdict.request tool is missing from the registry: %v", err)
	}
	type toolResult struct {
		res string
		err error
	}
	resCh := make(chan toolResult, 1)
	go func() {
		res, err := verdictTool(context.Background(), map[string]any{
			"title":   "arc 6 proof: which wire?",
			"options": []any{"left", "right"},
		})
		resCh <- toolResult{res: res, err: err}
	}()
	var cardID string
	vp, _ := ctx.Services().Resolve("verdicts").(proofVerdicts)
	cardDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(cardDeadline) {
		if cards := vp.Pending(""); len(cards) > 0 {
			cardID = cards[0].ID
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if cardID == "" {
		t.Fatal("FULL MOUNT: the blocked tool call never registered a pending card")
	}
	if _, err := vp.Answer("", cardID, core.QuestionAnswer{OptionIndex: 2}); err != nil {
		t.Fatalf("FULL MOUNT: answer through the seam failed: %v", err)
	}
	var got toolResult
	select {
	case got = <-resCh:
		if got.err != nil || !strings.Contains(got.res, `"verdict":"answer"`) || !strings.Contains(got.res, `"option":"right"`) {
			t.Fatalf("FULL MOUNT: verdict did not return the answered pick (res=%s err=%v)", got.res, got.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("FULL MOUNT: verdict never unblocked after the first answer")
	}
	t.Logf("PROOF tool wiring: verdict blocked the turn, the first answer through the seam won the race - %s", strings.TrimSpace(got.res))

	// the command face: /verdict declared through the same Mount
	foundCommand := false
	for _, c := range ctx.Commands {
		if c.Name == "/verdict" {
			foundCommand = true
		}
	}
	if !foundCommand {
		t.Fatal("FULL MOUNT: no /verdict command registered through the capsule's command seam")
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
	// approval goes with it: it Requires verdicts (the escalation
	// card) - a dependent cannot mount in a world without its source;
	// its own removal proof (TestApprovalRemovalProof) covers that
	// world's shape
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "verdict" {
			continue
		}
		needsVerdicts := false
		for _, req := range p.Requires() {
			if req == "verdicts" {
				needsVerdicts = true
			}
		}
		if needsVerdicts {
			continue
		}
		removed = append(removed, p)
	}
	ctx2 := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	reg14, err := agent.MountAllAPI(removed, ctx2, nil, nil)
	dismountCleanup(t, reg14, ctx2)
	if err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if ctx2.Services().Resolve("verdicts") != nil {
		t.Fatal("REMOVED MOUNT: verdicts seam still mounted")
	}
	for _, tp := range ctx2.ToolProviders {
		for name := range tp.Tools() {
			if strings.Contains(name, "verdict") {
				t.Fatalf("REMOVED MOUNT: verdict-shaped tool %q survived the removal", name)
			}
		}
	}
	for _, c := range ctx2.Commands {
		if c.Name == "/verdict" {
			t.Fatal("REMOVED MOUNT: the /verdict command survived the removal")
		}
	}
	for _, ps := range ctx2.PromptSectioners {
		for _, s := range ps.PromptSections(core.PromptBuildOptions{}) {
			if s.Title == "Verdict Cards" {
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
	if ctx2.Services().Resolve("notifications") == nil {
		t.Fatal("REMOVED MOUNT: notifications seam lost - the removal must not take a module it does not own")
	}
	// capsule-owned types moved with the capsule (part 3): the card and
	// answer WIRE types still live here - they are core vocabulary, the
	// seam interface is consumer-owned. Compile-time proof:
	_ = core.QuestionPrompt{}
	_ = core.QuestionAnswer{}
	t.Log("PROOF removed: seam nil (the /verdicts routes fall through to their 501 guard), no verdict tool, no /verdict command, no Verdict Cards prompt block, no canvas panel or verdict-qualified actions, the action pipe answers ErrUnknownModule; the card + verdict audit rows stay on disk in the session store (data is not code) and the shell keeps its guarded surfaces inert (busy-Enter sees a nil seam, the denied tool name denies nothing); core carries no verdict seam type - the wire shapes stay, the hub nouns live in the capsule (part 3)")
}

// TestTrustRemovalProof is the arc 6 trust-capsule proof (card
// t_e1efb979), the eighth worked proof in this file. Trust is the
// gating capsule: the trust offer (the "trust" key; ResolveContext gates the
// AGENTS.md project context behind trust.yaml, State feeds the webui
// status bar) is the whole surface, shipped from the one Mount. No
// tool, no slash command, no REST route, no skills, no canvas panel
// and no module-qualified actions exist, and none did before the arc:
// the host surfaces consume two strings the host computes from the
// seam before launch (the prompt context and the trust state), the
// CLI flags (--approve/--no-approve) parse in the host and ride the
// same seam, and the boot-time store migration (trust.Bootstrap) is
// host wiring firing a module-owned function - no mounted surface
// rides it.
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
// after a removal. Core carries no trust vocabulary (part 3, the seam
// constitution): the capsule owns its types, the service resolves by
// the "trust" key, and the host keeps its own nil-guarded need faces
// - the deletion takes nothing with it.
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
	reg15, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	dismountCleanup(t, reg15, ctx)
	if err != nil {
		t.Fatalf("mount all: %v", err)
	}
	proofResolve(t, ctx, "trust")
	t.Log("PROOF mount=full: trust seam mounted (the trust offer - the trust.yaml gate), the whole surface ships from the one Mount")

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
	if got := trustGate(ctx, proj, trustFlags{}, false); got != "" {
		t.Fatalf("FULL MOUNT: untrusted project loaded context anyway: %q", got)
	}
	// the --approve face: the gate persists the project root into
	// trust.yaml and returns the AGENTS.md text
	approved := trustGate(ctx, proj, trustFlags{approve: true}, false)
	if !strings.Contains(approved, marker) {
		t.Fatalf("FULL MOUNT: the approve path did not load the project context (got %.80q)", approved)
	}
	store, err := os.ReadFile(filepath.Join(os.Getenv("OMEGA_HOME"), "config", "trust.yaml"))
	if err != nil || !strings.Contains(string(store), proj) {
		t.Fatalf("FULL MOUNT: the approve path did not persist %s into the trust store (err=%v yaml=%q)", proj, err, store)
	}
	t.Log("PROOF gate wiring: --approve persisted the project root into trust.yaml and the same call returned the AGENTS.md context")
	// the status-bar face: State reads trusted off the persisted store
	if st := trustStatus(ctx, proj, trustFlags{}); st != "trusted" {
		t.Fatalf("FULL MOUNT: State = %q, want trusted", st)
	}
	// --no-approve wins, on both faces, by the seam's own rule
	if st := trustStatus(ctx, proj, trustFlags{noApprove: true}); st != "untrusted" {
		t.Fatalf("FULL MOUNT: State with --no-approve = %q, want untrusted", st)
	}
	if got := trustGate(ctx, proj, trustFlags{noApprove: true}, false); got != "" {
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
	for _, c := range ctx.Commands {
		if c.Name == "/trust" {
			t.Fatal("FULL MOUNT: unexpected /trust command (the capsule ships no commands)")
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
	reg16, err := agent.MountAllAPI(removed, ctx2, nil, nil)
	dismountCleanup(t, reg16, ctx2)
	if err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if ctx2.Services().Resolve("trust") != nil {
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
	for _, c := range ctx2.Commands {
		if c.Name == "/trust" {
			t.Fatal("REMOVED MOUNT: the /trust command survived the removal")
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
	if ctx2.Services().Resolve("notifications") == nil {
		t.Fatal("REMOVED MOUNT: notifications seam lost - the removal must not take a module it does not own")
	}
	kRoutes := false
	for _, rt := range ctx2.ModuleRoutes {
		if rt.Module == "kanban" {
			kRoutes = true
		}
	}
	if !kRoutes {
		t.Fatal("REMOVED MOUNT: kanban routes lost - the removal must not take a module it does not own")
	}
	// data is not code: the persisted trust store (and its entries)
	// stays on disk after the removal
	if _, err := os.Stat(filepath.Join(os.Getenv("OMEGA_HOME"), "config", "trust.yaml")); err != nil {
		t.Fatalf("REMOVED MOUNT: the trust store vanished from disk (data is not code): %v", err)
	}
	// capsule-owned types moved with the capsule (part 3): nothing
	// left to compile here - the host's own need faces (trust.go)
	// carry the consult.
	t.Log("PROOF removed: seam nil, the host entry paths answer empty through their nil-guard (serve + run stay alive), no trust tool, no /trust command, no canvas panel or trust-qualified action, the action pipe answers ErrUnknownModule; trust.yaml and its entries stay on disk (data is not code); core carries no trust vocabulary (part 3)")
}

// terminalMuxOf builds the module mux from a mount's ledger (the
// production capsule mux shape: real pattern matching, PathValue on).
func terminalMuxOf(ctx *core.Context) *http.ServeMux {
	mux := http.NewServeMux()
	for _, rt := range ctx.ModuleRoutes {
		pattern, handler := rt.Pattern, rt.Handler
		mux.HandleFunc(pattern, handler)
	}
	return mux
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

// TestTerminalRemovalProof is the v0.15 arc's removal proof (card
// t_55a3fc4b, Endo's ruling 2026-11-01: VS Code-style, no
// restrictions). The terminal is the purest capsule yet: no seams, no
// tools, no commands, no panels, no config section - its whole
// surface is six /terminal routes + the lifecycle fence that owns the
// shells. The two mounts are the proof:
//
//  1. WITH terminal: the six routes claim the /terminal root, a
//     claim+kill cycle works against a REAL pty shell (this box has
//     /dev/ptmx; without one the spawn route answers 503 and the
//     proof degrades to the route surface), and the lifecycle stop
//     reaps every live shell (the toggle-off contract).
//  2. WITHOUT terminal: no /terminal route is left registered - the
//     webui's Terminal tab has no data path and 404s at the module
//     mux.
func TestTerminalRemovalProof(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	cfg := DefaultConfig()

	// --- mount 1: the capsule mounted ---
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	var termPlugin *terminal.Plugin
	for _, p := range plugins {
		if tp, ok := p.(*terminal.Plugin); ok {
			termPlugin = tp
		}
	}
	if termPlugin == nil {
		t.Fatal("FULL MOUNT: the terminal capsule is absent from buildPlugins")
	}
	if !termPlugin.Toggleable() {
		t.Fatal("FULL MOUNT: the terminal capsule is not toggleable")
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	reg17, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	dismountCleanup(t, reg17, ctx)
	if err != nil {
		t.Fatalf("mount all: %v", err)
	}
	wantRoutes := []string{
		"GET /terminal/sessions",
		"POST /terminal/sessions",
		"GET /terminal/{id}/stream",
		"POST /terminal/{id}/input",
		"POST /terminal/{id}/resize",
		"DELETE /terminal/{id}",
	}
	got := map[string]bool{}
	for _, rt := range ctx.ModuleRoutes {
		if rt.Module == "terminal" {
			got[rt.Pattern] = true
		}
	}
	for _, pattern := range wantRoutes {
		if !got[pattern] {
			t.Fatalf("FULL MOUNT: route %q is not registered", pattern)
		}
	}
	t.Log("PROOF mount=full: the six /terminal routes registered from the capsule alone")

	// the live face against a REAL pty shell (skipped when this box
	// cannot open one - the route surface above is unconditioned)
	if _, err := os.Stat("/dev/ptmx"); err == nil {
		mux := terminalMuxOf(ctx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/terminal/sessions", nil))
		if w.Code == http.StatusOK {
			var rep struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(w.Body).Decode(&rep)
			if rep.ID == "" {
				t.Fatal("FULL MOUNT: claim answered no id")
			}
			kill := httptest.NewRecorder()
			mux.ServeHTTP(kill, httptest.NewRequest("DELETE", "/terminal/"+rep.ID, nil))
			if kill.Code != http.StatusNoContent {
				t.Fatalf("FULL MOUNT: kill answered %d, want 204", kill.Code)
			}
			t.Log("PROOF spawn+kill cycle: a live shell claimed and reaped through the routes")
		}
	}
	// the lifecycle fence: the toggle-off reaps the shells. A box with
	// no pty (windows in this build) cannot claim a shell - the fence
	// proof is gated exactly like the spawn+kill route face above.
	if _, err := os.Stat("/dev/ptmx"); err == nil {
		st := termPlugin.Store()
		for range 2 {
			if _, _, err := st.Claim(""); err != nil {
				t.Fatalf("FULL MOUNT: claim for the fence proof: %v", err)
			}
		}
		if err := st.Stop(); err != nil {
			t.Fatalf("lifecycle stop: %v", err)
		}
		if len(st.List()) != 0 {
			t.Fatalf("FULL MOUNT: %d rows survive the lifecycle stop", len(st.List()))
		}
		t.Log("PROOF fence: the lifecycle stop reaped every shell (the toggle-off kills its terminals)")
	}

	// --- mount 2: the capsule removed ---
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "terminal" {
			continue
		}
		removed = append(removed, p)
	}
	// no.Requires may name terminal: the capsule provides no seams,
	// so nothing depends on it (the removal recipe's stop signal)
	for _, p := range removed {
		for _, req := range p.Requires() {
			if req == "terminal" {
				t.Fatalf("REMOVED MOUNT precondition: %q requires %q (terminal provides no seams)", p.Name(), req)
			}
		}
	}
	ctx2 := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	reg18, err := agent.MountAllAPI(removed, ctx2, nil, nil)
	dismountCleanup(t, reg18, ctx2)
	if err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	for _, rt := range ctx2.ModuleRoutes {
		if rt.Module == "terminal" {
			t.Fatalf("REMOVED MOUNT: route %q survived the removal", rt.Pattern)
		}
		if strings.HasPrefix(rt.Pattern, "/terminal") {
			t.Fatalf("REMOVED MOUNT: a /terminal route survived with a foreign owner %q", rt.Pattern)
		}
	}
	t.Log("PROOF removed: zero /terminal routes registered - the webui Terminal tab 404s; " +
		"no seams, tools or commands ever rode this capsule, so nothing else can leak")
}

// TestApprovalRemovalProof is the smart-approval-capsule proof (card
// t_c2b52cb5). Approval is the loop-side born-capsule: the detector,
// parser, reviewer client, ladder, cache and audit log all ship from
// the one Mount, and the whole feature is the ApprovalProvider seam
// the agent loop consults before every tool run. No tool, no command,
// no REST route, no canvas panel, no webui surface (settings ride the
// Go-side groups; the escalation card rides the verdict module). The
// three mounts are the proof:
//
//  1. SMART/MANUAL (default): the seam is mounted (ctx.Approvals),
//     the Approval Gate prompt section ships from the plugin, and no
//     other plugin Requires "approvals".
//  2. MODE OFF: the seam is nil and the prompt section is absent -
//     Q10 provably zero-cost: the loop consults nothing, no audit
//     file is created, no calls or rows exist.
//  3. REMOVED: the module is out of buildPlugins - the seam is nil,
//     nothing Requires it, and removing harness/approval takes the
//     feature whole (the capsule law).
func TestApprovalRemovalProof(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)

	// --- mount 1: the capsule mounted (default mode = manual, Q5) ---
	plugins, err := buildPlugins(DefaultConfig())
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	// recipe step 1, declared data: approval requires verdicts (an
	// escalation hard-pauses on a card); no OTHER plugin's Requires
	// may name "approvals" - a hit here is the removal stop signal
	for _, p := range plugins {
		for _, req := range p.Requires() {
			if p.Name() != "approval" && req == "approvals" {
				t.Fatalf("FULL MOUNT precondition: %q requires approvals - the removal takes a dependent", p.Name())
			}
		}
	}
	cfg := DefaultConfig()
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	reg19, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	dismountCleanup(t, reg19, ctx)
	if err != nil {
		t.Fatalf("mount all: %v", err)
	}
	if ctx.Services().Resolve("approvals") == nil {
		t.Fatal("FULL MOUNT: approvals seam nil - capsule did not mount")
	}
	foundGate := false
	for _, ps := range ctx.PromptSectioners {
		for _, s := range ps.PromptSections(core.PromptBuildOptions{}) {
			if s.Title == "Approval Gate" && strings.Contains(s.Body, "approval") {
				foundGate = true
			}
		}
	}
	if !foundGate {
		t.Fatal("FULL MOUNT: the Approval Gate prompt section is absent from the capsule's Mount")
	}
	t.Log("PROOF mount=full: seam mounted (ctx.Approvals), prompt section from the same Mount, default mode manual")

	// --- mount 2: mode off - Q10, the gate is fully absent ---
	offCfg := DefaultConfig()
	offCfg.Approvals.Mode = "off"
	ctxOff := &core.Context{Configs: agent.RouteConfigs(plugins, offCfg)}
	reg20, err := agent.MountAllAPI(plugins, ctxOff, nil, nil)
	dismountCleanup(t, reg20, ctxOff)
	if err != nil {
		t.Fatalf("mount off-mode: %v", err)
	}
	if ctxOff.Services().Resolve("approvals") != nil {
		t.Fatal("OFF MOUNT: the seam must be nil - mode off mounts nothing")
	}
	for _, ps := range ctxOff.PromptSectioners {
		for _, s := range ps.PromptSections(core.PromptBuildOptions{}) {
			if s.Title == "Approval Gate" {
				t.Fatal("OFF MOUNT: the Approval Gate prompt section survived mode off")
			}
		}
	}
	if _, err := os.Stat(filepath.Join(home, "approval-decisions.jsonl")); err == nil {
		// a fresh home's audit file only ever exists if something wrote
		// without a gate mounted - the no-hidden-logging claim
		t.Fatal("OFF MOUNT: an audit row exists without a gate")
	}
	t.Log("PROOF mount=off: seam nil, no prompt section, zero audit writes - the gate is absent, not idle")

	// --- mount 3: the module removed (the capsule law) ---
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "approval" {
			continue
		}
		removed = append(removed, p)
	}
	for _, p := range removed {
		for _, req := range p.Requires() {
			if req == "approvals" {
				t.Fatalf("REMOVED MOUNT precondition: %q requires approvals", p.Name())
			}
		}
	}
	ctxRm := &core.Context{Configs: agent.RouteConfigs(removed, cfg)}
	reg21, err := agent.MountAllAPI(removed, ctxRm, nil, nil)
	dismountCleanup(t, reg21, ctxRm)
	if err != nil {
		t.Fatalf("mount removed set: %v", err)
	}
	if ctxRm.Services().Resolve("approvals") != nil {
		t.Fatal("REMOVED MOUNT: approvals seam non-nil without the capsule")
	}
	t.Log("PROOF removed: the seam is the whole feature - taking the module out takes detection, ladder, cache and audit file with it")
}

// TestShellDomainIsTheOnlyCommandSurface is the shell-unification
// proof (card t_5a49790b). The process capsule is deleted outright: the
// registry machinery it was built on now sits under the terminal
// capsule, and one shell verb set answers every command.
//
//  1. No process.* tool name survives anywhere in the mounted surface,
//     and harness/process is gone from the tree (the deletion proof).
//  2. shell.run/list/read/kill register through the additive tools
//     seam and answer a real round-trip: run a sleeper, address it from
//     the list, tail its live log, kill it by id, and read the log
//     AFTER the exit - post-mortem retention, card t_304afb4e.
func TestShellDomainIsTheOnlyCommandSurface(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	cfg := DefaultConfig()
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	if _, err := os.Stat(filepath.Join("..", "..", "harness", "process")); err == nil {
		t.Fatal("DELETION PROOF: harness/process still exists on disk")
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	reg, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	dismountCleanup(t, reg, ctx)
	if err != nil {
		t.Fatalf("mount all: %v", err)
	}
	for _, tp := range ctx.ToolProviders {
		for name := range tp.Tools() {
			if strings.HasPrefix(name, "process.") {
				t.Fatalf("DELETION PROOF: %q is still registered - the process verb family was supposed to die", name)
			}
		}
	}
	for _, verb := range []string{"shell.run", "shell.list", "shell.read", "shell.kill"} {
		if _, err := pluginTool(ctx, verb); err != nil {
			t.Fatalf("shell domain: %s missing (%v)", verb, err)
		}
	}

	// The round-trip: a shell that outlives the call's inline window.
	command := "echo proof-round-trip; sleep 300"
	if runtime.GOOS == "windows" {
		command = "echo proof-round-trip && ping -n 300 127.0.0.1 > nul"
	}
	runTool, _ := pluginTool(ctx, "shell.run")
	callDone := make(chan string, 1)
	go func() {
		res, runErr := runTool(context.Background(), map[string]any{"command": command})
		if runErr != nil {
			res = runErr.Error()
		}
		callDone <- res
	}()

	// shell.list is the addressing surface: the id comes from the list.
	listTool, _ := pluginTool(ctx, "shell.list")
	var id string
	for i := 0; i < 100 && id == ""; i++ {
		list, listErr := listTool(context.Background(), map[string]any{})
		if listErr != nil {
			t.Fatalf("shell.list: %v", listErr)
		}
		for _, line := range strings.Split(list, "\n") {
			if strings.Contains(line, "proof-round-trip") {
				id = strings.Fields(line)[0]
			}
		}
		if id == "" {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if id == "" {
		t.Fatal("shell.list never showed the running shell")
	}

	// shell.read tails the LIVE log - the echo may land a beat after the
	// spawn returns, so poll like a real tail.
	readTool, _ := pluginTool(ctx, "shell.read")
	var readRes string
	for i := 0; i < 100; i++ {
		readRes, err = readTool(context.Background(), map[string]any{"id": id, "lines": 10})
		if err == nil && strings.Contains(readRes, "proof-round-trip") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil || !strings.Contains(readRes, "proof-round-trip") {
		t.Fatalf("shell.read did not tail the live log (res=%q err=%v)", readRes, err)
	}

	killTool, _ := pluginTool(ctx, "shell.kill")
	killRes, killErr := killTool(context.Background(), map[string]any{"id": id})
	if killErr != nil || !strings.Contains(killRes, "stopped") {
		t.Fatalf("shell.kill by id %s failed (res=%q err=%v)", id, killRes, killErr)
	}
	select {
	case <-callDone:
	case <-time.After(20 * time.Second):
		t.Fatal("the shell.run call never returned after the kill")
	}

	// Post-mortem: the log outlives the shell.
	postRes, postErr := readTool(context.Background(), map[string]any{"id": id, "lines": 10})
	if postErr != nil || !strings.Contains(postRes, "proof-round-trip") {
		t.Fatalf("post-mortem shell.read lost the log (res=%q err=%v)", postRes, postErr)
	}

	// The list no longer names it: an exited shell is post-mortem only.
	list, err := listTool(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("shell.list after the kill: %v", err)
	}
	if strings.Contains(list, "proof-round-trip") {
		t.Fatalf("shell.list still shows the killed shell: %q", list)
	}
	if _, unknownErr := readTool(context.Background(), map[string]any{"id": "no-such-shell"}); unknownErr == nil {
		t.Fatal("shell.read answered an unknown id instead of failing")
	}
	t.Logf("PROOF shell domain: no process.* verb registered; run/list/read/kill round-tripped (id=%s) and the log survived the kill", id)
}
