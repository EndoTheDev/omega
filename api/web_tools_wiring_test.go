package api

// TestChannelMountCarriesWebTools, the shell half of the carve (card
// t_905fa502): the web tool pair's registration is PLUGIN mount work
// (shell vocabulary); the wire-shape tests over the pair itself live in
// agent/api next to web_tools.go.

import (
	"testing"

	"github.com/EndoTheDev/omega/agent"
	agentapi "github.com/EndoTheDev/omega/agent/api"
	"github.com/EndoTheDev/omega/core"
)

// TestChannelMountCarriesWebTools verifies the channel mount registers
// the web tool pair from the config - the pair's only mount since the
// v0.13 dissolution (card t_f76ecdbe). Natural names, shell tier:
// the channel plugin is not Toggleable, so the pair is never
// disable-list material.
func TestChannelMountCarriesWebTools(t *testing.T) {
	p := NewPlugin()
	ctx := &core.Context{
		Configs: map[string]any{
			"http_channel": Config{WebTools: agentapi.WebToolsConfig{APIKey: "k", BaseURL: "http://host:1"}},
		},
	}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount failed: %v", err)
	}
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("expected 1 tool provider, got %d", len(ctx.ToolProviders))
	}
	tools := ctx.ToolProviders[0].Tools()
	for _, name := range []string{"web.search", "web.fetch"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("mounted provider missing %s", name)
		}
	}
	if _, ok := tools["web"]; ok {
		t.Error("the unprefixed consolidated web tool must be retired")
	}
	if _, toggleable := agent.Plugin(p).(agent.Toggleable); toggleable {
		t.Error("the channel must stay shell vocabulary (not Toggleable)")
	}
}
