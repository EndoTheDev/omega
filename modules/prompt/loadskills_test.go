package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/logging"
	"github.com/EndoTheDev/omega/modules/skills"
)

// TestBuildPromptNoSectioners verifies the prompt builder produces a
// valid prompt when no module contributes prompt sections.
func TestBuildPromptNoSectioners(t *testing.T) {
	b := NewPromptBuilder(&core.Context{})
	prompt, ok := b.BuildPrompt(context.Background(), core.PromptBuildOptions{})
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
	// The file is always SKILL.md and the folder name is the skill
	// name (card t_5baad951) - the frontmatter name is retired, so the
	// prompt must show "my-skill", never "Custom Name".
	content := "---\nname: Custom Name\ndescription: A custom description\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := &core.Context{
		Configs: map[string]any{
			"skills":  skills.Config{Dir: dir},
			"logging": logging.Config{Enabled: false},
		},
	}
	if err := agent.MountAll([]agent.Plugin{logging.NewPlugin(), skills.NewPlugin(), NewPlugin()}, ctx); err != nil {
		t.Fatalf("MountAll: %v", err)
	}
	prompt, ok := ctx.PromptBuilder.BuildPrompt(context.Background(), core.PromptBuildOptions{})
	if !ok {
		t.Fatal("BuildPrompt returned false")
	}
	if !strings.Contains(prompt, "my-skill") {
		t.Error("prompt missing skill name 'my-skill' (folder name is authoritative)")
	}
	if strings.Contains(prompt, "Custom Name") {
		t.Error("prompt shows retired frontmatter name 'Custom Name' - folder name must win")
	}
	if !strings.Contains(prompt, "A custom description") {
		t.Error("prompt missing skill description")
	}
}