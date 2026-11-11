package main

// agentruns_test.go: the host's per-agent run factory (card t_a7fab745).
// Law: the agent the chat floor runs for a home carries THAT home's
// provider/model, system prompt and max_turns - the agent's config layer
// over the home config, mounted the same way `omega run` mounts an
// OMEGA_AGENT child.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func TestAgentRunFactoryLayersTheHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("OMEGA_AGENT", "")
	t.Setenv("OMEGA_MODEL", "")

	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "config.yaml"), []byte(
		"provider:\n  type: ollama\n  model_name: daemon-model\n"+
			"system_prompt: daemon prompt\nmax_turns: 3\nserver:\n  port: 8099\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatalf("scaffold scout: %v", err)
	}
	if err := os.WriteFile(agent.AgentConfigPath(home, "scout"), []byte(
		"provider:\n  model_name: scout-model\nsystem_prompt: scout prompt\nmax_turns: 9\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	build := &agentRuns{}
	ag, err := build.agent(context.Background(), "scout")
	if err != nil {
		t.Fatalf("build scout agent: %v", err)
	}
	// The daemon holds these mounts for its lifetime; the test releases
	// them so windows can delete the temp home.
	defer func() {
		if err := build.close(); err != nil {
			t.Fatalf("close agent mounts: %v", err)
		}
	}()
	if got := ag.ModelName(); got != "scout-model" {
		t.Fatalf("agent model = %q, want the layer's scout-model", got)
	}
	if got := ag.PromptCustom(); got != "scout prompt" {
		t.Fatalf("agent system prompt = %q, want the layer's", got)
	}
	if got := ag.MaxTurns(); got != 9 {
		t.Fatalf("agent max_turns = %d, want the layer's 9", got)
	}

	// One build per home per process: the second ask is the same agent.
	again, err := build.agent(context.Background(), "scout")
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	if again != ag {
		t.Fatal("the factory rebuilt the agent instead of serving the cached one")
	}

	// The daemon's own config is untouched by an agent build.
	base, err := LoadConfig(resolveConfigPath(""))
	if err != nil {
		t.Fatalf("reload base config: %v", err)
	}
	if base.Provider.ModelName != "daemon-model" || base.SystemPrompt != "daemon prompt" || base.MaxTurns != 3 {
		t.Fatalf("base config = %q / %q / %d, want the daemon's own values",
			base.Provider.ModelName, base.SystemPrompt, base.MaxTurns)
	}
}
