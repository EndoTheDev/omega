package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
)

// stubProvider satisfies ai.Provider for testing.
type stubProvider struct{ name string }

func (s *stubProvider) Stream(_ context.Context, _ []ai.Message, _ []ai.ToolSchema) <-chan ai.StreamEvent {
	return nil
}
func (s *stubProvider) ModelName() string                  { return s.name }
func (s *stubProvider) SetThinkingLevel(level string)      {}
func (s *stubProvider) SetModel(model string)              { s.name = model }
func (s *stubProvider) ListModels() ([]string, error)      { return nil, nil }
func (s *stubProvider) ModelInfo() (ai.ModelInfo, error)    { return ai.ModelInfo{}, nil }

// stubCompactor satisfies CompactionProvider for testing.
type stubCompactor struct{}

func (stubCompactor) Compact(_ context.Context, msgs []ai.Message) ([]ai.Message, error) {
	return msgs, nil
}

// stubToolProvider satisfies ToolProvider for testing.
type stubToolProvider struct{ tools map[string]Tool }

func (s stubToolProvider) Tools() map[string]Tool { return s.tools }

// testPlugin is a configurable Plugin for testing MountAll.
type testPlugin struct {
	name     string
	provides []string
	requires []string
	mount    func(*Context) error
}

func (p testPlugin) Name() string                  { return p.name }
func (p testPlugin) Provides() []string            { return p.provides }
func (p testPlugin) Requires() []string            { return p.requires }
func (p testPlugin) Mount(ctx *Context) error      { return p.mount(ctx) }

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
	if err := MountAll(plugins, ctx); err != nil {
		t.Fatalf("MountAll: %v", err)
	}
	if order[0] != "provider" {
		t.Fatalf("provider should mount first, got %q", order[0])
	}
	if order[1] != "compactor" {
		t.Fatalf("compactor should mount second, got %q", order[1])
	}
}

func TestMountAllMissingDep(t *testing.T) {
	p := testPlugin{
		name:     "compactor",
		provides: []string{"compactor"},
		requires: []string{"provider"},
		mount:    func(ctx *Context) error { return nil },
	}

	err := MountAll([]Plugin{p}, &Context{})
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

	err := MountAll([]Plugin{p1, p2}, &Context{})
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
				tools: map[string]Tool{"a": {Description: "tool a"}},
			})
			return nil
		},
	}
	p2 := testPlugin{
		name:     "tools-b",
		provides: []string{"tools"},
		mount: func(ctx *Context) error {
			ctx.ToolProviders = append(ctx.ToolProviders, stubToolProvider{
				tools: map[string]Tool{"b": {Description: "tool b"}},
			})
			return nil
		},
	}

	ctx := &Context{}
	if err := MountAll([]Plugin{p1, p2}, ctx); err != nil {
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

	err := MountAll([]Plugin{p}, &Context{})
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
	if err := MountAll([]Plugin{p}, ctx); err != nil {
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

	err := MountAll([]Plugin{a, b}, &Context{})
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
				tools: map[string]Tool{
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
				ExtensionCommand{Name: "/foo", Description: "foo command"},
				ExtensionCommand{Name: "/bar", Description: "bar command"},
			)
			return nil
		},
	}

	ctx := &Context{}
	if err := MountAll([]Plugin{p1, p2}, ctx); err != nil {
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