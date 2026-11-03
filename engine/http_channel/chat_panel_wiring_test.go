package http_channel

// TestChatPanelRegistersThroughTheMountSeam (chat-last 2b ruling), the
// shell half of the carve (t_905fa502): the plugin's Mount only CREATES
// the capsule - registration is gated on the channel actually starting.
// The original body lives in agent/api for the base surface; the
// integration walk (real Plugin + NewPlugin mount + the Start-gated
// registration) stays with the shell because the plugin seam is shell
// vocabulary.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agentapi "github.com/EndoTheDev/omega/agent/api"
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// TestChatPanelRegistersThroughTheStartSeam (chat-last 2b ruling): the
// plugin's Mount only CREATES the capsule - registration is gated on
// the channel actually starting. One chat panel per live frontend: a
// serve-less process never reaches Start, so its canvas carries no dead
// http chat panel; here the channel-start handoff registers it with
// the canvas seam, and the pipe routes to the capsule's handler.
func TestChatPanelRegistersThroughTheMountSeam(t *testing.T) {
	s, st := newTestServerWithStore(t)
		plugin := NewPlugin()
	pctx := &core.Context{Configs: map[string]any{"http_channel": Config{Port: 1}}}
	if _, err := agent.MountAllAPI([]agent.Plugin{plugin}, pctx, nil, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	// Mount registers nothing: the panel exists only once a live
	// frontend starts.
	if len(pctx.PanelProviders) != 0 {
		t.Fatalf("panel providers = %d, want 0 (registration is Start-gated)", len(pctx.PanelProviders))
	}
	// The channel-start handoff (HTTPChannel.Start): bind the capsule
	// to its server and register it before the canvas runtime builds.
	s.BindChatCapsule(plugin.chat)
	plugin.chat.PinServer(s.Server) // the wire publishes the served server (the Start handoff, http_channel.go's wire)
	s.RegisterChatPanel(pctx)
	canvas, err := agent.NewCanvas(pctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	snapshot := canvas.Snapshot()
	if len(snapshot) != 1 || snapshot[0].Kind != core.PanelKindChat {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	// The action pipe routes: an unknown session is the capsule's
	// error, verbatim - module known, action routed.
	_, stopErr := canvas.Dispatch(core.ActionCall{Module: agentapi.ChatModuleNamespace, Action: agentapi.ChatActionStop, Args: json.RawMessage(`{"session":"ghost"}`)})
	if stopErr == nil || !strings.Contains(stopErr.Error(), "session not active") {
		t.Fatalf("stop dispatch = %v", stopErr)
	}
	// A retry against a session with no user message is the replay
	// guard's answer, and a failed prepare releases the run slot.
	if err := st.CreateSession(context.Background(), "s-empty", "", "Empty"); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, retryErr := canvas.Dispatch(core.ActionCall{Module: agentapi.ChatModuleNamespace, Action: agentapi.ChatActionRetry, Args: json.RawMessage(`{"session":"s-empty"}`)})
	if retryErr == nil || !strings.Contains(retryErr.Error(), "nothing to retry") {
		t.Fatalf("retry dispatch = %v", retryErr)
	}
	if s.ActiveRun("s-empty") {
		t.Fatal("failed prepare left the run slot held")
	}
}

