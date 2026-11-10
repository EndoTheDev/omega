package main

// agents_test.go: the agent layering + CLI (card t_72ecca67, settled
// continuous-agent design; the layering laws: the agent's provider/
// model/system_prompt win, shared infrastructure does not move, and
// every agent-writable personal path re-points into the agent's own
// home unless the layer set it explicitly. Session/notify/boards data
// isolation resolves through the agent's data/ dir, not this layer.)

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func agentLayerBase() Config {
	cfg := DefaultConfig()
	cfg.Provider.Host = "http://localhost:11434"
	cfg.Provider.ModelName = "glm-5.3"
	return cfg
}

// TestAgentLayerStorePinSurvives pins the delegate child's store pin
// (card t_93b4e521). The agent layer's data isolation re-points a store
// that still reads as the home default at <home>/agents/<name>/data/
// omega.db - correct for an agent's own sessions, fatal for a delegated
// child whose caller pre-created its session in the CALLER's store:
// `run --session <id>` answered "session not found" and every prod
// delegate child died at startup. An env-set OMEGA_DB_PATH is caller
// intent and survives the layer; with no pin, the agent's own store
// stands.
func TestAgentLayerStorePinSurvives(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := agent.ScaffoldAgent(home, "coder", "probe duty"); err != nil {
		t.Fatal(err)
	}

	cfg := agentLayerBase()
	if err := resolveHomePaths(&cfg); err != nil {
		t.Fatal(err)
	}
	callerDB := cfg.Store.DBPath // the home store the caller opened
	t.Setenv("OMEGA_DB_PATH", callerDB)
	if err := ApplyAgentLayer(&cfg, home, "coder"); err != nil {
		t.Fatalf("apply layer: %v", err)
	}
	if cfg.Store.DBPath == callerDB {
		t.Fatalf("premise broken: the layer no longer re-points the home store (%q)", cfg.Store.DBPath)
	}
	applyDBPathEnvOverride(&cfg)
	if want := canonPath(callerDB); cfg.Store.DBPath != want {
		t.Fatalf("store pin after the agent layer = %q, want the caller's %q", cfg.Store.DBPath, want)
	}

	// No pin in the env: the agent's own data dir stands (isolation).
	t.Setenv("OMEGA_DB_PATH", "")
	unpinned := agentLayerBase()
	if err := resolveHomePaths(&unpinned); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAgentLayer(&unpinned, home, "coder"); err != nil {
		t.Fatalf("apply layer: %v", err)
	}
	applyDBPathEnvOverride(&unpinned)
	if want := canonPath(filepath.Join(agent.AgentDir(home, "coder"), "data", "omega.db")); unpinned.Store.DBPath != want {
		t.Fatalf("unpinned store = %q, want the agent's own %q", unpinned.Store.DBPath, want)
	}
}

func TestApplyAgentLayerOverridesModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := agent.ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	layer := "provider:\n  type: openai\n  model_name: qwen3-coder\nsystem_prompt: \"You are scout.\"\n"
	if err := os.WriteFile(agent.AgentConfigPath(home, "scout"), []byte(layer), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := agentLayerBase()
	if err := ApplyAgentLayer(&cfg, home, "scout"); err != nil {
		t.Fatalf("apply layer: %v", err)
	}
	if cfg.Provider.ModelName != "qwen3-coder" {
		t.Errorf("model = %q, want the layer's qwen3-coder", cfg.Provider.ModelName)
	}
	if cfg.Provider.Type != "openai" {
		t.Errorf("type = %q, want openai", cfg.Provider.Type)
	}
	if cfg.SystemPrompt != "You are scout." {
		t.Errorf("system_prompt = %q", cfg.SystemPrompt)
	}
	// the layer does not touch api_key-less knobs it never set: host
	// stays (provider sub-keys merge like yaml overlay).
	if cfg.Provider.Host != "http://localhost:11434" {
		t.Errorf("host clobbered: %q", cfg.Provider.Host)
	}
}

