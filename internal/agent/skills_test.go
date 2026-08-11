package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSkills(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "learn.md"), `---
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
	writeFile(t, filepath.Join(dir, "plain.md"), "Just some markdown content.\nNo frontmatter here.")

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

func TestLoadSkillsSkipsNonMD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a skill")
	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills, got %d", len(skills))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}
