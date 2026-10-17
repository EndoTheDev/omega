package skills

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	ctx := &agent.Context{CWD: dir}
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
	// The Configs map routes per-extension config. The nil-Configs path
	// is covered above; the config path is exercised in integration.
	// This test just verifies Mount doesn't
	// panic with a nil Config.
	p := NewPlugin()
	ctx := &agent.Context{}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount with nil config: %v", err)
	}
}

// --- Coverage tests for uncovered branches ---

// TestRunReadDirNotConfigured covers the sp.Dir == "" branch of runRead.
func TestRunReadDirNotConfigured(t *testing.T) {
	sp := &SkillsProvider{Dir: ""}
	tools := sp.Tools()
	_, err := tools["skills.read"].Run(context.Background(), map[string]any{"name": "whatever"})
	if err == nil {
		t.Fatal("expected error when skills directory not configured")
	}
	if !strings.Contains(err.Error(), "skills directory not configured") {
		t.Fatalf("expected 'skills directory not configured' error, got: %v", err)
	}
}

// TestRunReadSkillNotFound covers the os.IsNotExist path of runRead,
// including the available skill listing.
func TestRunReadSkillNotFound(t *testing.T) {
	dir := makeSkillDir(t)
	sp := NewSkillsProvider(dir)
	tools := sp.Tools()
	_, err := tools["skills.read"].Run(context.Background(), map[string]any{"name": "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
	if !strings.Contains(err.Error(), `skill "nonexistent" not found`) {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
	// The available listing should mention the test-skill directory.
	if !strings.Contains(err.Error(), "test-skill") {
		t.Fatalf("expected available listing to contain 'test-skill', got: %v", err)
	}
}

// TestRunReadSkillNotFoundEmptyDir covers the os.IsNotExist path when
// the skills directory has no subdirectories (empty available list).
func TestRunReadSkillNotFoundEmptyDir(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	tools := sp.Tools()
	_, err := tools["skills.read"].Run(context.Background(), map[string]any{"name": "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
	if !strings.Contains(err.Error(), `skill "nonexistent" not found`) {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

// TestRunReadNonNotExistError covers the non-NotExist error path from
// loadSkill in runRead. We trigger this by making the skill file a
// directory instead of a readable file, which causes os.Open to fail
// with a non-NotExist error.
func TestRunReadNonNotExistError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based tests are unreliable on Windows")
	}
	dir := t.TempDir()
	// Create a skill "directory" where the .md file would be, but make
	// it a directory with no read permission so os.Open fails with a
	// non-IsNotExist error.
	skillName := "badskill"
	skillDir := filepath.Join(dir, skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create the .md path as a directory (not a file) so os.Open fails.
	mdPath := filepath.Join(skillDir, skillName+".md")
	if err := os.MkdirAll(mdPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Remove read permission on the .md "file" (which is a dir).
	if err := os.Chmod(mdPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(mdPath, 0o755)
	})

	sp := NewSkillsProvider(dir)
	tools := sp.Tools()
	_, err := tools["skills.read"].Run(context.Background(), map[string]any{"name": skillName})
	if err == nil {
		t.Fatal("expected error for non-NotExist loadSkill failure")
	}
	// The error should NOT be a "not found" error.
	if strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected non-NotExist error, got 'not found': %v", err)
	}
}

// TestScanSkillsNonNotExistError covers the non-NotExist error on
// os.ReadDir in scanSkills.
func TestScanSkillsNonNotExistError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based tests are unreliable on Windows")
	}
	dir := t.TempDir()
	// Create a subdirectory, then revoke all permissions on the parent
	// so ReadDir fails with a permission error (not IsNotExist).
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
	})

	_, err := scanSkills(dir)
	if err == nil {
		t.Fatal("expected error from scanSkills with unreadable dir")
	}
	if os.IsNotExist(err) {
		t.Fatalf("expected non-NotExist error, got: %v", err)
	}
}

// TestScanSkillsLoadSkillNonNotExistError covers the non-NotExist error
// from loadSkill inside the scanSkills loop.
func TestScanSkillsLoadSkillNonNotExistError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based tests are unreliable on Windows")
	}
	dir := t.TempDir()
	skillName := "badload"
	skillDir := filepath.Join(dir, skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create the .md path as a directory (not a file) so os.Open fails
	// with a non-IsNotExist error.
	mdPath := filepath.Join(skillDir, skillName+".md")
	if err := os.MkdirAll(mdPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(mdPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(mdPath, 0o755)
	})

	_, err := scanSkills(dir)
	if err == nil {
		t.Fatal("expected error from scanSkills with unreadable skill file")
	}
	if os.IsNotExist(err) {
		t.Fatalf("expected non-NotExist error, got: %v", err)
	}
	if !strings.Contains(err.Error(), skillName) {
		t.Fatalf("expected error to contain skill name %q, got: %v", skillName, err)
	}
}

// TestLoadSkillNoFrontmatter covers the branch in loadSkill where the
// first line is not "---" — the file has no frontmatter.
func TestLoadSkillNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nofrontmatter.md")
	content := "This is just markdown.\nNo frontmatter here.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := loadSkill(path)
	if err != nil {
		t.Fatalf("loadSkill: %v", err)
	}
	if s.Name != "" {
		t.Fatalf("expected empty name for no-frontmatter file, got %q", s.Name)
	}
	if s.Description != "" {
		t.Fatalf("expected empty description for no-frontmatter file, got %q", s.Description)
	}
	if !strings.Contains(s.Content, "This is just markdown.") {
		t.Fatalf("expected content to contain body text, got %q", s.Content)
	}
}

// TestHandleCommandEmptySkills covers the [no skills loaded] path in
// HandleCommand when scanSkills returns an empty slice.
func TestHandleCommandEmptySkills(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	out, err := sp.HandleCommand(context.Background(), "/skills", "")
	if err != nil {
		t.Fatalf("HandleCommand: %v", err)
	}
	if !strings.Contains(out, "[no skills loaded]") {
		t.Fatalf("expected '[no skills loaded]' output, got %q", out)
	}
}

// TestPluginMountWithDirConfig covers the Mount path where
// ctx.Configs has a skills config with Dir set.
func TestPluginMountWithDirConfig(t *testing.T) {
	dir := makeSkillDir(t)
	p := NewPlugin()
	ctx := &agent.Context{
		Configs: map[string]any{
			"skills": Config{Dir: dir},
		},
	}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount with config: %v", err)
	}
	if ctx.Skills == nil {
		t.Fatal("Skills slot not populated")
	}
	// Verify the provider uses the configured dir by loading skills.
	sp, ok := ctx.Skills.(*SkillsProvider)
	if !ok {
		t.Fatal("expected *SkillsProvider")
	}
	if sp.Dir != dir {
		t.Fatalf("expected Dir %q, got %q", dir, sp.Dir)
	}
	skills, err := sp.LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill from configured dir, got %d", len(skills))
	}
}