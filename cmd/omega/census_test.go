package main

// The E1 runtime proofs against the REAL plugin set: the census test
// mirrors the nine removal proofs at runtime - mount every module of
// buildPlugins, census, DismountAll in reverse topo order, and demand
// after-dismount == before-mount. Plus the omega.json toggle file.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

func TestCensusAllModules(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	cfg := DefaultConfig()
	cfg.Store.DBPath = filepath.Join(home, "data", "omega.db")
	cfg.Skills.Dir = filepath.Join(home, "skills")
	cfg.Memory.File = filepath.Join(home, "agents", "default", "memory.md")
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("build plugins: %v", err)
	}
	ctx := &agent.Context{
		CWD:     home,
		Configs: agent.RouteConfigs(plugins, cfg),
	}
	reg, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	mounted := core.Census(ctx)
	if equal := mounted.Equal(core.Census(&agent.Context{})); equal {
		t.Fatal("the mounted set left no census surface - the proof would prove nothing")
	}
	if err := agent.DismountAll(reg, ctx); err != nil {
		t.Fatal(err)
	}
	fresh := core.Census(&agent.Context{})
	after := core.Census(ctx)
	if !after.Equal(fresh) {
		t.Fatalf("dismounted tree is not the pre-mount census: %v", fresh.Diff(after))
	}
	// Handles too: every fence entry unregistered.
	if n := len(ctx.Lifecycle.Snapshot()); n != 0 {
		for _, h := range ctx.Lifecycle.Snapshot() {
			t.Logf("left handle: %s/%s", h.Owner, h.Name)
		}
		t.Fatalf("%d lifecycle handles survived the teardown", n)
	}
}

func TestLoadDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	// Absent file: empty list, every capsule on (Q2).
	off, err := LoadDisabled("")
	if err != nil || len(off) != 0 {
		t.Fatalf("missing omega.json: %v %v", off, err)
	}
	// The config/ layout is the primary location.
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfgDir, "omega.json")
	if err := os.WriteFile(path, []byte(`{"disabled": ["kanban"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	off, err = LoadDisabled("")
	if err != nil || len(off) != 1 || off[0] != "kanban" {
		t.Fatalf("config/omega.json not read: %v %v", off, err)
	}
	// Sibling of an explicit config.yaml wins.
	altDir := t.TempDir()
	cfgPath := filepath.Join(altDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("http_timeout: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(altDir, "omega.json"), []byte(`{"disabled": ["cron", "web"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	off, err = LoadDisabled(cfgPath)
	if err != nil || len(off) != 2 {
		t.Fatalf("sibling omega.json not read: %v %v", off, err)
	}
	// Malformed JSON fails loud (Q5 - no silent degradation).
	if err := os.WriteFile(filepath.Join(altDir, "omega.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDisabled(cfgPath); err == nil || !strings.Contains(err.Error(), "omega.json") {
		t.Fatalf("malformed omega.json must fail loud: %v", err)
	}
}

func TestLoadDisabledThroughConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("http_timeout: 5\nprovider:\n  model_name: test-model\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string][]string{"disabled": {"kanban"}})
	if err := os.WriteFile(filepath.Join(cfgDir, "omega.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(cfgDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Disabled) != 1 || cfg.Disabled[0] != "kanban" {
		t.Fatalf("toggle list not routed into config: %+v", cfg.Disabled)
	}
}
