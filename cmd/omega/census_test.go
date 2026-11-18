package main

// The E1 runtime proofs against the REAL plugin set.

import (
	"os"
	"path/filepath"
	"regexp"
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
	if n := len(ctx.Lifecycle.Snapshot()); n != 0 {
		for _, h := range ctx.Lifecycle.Snapshot() {
			t.Logf("left handle: %s/%s", h.Owner, h.Name)
		}
		t.Fatalf("%d lifecycle handles survived the teardown", n)
	}
}

// captureStderr redirects os.Stderr for the duration of fn and returns what was
// written.
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

// The migration journey: an install with the v0.12 omega.json sidecar boots once.
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
	if len(cfg.Disabled.Modules) != 1 || cfg.Disabled.Modules[0] != "kanban" {
		t.Fatalf("folded entries not honored by this boot: %+v", cfg.Disabled.Modules)
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
	if len(again.Disabled.Modules) != 1 || again.Disabled.Modules[0] != "kanban" {
		t.Fatalf("folded state did not survive the reboot: %+v", again.Disabled.Modules)
	}
	if warning != "" {
		t.Fatalf("a completed migration must not warn again: %q", warning)
	}
}

// Sibling-of-explicit-config.yaml is the primary sidecar location, the same order the.
func TestFoldOmegaJSONSiblingAndHomeSearch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	// The fold-created config.yaml carries no provider stanza.
	t.Setenv("OMEGA_MODEL", "test-model")
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
	// First-wins dedupe + normalize (sorted): [kanban cron kanban] folds as [cron kanban].
	if len(cfg.Disabled.Modules) != 2 || cfg.Disabled.Modules[0] != "cron" || cfg.Disabled.Modules[1] != "kanban" {
		t.Fatalf("wrong folded list, want [cron kanban]: %+v", cfg.Disabled.Modules)
	}
	// The warning names the fold target on canon spelling.
	if !strings.Contains(warning, filepath.ToSlash(filepath.Join(home, "config", "config.yaml"))) {
		t.Fatalf("warning must name the fold target, got: %q", warning)
	}
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

// The plain yaml key: config.yaml carries disabled.
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
	if len(cfg.Disabled.Modules) != 2 || cfg.Disabled.Modules[0] != "kanban" || cfg.Disabled.Modules[1] != "delegate" {
		t.Fatalf("disabled: key not decoded: %+v", cfg.Disabled.Modules)
	}
	// And the toggle writer round-trips it.
	if err := writeDisabled(cfgPath, DisabledNamespaces{Modules: []string{"delegate"}}); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Disabled.Modules) != 1 || cfg.Disabled.Modules[0] != "delegate" {
		t.Fatalf("toggle round-trip broke the list: %+v", cfg.Disabled.Modules)
	}
	if _, statErr := os.Stat(filepath.Join(cfgDir, "omega.json")); !os.IsNotExist(statErr) {
		t.Fatalf("a plain-key boot must not resurrect omega.json: %v", statErr)
	}
	if err := writeDisabled(cfgPath, DisabledNamespaces{}); err != nil {
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

// The node-level write law.
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
	if err := writeDisabled(path, DisabledNamespaces{Modules: []string{"kanban"}}); err != nil {
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
	if !regexp.MustCompile(`(?m)^disabled:\n    modules:\n        - kanban\n?$`).MatchString(body) {
		t.Fatalf("disabled: must append as the last key, namespaced mapping (yaml.v3 4-space list indent):\n%s", body)
	}
	if !strings.HasSuffix(body, "disabled:\n    modules:\n        - kanban\n") {
		t.Fatalf("disabled list malformed:\n%s", body)
	}
}

// A toggle write must never silently destroy a config.yaml boot has to parse.
func TestWriteDisabledLoudOnBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("provider:\n  [broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeDisabled(path, DisabledNamespaces{Modules: []string{"kanban"}}); err == nil {
		t.Fatal("broken config.yaml must fail the write loud")
	}
	// The broken byte is untouched - no half-written state.
	if data, err := os.ReadFile(path); err == nil && !strings.Contains(string(data), "[broken") {
		t.Fatal("the failed write altered the file")
	}
}
