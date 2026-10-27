package main

// The E2 host-side tests: the omega.json write boot shares with the
// UI toggle (Q7's one code path - the file LoadDisabled reads is the
// file SaveDisabled writes), and the disabled-list derivation from
// the live registry.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

func TestSaveDisabledRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omega.json")
	if err := SaveDisabled(path, []string{"kanban", "web"}); err != nil {
		t.Fatal(err)
	}
	// LoadDisabled reads the same file boot reads: the write must be
	// legible to it (one code path, Q7).
	off, err := LoadDisabled(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(off) != 2 || off[0] != "kanban" || off[1] != "web" {
		t.Fatalf("LoadDisabled read %v, want [kanban web]", off)
	}
}

func TestSaveDisabledPreservesOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omega.json")
	if err := os.WriteFile(path, []byte(`{"note":"hand-written"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveDisabled(path, []string{"kanban"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !contains(body, "hand-written") || !contains(body, "kanban") {
		t.Fatalf("the write lost keys: %s", body)
	}
}

func TestSaveDisabledEmptyRemovesKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omega.json")
	if err := SaveDisabled(path, []string{"kanban"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveDisabled(path, nil); err != nil {
		t.Fatal(err)
	}
	off, err := LoadDisabled(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(off) != 0 {
		t.Fatalf("the empty list must clear the toggle state, got %v", off)
	}
}

func TestSaveDisabledLoudOnBrokenFile(t *testing.T) {
	// A malformed omega.json must fail the write loud: boot parses
	// this file, and a silent overwrite would destroy the state boot
	// needs.
	path := filepath.Join(t.TempDir(), "omega.json")
	if err := os.WriteFile(path, []byte(`{disabled`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveDisabled(path, []string{"kanban"}); err == nil {
		t.Fatal("a broken omega.json must fail the write loud")
	}
}

// The live re-wire wiring (v0.12 E2): the host's Toggle must push the
// changed Context onto every LiveSeams-capable channel - the serving
// surface cannot follow the live Context on its own, the channel only
// read it once at mount.
type rewireProbe struct {
	core.Channel
	rewired []int
	fail    int
}

func (r *rewireProbe) RewireSeams(ctx *core.Context) error {
	if r.fail > 0 {
		r.fail--
		return errors.New("canvas register: wiring bug")
	}
	r.rewired = append(r.rewired, 1)
	return nil
}

type hostToggleCapsule struct{ name string }

func (c *hostToggleCapsule) Name() string       { return c.name }
func (c *hostToggleCapsule) Provides() []string { return nil }
func (c *hostToggleCapsule) Requires() []string { return nil }
func (c *hostToggleCapsule) Toggleable() bool   { return true }
func (c *hostToggleCapsule) Mount(*core.Context) error {
	return errors.New("hostsToggleCapsule mounts through the API")
}
func (c *hostToggleCapsule) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	return api.RegisterTool(c.name+".probe", core.Tool{
		Description: "probe",
		Run:         func(context.Context, map[string]any) (string, error) { return "ok", nil },
	})
}

func TestToggleRewiresChannels(t *testing.T) {
	pctx := &agent.Context{}
	probe := &rewireProbe{}
	pctx.Channels = []core.Channel{probe}
	reg, err := agent.MountAllAPI([]agent.Plugin{&hostToggleCapsule{name: "mod"}}, pctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := &omegaModules{pctx: pctx, reg: reg, omegaPath: filepath.Join(t.TempDir(), "omega.json")}
	if err := m.Toggle(context.Background(), "mod", false); err != nil {
		t.Fatal(err)
	}
	if len(probe.rewired) != 1 {
		t.Fatalf("disable rewired %d times, want 1", len(probe.rewired))
	}
	if err := m.Toggle(context.Background(), "mod", true); err != nil {
		t.Fatal(err)
	}
	if len(probe.rewired) != 2 {
		t.Fatalf("enable rewired %d times, want 2", len(probe.rewired))
	}

	// a failing re-wire is loud: the toggle applied, the caller hears
	// why the surfaces may still show it
	probe.fail = 1
	if err := m.Toggle(context.Background(), "mod", false); err == nil {
		t.Fatal("failed re-wire answered nil - stale routes would render a dead module")
	}
}
