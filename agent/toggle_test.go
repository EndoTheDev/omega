package agent

// The E2 toggle-surface proofs: introspection (Modules), the Q7 one-
// path cycle (disable through the journal, enable through the scoped
// proxy), the Q3 dependents cascade and the missing-edge rejection,
// the tier law at toggle time, and the Q12 refusal ledger.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// statOf finds one module's introspection entry.
func statOf(t *testing.T, reg *Registry, name string) core.ModuleStatus {
	t.Helper()
	for _, m := range reg.Modules() {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("module %q missing from introspection", name)
	return core.ModuleStatus{}
}

func TestToggleRoundTripCensus(t *testing.T) {
	// Q7, surfaced: disable -> enable cycles through the same mount
	// machinery and the census comes back to its baseline each turn.
	ctx := &core.Context{}
	reg, err := MountAllAPI([]Plugin{&toggleCapsule{name: "base"}, &toggleCapsule{name: "mod"}}, ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	with := core.Census(ctx) // both mounted
	if err := reg.Toggle(ctx, "mod", false, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	base := core.Census(ctx) // only base; mod fully unwound
	if base.Equal(with) {
		t.Fatal("the disabled module left census surface")
	}
	for i := 0; i < 3; i++ {
		if err := reg.Toggle(ctx, "mod", true, core.StartDeps{}); err != nil {
			t.Fatal(err)
		}
		if after := core.Census(ctx); !after.Equal(with) {
			t.Fatalf("cycle %d: enable drifted: %v", i, after.Diff(with))
		}
		if err := reg.Toggle(ctx, "mod", false, core.StartDeps{}); err != nil {
			t.Fatal(err)
		}
		if after := core.Census(ctx); !after.Equal(base) {
			t.Fatalf("cycle %d: dismount drifted: %v", i, after.Diff(base))
		}
	}
}

func TestToggleIntrospection(t *testing.T) {
	// The settings list law: core provides the vocabulary, the
	// registry the values - name, state, edges, tier.
	ctx := &core.Context{}
	reg, err := MountAllAPI([]Plugin{&toggleCapsule{name: "kanban", provides: []string{"kanban"}}, &classicNoProof{name: "store"}}, ctx, nil, []string{"kanban"})
	if err != nil {
		t.Fatal(err)
	}
	kanban := statOf(t, reg, "kanban")
	if kanban.State != "disabled" {
		t.Fatalf("kanban state %q, want disabled", kanban.State)
	}
	if !kanban.Toggleable {
		t.Fatal("kanban must introspect toggleable")
	}
	store := statOf(t, reg, "store")
	if store.Toggleable {
		t.Fatal("store must introspect NOT toggleable (tier law)")
	}
	if store.State != "mounted" {
		t.Fatalf("store state %q, want mounted", store.State)
	}
	// Toggle on flips the state through the introspection.
	if err := reg.Toggle(ctx, "kanban", true, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	if got := statOf(t, reg, "kanban"); got.State != "mounted" {
		t.Fatalf("kanban state after enable %q, want mounted", got.State)
	}
}

func TestToggleCascadeDependents(t *testing.T) {
	// Q3, surfaced: disabling a lone provider takes its dependents
	// down in the same toggle, and the introspection lists them.
	base := &toggleCapsule{name: "notifications", provides: []string{"notifications"}}
	dep := &dependentToggle{name: "cron", requires: []string{"notifications"}}
	ctx := &core.Context{}
	reg, err := MountAllAPI([]Plugin{Plugin(dep), base}, ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := reg.DependentModules("notifications"); len(got) != 1 || got[0] != "cron" {
		t.Fatalf("dependents %v, want [cron]", got)
	}
	st := statOf(t, reg, "notifications")
	if len(st.Dependents) != 1 || st.Dependents[0] != "cron" {
		t.Fatalf("introspection dependents %v, want [cron]", st.Dependents)
	}
	if err := reg.Toggle(ctx, "notifications", false, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	if reg.isActive("cron") || reg.isActive("notifications") {
		t.Fatal("the cascade must take the dependents down too")
	}
	depStat := statOf(t, reg, "cron")
	if depStat.State != "disabled" {
		t.Fatalf("cron state after cascade %q, want disabled", depStat.State)
	}
	if !depStat.Toggleable {
		t.Fatal("dependent capsule must introspect toggleable")
	}
}

// dependentToggleMount is a dependent that leaves census surface: a
// tool the cascade must unwind with it.
type dependentToggleMount struct {
	name     string
	requires []string
}

func (c *dependentToggleMount) Name() string       { return c.name }
func (c *dependentToggleMount) Provides() []string { return []string{c.name} }
func (c *dependentToggleMount) Requires() []string { return c.requires }
func (c *dependentToggleMount) Toggleable() bool   { return true }
func (c *dependentToggleMount) Mount(*core.Context) error {
	return errors.New("dependentToggleMount mounts through the API")
}

func (c *dependentToggleMount) MountWithAPI(ctx *core.Context, api *ExtensionAPI) error {
	return api.RegisterTool(c.name+".probe", core.Tool{
		Description: "probe",
		Run:         func(context.Context, map[string]any) (string, error) { return "ok", nil },
	})
}

func TestToggleCascadeUnwindsDependentSurface(t *testing.T) {
	// The cascade is not just bookkeeping: the dependent's census
	// surface (its tool, command, handle) disappears with it, and the
	// census comes back to the pre-cascade baseline on re-enable.
	ctx := &core.Context{}
	base := &toggleCapsule{name: "notifications", provides: []string{"notifications"}}
	reg, err := MountAllAPI([]Plugin{Plugin(&dependentToggleMount{name: "cron", requires: []string{"notifications"}}), base}, ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// cron mounted after notifications (topo: dependent later), so its
	// census surface is on top of the base's.
	full := core.Census(ctx)
	if err := reg.Toggle(ctx, "notifications", false, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	after := core.Census(ctx)
	if after.Equal(full) {
		t.Fatal("the cascade left the dependents' surface up")
	}
	// Exactly the dependent's surface went: its tool, command and
	// handle are gone, the base's census stays.
	for _, tool := range after.Tools {
		if tool == "cron.probe" {
			t.Fatal("the dependent's tool survived the cascade")
		}
	}
	for _, c := range after.Commands {
		if c == "/cron" {
			t.Fatal("the dependent's command survived the cascade")
		}
	}
	for _, h := range after.Handles {
		if strings.HasPrefix(h, "cron/") {
			t.Fatal("the dependent's fence handle survived the cascade")
		}
	}
	if err := reg.Toggle(ctx, "notifications", true, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Toggle(ctx, "cron", true, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	if back := core.Census(ctx); !back.Equal(full) {
		t.Fatalf("re-enable cycle drifted: %v", back.Diff(full))
	}
}

// dependentToggle is the minimal dependent capsule (writes nothing).
type dependentToggle struct {
	name     string
	requires []string
}

func (c *dependentToggle) Name() string       { return c.name }
func (c *dependentToggle) Provides() []string { return []string{c.name} }
func (c *dependentToggle) Requires() []string { return c.requires }
func (c *dependentToggle) Toggleable() bool   { return true }
func (c *dependentToggle) Mount(*core.Context) error {
	return errors.New("dependentToggle mounts through the API")
}
func (c *dependentToggle) MountWithAPI(*core.Context, *ExtensionAPI) error { return nil }

func TestToggleEnableMissingEdgeRejected(t *testing.T) {
	// Q3 at toggle time: enabling the dependent first is rejected
	// naming the disabled provider; enabling the provider first works.
	ctx := &core.Context{}
	reg, err := MountAllAPI([]Plugin{Plugin(&dependentToggle{name: "cron", requires: []string{"notifications"}}), &toggleCapsule{name: "notifications", provides: []string{"notifications"}}}, ctx, nil, []string{"notifications", "cron"})
	if err != nil {
		t.Fatal(err)
	}
	err = reg.Toggle(ctx, "cron", true, core.StartDeps{})
	if err == nil {
		t.Fatal("enabling a module whose provider is disabled must reject")
	}
	for _, want := range []string{"cron", "notifications", "Q3"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("rejection must name the edge (%s): %v", want, err)
		}
	}
	if len(reg.Refusals()) == 0 {
		t.Fatal("the rejection must ride the refusal ledger (Q12)")
	}
	if err := reg.Toggle(ctx, "notifications", true, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Toggle(ctx, "cron", true, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
}

func TestToggleTierLaw(t *testing.T) {
	// Disabling a machine-critical module is rejected at toggle time
	// with the same tier text boot uses.
	ctx := &core.Context{}
	reg, err := MountAllAPI([]Plugin{&toggleCapsule{name: "base"}, &classicNoProof{name: "store"}}, ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = reg.Toggle(ctx, "store", false, core.StartDeps{})
	if err == nil || !strings.Contains(err.Error(), "not toggleable") {
		t.Fatalf("tier law must reject the disable: %v", err)
	}
	if !reg.isActive("store") {
		t.Fatal("the rejected module must stay mounted")
	}
	// The unknown name fails loud too.
	if err := reg.Toggle(ctx, "kaban", false, core.StartDeps{}); err == nil || !strings.Contains(err.Error(), "no such module") {
		t.Fatalf("unknown module must fail loud: %v", err)
	}
}

func TestToggleRemountPanicRollsBackAndRejects(t *testing.T) {
	// Q11 at toggle time: a module that panics on remount is rejected
	// with the ModulePanicError text, the census holds its baseline
	// and the server keeps running. The panicker reached the catalog
	// through the boot skip - disabled, not mounted.
	ctx := &core.Context{}
	reg, err := MountAllAPI([]Plugin{&toggleCapsule{name: "panicker", panicMount: true}}, ctx, nil, []string{"panicker"})
	if err != nil {
		t.Fatal(err)
	}
	base := core.Census(ctx)
	err = reg.Toggle(ctx, "panicker", true, core.StartDeps{})
	if err == nil {
		t.Fatal("a panicking remount must reject the toggle")
	}
	if _, ok := err.(*core.ModulePanicError); !ok {
		t.Fatalf("rejection is %T, want *core.ModulePanicError", err)
	}
	if !strings.Contains(err.Error(), "panicker") {
		t.Fatalf("rejection must name the module: %v", err)
	}
	if after := core.Census(ctx); !after.Equal(base) {
		t.Fatalf("rejected toggle left census drift: %v", after.Diff(base))
	}
}

// emitterProbeCapsule mounts the notifications seam and an emitter
// hook that counts its deliveries - the emit-on-remount law's test
// subject.
type emitterProbeCapsule struct {
	name   string
	counts *int
	seen   *core.NotificationsProvider
}

func (c *emitterProbeCapsule) Name() string       { return c.name }
func (c *emitterProbeCapsule) Provides() []string { return []string{"notifications"} }
func (c *emitterProbeCapsule) Requires() []string { return nil }
func (c *emitterProbeCapsule) Toggleable() bool   { return true }
func (c *emitterProbeCapsule) Mount(*core.Context) error {
	return errors.New("emitterProbeCapsule mounts through the API")
}

func (c *emitterProbeCapsule) MountWithAPI(_ *core.Context, api *ExtensionAPI) error {
	if err := api.RegisterNotifications(plugNotifier{}); err != nil {
		return err
	}
	counts, seen := c.counts, c.seen
	return api.RegisterEmitter(func(n core.NotificationsProvider) {
		*counts++
		*seen = n
	})
}

func TestRemountReplaysEmitter(t *testing.T) {
	// The emit-on-remount law: the boot deliver runs once, at
	// MountAllAPI's end; a remount's fresh hooks register past it. The
	// toggle cycle must replay them - or the module's finished-things
	// handoff stays nil until the next restart.
	counts := 0
	var seen core.NotificationsProvider
	ctx := &core.Context{}
	reg, err := MountAllAPI([]Plugin{&emitterProbeCapsule{name: "mod", counts: &counts, seen: &seen}}, ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	full := core.Census(ctx)
	if counts != 1 || seen == nil {
		t.Fatalf("boot delivery: counts=%d seen=%v", counts, seen)
	}
	if err := reg.Toggle(ctx, "mod", false, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	off := core.Census(ctx)
	if counts != 1 {
		t.Fatalf("dismount must not re-deliver: counts=%d", counts)
	}
	if err := reg.Toggle(ctx, "mod", true, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	if counts != 2 || seen == nil {
		t.Fatalf("remount replay: counts=%d seen=%v", counts, seen)
	}
	if after := core.Census(ctx); !after.Equal(full) {
		t.Fatalf("the replay drifted the census: %v", after.Diff(full))
	}
	// The undo is per-lifetime: the second disable removes the second
	// hook and the count stops where the replay left it.
	if err := reg.Toggle(ctx, "mod", false, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	if counts != 2 {
		t.Fatalf("the undo delivered an extra: counts=%d", counts)
	}
	if after := core.Census(ctx); !after.Equal(off) {
		t.Fatalf("second dismount drifted: %v", after.Diff(off))
	}
}
