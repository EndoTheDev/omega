package http_channel

// The live-toggle storm pin (card t_7ee284e8): the Oct-27 lane ran a
// toggle storm (kanban, memory, notifications, verdict, mcp, delegate,
// web, trust) and the chat panel died for the whole boot - every push
// logging `canvas: unknown module: "http_channel"` until the next
// restart. The old RegisterPanelProvider journal undid with a slice
// truncation captured at boot time, so dismounting a module mounted
// EARLY chopped every provider appended later - the Start-time chat
// registration included - and Canvas.Resync then deleted the namespace
// while the capsule's closure kept pushing into the void. The journal
// undo is removeOne since d73fecd; this test walks the REAL surfaces -
// MountAllAPI, Dismount, remount, the channel's RewireSeams - to pin
// the storm: the engine-ring namespace routes and pushes before, after
// the disable and after the re-enable.

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/modules/store"
)

// stormPanel is one toggleable module's live panel provider.
type stormPanel struct {
	mod string
	// update is the PanelUpdateSink closure the canvas runtime
	// installs at registration - the capsule pushes through its own
	// faces with it (kanban's capsule law); a nil sink drops pushes
	// silently, so the test asserts the LAND, not the call.
	update func(core.Panel) error
}

func (p *stormPanel) Panels() []core.Panel {
	return []core.Panel{{
		ID:      "board",
		Title:   "Board",
		Kind:    "storm.board",
		Actions: []core.PanelAction{{ID: p.mod + ".refresh", Label: "refresh", Module: p.mod}},
	}}
}
func (p *stormPanel) HandleAction(call core.ActionCall) (any, error) {
	return "ok", nil
}
func (p *stormPanel) SetPanelUpdate(update func(core.Panel) error) { p.update = update }
func (p *stormPanel) push() error {
	if p.update == nil {
		return errors.New("no sink installed - a live-enabled module pushes dead")
	}
	return p.update(core.Panel{ID: "board", Kind: "storm.board", Payload: "back"})
}

// stormer mounts through the registration surface and toggles - the
// boot-early module the storm dismounts first. live pins the capsule
// the latest mount registered (every mount builds a fresh one).
type stormer struct {
	name string
	live *stormPanel
}

func (s *stormer) Name() string       { return s.name }
func (s *stormer) Provides() []string { return []string{s.name} }
func (s *stormer) Requires() []string { return nil }
func (s *stormer) Toggleable() bool   { return true }
func (s *stormer) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	if err := api.RegisterRoute("GET /"+s.name, func(w http.ResponseWriter, r *http.Request) {}); err != nil {
		return err
	}
	s.live = &stormPanel{mod: s.name}
	return api.RegisterPanelProvider(s.live)
}

func TestToggleStormLeavesEngineRingPushing(t *testing.T) {
	prov := newMultiProvider("step")
	stormPlugin := &stormer{name: "earlymod"}
	hcPlugin := NewPlugin()
	ctx := &core.Context{}
	reg, err := agent.MountAllAPI([]agent.Plugin{stormPlugin, hcPlugin}, ctx, nil, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	ctx.Provider = prov

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	hch, ok := hcPlugin.mounted[0].(*HTTPChannel)
	if !ok {
		t.Fatalf("mounted channel %T is not an HTTPChannel", hcPlugin.mounted[0])
	}
	if err := hch.wire(core.ChannelDeps{Ctx: ctx, Store: st, Version: "test"}); err != nil {
		t.Fatalf("wire: %v", err)
	}
	canvas := hch.srv.canvas
	assertChatRoutes(t, canvas, "after boot")

	// disable the early module live (the storm's kanban step), then
	// the host re-wire: canvas.Resync drops the module's namespace.
	// The Start-time chat registration rides START, not the mount
	// journal - it must survive the journal's undo untouched.
	if err := reg.Dismount(ctx, "earlymod"); err != nil {
		t.Fatalf("dismount earlymod: %v", err)
	}
	if err := hch.RewireSeams(ctx); err != nil {
		t.Fatalf("RewireSeams (disable): %v", err)
	}
	assertChatRoutes(t, canvas, "after disable")

	// re-enable (the storm's verdict/kanban enable step): the remount
	// appends a fresh provider through the scoped proxy, Resync
	// re-registers it - with the sink, so its pushes work - and the
	// chat namespace keeps pushing right through it.
	if err := reg.Mount(ctx, stormPlugin, core.StartDeps{}); err != nil {
		t.Fatalf("remount earlymod: %v", err)
	}
	if err := hch.RewireSeams(ctx); err != nil {
		t.Fatalf("RewireSeams (enable): %v", err)
	}
	assertChatRoutes(t, canvas, "after enable")
	// the push must ride the module's OWN face: the sink the
	// re-registration installed. A dead sink fails the land below.
	if err := stormPlugin.live.push(); err != nil {
		t.Fatalf("re-enabled module's push dead: %v (the sink must ride the re-registration)", err)
	}
	snap := canvas.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot = %d panels, want storm board + chat: %+v", len(snap), snap)
	}
	for _, pn := range snap {
		if pn.ID == "board" && pn.Payload != "back" {
			t.Fatalf("board push did not land: %+v", pn)
		}
		if pn.ID == "chat" {
			cp, ok := pn.Payload.(core.ChatPayload)
			if !ok || cp.Session != "storm-pin" {
				t.Fatalf("chat payload lost across the storm: %+v", pn.Payload)
			}
		}
	}
}

// assertChatRoutes pins the engine-ring namespace in the handler map:
// the panel seeds the snapshot, an action routes to the capsule, and a
// push lands - the live row needs no snapshot refresh.
func assertChatRoutes(t *testing.T, canvas *agent.Canvas, at string) {
	t.Helper()
	snap := canvas.Snapshot()
	ids := map[string]bool{}
	for _, p := range snap {
		ids[p.ID] = true
	}
	if !ids["chat"] {
		t.Fatalf("chat panel missing from the canvas %s: %+v (the unknown-module incident)", at, snap)
	}
	if _, derr := canvas.Dispatch(core.ActionCall{Module: "http_channel", Action: "stop"}); derr != nil && strings.Contains(derr.Error(), "unknown module") {
		t.Fatalf("chat action routed nowhere %s: %v", at, derr)
	}
	if err := canvas.Update("http_channel", core.Panel{
		ID:      "chat",
		Title:   "Chat",
		Kind:    core.PanelKindChat,
		Payload: core.ChatPayload{Session: "storm-pin", Running: false},
	}); err != nil {
		t.Fatalf("chat push rejected %s: %v", at, err)
	}
}
