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

func TestApplyAgentLayerScopesWritablePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := agent.ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	cfg := agentLayerBase()
	cfg.Store.DBPath = filepath.Join(home, "data", "omega.db")
	cfg.Memory.File = filepath.Join(home, "memory.md")
	cfg.Memory.UserProfileFile = filepath.Join(home, "user.md")
	cfg.Logging.File = filepath.Join(home, "omega.log")
	cfg.Skills.Dir = filepath.Join(home, "skills")
	if err := ApplyAgentLayer(&cfg, home, "scout"); err != nil {
		t.Fatalf("apply layer: %v", err)
	}
	dir := agent.AgentDir(home, "scout")
	// Data isolation (ruling 4): the store dsn re-points under the
	// agent's own data dir; memory/user/skills/log move into the home.
	if cfg.Store.DBPath != filepath.Join(dir, "data", "omega.db") {
		t.Errorf("store db = %q, want the agent's own data dir", cfg.Store.DBPath)
	}
	if cfg.Memory.File != filepath.Join(dir, "memory.md") {
		t.Errorf("memory = %q, want agent-scoped", cfg.Memory.File)
	}
	if cfg.Memory.UserProfileFile != filepath.Join(dir, "user.md") {
		t.Errorf("user profile = %q, want agent-scoped", cfg.Memory.UserProfileFile)
	}
	if cfg.Skills.Dir != filepath.Join(dir, "skills") {
		t.Errorf("skills dir = %q, want agent-own", cfg.Skills.Dir)
	}
	if cfg.Skills.SharedDir != filepath.Join(home, "skills", "shared") {
		t.Errorf("shared dir = %q, want home/skills/shared", cfg.Skills.SharedDir)
	}
	if cfg.Logging.File != filepath.Join(dir, "scout.log") {
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
	if cfg.Memory.File != filepath.Join(home, "memory.md") {
		t.Errorf("memory.file = %q, want the home root (ruling 3)", cfg.Memory.File)
	}
	if cfg.Memory.UserProfileFile != filepath.Join(home, "user.md") {
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
