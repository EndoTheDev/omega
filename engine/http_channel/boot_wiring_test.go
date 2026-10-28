package http_channel

// The boot-wiring pin (card t_7ee284e8): at lane boot the canvas
// handlers map carries the engine-ring panel namespace. The Oct-27
// incident: a toggle-storm chopped the Start-time chat registration
// off the Context (a journal truncation undo, retired by d73fecd) and
// every chat panel push thereafter logged
//   canvas: unknown module: "http_channel"
// until the next restart. The wire path - registerAt, then NewCanvas -
// must leave the chat namespace routed and pushing, and the served
// snapshot must carry the panel. This walks the REAL MountAllAPI +
// HTTPChannel.wire surface, no stand-ins for the wiring itself.

import (
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/modules/store"
)

func TestBootWiringKeepsEngineRingInHandlers(t *testing.T) {
	prov := newMultiProvider("step")
	plugin := NewPlugin()
	ctx := &core.Context{}
	reg, err := agent.MountAllAPI([]agent.Plugin{plugin}, ctx, nil, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	if len(ctx.Channels) == 0 {
		t.Fatalf("http_channel mounted no channels")
	}
	// the host-built seam (wireCaptioner analog): mount builds no
	// Provider, the host attaches one before the channels start.
	ctx.Provider = prov

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	var chans []*HTTPChannel
	for _, ch := range plugin.mounted {
		hch, ok := ch.(*HTTPChannel)
		if !ok {
			t.Fatalf("mounted channel %T is not an HTTPChannel", ch)
		}
		chans = append(chans, hch)
	}
	if len(chans) != 1 {
		t.Fatalf("binds = %d, want the single default bind", len(chans))
	}
	if err := chans[0].wire(core.ChannelDeps{Ctx: ctx, Store: st, Version: "test"}); err != nil {
		t.Fatalf("wire: %v", err)
	}

	canvas := chans[0].srv.canvas
	if canvas == nil {
		t.Fatalf("wire left the server without a canvas")
	}
	// 1. the handler map routes the engine-ring namespace: Snapshot
	// seeds the chat panel, Dispatch reaches the capsule (a not-started
	// channel answers its own degradation, never ErrUnknownModule).
	snap := canvas.Snapshot()
	if len(snap) != 1 || snap[0].ID != "chat" || snap[0].Kind != core.PanelKindChat {
		t.Fatalf("boot snapshot lost the chat panel: %+v", snap)
	}
	if _, derr := canvas.Dispatch(core.ActionCall{Module: "http_channel", Action: "stop"}); derr != nil && strings.Contains(derr.Error(), "unknown module") {
		t.Fatalf("boot dispatched into unknown module: %v (the incident)", derr)
	}

	// 2. a push lands: the capsule's installed closure routes into the
	// same namespace - the live row without a snapshot refresh.
	panel := core.Panel{
		ID:      "chat",
		Title:   "Chat",
		Kind:    core.PanelKindChat,
		Payload: core.ChatPayload{Session: "boot-pin", Running: true},
	}
	if err := canvas.Update("http_channel", panel); err != nil {
		t.Fatalf("chat push after boot rejected: %v", err)
	}
	snap = canvas.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("pushed panel displaced the snapshot: %+v", snap)
	}
	cp, ok := snap[0].Payload.(core.ChatPayload)
	if !ok || cp.Session != "boot-pin" || !cp.Running {
		t.Fatalf("push payload did not land: %+v", snap[0].Payload)
	}

	// 3. the teardown proof stays valid: the channel registration
	// dismounts clean (the chat capsule rides Start-time registration,
	// not the mount journal - the journal may never own it).
	if err := reg.Dismount(ctx, "http_channel"); err != nil {
		t.Fatalf("dismount (teardown proof): %v", err)
	}
	if len(ctx.Channels) != 0 {
		t.Fatalf("dismount left channels: %d", len(ctx.Channels))
	}
}
