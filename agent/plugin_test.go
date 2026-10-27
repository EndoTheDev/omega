package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// stubProvider satisfies ai.Provider for testing.
type stubProvider struct{ name string }

func (s *stubProvider) Stream(_ context.Context, _ []core.Message, _ []ai.ToolSchema) <-chan core.ProviderStreamEvent {
	return nil
}
func (s *stubProvider) ModelName() string { return s.name }
func (s *stubProvider) WithModel(model string) ai.Provider {
	cp := *s
	cp.name = model
	return &cp
}
func (s *stubProvider) SetThinkingLevel(level string) {}
func (s *stubProvider) WithThinkingLevel(string) ai.Provider {
	return s
}
func (s *stubProvider) ListModels() ([]string, error)    { return nil, nil }
func (s *stubProvider) ModelInfo() (ai.ModelInfo, error) { return ai.ModelInfo{}, nil }

// stubCompactor satisfies CompactionProvider for testing.
type stubCompactor struct{}

func (stubCompactor) Compact(_ context.Context, msgs []core.Message) ([]core.Message, error) {
	return msgs, nil
}

// stubToolProvider satisfies ToolProvider for testing.
type stubToolProvider struct{ tools map[string]core.Tool }

func (s stubToolProvider) Tools() map[string]core.Tool { return s.tools }

// testPlugin is a configurable Plugin for testing MountAll.
type testPlugin struct {
	name     string
	provides []string
	requires []string
	mount    func(*Context) error
}

func (p testPlugin) Name() string                                       { return p.name }
func (p testPlugin) Provides() []string                                 { return p.provides }
func (p testPlugin) Requires() []string                                 { return p.requires }
func (p testPlugin) MountWithAPI(ctx *Context, api *ExtensionAPI) error { return p.mount(ctx) }

func TestMountAllOrdering(t *testing.T) {
	var order []string
	mk := func(name string, provides, requires []string) testPlugin {
		return testPlugin{
			name:     name,
			provides: provides,
			requires: requires,
			mount: func(ctx *Context) error {
				order = append(order, name)
				return nil
			},
		}
	}

	// compactor requires provider — must mount after.
	plugins := []Plugin{
		mk("compactor", []string{"compactor"}, []string{"provider"}),
		mk("provider", []string{"provider"}, nil),
	}

	ctx := &Context{}
	if _, err := MountAllAPI(plugins, ctx, nil, nil); err != nil {
		t.Fatalf("MountAll: %v", err)
	}
	if order[0] != "provider" {
		t.Fatalf("provider should mount first, got %q", order[0])
	}
	if order[1] != "compactor" {
		t.Fatalf("compactor should mount second, got %q", order[1])
	}
}

// TestMountAllRequiresEdgeForcesOrder proves a Requires edge forces
// mount order regardless of slice position (card t_13a2af8a: mount
// order is declared data). The notifications-shaped consumer is listed
// before its producer in the slice; topoSort must still mount the
// producer first.
func TestMountAllRequiresEdgeForcesOrder(t *testing.T) {
	var order []string
	mk := func(name string, provides, requires []string) testPlugin {
		return testPlugin{
			name:     name,
			provides: provides,
			requires: requires,
			mount: func(ctx *Context) error {
				order = append(order, name)
				return nil
			},
		}
	}

	plugins := []Plugin{
		mk("consumer", []string{"tools"}, []string{"notifications"}),
		mk("independent", []string{"independent"}, nil),
		mk("notifications", []string{"notifications"}, nil),
	}

	ctx := &Context{}
	if _, err := MountAllAPI(plugins, ctx, nil, nil); err != nil {
		t.Fatalf("MountAll: %v", err)
	}
	seen := map[string]int{}
	for i, name := range order {
		seen[name] = i
	}
	if seen["notifications"] > seen["consumer"] {
		t.Fatalf("notifications must mount before its consumer, got order %v", order)
	}
}

func TestMountAllMissingDep(t *testing.T) {
	p := testPlugin{
		name:     "compactor",
		provides: []string{"compactor"},
		requires: []string{"provider"},
		mount:    func(ctx *Context) error { return nil },
	}

	_, err := MountAllAPI([]Plugin{p}, &Context{}, nil, nil)
	if err == nil {
		t.Fatal("expected missing dep error")
	}
	if !strings.Contains(err.Error(), "requires seam") {
		t.Fatalf("expected missing dep error, got %v", err)
	}
}

