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