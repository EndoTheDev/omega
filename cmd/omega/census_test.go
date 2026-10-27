package main

// The E1 runtime proofs against the REAL plugin set: the census test
// mirrors the nine removal proofs at runtime - mount every module of
// buildPlugins, census, DismountAll in reverse topo order, and demand
// after-dismount == before-mount. Plus the config.yaml disabled: list
// and its omega.json fold migration.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
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

// captureStderr redirects os.Stderr for the duration of fn and returns
// what was written - the fold migration warns on the boot path, which
// has no logger yet.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	r.Close()
	return string(buf[:n])
}

// The migration journey: an install with the v0.12 omega.json sidecar
// boots once - the disable list folds into config.yaml's disabled:
// key, the sidecar is deleted, and the boot honors the folded entries
// in the same run. No silent drops.
func TestFoldOmegaJSONMigration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "omega.json"),
		[]byte(`{"disabled": ["kanban"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMEGA_MODEL", "test-model")
	var warning string
	cfg, err := func() (Config, error) {
		var cfg Config
		var err error
		warning = captureStderr(t, func() {
			cfg, err = LoadConfig("")
		})
		return cfg, err
	}()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Disabled) != 1 || cfg.Disabled[0] != "kanban" {
		t.Fatalf("folded entries not honored by this boot: %+v", cfg.Disabled)
	}
	if _, statErr := os.Stat(filepath.Join(cfgDir, "omega.json")); !os.IsNotExist(statErr) {
		t.Fatalf("omega.json survived the fold: %v", statErr)
	}
	yamlPath := filepath.Join(cfgDir, "config.yaml")
	if _, statErr := os.Stat(yamlPath); statErr != nil {
		t.Fatalf("fold did not create the default config.yaml: %v", statErr)
	}
	if !strings.Contains(warning, "kanban") || !strings.Contains(warning, "folded") {
		t.Fatalf("boot warning must name the folded entries, got: %q", warning)
	}
	// Idempotent: the second boot sees no sidecar and warns nothing.
	again, err := func() (Config, error) {
		var cfg Config
		var err error
		warning = captureStderr(t, func() {
			cfg, err = LoadConfig("")
		})
		return cfg, err
	}()
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Disabled) != 1 || again.Disabled[0] != "kanban" {
		t.Fatalf("folded state did not survive the reboot: %+v", again.Disabled)
	}
	if warning != "" {
		t.Fatalf("a completed migration must not warn again: %q", warning)
	}
}

// Sibling-of-explicit-config.yaml is the primary sidecar location, the
// same order the old LoadDisabled searched.
func TestFoldOmegaJSONSiblingAndHomeSearch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	// The fold-created config.yaml carries no provider stanza; the
	// model rides the env, like the toggle-writing fresh install it
	// mirrors.
	t.Setenv("OMEGA_MODEL", "test-model")
	// A home/config/omega.json folds when no explicit config rides along.
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "omega.json"),
		[]byte(`{"disabled": ["kanban", "cron", "kanban"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var warning string
	cfg, err := func() (Config, error) {
		var cfg Config
		var err error
		warning = captureStderr(t, func() {
			cfg, err = LoadConfig("")
		})
		return cfg, err
	}()
	if err != nil {
		t.Fatal(err)
	}
	// First-wins dedupe: [kanban cron kanban] folds as [kanban cron].
	if len(cfg.Disabled) != 2 || cfg.Disabled[0] != "kanban" || cfg.Disabled[1] != "cron" {
		t.Fatalf("wrong folded list, want [kanban cron]: %+v", cfg.Disabled)
	}
	if !strings.Contains(warning, filepath.Join(home, "config", "config.yaml")) {
		t.Fatalf("warning must name the fold target, got: %q", warning)
	}
	// An empty sidecar is dead weight: deleted, nothing folded.
	if err := os.WriteFile(filepath.Join(cfgDir, "omega.json"),
		[]byte(`{"disabled": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(filepath.Join(cfgDir, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(cfgDir, "omega.json")); !os.IsNotExist(statErr) {
		t.Fatalf("an empty sidecar must still be buried: %v", statErr)
	}
	// Malformed JSON fails the boot loud (Q5 - no silent degradation).
	if err := os.WriteFile(filepath.Join(cfgDir, "omega.json"),
		[]byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(filepath.Join(cfgDir, "config.yaml")); err == nil || !strings.Contains(err.Error(), "omega.json") {
		t.Fatalf("malformed omega.json must fail loud: %v", err)
	}
}

// The plain yaml key: config.yaml carries disabled: directly, no
// sidecar involved - the list rides the normal decode.
func TestDisabledPlainYAMLKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	if err := os.WriteFile(cfgPath,
		[]byte("http_timeout: 5\nprovider:\n  model_name: test-model\ndisabled:\n  - kanban\n  - delegate\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Disabled) != 2 || cfg.Disabled[0] != "kanban" || cfg.Disabled[1] != "delegate" {
		t.Fatalf("disabled: key not decoded: %+v", cfg.Disabled)
	}
	// And the toggle writer round-trips it: flip kanban on, the file
	// loses only that entry.
	if err := writeDisabled(cfgPath, []string{"delegate"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Disabled) != 1 || cfg.Disabled[0] != "delegate" {
		t.Fatalf("toggle round-trip broke the list: %+v", cfg.Disabled)
	}
	if _, statErr := os.Stat(filepath.Join(cfgDir, "omega.json")); !os.IsNotExist(statErr) {
		t.Fatalf("a plain-key boot must not resurrect omega.json: %v", statErr)
	}
	// All-on: the key leaves the document entirely.
	if err := writeDisabled(cfgPath, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "disabled") {
		t.Fatalf("an all-on home must carry no toggle state: %q", string(data))
	}
}

// The node-level write law: comments, unknown keys, and key order
// survive a toggle write verbatim - the disabled: key is appended
// last, the rest of the document is untouched text.
func TestWriteDisabledPreservesDocument(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "# my hand-tuned config\nprovider:\n  model_name: test-model # keep\nunknown_key: keep-me\nskills:\n  dir: mine\n"
	path := filepath.Join(cfgDir, "config.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeDisabled(path, []string{"kanban"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"# my hand-tuned config", "test-model # keep", "unknown_key: keep-me", "dir: mine"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the write lost %q:\n%s", want, body)
		}
	}
	if !regexp.MustCompile(`(?m)^disabled:\n    - kanban\n?$`).MatchString(body) {
		t.Fatalf("disabled: must append as the last key (yaml.v3 4-space list indent):\n%s", body)
	}
	if !strings.HasSuffix(body, "disabled:\n    - kanban\n") {
		t.Fatalf("disabled list malformed:\n%s", body)
	}
}

// A toggle write must never silently destroy a config.yaml boot has to
// parse: malformed yaml fails the write loud.
func TestWriteDisabledLoudOnBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("provider:\n  [broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeDisabled(path, []string{"kanban"}); err == nil {
		t.Fatal("broken config.yaml must fail the write loud")
	}
	// The broken byte is untouched - no half-written state.
	if data, err := os.ReadFile(path); err == nil && !strings.Contains(string(data), "[broken") {
		t.Fatal("the failed write altered the file")
	}
}