// TestApplyModelEnvOverrideBeatsLayer pins the wake contract's model
// specificity: an explicit OMEGA_MODEL (a delegate fire's or cron
// fire's per-run model riding the env) wins over a persona layer's
// provider.model, which otherwise applies AFTER applyEnv and would
// silently clobber it. Empty env = inherit - the layer's model stands.
func TestApplyModelEnvOverrideBeatsLayer(t *testing.T) {
	t.Setenv("OMEGA_MODEL", " glm-5.2 ")
	cfg := agentLayerBase()
	cfg.Provider.ModelName = "qwen3-coder" // what the layer just wrote
	applyModelEnvOverride(&cfg)
	if cfg.Provider.ModelName != "glm-5.2" {
		t.Errorf("explicit model = %q, want the OMEGA_MODEL glm-5.2 (trimmed)", cfg.Provider.ModelName)
	}

	t.Setenv("OMEGA_MODEL", "")
	cfg = agentLayerBase()
	cfg.Provider.ModelName = "qwen3-coder"
	applyModelEnvOverride(&cfg)
	if cfg.Provider.ModelName != "qwen3-coder" {
		t.Errorf("empty env must inherit, got %q", cfg.Provider.ModelName)
	}
}

func TestApplyAgentLayerScopesWritablePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := agent.ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	cfg := agentLayerBase()
	// the boundary's resolved main dsn (canon spelling, card t_630e595d)
	cfg.Store.DBPath = filepath.ToSlash(filepath.Join(home, "data", "omega.db"))
	cfg.Memory.File = filepath.ToSlash(filepath.Join(home, "memory.md"))
	cfg.Memory.UserProfileFile = filepath.ToSlash(filepath.Join(home, "user.md"))
	cfg.Logging.File = filepath.ToSlash(filepath.Join(home, "omega.log"))
	cfg.Skills.Dir = filepath.ToSlash(filepath.Join(home, "skills"))
	if err := ApplyAgentLayer(&cfg, home, "scout"); err != nil {
		t.Fatalf("apply layer: %v", err)
	}
	dir := filepath.ToSlash(agent.AgentDir(home, "scout"))
	// Data isolation (ruling 4): the store dsn re-points under the
	// agent's own data dir; memory/user/skills/log move into the home.
	// All wants on canon spelling (the boundary's policy).
	if cfg.Store.DBPath != filepath.ToSlash(filepath.Join(dir, "data", "omega.db")) {
		t.Errorf("store db = %q, want the agent's own data dir", cfg.Store.DBPath)
	}
	if cfg.Memory.File != filepath.ToSlash(filepath.Join(dir, "memory.md")) {
		t.Errorf("memory = %q, want agent-scoped", cfg.Memory.File)
	}
	if cfg.Memory.UserProfileFile != filepath.ToSlash(filepath.Join(dir, "user.md")) {
		t.Errorf("user profile = %q, want agent-scoped", cfg.Memory.UserProfileFile)
	}
	if cfg.Skills.Dir != filepath.ToSlash(filepath.Join(dir, "skills")) {
		t.Errorf("skills dir = %q, want agent-own", cfg.Skills.Dir)
	}
	if cfg.Skills.SharedDir != filepath.ToSlash(filepath.Join(home, "skills", "shared")) {
		t.Errorf("shared dir = %q, want home/skills/shared", cfg.Skills.SharedDir)
	}
	if cfg.Logging.File != filepath.ToSlash(filepath.Join(dir, "scout.log")) {
		t.Errorf("log = %q, want the agent's own <name>.log (ruling 1: no logs/ dir)", cfg.Logging.File)
	}
}

func TestApplyAgentLayerRespectsLayerPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := agent.ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	layer := "memory:\n  file: custom-mem.md\nlogging:\n  file: custom.log\n"
	if err := os.WriteFile(agent.AgentConfigPath(home, "scout"), []byte(layer), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := agentLayerBase()
	if err := ApplyAgentLayer(&cfg, home, "scout"); err != nil {
		t.Fatal(err)
	}
	if cfg.Memory.File != "custom-mem.md" {
		t.Errorf("layer-set memory.file clobbered: %q", cfg.Memory.File)
	}
	if cfg.Logging.File != "custom.log" {
		t.Errorf("layer-set logging.file clobbered: %q", cfg.Logging.File)
	}
}

func TestApplyAgentLayerUnknownAgent(t *testing.T) {
	home := t.TempDir()
	if err := ApplyAgentLayer(&Config{}, home, "ghost"); err == nil {
		t.Errorf("unknown agent accepted")
	}
}

func TestCmdAgentsCreateAndList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := cmdAgents([]string{"create", "scout", "probe", "duty"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := os.Stat(agent.AgentConfigPath(home, "scout")); err != nil {
		t.Errorf("scaffold missing after create: %v", err)
	}
	// listing includes it (capture via os.Stdout swap is overkill for
	// the gate - the scaffold check plus LoadProfiles covers the data;
	// assert printAgents does not error).
	if err := printAgents(home); err != nil {
		t.Errorf("printAgents: %v", err)
	}
	// unknown subcommand errors.
	if err := cmdAgents([]string{"frobnicate"}); err == nil {
		t.Errorf("frobnicate accepted")
	}
	// create is idempotent-hostile: second create is a conflict.
	if err := cmdAgents([]string{"create", "scout"}); err == nil {
		t.Errorf("duplicate create accepted")
	}
}

func TestResolveAgentName(t *testing.T) {
	t.Setenv("OMEGA_AGENT", "")
	if resolveAgentName() != "" {
		t.Errorf("empty env surfaced a name")
	}
	t.Setenv("OMEGA_AGENT", "scout")
	if resolveAgentName() != "scout" {
		t.Errorf("resolveAgentName lost the target")
	}
	t.Setenv("OMEGA_AGENT", " scout ")
	if resolveAgentName() != "scout" {
		t.Errorf("whitespace not trimmed")
	}
}

func TestResolveHomePathsMainPersonaAtRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	// the settled default: main's persona files live at the home root
	// (ruling 3) - the home config's relative defaults resolve there.
	cfg := DefaultConfig()
	if cfg.Memory.File != "memory.md" || cfg.Memory.UserProfileFile != "user.md" {
		t.Fatalf("precondition: defaults, got %q / %q", cfg.Memory.File, cfg.Memory.UserProfileFile)
	}
	if err := resolveHomePaths(&cfg); err != nil {
		t.Fatal(err)
	}
	// want: canon spelling (the resolveHomePaths boundary normalizes
	// the separators it constructs - card t_630e595d, ruling 3).
	wantMem := filepath.ToSlash(filepath.Join(home, "memory.md"))
	if cfg.Memory.File != wantMem {
		t.Errorf("memory.file = %q, want the home root (ruling 3)", cfg.Memory.File)
	}
	wantUser := filepath.ToSlash(filepath.Join(home, "user.md"))
	if cfg.Memory.UserProfileFile != wantUser {
		t.Errorf("user profile = %q, want the home root", cfg.Memory.UserProfileFile)
	}
}

func TestApplyAgentLayerMainLayersNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	cfg := agentLayerBase()
	cfg.Memory.File = filepath.Join(home, "memory.md")
	if err := ApplyAgentLayer(&cfg, home, agent.DefaultAgentName); err != nil {
		t.Fatalf("apply main layer: %v", err)
	}
	if cfg.Memory.File != filepath.Join(home, "memory.md") {
		t.Errorf("main persona file moved: %q (main = the home root, ruling 1)", cfg.Memory.File)
	}
}

func TestHelpMentionsAgents(t *testing.T) {
	if !strings.Contains(helpText, "omega agents") {
		t.Errorf("help text lost the agents command")
	}
}

