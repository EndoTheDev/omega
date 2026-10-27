package agent

// The E1 dismount proofs (implementation sketches 1 and 2): the
// panicking plugin at boot re-panics named; at remount the Context
// holds zero of its contributions; teardown reverses the recorded
// topo order; the churn stays flat on goroutines.

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// toggleCapsule is the E1 test capsule: it mounts a tool, a command,
// a route and a fence handle - surfaces the census and the journal
// must unwind.
type toggleCapsule struct {
	name       string
	provides   []string
	panicMount bool
}

func (c *toggleCapsule) Name() string { return c.name }
func (c *toggleCapsule) Provides() []string {
	if c.provides != nil {
		return c.provides
	}
	return nil
}
func (c *toggleCapsule) Requires() []string { return nil }
func (c *toggleCapsule) Toggleable() bool   { return true }

func (c *toggleCapsule) Mount(*core.Context) error {
	return errors.New("toggleCapsule mounts through the API")
}

func (c *toggleCapsule) MountWithAPI(ctx *core.Context, api *ExtensionAPI) error {
	if c.panicMount {
		panic("boom from " + c.name)
	}
	if err := api.RegisterTool(c.name+".probe", core.Tool{
		Description: "probe",
		Run:         func(context.Context, map[string]any) (string, error) { return "ok", nil },
	}); err != nil {
		return err
	}
	if err := api.RegisterCommand("/"+c.name, "toggle probe", nil,
		func(context.Context, string, string) (core.CommandResult, error) { return core.CommandResult{}, nil }); err != nil {
		return err
	}
	if err := api.RegisterRoute("GET /"+c.name, func(w http.ResponseWriter, r *http.Request) {}); err != nil {
		return err
	}
	return api.RegisterLifecycle(c.name+".handle",
		func(context.Context, core.StartDeps) error { return nil },
		func() error { return nil })
}

// classicNoProof registers nothing: it exists for the Q3 edge and
// introspection cases, where only its name and provides matter.
type classicNoProof struct{ name string }

func (p *classicNoProof) Name() string       { return p.name }
func (p *classicNoProof) Provides() []string { return []string{p.name} }
func (p *classicNoProof) Requires() []string { return nil }
func (p *classicNoProof) MountWithAPI(ctx *core.Context, api *ExtensionAPI) error {
	return nil
}

// classicWithProof reverses its own writes.
type classicWithProof struct{ name string }

func (p *classicWithProof) Name() string       { return p.name }
func (p *classicWithProof) Provides() []string { return []string{p.name} }
func (p *classicWithProof) Requires() []string { return nil }
func (p *classicWithProof) MountWithAPI(ctx *core.Context, api *ExtensionAPI) error {
	return api.RegisterCommand("/"+p.name, "", nil, func(_ context.Context, _ string, _ string) (core.CommandResult, error) {
		return core.CommandResult{}, nil
	})
}
func (p *classicWithProof) Dismount(ctx *core.Context) error {
	for i, c := range ctx.Commands {
		if c.Name == "/"+p.name {
			ctx.Commands = append(ctx.Commands[:i], ctx.Commands[i+1:]...)
			return nil
		}
	}
	return nil
}

// dependentCapsule requires a seam, for the Q3 edge test.
type dependentCapsule struct {
	name     string
	requires []string
	toggle   bool
}

func (c *dependentCapsule) Name() string                                            { return c.name }
func (c *dependentCapsule) Provides() []string                                      { return nil }
func (c *dependentCapsule) Requires() []string                                      { return c.requires }
func (c *dependentCapsule) MountWithAPI(ctx *core.Context, api *ExtensionAPI) error { return nil }
func (c *dependentCapsule) Toggleable() bool                                        { return c.toggle }

func TestBootPanicRePanicsNamed(t *testing.T) {
	// Sketch 1, first step: a panicking plugin at boot re-panics with
	// the module named and the fix-it hint attached (Q11).
	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("boot panic must re-panic, not vanish")
		}
		mpe, ok := rec.(*core.ModulePanicError)
		if !ok {
			t.Fatalf("boot panic value is %T, want *core.ModulePanicError: %v", rec, rec)
		}
		if mpe.Name != "panicker" {
			t.Fatalf("boot panic names %q, want panicker", mpe.Name)
		}
		if !strings.Contains(mpe.Suggestion, "disabled") || !strings.Contains(mpe.Suggestion, "omega.json") {
			t.Fatalf("fix-it hint missing: %q", mpe.Suggestion)
		}
	}()
	_, _ = MountAllAPI([]Plugin{&toggleCapsule{name: "panicker", panicMount: true}}, &core.Context{}, nil, nil)
}