func TestMountAllConflict(t *testing.T) {
	p1 := testPlugin{name: "p1", provides: []string{"provider"}, mount: func(c *Context) error { return nil }}
	p2 := testPlugin{name: "p2", provides: []string{"provider"}, mount: func(c *Context) error { return nil }}

	_, err := MountAllAPI([]Plugin{p1, p2}, &Context{}, nil, nil)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Fatalf("expected conflict error mentioning both plugins, got %v", err)
	}
}

func TestMountAllAdditiveTools(t *testing.T) {
	p1 := testPlugin{
		name:     "tools-a",
		provides: []string{"tools"},
		mount: func(ctx *Context) error {
			ctx.ToolProviders = append(ctx.ToolProviders, stubToolProvider{
				tools: map[string]core.Tool{"a": {Description: "tool a"}},
			})
			return nil
		},
	}
	p2 := testPlugin{
		name:     "tools-b",
		provides: []string{"tools"},
		mount: func(ctx *Context) error {
			ctx.ToolProviders = append(ctx.ToolProviders, stubToolProvider{
				tools: map[string]core.Tool{"b": {Description: "tool b"}},
			})
			return nil
		},
	}

	ctx := &Context{}
	if _, err := MountAllAPI([]Plugin{p1, p2}, ctx, nil, nil); err != nil {
		t.Fatalf("MountAll: %v", err)
	}
	if len(ctx.ToolProviders) != 2 {
		t.Fatalf("expected 2 tool providers, got %d", len(ctx.ToolProviders))
	}
}

func TestMountAllMountError(t *testing.T) {
	p := testPlugin{
		name:     "broken",
		provides: []string{"provider"},
		mount:    func(ctx *Context) error { return errors.New("boom") },
	}

	_, err := MountAllAPI([]Plugin{p}, &Context{}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected mount error, got %v", err)
	}
}

func TestMountAllPopulatesSlots(t *testing.T) {
	prov := &stubProvider{name: "test-model"}
	p := testPlugin{
		name:     "provider",
		provides: []string{"provider"},
		mount: func(ctx *Context) error {
			ctx.Provider = prov
			return nil
		},
	}

	ctx := &Context{}
	if _, err := MountAllAPI([]Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("MountAll: %v", err)
	}
	if ctx.Provider == nil {
		t.Fatal("Provider slot not populated")
	}
	if ctx.Provider.ModelName() != "test-model" {
		t.Fatalf("expected model name %q, got %q", "test-model", ctx.Provider.ModelName())
	}
	if len(ctx.Infos) != 1 || ctx.Infos[0].Name != "provider" {
		t.Fatalf("expected 1 info entry for provider, got %v", ctx.Infos)
	}
}

func TestMountAllCircularDep(t *testing.T) {
	a := testPlugin{name: "a", provides: []string{"a"}, requires: []string{"b"}, mount: func(c *Context) error { return nil }}
	b := testPlugin{name: "b", provides: []string{"b"}, requires: []string{"a"}, mount: func(c *Context) error { return nil }}

	_, err := MountAllAPI([]Plugin{a, b}, &Context{}, nil, nil)
	if err == nil {
		t.Fatal("expected circular dependency error")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Fatalf("expected circular dependency error, got %v", err)
	}
}

