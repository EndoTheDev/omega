package api

// The live-toggle storm pin.

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// stormPanel is one toggleable module's live panel provider.
type stormPanel struct {
	mod string
	// update is the PanelUpdateSink closure the canvas runtime installs at registration -
	// the capsule pushes through its own faces with it (kanban's capsule law); a nil sink
	// drops pushes silently, so the test asserts the LAND, not the call.
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

// stormer mounts through the registration surface and toggles.
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
	ctx.ApplyOffer("provider", "test", prov)

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
	canvas := hch.srv.Canvas()
	assertChatRoutes(t, canvas, "after boot")

	// disable the early module live (the storm's kanban step), then the host re-wire. The
	// Start-time chat registration rides START, not the mount journal - it must survive
	// the journal's undo untouched.
	if err := reg.Dismount(ctx, "earlymod"); err != nil {
		t.Fatalf("dismount earlymod: %v", err)
	}
	if err := hch.RewireSeams(ctx); err != nil {
		t.Fatalf("RewireSeams (disable): %v", err)
	}
	assertChatRoutes(t, canvas, "after disable")

	if err := reg.Mount(ctx, stormPlugin, core.StartDeps{}); err != nil {
		t.Fatalf("remount earlymod: %v", err)
	}
	if err := hch.RewireSeams(ctx); err != nil {
		t.Fatalf("RewireSeams (enable): %v", err)
	}
	assertChatRoutes(t, canvas, "after enable")
	// the push must ride the module's OWN face: the sink the re-registration installed.
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

// assertChatRoutes pins the shell namespace in the handler map.
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
