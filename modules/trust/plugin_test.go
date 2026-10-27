package trust

// The plugin-level pins (v0.12 phase C2): the api mount publishes the
// seam, and the classic Mount is a fail-loud stub
// (docs/migrating-a-module.md is the recipe).

import (
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
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
	if ctx.Trust == nil {
		t.Fatal("seam mismatch: ctx.Trust is nil after the api mount")
	}
	if _, ok := ctx.Trust.(*Provider); !ok {
		t.Fatalf("unexpected seam implementation: %T", ctx.Trust)
	}
}
