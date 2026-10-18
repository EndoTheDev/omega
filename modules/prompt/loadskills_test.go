package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/modules/skills"
)

// TestBuildPromptNoSectioners verifies the prompt builder produces a
// valid prompt when no module contributes prompt sections.
func TestBuildPromptNoSectioners(t *testing.T) {
	b := NewPromptBuilder(&agent.Context{})
	prompt, ok := b.BuildPrompt(context.Background(), agent.PromptBuildOptions{})
	if !ok {
		t.Fatal("BuildPrompt returned false")
	}
	if prompt == "" {
		t.Fatal("BuildPrompt returned empty prompt")
	}
	if !strings.Contains(prompt, "## Guidelines") {
		t.Error("prompt missing Guidelines section")
	}
}

// TestBuildPromptWithSkillsSection verifies skills mounted through the
// skills plugin appear in the Available Skills section, contributed by
// the skills module itself.
func TestBuildPromptWithSkillsSection(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: Custom Name\ndescription: A custom description\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "my-skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := &agent.Context{
		Configs: map[string]any{"skills": skills.Config{Dir: dir}},
	}
	if err := agent.MountAll([]agent.Plugin{skills.NewPlugin(), NewPlugin()}, ctx); err != nil {
		t.Fatalf("MountAll: %v", err)
	}
	prompt, ok := ctx.PromptBuilder.BuildPrompt(context.Background(), agent.PromptBuildOptions{})
	if !ok {
		t.Fatal("BuildPrompt returned false")
	}
	if !strings.Contains(prompt, "Custom Name") {
		t.Error("prompt missing skill name 'Custom Name'")
	}
	if !strings.Contains(prompt, "A custom description") {
		t.Error("prompt missing skill description")
	}
}