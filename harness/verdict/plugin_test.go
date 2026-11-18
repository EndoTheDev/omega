package verdict

// The plugin-level pins (v0.12 phase C2).

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// stubLogger is the minimal agent.LoggerProvider.
type stubLogger struct{}

func (stubLogger) Printf(format string, args ...any) {}
func (stubLogger) Warnf(format string, args ...any)  {}
func (stubLogger) Errorf(format string, args ...any) {}
func (stubLogger) Close() error                      { return nil }

// stubLoggerPlugin provides the logging seam for the mount pins.
type stubLoggerPlugin struct{}

func (stubLoggerPlugin) Name() string       { return "logging" }
func (stubLoggerPlugin) Provides() []string { return []string{"logger"} }
func (stubLoggerPlugin) Requires() []string { return nil }
func (stubLoggerPlugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	ctx.ApplyOffer("logger", "logging", stubLogger{})
	return nil
}

// stubStorePlugin provides the store seam.
type stubStorePlugin struct{}

func (stubStorePlugin) Name() string       { return "store" }
func (stubStorePlugin) Provides() []string { return []string{"store"} }
func (stubStorePlugin) Requires() []string { return nil }
func (stubStorePlugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	// compile-safe: a nil store is the unconfigured host posture - offer
	// nothing, the resolve reads nil the same way.
	return nil
}

// TestMountWithAPIShape pins the migrated mount's registrations.
func TestMountWithAPIShape(t *testing.T) {
	p := NewPlugin()
	ctx := &core.Context{}
	plugins := []agent.Plugin{stubLoggerPlugin{}, stubStorePlugin{}, p}
	if _, err := agent.MountAllAPI(plugins, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if p.hub == nil {
		t.Fatal("the hub was not built")
	}
	if ctx.Services().Resolve("verdicts") == nil {
		t.Fatal("seam mismatch: the verdicts offer is nil after the api mount")
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
	if len(ctx.PromptSectioners) != 1 {
		t.Fatalf("prompt sectioners = %d, want the Verdict Cards contributor", len(ctx.PromptSectioners))
	}
	_ = context.Background()
}