// testStrPtr is the one-line *string helper for the write-lane calls
// below (cmd/omega's tests have no shared pointer helper).
func testStrPtr(s string) *string { return &s }

// TestAgentConfigLayerTier3bRoundTrip is the write lane's end-to-end
// witness (settings tier 3b prerequisite, card t_53523d5b): the two
// additive PUT fields are exercised through the real write lane and read
// back by BOTH consumers - the wake's layer overlay (ApplyAgentLayer) and
// the settings scope payload (settingsScopes) - because a write nobody
// reads is not a write lane.
//
// The reset half is the reason the lane grew a delete verb at all: the
// scope payload's overridden flag is STRUCTURAL (the file names the
// key), so "reset to main" must REMOVE the key, never write main's value
// down (which would leave a permanent phantom override, and for
// provider.api_key would persist the read mask as the agent's secret).
func TestAgentConfigLayerTier3bRoundTrip(t *testing.T) {
	// The agent package cannot import this package's mask constant
	// (layer law), so its mirror must be pinned equal here or the
	// refusal lane guards a different string than the read lane emits.
	if agent.MaskedSecretPlaceholder != maskedSecret {
		t.Fatalf("agent.MaskedSecretPlaceholder = %q, maskedSecret = %q - the mask mirror drifted",
			agent.MaskedSecretPlaceholder, maskedSecret)
	}

	home := scopeHome(t, scopeMainConfig, map[string]string{"alpha": scopeAlphaConfig})
	mainConfigPath := filepath.Join(home, "config", "config.yaml")

	// --- max_turns: write -> layer overlay --------------------------
	turns := 42
	if err := agent.UpdateAgentConfigLayer(home, "alpha", agent.AgentConfigUpdates{MaxTurns: &turns}); err != nil {
		t.Fatalf("write max_turns: %v", err)
	}
	lay, err := agent.ReadAgentConfigLayer(home, "alpha")
	if err != nil {
		t.Fatalf("read layer: %v", err)
	}
	if lay.MaxTurns == nil || *lay.MaxTurns != 42 {
		t.Fatalf("layer max_turns = %v, want 42", lay.MaxTurns)
	}

	// The wake's own overlay: the layer's int lands on cfg.
	cfg := agentLayerBase()
	cfg.MaxTurns = 11 // main's value, which the layer must win over
	if err := ApplyAgentLayer(&cfg, home, "alpha"); err != nil {
		t.Fatalf("apply layer: %v", err)
	}
	if cfg.MaxTurns != 42 {
		t.Fatalf("cfg.MaxTurns after the layer = %d, want 42 (the layer's int)", cfg.MaxTurns)
	}

	// --- the settings scope payload: 42 is the agent's own -----------
	schema, err := settingsSchema(mainConfigPath)
	if err != nil {
		t.Fatalf("settingsSchema: %v", err)
	}
	alpha := scopeByName(t, schema, "alpha")
	if k := scopeKnob(t, alpha, "max_turns"); k.Value != 42 || !k.Overridden {
		t.Fatalf("alpha max_turns = %+v, want the written 42, overridden", k)
	}

	// --- reset: the key goes, main's value comes back ---------------
	if err := agent.UpdateAgentConfigLayer(home, "alpha", agent.AgentConfigUpdates{
		Unset: []string{"system_prompt", "max_turns"},
	}); err != nil {
		t.Fatalf("unset: %v", err)
	}
	lay, err = agent.ReadAgentConfigLayer(home, "alpha")
	if err != nil {
		t.Fatalf("read layer after unset: %v", err)
	}
	if lay.MaxTurns != nil || lay.SystemPrompt.Set {
		t.Fatalf("after the reset the layer still overrides: %+v", lay)
	}

	// The wake inherits main again.
	cfg = agentLayerBase()
	cfg.MaxTurns = 11
	cfg.SystemPrompt = "main prompt"
	if err := ApplyAgentLayer(&cfg, home, "alpha"); err != nil {
		t.Fatalf("apply layer after unset: %v", err)
	}
	if cfg.MaxTurns != 11 {
		t.Errorf("cfg.MaxTurns after the reset = %d, want main's 11", cfg.MaxTurns)
	}
	if cfg.SystemPrompt != "main prompt" {
		t.Errorf("cfg.SystemPrompt after the reset = %q, want main's", cfg.SystemPrompt)
	}

	// The scope payload says "inherited", not "overridden with main's
	// value" - asserted against the payload's OWN main knob, so the two
	// surfaces cannot drift.
	schema, err = settingsSchema(mainConfigPath)
	if err != nil {
		t.Fatalf("settingsSchema after unset: %v", err)
	}
	alpha = scopeByName(t, schema, "alpha")
	if k := scopeKnob(t, alpha, "max_turns"); k.Overridden || k.Value != mainKnob(t, schema, "max_turns").Value {
		t.Errorf("alpha max_turns = %+v, want inherited main value + overridden=false", k)
	}
	if k := scopeKnob(t, alpha, "system_prompt"); k.Overridden || k.Value != mainKnob(t, schema, "system_prompt").Value {
		t.Errorf("alpha system_prompt = %+v, want inherited main value + overridden=false", k)
	}
	// The knobs the reset did not name keep their overrides.
	if k := scopeKnob(t, alpha, "provider.type"); !k.Overridden {
		t.Errorf("provider.type = %+v, want the untouched override", k)
	}
	if k := scopeKnob(t, alpha, "provider.api_key"); !k.Overridden || k.Value != maskedSecret {
		t.Errorf("provider.api_key = %+v, want the mask + overridden", k)
	}

	// --- a provider sub-key reset falls back for THAT knob only -----
	if err := agent.UpdateAgentConfigLayer(home, "alpha", agent.AgentConfigUpdates{
		ModelName: testStrPtr("scout-model"),
	}); err != nil {
		t.Fatalf("set model_name: %v", err)
	}
	if err := agent.UpdateAgentConfigLayer(home, "alpha", agent.AgentConfigUpdates{
		Unset: []string{"provider.model_name"},
	}); err != nil {
		t.Fatalf("unset model_name: %v", err)
	}
	cfg = agentLayerBase()
	cfg.Provider.ModelName = "main-model"
	if err := ApplyAgentLayer(&cfg, home, "alpha"); err != nil {
		t.Fatalf("apply layer after model_name reset: %v", err)
	}
	if cfg.Provider.ModelName != "main-model" {
		t.Errorf("model_name after the reset = %q, want main's main-model", cfg.Provider.ModelName)
	}
	if cfg.Provider.Type != "openai" {
		t.Errorf("provider.type after the model_name reset = %q, want the layer's openai", cfg.Provider.Type)
	}
	schema, err = settingsSchema(mainConfigPath)
	if err != nil {
		t.Fatalf("settingsSchema after model_name reset: %v", err)
	}
	alpha = scopeByName(t, schema, "alpha")
	if k := scopeKnob(t, alpha, "provider.model_name"); k.Overridden {
		t.Errorf("provider.model_name = %+v, want inherited after its reset", k)
	}
	if k := scopeKnob(t, alpha, "provider.type"); !k.Overridden || k.Value != "openai" {
		t.Errorf("provider.type = %+v, want the untouched openai override", k)
	}

	// --- no path ever persists the read mask as the secret ----------
	if err := agent.UpdateAgentConfigLayer(home, "alpha", agent.AgentConfigUpdates{
		APIKey: testStrPtr(agent.MaskedSecretPlaceholder),
	}); err == nil {
		t.Error("the write lane accepted the read mask as an api_key")
	}
	raw, err := os.ReadFile(agent.AgentConfigPath(home, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), maskedSecret) {
		t.Errorf("the layer carries the read mask:\n%s", raw)
	}
}
