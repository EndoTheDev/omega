package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Compile-time interface checks.
var (
	_ core.SkillsProvider = (*SkillsProvider)(nil)
	_ core.ToolProvider   = (*SkillsProvider)(nil)
	_ agent.Plugin        = (*Plugin)(nil)
)

// makeSkillDir creates a temp skills directory with one skill in the
// contracted shape: <name>/SKILL.md, the folder name is the skill
// name. The frontmatter deliberately carries a different name to pin
// the ruling that the folder wins (card t_5baad951).
func makeSkillDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: Not The Folder\ndescription: A test skill\n---\nThis is the body.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
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

// TestLoadSkillsLegacyShapeNotLoaded pins the retired format: a folder
// holding <name>.md without SKILL.md is invisible to discovery (card
// t_5baad951). Only the SKILL.md sibling loads.
func TestLoadSkillsLegacyShapeNotLoaded(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "old-skill", "old-skill.md")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("---\ndescription: legacy shape\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeProperSkill(t, dir, "new-skill")

	sp := NewSkillsProvider(dir)
	skills, err := sp.LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill (legacy folder invisible), got %d: %+v", len(skills), skills)
	}
	if skills[0].Name != "new-skill" {
		t.Fatalf("expected new-skill, got %q", skills[0].Name)
	}
}

// TestWarnLegacyShape covers the boot warning: a legacy folder is
// reported by name with the rename cure, a proper folder is silent.
func TestWarnLegacyShape(t *testing.T) {
	dir := t.TempDir()
	writeProperSkill(t, dir, "proper-skill")
	legacyDir := filepath.Join(dir, "old-skill")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "old-skill.md"), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sp := NewSkillsProvider(dir)
	log := &captureLogger{}
	warned := sp.WarnLegacyShape(log)
	if len(warned) != 1 || warned[0] != "old-skill" {
		t.Fatalf("warned = %v, want [old-skill]", warned)
	}
	if len(log.msgs) != 1 {
		t.Fatalf("expected 1 log warning, got %d: %v", len(log.msgs), log.msgs)
	}
	if !strings.Contains(log.msgs[0], `skill "old-skill"`) || !strings.Contains(log.msgs[0], "rename the file to SKILL.md") {
		t.Fatalf("warning must name the skill and the rename, got: %q", log.msgs[0])
	}

	// A proper skills tree warns nothing.
	clean := NewSkillsProvider(makeSkillDir(t))
	log2 := &captureLogger{}
	if got := clean.WarnLegacyShape(log2); len(got) != 0 || len(log2.msgs) != 0 {
		t.Fatalf("proper shape must warn nothing, got %v / %v", got, log2.msgs)
	}
}

