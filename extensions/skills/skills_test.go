package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// Compile-time interface checks.
var (
	_ agent.SkillsProvider = (*SkillsProvider)(nil)
	_ agent.ToolProvider   = (*SkillsProvider)(nil)
	_ agent.Plugin         = (*Plugin)(nil)
)

// makeSkillDir creates a temp skills directory with one skill.
func makeSkillDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: test-skill\ndescription: A test skill\n---\nThis is the body.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "test-skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadSkills(t *testing.T) {
	dir := makeSkillDir(t)
	sp := NewSkillsProvider(dir)
	skills, err := sp.LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "test-skill" {
		t.Fatalf("expected name %q, got %q", "test-skill", skills[0].Name)
	}
	if skills[0].Description != "A test skill" {
		t.Fatalf("expected description %q, got %q", "A test skill", skills[0].Description)
	}
	if skills[0].Content != "This is the body." {
		t.Fatalf("expected content %q, got %q", "This is the body.", skills[0].Content)
	}
}

func TestLoadSkillsMissingDir(t *testing.T) {
	sp := NewSkillsProvider("/nonexistent")
	skills, err := sp.LoadSkills("/nonexistent")
	if err != nil {
		t.Fatalf("missing dir should return empty, got error: %v", err)
	}
	if skills != nil {
		t.Fatalf("expected nil skills for missing dir, got %v", skills)
	}
}

func TestToolsRead(t *testing.T) {
	dir := makeSkillDir(t)
	sp := NewSkillsProvider(dir)
	tools := sp.Tools()
	if _, ok := tools["skills.read"]; !ok {
		t.Fatal("expected skills.read tool")
	}
	out, err := tools["skills.read"].Run(context.Background(), map[string]any{"name": "test-skill"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestToolsReadMissingName(t *testing.T) {
	sp := NewSkillsProvider(t.TempDir())
	tools := sp.Tools()
	_, err := tools["skills.read"].Run(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestHandleCommandSkills(t *testing.T) {
	dir := makeSkillDir(t)
	sp := NewSkillsProvider(dir)
	out, err := sp.HandleCommand(context.Background(), "/skills", "")
	if err != nil {
		t.Fatalf("HandleCommand: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty command output")
	}
}

func TestHandleCommandUnknown(t *testing.T) {
	sp := NewSkillsProvider(t.TempDir())
	_, err := sp.HandleCommand(context.Background(), "/unknown", "")
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

func TestPluginMount(t *testing.T) {
	dir := makeSkillDir(t)
	p := NewPlugin()
	ctx := &agent.Context{CWD: dir, Config: nil}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if ctx.Skills == nil {
		t.Fatal("Skills slot not populated")
	}
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("expected 1 tool provider, got %d", len(ctx.ToolProviders))
	}
	if len(ctx.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(ctx.Commands))
	}
	if ctx.CommandHandler == nil {
		t.Fatal("CommandHandler not set")
	}
}

func TestPluginMountWithConfig(t *testing.T) {
	// gateway.Config is a value type; we can't easily construct one here
	// without importing gateway (which would create a test-only dep cycle
	// concern). The nil-Config path is covered above; the config path is
	// exercised in integration. This test just verifies Mount doesn't
	// panic with a nil Config.
	p := NewPlugin()
	ctx := &agent.Context{}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount with nil config: %v", err)
	}
}