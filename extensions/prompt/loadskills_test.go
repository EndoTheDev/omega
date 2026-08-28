package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// TestLoadSkillsEmptyDir verifies loadSkills returns nil when no
// skills directory is configured.
func TestLoadSkillsEmptyDir(t *testing.T) {
	t.Setenv("OMEGA_SKILLS_DIR", "")
	b := NewPromptBuilder("", nil)
	if skills := b.loadSkills(); skills != nil {
		t.Errorf("loadSkills() with empty dir = %v, want nil", skills)
	}
}

// TestLoadSkillsMissingDir verifies loadSkills returns nil when the
// skills directory does not exist or is unreadable.
func TestLoadSkillsMissingDir(t *testing.T) {
	b := NewPromptBuilder(filepath.Join(t.TempDir(), "does-not-exist"), nil)
	if skills := b.loadSkills(); skills != nil {
		t.Errorf("loadSkills() with missing dir = %v, want nil", skills)
	}
}

// TestLoadSkillsValid verifies a skill dir with frontmatter is parsed
// with name/description overrides applied.
func TestLoadSkillsValid(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: Custom Name\ndescription: A custom description\n---\n\nBody text here.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "my-skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewPromptBuilder(dir, nil)
	got := b.loadSkills()
	if len(got) != 1 {
		t.Fatalf("loadSkills() returned %d skills, want 1: %v", len(got), got)
	}
	want := agent.Skill{Name: "Custom Name", Description: "A custom description", Dir: skillDir}
	if got[0] != want {
		t.Errorf("loadSkills()[0] = %+v, want %+v", got[0], want)
	}
}

// TestLoadSkillsDefaults verifies frontmatter without name/description
// keys keeps the dirname-based defaults.
func TestLoadSkillsDefaults(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "plain-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nversion: 2\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "plain-skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewPromptBuilder(dir, nil)
	got := b.loadSkills()
	if len(got) != 1 {
		t.Fatalf("loadSkills() returned %d skills, want 1: %v", len(got), got)
	}
	want := agent.Skill{Name: "plain-skill", Dir: skillDir}
	if got[0] != want {
		t.Errorf("loadSkills()[0] = %+v, want %+v", got[0], want)
	}
}

// TestLoadSkillsSkips verifies non-dir entries, dot-dirs, and skill
// dirs without a matching .md file are all skipped.
func TestLoadSkillsSkips(t *testing.T) {
	dir := t.TempDir()

	// Plain file in skills dir (not a dir).
	if err := os.WriteFile(filepath.Join(dir, "loose.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Dot-dir with a valid skill file (must still be skipped).
	dotDir := filepath.Join(dir, ".hidden")
	if err := os.MkdirAll(dotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dotDir, ".hidden.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Skill dir without a matching .md file.
	noMd := filepath.Join(dir, "nomd-skill")
	if err := os.MkdirAll(noMd, 0o755); err != nil {
		t.Fatal(err)
	}

	b := NewPromptBuilder(dir, nil)
	if skills := b.loadSkills(); skills != nil {
		t.Errorf("loadSkills() = %v, want nil (all entries skipped)", skills)
	}
}
