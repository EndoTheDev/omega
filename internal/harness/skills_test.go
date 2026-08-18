package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSkills(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "learn-skill", `---
name: learn-skill
description: Teaches the agent a new skill
---

You are a skill-learning assistant. When the user provides a SKILL.md file,
parse its YAML frontmatter and add it to the skills directory.
`)

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	s := skills[0]
	if s.Name != "learn-skill" {
		t.Errorf("name = %q, want learn-skill", s.Name)
	}
	if s.Description != "Teaches the agent a new skill" {
		t.Errorf("description = %q", s.Description)
	}
	if s.Content == "" {
		t.Error("content is empty")
	}
	if s.Dir != filepath.Join(dir, "learn-skill") {
		t.Errorf("dir = %q, want %q", s.Dir, filepath.Join(dir, "learn-skill"))
	}
}

func TestLoadSkillsMultiple(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "alpha", "---\nname: alpha\ndescription: Alpha skill\n---\nAlpha content.")
	writeSkill(t, dir, "beta", "---\nname: beta\ndescription: Beta skill\n---\nBeta content.")

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
}

func TestLoadSkillsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills, got %d", len(skills))
	}
}

func TestLoadSkillsMissingDir(t *testing.T) {
	skills, err := LoadSkills(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if skills != nil {
		t.Fatalf("expected nil skills for missing dir, got %v", skills)
	}
}

func TestLoadSkillsNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "plain", "Just some markdown content.\nNo frontmatter here.")

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "" {
		t.Errorf("name = %q, want empty", skills[0].Name)
	}
	if skills[0].Content == "" {
		t.Error("content is empty for no-frontmatter file")
	}
}

func TestLoadSkillsSkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	// Hidden directory should be skipped.
	if err := os.Mkdir(filepath.Join(dir, ".hidden"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden", ".hidden.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills, got %d", len(skills))
	}
}

func TestLoadSkillsSkipsDirWithoutSkillFile(t *testing.T) {
	dir := t.TempDir()
	// Directory with no matching .md file should be skipped silently.
	if err := os.Mkdir(filepath.Join(dir, "empty-skill"), 0755); err != nil {
		t.Fatal(err)
	}
	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills, got %d", len(skills))
	}
}

// writeSkill creates a skill directory with a <name>/<name>.md file
// inside dir.
func writeSkill(t *testing.T, dir, name, content string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", skillDir, err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, name+".md"), []byte(content), 0644); err != nil {
		t.Fatalf("writeFile %s: %v", filepath.Join(skillDir, name+".md"), err)
	}
}