// TestPluginMountWarnsLegacyShape covers the mount-time hook: a legacy
// folder under the configured dir surfaces the boot warning through
// the context logger.
func TestPluginMountWarnsLegacyShape(t *testing.T) {
	dir := t.TempDir()
	legacyDir := filepath.Join(dir, "old-skill")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "old-skill.md"), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	log := &captureLogger{}
	p := NewPlugin()
	ctx := &core.Context{
		Logger: log,
		Configs: map[string]any{
			"skills": Config{Dir: dir},
		},
	}
	if _, err := agent.MountAllAPI([]agent.Plugin{stubProvides{seam: "logging"}, p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if len(log.msgs) != 1 {
		t.Fatalf("expected 1 boot warning, got %d: %v", len(log.msgs), log.msgs)
	}
	if !strings.Contains(log.msgs[0], "old-skill") {
		t.Fatalf("boot warning must name the skill, got: %q", log.msgs[0])
	}
}

// writeProperSkill seeds dir with <name>/SKILL.md in the contracted
// shape (card t_5baad951).
func writeProperSkill(t *testing.T, dir, name string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: the " + name + " skill\n---\nbody of " + name + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// captureLogger is a core.LoggerProvider test double.
type captureLogger struct {
	msgs []string
}

func (c *captureLogger) Printf(format string, args ...any) {
	c.msgs = append(c.msgs, fmt.Sprintf(format, args...))
}

func (c *captureLogger) Errorf(format string, args ...any) {
	c.msgs = append(c.msgs, "ERROR: "+fmt.Sprintf(format, args...))
}

func (c *captureLogger) Close() error { return nil }

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
	ctx := &core.Context{CWD: dir}
	if _, err := agent.MountAllAPI([]agent.Plugin{stubProvides{seam: "logging"}, p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
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
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{stubProvides{seam: "logging"}, p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount with nil config: %v", err)
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
	// Create the SKILL.md path as a directory (not a file) so os.Open
	// fails with a non-IsNotExist error.
	mdPath := filepath.Join(skillDir, "SKILL.md")
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
	// Create the SKILL.md path as a directory (not a file) so os.Open
	// fails with a non-IsNotExist error.
	mdPath := filepath.Join(skillDir, "SKILL.md")
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
	ctx := &core.Context{
		Configs: map[string]any{
			"skills": Config{Dir: dir},
		},
	}
	if _, err := agent.MountAllAPI([]agent.Plugin{stubProvides{seam: "logging"}, p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount with config: %v", err)
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

// TestPromptSections verifies the skills provider contributes the
// Available Skills section, and nil when no skills are installed.
func TestPromptSections(t *testing.T) {
	sp := NewSkillsProvider(makeSkillDir(t))
	secs := sp.PromptSections(core.PromptBuildOptions{})
	if len(secs) != 1 {
		t.Fatalf("expected 1 section, got %d", len(secs))
	}
	s := secs[0]
	if s.Title != "Available Skills" {
		t.Errorf("Title = %q, want %q", s.Title, "Available Skills")
	}
	if s.Order != promptSectionOrder {
		t.Errorf("Order = %d, want %d", s.Order, promptSectionOrder)
	}
	want := "Call the skills.read tool with a skill name to read its full content.\n- test-skill: A test skill\n"
	if s.Body != want {
		t.Errorf("Body = %q, want %q", s.Body, want)
	}

	empty := NewSkillsProvider(t.TempDir())
	if got := empty.PromptSections(core.PromptBuildOptions{Extensions: nil}); got != nil {
		t.Errorf("expected nil sections with no skills, got %v", got)
	}
}

// TestSkillsProviderImplementsPromptSectioner verifies the seam contract.
func TestSkillsProviderImplementsPromptSectioner(t *testing.T) {
	var _ core.PromptSectioner = (*SkillsProvider)(nil)
}

// --- inspector snapshot (card t_db430fea) ---

func TestListSkillsSortedAndLive(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"bb", "aa"} {
		skillDir := filepath.Join(dir, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: the " + name + " skill\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sp := NewSkillsProvider(dir)
	skills, err := sp.ListSkills()
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(skills) != 2 || skills[0].Name != "aa" || skills[1].Name != "bb" {
		t.Fatalf("skills = %+v, want sorted [aa bb]", skills)
	}
}

func TestListSkillsMissingDirIsEmpty(t *testing.T) {
	sp := NewSkillsProvider(t.TempDir())
	skills, err := sp.ListSkills()
	if err != nil || len(skills) != 0 {
		t.Fatalf("ListSkills missing dir = (%v, %v), want empty, nil", skills, err)
	}
}

// Compile-time proof that the provider satisfies the read-only
// inspector seam the http_channel asserts.
func TestSkillsProviderImplementsSkillsLister(t *testing.T) {
	var _ core.SkillsLister = (*SkillsProvider)(nil)
}

// stubProvides satisfies the logging Requires edge in the mount set
// without registering anything: the seam it names is already
// populated on the Context the test built.
type stubProvides struct {
	plugin agent.Plugin
	seam   string
}

func (s stubProvides) Name() string       { return "stub-" + s.seam }
func (s stubProvides) Provides() []string { return []string{s.seam} }
func (s stubProvides) Requires() []string { return nil }

func (s stubProvides) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	_ = s.plugin
	return nil
}

// Mount satisfies the interface until phase F retires it; the stub
// never registers and is never dispatched (the API mount wins).
func (s stubProvides) Mount(ctx *core.Context) error { return nil }
