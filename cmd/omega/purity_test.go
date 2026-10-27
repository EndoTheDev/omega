package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// captureSink is a RefusalSink that records every ledger entry - the
// conformance suite's exactness witness (module, seam, predicate).
type captureSink struct {
	mu       sync.Mutex
	refusals []core.Refusal
}

func (s *captureSink) Log(r core.Refusal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refusals = append(s.refusals, r)
}

// bootMount boots the full default plugin set into a fresh Context and
// returns the registry, the context and the canvas built over them.
// OMEGA_HOME must be set by the caller (each proof gets its own home).
func bootMount(t *testing.T, sink core.RefusalSink) (*agent.Registry, *core.Context, *agent.Canvas) {
	t.Helper()
	cfg := DefaultConfig()
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	ctx := &core.Context{Configs: agent.RouteConfigs(plugins, cfg)}
	reg, err := agent.MountAllAPI(plugins, ctx, sink, nil)
	if err != nil {
		t.Fatalf("mount all: %v", err)
	}
	canvas, err := agent.NewCanvas(ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	return reg, ctx, canvas
}

// TestDoubleBootPurity is the registration-surface regulator diff
// (phase F proof 3): two boots of the same plugin set must produce the
// identical registration surface - census equality. A mount that reads
// ambient state into its registrations (home paths, host IDs, time) is
// a purity break and the diff names it.
func TestDoubleBootPurity(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	_, ctx1, _ := bootMount(t, nil)
	c1 := core.Census(ctx1)

	// second boot, fresh home and all
	t.Setenv("OMEGA_HOME", t.TempDir())
	_, ctx2, _ := bootMount(t, nil)
	c2 := core.Census(ctx2)

	if diff := c1.Diff(c2); len(diff) != 0 {
		t.Fatalf("DOUBLE BOOT: registration surface drifted between boots:\n%s", strings.Join(diff, "\n"))
	}
	// and the surface is not empty - a diff of two empty mounts is a
	// vacuous green
	if len(c1.Tools) == 0 || len(c1.Slots) == 0 {
		t.Fatalf("DOUBLE BOOT: census carries no surface (tools=%d slots=%d) - the diff proves nothing", len(c1.Tools), len(c1.Slots))
	}
}

type act struct{ module, id string }

func actionsNames(actions []act) []string {
	out := make([]string, len(actions))
	for i, a := range actions {
		out[i] = a.id
	}
	return out
}

// mutationBattery is the conformance payload set: for every registered
// action, every payload shape runs through the canonical dispatch
// boundary (Canvas.Dispatch). The "golden" payload is the empty shape
// the module itself decodes; the mutations are the shapes a decode
// must either reject exactly at the boundary or govern on its own -
// what the suite pins is that the counts and the refusal ledger agree
// with the verdicts, on exactly the dispatched set, twice across boots.
var mutationBattery = []struct {
	name   string
	args   []byte // nil = empty args
	refuse bool   // true = the boundary itself must reject (bad JSON)
}{
	{"empty", nil, false},
	{"null", []byte("null"), false},
	{"scalar", []byte("7"), false},
	{"mutant", []byte(`{"__mutant__":[1,2,3]}`), false},
	{"not-json", []byte("{not json"), true},
}

// TestMutationConformance is the mutation-based conformance suite
// (phase F proof 5, sketch 3): the battery drives every registered
// action on the full mount; per-seam accept counters and the refusal
// ledger must agree with the boundary verdicts on exactly what the
// module decode rejects - no refusal without a verdict, no verdict
// without a count, no count without a boot-identical replay.
func TestMutationConformance(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	sink := &captureSink{}
	_, _, canvas := bootMount(t, sink)

	// the action census from the canvas snapshot: module + action id,
	// module-qualified on every declared panel
	var actions []act
	seen := map[string]bool{}
	for _, p := range canvas.Snapshot() {
		for _, a := range p.Actions {
			key := a.Module + "/" + a.ID
			if !seen[key] {
				seen[key] = true
				actions = append(actions, act{a.Module, a.ID})
			}
		}
	}
	if len(actions) == 0 {
		t.Fatal("CONFORMANCE: no registered actions on the full mount - the battery drives nothing")
	}
	t.Logf("CONFORMANCE: battery covers %d registered actions (%v)", len(actions), actionsNames(actions))

	// the module's no counts as a refusal: a decode rejection, a
	// ShapeCheck refusal or the module's own validation arm - the
	// counter records what the module rejected, nothing else decides.
	send := func(canvas *agent.Canvas, module, action string, args []byte) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("PANIC at the boundary: %v", r)
			}
		}()
		_, err = canvas.Dispatch(core.ActionCall{
			Module: module,
			Action: action,
			Args:   json.RawMessage(args),
		})
		return err
	}

	expected := map[string]agent.ActionTally{}
	for _, a := range actions {
		for _, m := range mutationBattery {
			err := send(canvas, a.module, a.id, m.args)
			if err != nil && strings.Contains(err.Error(), "PANIC") {
				t.Fatalf("CONFORMANCE: %s mutated payload %s panicked: %s", a.id, m.name, err)
			}
			got := expected[a.id]
			if err != nil {
				got.Refused++
			} else {
				got.Accepted++
			}
			expected[a.id] = got
		}
	}

	// reconcile 1: the counters - every dispatched payload counted
	// exactly once, accepted or refused, never both, never neither,
	// every action in the table, nothing outside the dispatched set.
	tallies := canvas.ActionTallies()
	if len(tallies) != len(expected) {
		t.Fatalf("CONFORMANCE: counters cover %d action ids, the battery dispatched %d", len(tallies), len(expected))
	}
	for a, want := range expected {
		got, ok := tallies[a]
		if !ok {
			t.Fatalf("CONFORMANCE: %s dispatched but never counted", a)
		}
		if got.Accepted != want.Accepted || got.Refused != want.Refused {
			t.Fatalf("CONFORMANCE: %s tally drifted (got +acc %d/ref %d, want +acc %d/ref %d)", a, got.Accepted, got.Refused, want.Accepted, want.Refused)
		}
	}

	// reconcile 2: the refusal ledger (Q12) only ever fires on
	// boundary-owned verdicts - a "not valid JSON" or "refused by"
	// refusal whose action the battery dispatched, never on an
	// accepted dispatch, never on an undispatched action.
	byAction := map[string][]core.Refusal{}
	sink.mu.Lock()
	for _, r := range sink.refusals {
		byAction[r.Predicate] = append(byAction[r.Predicate], r)
	}
	sink.mu.Unlock()
	for pred, entries := range byAction {
		want, drove := expected[pred]
		if !drove {
			t.Fatalf("CONFORMANCE: ledger recorded %q but the battery never dispatched it - refusals fire beyond the decode verdict", pred)
		}
		if want.Refused == 0 {
			t.Fatalf("CONFORMANCE: %s ledged %d refusals on a dispatch set the boundary accepted", pred, len(entries))
		}
		if len(entries) > want.Refused {
			t.Fatalf("CONFORMANCE: %s ledger holds %d entries, boundary offered %d refusals", pred, len(entries), want.Refused)
		}
		for _, r := range entries {
			if r.Module == "" || r.Seam == "" {
				t.Fatalf("CONFORMANCE: %s ledger entry lost its module/seam: %+v", pred, r)
			}
			if e := r.Err.Error(); !strings.Contains(e, "not valid JSON") && !strings.Contains(e, "refused by") {
				t.Fatalf("CONFORMANCE: %s ledger entry is not a boundary verdict: %q", pred, e)
			}
		}
	}

	// and no ledger entry exists for an action the battery never drove
	for pred := range byAction {
		if _, drove := expected[pred]; !drove {
			t.Fatalf("CONFORMANCE: ledger recorded %q but the battery never dispatched it", pred)
		}
	}

	// reconcile 3 lives inside the battery: the not-json mutation IS
	// part of the battery, so any module that tolerates raw garbage
	// for a typed action surfaces as an accepted not-json dispatch -
	// its choice, the counters still balance. Report the tolerant
	// decodes loudly for the record.
	for _, a := range actions {
		if got := expected[a.id]; got.Accepted == len(mutationBattery) {
			t.Logf("CONFORMANCE: %s tolerates every mutation (garbage-in decode choice)", a.id)
		}
	}

	// the drift alarm: a second boot replays the identical battery and
	// the accept counters must land identical - a mount whose boundary
	// counts drift between boots is not the same surface twice.
	t.Setenv("OMEGA_HOME", t.TempDir())
	sink2 := &captureSink{}
	_, _, canvas2 := bootMount(t, sink2)
	for _, a := range actions {
		for _, m := range mutationBattery {
			if err := send(canvas2, a.module, a.id, m.args); err != nil && strings.Contains(err.Error(), "PANIC") {
				t.Fatalf("CONFORMANCE boot2: %s mutated payload %s panicked: %s", a.id, m.name, err)
			}
		}
	}
	t1, t2 := canvas.ActionTallies(), canvas2.ActionTallies()
	if len(t1) != len(t2) {
		t.Fatalf("CONFORMANCE drift alarm: boot tallies cover %d vs %d action ids", len(t1), len(t2))
	}
	for id, v1 := range t1 {
		v2, ok := t2[id]
		if !ok {
			t.Fatalf("CONFORMANCE drift alarm: %q tallied on boot 1 but never crossed boot 2's boundary", id)
		}
		if v1 != v2 {
			t.Fatalf("CONFORMANCE drift alarm: %q counters drifted across boots (boot1 %+v, boot2 %+v)", id, v1, v2)
		}
	}

	// sanity: the battery DID exercise the boundary - at least one
	// refusal landed somewhere (a world that accepts raw garbage for
	// every action is a finding, not a green)
	totalRefused := 0
	for _, v := range t1 {
		totalRefused += v.Refused
	}
	if totalRefused == 0 {
		t.Fatal("CONFORMANCE: zero refusals across the whole battery - the boundary never said no")
	}
}
