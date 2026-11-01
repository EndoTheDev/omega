package agent

// engine/agent/configedit_test.go: the agents-tab edit write path
// gates (card t_2d8deb8c). The law: node-preserving upserts only -
// a hand-edited layer's comments and unknown keys survive every
// write, the default agent never gets written, and an unscaffolded
// provider type refuses loudly.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func strPtr(s string) *string { return &s }

// TestUpdateAgentConfigLayerUpsertsAndPreserves pins the write law: a
// commented scaffold layer keeps its comments and gains the fields.
func TestUpdateAgentConfigLayerUpsertsAndPreserves(t *testing.T) {
	home := t.TempDir()
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	seeds := "description: \"probe duty\"\n# keep this comment\nprovider:\n  type: ollama\n  # model comment\n  model_name: glm-5.3\n"
	path := AgentConfigPath(home, "scout")
	if err := WriteFileAtomic(path, []byte(seeds), 0o644); err != nil {
		t.Fatal(err)
	}

	err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{
		Description: strPtr("night watch"),
		ModelName:   strPtr("claude-4"),
		Host:        strPtr("http://localhost:11434"),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(out)
	if !strings.Contains(body, "night watch") {
		t.Errorf("description not upserted:\n%s", body)
	}
	if !strings.Contains(body, "model_name: claude-4") {
		t.Errorf("model_name not upserted:\n%s", body)
	}
	if !strings.Contains(body, "host: http://localhost:11434") {
		t.Errorf("host not upserted:\n%s", body)
	}
	if !strings.Contains(body, "keep this comment") || !strings.Contains(body, "model comment") {
		t.Errorf("untouched keys lost their comments:\n%s", body)
	}
	if !strings.Contains(body, "type: ollama") {
		t.Errorf("untouched provider.type was rewritten:\n%s", body)
	}
	// The API key is nil: absent facts can never be blanked.
	if strings.Contains(body, "api_key") {
		t.Errorf("nil api_key was written:\n%s", body)
	}

	// The profile read reflects the edit (the same parse the tab shows).
	p := loadProfile(home, "scout")
	if p.Description != "night watch" || p.ModelName != "claude-4" {
		t.Errorf("profile after edit: %+v", p)
	}
}

// TestUpdateAgentConfigLayerFreshFile writes a missing layer file.
func TestUpdateAgentConfigLayerFreshFile(t *testing.T) {
	home := t.TempDir()
	if err := ScaffoldAgent(home, "writer", ""); err != nil {
		t.Fatal(err)
	}
	path := AgentConfigPath(home, "writer")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	err := UpdateAgentConfigLayer(home, "writer", AgentConfigUpdates{
		SystemPrompt: strPtr("be terse"),
		ProviderType: strPtr("openai"),
	})
	if err != nil {
		t.Fatal(err)
	}
	p := loadProfile(home, "writer")
	if p.ProviderType != "openai" {
		t.Errorf("provider type = %q", p.ProviderType)
	}
}

// TestUpdateAgentConfigLayerRefusals pins the two refusal lanes.
func TestUpdateAgentConfigLayerRefusals(t *testing.T) {
	home := t.TempDir()
	if err := ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	// Give the home config a sentinel body first: a missing file reads
	// as a phantom write otherwise (both "before" and "after" are
	// absence, indistinguishable from a write).
	sentinel := "# home config sentinel\nprovider:\n  type: ollama\n"
	if err := WriteFileAtomic(AgentConfigPath(home, DefaultAgentName), []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	err := UpdateAgentConfigLayer(home, DefaultAgentName, AgentConfigUpdates{Description: strPtr("x")})
	if !errors.Is(err, ErrAgentConfigLocked) {
		t.Fatalf("default edit err = %v, want ErrAgentConfigLocked", err)
	}
	if after, _ := os.ReadFile(AgentConfigPath(home, DefaultAgentName)); string(after) != sentinel {
		t.Errorf("the default agent's config was written")
	}
	// unscaffolded provider type: the loud 400 lane.
	err = UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{ProviderType: strPtr("gemini")})
	var scaffold *AgentConfigScaffoldError
	if !errors.As(err, &scaffold) {
		t.Fatalf("bad provider err = %v, want AgentConfigScaffoldError", err)
	}
	if !filepath.IsAbs(AgentConfigPath(home, "scout")) {
		t.Fatal("path helper broke")
	}
}