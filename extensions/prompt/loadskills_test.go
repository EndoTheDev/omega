package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/extensions/skills"
)

// TestBuildPromptNilSkills verifies the prompt builder produces a
// valid prompt when no skills provider is mounted.
func TestBuildPromptNilSkills(t *testing.T) {
	b := NewPromptBuilder(nil, nil)
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

// TestBuildPromptWithSkills verifies skills from the SkillsProvider
// appear in the Available Skills section.
func TestBuildPromptWithSkills(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: Custom Name\ndescription: A custom description\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "my-skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	sp := skills.NewSkillsProvider(dir)
	b := NewPromptBuilder(sp, nil)
	prompt, ok := b.BuildPrompt(context.Background(), agent.PromptBuildOptions{})
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
