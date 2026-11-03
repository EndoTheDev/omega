package trust

// The plugin-level pins (v0.12 phase C2): the api mount publishes the
// seam, and the classic Mount is a fail-loud stub
// (docs/migrating-a-module.md is the recipe).

import (
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// TestMountWithAPIShape pins the migrated mount's registrations: the
// trust lands on the exact field the classic mount assigned, and the
// classic stub fails loud.
func TestMountWithAPIShape(t *testing.T) {
	p := NewPlugin()
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if ctx.Services().Resolve("trust") == nil {
		t.Fatal("seam mismatch: the trust offer is nil after the api mount")
	}
	if _, ok := ctx.Services().Resolve("trust").(*Provider); !ok {
		t.Fatal("unexpected seam implementation")
	}
}
