package main

// The E2 host-side tests: the config.yaml `disabled:` write boot
// shares with the UI toggle (Q7's one code path - the key LoadConfig
// decodes is the key writeDisabled writes), and the disabled-list
// derivation from the live registry.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

const toggleYAML = "provider:\n  model_name: test-model\n"

func TestWriteDisabledRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(toggleYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeDisabled(path, DisabledNamespaces{Modules: []string{"kanban", "delegate"}}); err != nil {
		t.Fatal(err)
	}
	// LoadConfig decodes the same file boot reads: the write must be
	// legible to it (one code path, Q7).
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	// writeDisabled normalizes namespaces (sorted, deterministic yaml):
	// the file loses only kanban, the survivors come back sorted.
	if len(cfg.Disabled.Modules) != 2 || cfg.Disabled.Modules[0] != "delegate" || cfg.Disabled.Modules[1] != "kanban" {
		t.Fatalf("LoadConfig read %v, want [delegate kanban]", cfg.Disabled.Modules)
	}
	// Missing config.yaml with an empty list: nothing to say, nowhere
	// to say it - and no litter file either.
	fresh := filepath.Join(t.TempDir(), "config.yaml")
	if err := writeDisabled(fresh, DisabledNamespaces{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Fatalf("an empty all-on write created a file: %v", err)
	}
}

func TestWriteDisabledMissingFileCreates(t *testing.T) {
	// Fresh install with toggle state but no config.yaml: the write
	// creates a minimal one holding just the disabled: key. The boot
	// that precedes it carried the model by env (no file to read).
	t.Setenv("OMEGA_MODEL", "test-model")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := writeDisabled(path, DisabledNamespaces{Modules: []string{"kanban"}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Disabled.Modules) != 1 || cfg.Disabled.Modules[0] != "kanban" {
		t.Fatalf("created config not legible: %+v", cfg.Disabled.Modules)
	}
}

func TestWriteDisabledEmptyRemovesKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(toggleYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeDisabled(path, DisabledNamespaces{Modules: []string{"kanban"}}); err != nil {
		t.Fatal(err)
	}
	if err := writeDisabled(path, DisabledNamespaces{}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Disabled.Modules) != 0 {
		t.Fatalf("the empty list must clear the toggle state, got %v", cfg.Disabled.Modules)
	}
	if d, _ := os.ReadFile(path); !strings.Contains(string(d), "model_name") || strings.Contains(string(d), "kanban") {
		t.Fatalf("the key removal did not leave just the original document: %q", string(d))
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
	m := &omegaModules{pctx: pctx, reg: reg, configPath: filepath.Join(t.TempDir(), "config.yaml"), fold: &DisabledNamespaces{}}
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