// TestMountAllPopulatesInfos verifies that MountAll fills in
// ExtensionInfo.Tools, Commands, and ToolList after each mount.
// This is a regression test for the bug where /extensions showed
// zero tools and zero commands for all extensions.
func TestMountAllPopulatesInfos(t *testing.T) {
	p1 := testPlugin{
		name:     "tools-ext",
		provides: []string{"tools"},
		mount: func(ctx *Context) error {
			ctx.ToolProviders = append(ctx.ToolProviders, stubToolProvider{
				tools: map[string]core.Tool{
					"alpha": {Description: "alpha tool"},
					"beta":  {Description: "beta tool"},
				},
			})
			return nil
		},
	}
	p2 := testPlugin{
		name:     "cmd-ext",
		provides: []string{"cmd"},
		mount: func(ctx *Context) error {
			ctx.Commands = append(ctx.Commands,
				core.ExtensionCommand{Name: "/foo", Description: "foo command"},
				core.ExtensionCommand{Name: "/bar", Description: "bar command"},
			)
			return nil
		},
	}

	ctx := &Context{}
	if _, err := MountAllAPI([]Plugin{p1, p2}, ctx, nil, nil); err != nil {
		t.Fatalf("MountAll: %v", err)
	}

	if len(ctx.Infos) != 2 {
		t.Fatalf("expected 2 infos, got %d", len(ctx.Infos))
	}

	// tools-ext contributed 2 tools, 0 commands.
	ti := ctx.Infos[0]
	if ti.Name != "tools-ext" {
		t.Fatalf("expected tools-ext, got %q", ti.Name)
	}
	if ti.Tools != 2 {
		t.Fatalf("expected 2 tools, got %d", ti.Tools)
	}
	if ti.Commands != 0 {
		t.Fatalf("expected 0 commands, got %d", ti.Commands)
	}
	if len(ti.ToolList) != 2 {
		t.Fatalf("expected 2 tool entries, got %d", len(ti.ToolList))
	}
	if ti.ToolList[0].Name != "alpha" || ti.ToolList[0].Description != "alpha tool" {
		t.Fatalf("unexpected tool entry: %+v", ti.ToolList[0])
	}

	// cmd-ext contributed 0 tools, 2 commands.
	ci := ctx.Infos[1]
	if ci.Name != "cmd-ext" {
		t.Fatalf("expected cmd-ext, got %q", ci.Name)
	}
	if ci.Tools != 0 {
		t.Fatalf("expected 0 tools, got %d", ci.Tools)
	}
	if ci.Commands != 2 {
		t.Fatalf("expected 2 commands, got %d", ci.Commands)
	}
}

// keyPlugin is a testPlugin that also declares a config section
// (the Describer capability the config router consults).
type keyPlugin struct {
	testPlugin
	desc string
	key  string
}

func (p keyPlugin) Description() string { return p.desc }
func (p keyPlugin) ConfigKey() string   { return p.key }

// mapSource is a ConfigSource backed by a plain map - the shape the
// host's raw config presents to the router.
type mapSource map[string]any

func (m mapSource) ConfigSection(name string) (any, bool) {
	v, ok := m[name]
	return v, ok
}

// TestRouteConfigs covers the config router contract (arc 2b,
// t_889388cc): each plugin's declared ConfigKey pulls its matching
// section from the raw config into the routed map. Sections the host
// does not provide stay absent, plugins without a key or without the
// Describer capability contribute nothing, and a nil source routes
// nothing instead of panicking.
func TestRouteConfigs(t *testing.T) {
	src := mapSource{
		"store":  "store-section",
		"memory": "memory-section",
	}
	plugins := []Plugin{
		keyPlugin{testPlugin{name: "store", provides: []string{"store"}}, "sessions", "store"},
		keyPlugin{testPlugin{name: "memory", provides: []string{"memory"}}, "recalls", "memory"},
		keyPlugin{testPlugin{name: "notifications", provides: []string{"notifications"}}, "bell", "notifications"},
		keyPlugin{testPlugin{name: "tools", provides: []string{"tools"}}, "core tools", ""},
		testPlugin{name: "plain", mount: func(*Context) error { return nil }},
	}

	got := RouteConfigs(plugins, src)
	want := map[string]any{
		"store":  "store-section",
		"memory": "memory-section",
	}
	if len(got) != len(want) {
		t.Fatalf("RouteConfigs routed %d sections (%v), want %d", len(got), got, len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("RouteConfigs[%q] = %v, want %v", k, got[k], v)
		}
	}

	// A nil source routes nothing rather than panicking.
	if got := RouteConfigs(plugins, nil); len(got) != 0 {
		t.Fatalf("RouteConfigs(nil source) = %v, want empty", got)
	}
	// No plugins: empty map, no panic.
	if got := RouteConfigs(nil, src); len(got) != 0 {
		t.Fatalf("RouteConfigs(nil plugins) = %v, want empty", got)
	}
}
