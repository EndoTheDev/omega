package verdict

// The plugin-level pins (v0.12 phase C2): the api mount registers the
// seam, the verdict.request tool, the TUI renderer and the prompt
// section, and the classic Mount is a fail-loud stub
// (docs/migrating-a-module.md is the recipe).

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// stubLogger is the minimal core.LoggerProvider (the mount reads the
// logging seam to build the hub).
type stubLogger struct{}

func (stubLogger) Printf(format string, args ...any) {}
func (stubLogger) Errorf(format string, args ...any) {}
func (stubLogger) Close() error                      { return nil }

// stubLoggerPlugin provides the logging seam for the mount pins.
type stubLoggerPlugin struct{}

func (stubLoggerPlugin) Name() string       { return "logging" }
func (stubLoggerPlugin) Provides() []string { return []string{"logging"} }
func (stubLoggerPlugin) Requires() []string { return nil }
func (stubLoggerPlugin) Mount(ctx *core.Context) error {
	ctx.Logger = stubLogger{}
	return nil
}

// stubStorePlugin provides the store seam (the hub persists card +
// audit rows through it; nil works too but the mount reads it, so the
// pin carries one).
type stubStorePlugin struct{}

func (stubStorePlugin) Name() string       { return "store" }
func (stubStorePlugin) Provides() []string { return []string{"store"} }
func (stubStorePlugin) Requires() []string { return nil }
func (stubStorePlugin) Mount(ctx *core.Context) error {
	ctx.Store = nil // compile-safe: a nil store is the unconfigured host posture
	return nil
}

// TestMountWithAPIShape pins the migrated mount's registrations.
func TestMountWithAPIShape(t *testing.T) {
	p := NewPlugin()
	ctx := &core.Context{}
	plugins := []agent.Plugin{stubLoggerPlugin{}, stubStorePlugin{}, p}
	if _, err := agent.MountAllAPI(plugins, ctx, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if p.hub == nil {
		t.Fatal("the hub was not built")
	}
	if ctx.Verdicts == nil {
		t.Fatal("seam mismatch: ctx.Verdicts is nil after the api mount")
	}
	names := map[string]int{}
	for _, tp := range ctx.ToolProviders {
		for name := range tp.Tools() {
			names[name]++
		}
	}
	if len(names) != 1 || names["verdict.request"] != 1 {
		t.Fatalf("tool registry = %v, want {verdict.request: 1}", names)
	}
	if len(ctx.TUIProviders) != 1 {
		t.Fatalf("TUI providers = %d, want the answer-command renderer", len(ctx.TUIProviders))
	}
	if len(ctx.PromptSectioners) != 1 {
		t.Fatalf("prompt sectioners = %d, want the Verdict Cards contributor", len(ctx.PromptSectioners))
	}
	// the classic mount path is dead: reaching it fails loud
	if err := p.Mount(ctx); err == nil {
		t.Fatal("the retired classic mount must fail loud")
	}
	_ = context.Background()
}