func TestRemountPanicRollsBack(t *testing.T) {
	// The remount half of Q11: the same capsule on a live set - the
	// toggle is rejected, the server keeps running, the Context holds
	// zero of its contributions.
	ctx := &core.Context{}
	liveReg, err := MountAllAPI([]Plugin{&classicWithProof{name: "base"}}, ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := core.Census(ctx)
	err = liveReg.Mount(ctx, &toggleCapsule{name: "panicker", panicMount: true}, core.StartDeps{})
	if err == nil {
		t.Fatal("remount panic must reject the toggle")
	}
	mpe, ok := err.(*core.ModulePanicError)
	if !ok {
		t.Fatalf("remount error is %T, want *core.ModulePanicError", err)
	}
	if mpe.Name != "panicker" {
		t.Fatalf("rejection names %q, want panicker", mpe.Name)
	}
	if after := core.Census(ctx); !after.Equal(before) {
		t.Fatalf("rejected remount left contributions: %v", before.Diff(after))
	}
	if liveReg.isActive("panicker") {
		t.Fatal("rejected module must not be active")
	}
	// A second mount of a WELL-BEHAVED module still works: the
	// registry was unwound, not wedged.
	if err := liveReg.Mount(ctx, &toggleCapsule{name: "good"}, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
}

func TestToggleCensusRoundTrip(t *testing.T) {
	ctx := &core.Context{}
	liveReg, err := MountAllAPI([]Plugin{&classicWithProof{name: "base"}}, ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := core.Census(ctx)
	mod := &toggleCapsule{name: "mod"}
	if err := liveReg.Mount(ctx, mod, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	with := core.Census(ctx)
	if with.Equal(base) {
		t.Fatal("mounted module left no census surface")
	}
	// The fence handle registered but the host never started it.
	found := false
	for _, h := range ctx.RunGroup().Snapshot() {
		if h.Owner == "mod" && h.Name == "mod.handle" {
			if h.Started {
				t.Fatal("zero StartDeps must not start handles")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("fence handle not registered by the toggle mount")
	}
	if err := liveReg.Dismount(ctx, "mod"); err != nil {
		t.Fatal(err)
	}
	after := core.Census(ctx)
	if !after.Equal(base) {
		t.Fatalf("dismount did not unwind the module: %v", after.Diff(base))
	}
	if liveReg.isActive("mod") {
		t.Fatal("dismounted module still active")
	}
	// Re-enable works and the claims were released (Q5 one-tenant
	// holds across the toggle).
	if err := liveReg.Mount(ctx, mod, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	if got := core.Census(ctx); !got.Equal(with) {
		t.Fatalf("re-mount census differs: %v", got.Diff(with))
	}
}

// orderCapsule records its teardown order.
type orderCapsule struct {
	name     string
	requires []string
	on       *[]string
}

func (c *orderCapsule) Name() string       { return c.name }
func (c *orderCapsule) Provides() []string { return []string{c.name} }
func (c *orderCapsule) Requires() []string { return c.requires }
func (c *orderCapsule) Toggleable() bool   { return true }
func (c *orderCapsule) Mount(*core.Context) error {
	return errors.New("orderCapsule mounts through the API")
}
func (c *orderCapsule) MountWithAPI(*core.Context, *ExtensionAPI) error {
	return nil
}
func (c *orderCapsule) Dismount(*core.Context) error {
	*c.on = append(*c.on, c.name)
	return nil
}

func TestDismountAllReverseTopo(t *testing.T) {
	// Sketch 2: DismountAll replays MountAll's recorded topo order in
	// REVERSE - the dependent tears down BEFORE the seam it consumed.
	var dismounted []string
	root := &orderCapsule{name: "root", requires: []string{"leaf2"}, on: &dismounted}
	leaf := &orderCapsule{name: "leaf2", requires: nil, on: &dismounted}
	ctx := &core.Context{}
	reg, err := MountAllAPI([]Plugin{Plugin(root), Plugin(leaf)}, ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.mountOrder) != 2 || reg.mountOrder[0].Name() != "leaf2" || reg.mountOrder[1].Name() != "root" {
		t.Fatalf("topo order %v, want [leaf2 root]", reg.mountOrder)
	}
	if err := DismountAll(reg, ctx); err != nil {
		t.Fatal(err)
	}
	if len(dismounted) != 2 || dismounted[0] != "root" || dismounted[1] != "leaf2" {
		t.Fatalf("dismount order %v, want [root leaf2] (reverse topo)", dismounted)
	}
	if !core.Census(ctx).Equal(core.Census(&core.Context{})) {
		t.Fatalf("dismount all left surface: %v", core.Census(ctx).Diff(core.Census(&core.Context{})))
	}
}

func TestClassicDismount(t *testing.T) {
	ctx := &core.Context{}
	reg, err := MountAllAPI([]Plugin{&classicWithProof{name: "manual"}}, ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Dismount(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	if len(ctx.Commands) != 0 {
		t.Fatalf("classic Dismount did not remove its command: %+v", ctx.Commands)
	}
}

func TestDismountTwiceFails(t *testing.T) {
	ctx := &core.Context{}
	reg, _ := MountAllAPI([]Plugin{&classicWithProof{name: "once"}}, ctx, nil, nil)
	if err := reg.Dismount(ctx, "once"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Dismount(ctx, "once"); err == nil {
		t.Fatal("dismount of an unmounted module must fail")
	}
}

func TestDisableList(t *testing.T) {
	// Q1: the disabled capsule simply does not mount.
	off := &toggleCapsule{name: "kanban", provides: []string{"kanban"}}
	ctx := &core.Context{}
	reg, err := MountAllAPI([]Plugin{off}, ctx, nil, []string{"kanban"})
	if err != nil {
		t.Fatal(err)
	}
	if reg.isActive("kanban") {
		t.Fatal("disabled module mounted")
	}
	if len(ctx.Infos) != 0 {
		t.Fatalf("disabled module left inventory: %+v", ctx.Infos)
	}
	// Q3: an enabled module depending on the disabled one fails the
	// boot naming the edge.
	dep := &dependentCapsule{name: "depper", requires: []string{"kanban"}}
	_, err = MountAllAPI([]Plugin{off, dep}, ctx, nil, []string{"kanban"})
	if err == nil {
		t.Fatal("Q3: dependency on a disabled module must fail the boot")
	}
	for _, want := range []string{"depper", "kanban", "disabled"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Q3 error must name the edge (%s): %v", want, err)
		}
	}
	// The same set without the disable list mounts clean.
	if _, err := MountAllAPI([]Plugin{off, dep}, &core.Context{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Tier law: a machine-critical module is not disableable.
	_, err = MountAllAPI([]Plugin{&dependentCapsule{name: "store"}}, ctx, nil, []string{"store"})
	if err == nil || !strings.Contains(err.Error(), "not toggleable") {
		t.Fatalf("tier law: disabling machine-critical module must fail: %v", err)
	}
	// Unknown names fail loud (a typo must not silently boot).
	_, err = MountAllAPI([]Plugin{off}, ctx, nil, []string{"kaban"})
	if err == nil || !strings.Contains(err.Error(), "no such module") {
		t.Fatalf("unknown disabled name must fail loud: %v", err)
	}
}

// churnCapsule registers a fence handle whose Start spawns a
// goroutine - exactly the shape recover cannot unwind and the fence
// must own.
type churnCapsule struct{ stop chan struct{} }

func (c *churnCapsule) Name() string                 { return "churn" }
func (c *churnCapsule) Provides() []string           { return nil }
func (c *churnCapsule) Requires() []string           { return nil }
func (c *churnCapsule) Toggleable() bool             { return true }
func (c *churnCapsule) Dismount(*core.Context) error { return nil }
func (c *churnCapsule) MountWithAPI(ctx *core.Context, api *ExtensionAPI) error {
	return api.RegisterLifecycle("churn.pump",
		func(_ context.Context, _ core.StartDeps) error {
			c.stop = make(chan struct{})
			go churnLoop(c.stop)
			return nil
		},
		func() error { close(c.stop); return nil })
}

func churnLoop(stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestToggleChurnFlatGoroutines(t *testing.T) {
	// Sketch 2's churn test: N toggle cycles leave the goroutine
	// count flat, and the pprof goroutine dump (the backstop for
	// closures invisible to slot diffs) holds no stacks rooted in
	// the churning module.
	ctx := &core.Context{}
	reg, err := MountAllAPI([]Plugin{&classicWithProof{name: "base"}}, ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	gossip := &churnCapsule{}
	if err := reg.Mount(ctx, gossip, core.StartDeps{Run: context.Background(), Ctx: ctx}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()
	const cycles = 12
	for i := 0; i < cycles; i++ {
		if err := reg.Dismount(ctx, "churn"); err != nil {
			t.Fatal(err)
		}
		if err := reg.Mount(ctx, gossip, core.StartDeps{Run: context.Background(), Ctx: ctx}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := runtime.NumGoroutine(); got <= before+2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("churn leaked goroutines: %d -> %d over %d cycles", before, after, cycles)
	}
	// Last module state is mounted: stop it and every pump must be
	// gone from the dump (the backstop).
	if err := reg.Dismount(ctx, "churn"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	dump := new(strings.Builder)
	if err := pprof.Lookup("goroutine").WriteTo(dumpWriter{dump}, 2); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(dump.String(), "\n") {
		if strings.Contains(line, "churnLoop") {
			t.Fatalf("churn closure still alive in the goroutine dump: %s", line)
		}
	}
}

type dumpWriter struct{ *strings.Builder }

func (w dumpWriter) Write(p []byte) (int, error) { return w.Builder.Write(p) }
