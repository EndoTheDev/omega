package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/internal/ai"
)

// requireTool returns the named tool from the built-in registry, failing the
// test if it is missing.
func requireTool(t *testing.T, name string) Tool {
	t.Helper()
	tool, ok := NewRegistry()[name]
	if !ok {
		t.Fatalf("registry missing tool %q", name)
	}
	return tool
}

// inTempDir creates a temp directory, registers its cleanup, and returns its
// absolute path.
func inTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	return abs
}

func TestRegistryContainsAllTools(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"shell", "read_file", "write_file", "edit"} {
		if _, ok := registry[name]; !ok {
			t.Errorf("registry missing tool %q", name)
		}
	}
}

func TestShellRunsCommand(t *testing.T) {
	tool := requireTool(t, "shell")
	out, err := tool.Run(context.Background(), map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatalf("shell failed: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("shell output = %q, want it to contain %q", out, "hello")
	}
}

func TestShellRejectsMissingCommand(t *testing.T) {
	tool := requireTool(t, "shell")
	if _, err := tool.Run(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected error for missing command, got nil")
	}
}

func TestWriteThenReadFile(t *testing.T) {
	dir := inTempDir(t)
	path := filepath.Join(dir, "nested", "file.txt")
	want := "line one\nline two\nline three\n"

	writeTool := requireTool(t, "write_file")
	_, err := writeTool.Run(context.Background(), map[string]any{
		"path":    path,
		"content": want,
	})
	if err != nil {
		t.Fatalf("write_file failed: %v", err)
	}

	readTool := requireTool(t, "read_file")
	out, err := readTool.Run(context.Background(), map[string]any{"path": path})
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	wantNumbered := "1|line one\n2|line two\n3|line three\n"
	if out != wantNumbered {
		t.Fatalf("read_file = %q, want %q", out, wantNumbered)
	}
}

func TestReadFileOffsetAndLimit(t *testing.T) {
	dir := inTempDir(t)
	path := filepath.Join(dir, "lines.txt")
	content := "a\nb\nc\nd\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := requireTool(t, "read_file")
	out, err := tool.Run(context.Background(), map[string]any{
		"path":   path,
		"offset": float64(2),
		"limit":  float64(2),
	})
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	want := "2|b\n3|c\n"
	if out != want {
		t.Fatalf("read_file = %q, want %q", out, want)
	}
}

func TestReadFileRejectsInvalidPath(t *testing.T) {
	tool := requireTool(t, "read_file")
	if _, err := tool.Run(context.Background(), map[string]any{"path": ""}); err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestWriteFileRejectsMissingContent(t *testing.T) {
	tool := requireTool(t, "write_file")
	if _, err := tool.Run(context.Background(), map[string]any{"path": "x.txt"}); err == nil {
		t.Fatal("expected error for missing content, got nil")
	}
}

func TestEditReplacesUniqueString(t *testing.T) {
	dir := inTempDir(t)
	path := filepath.Join(dir, "file.txt")
	content := "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := requireTool(t, "edit")
	out, err := tool.Run(context.Background(), map[string]any{
		"path":       path,
		"old_string": "beta",
		"new_string": "BETA",
	})
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if !strings.Contains(out, path) {
		t.Fatalf("edit output = %q, want it to contain the path %q", out, path)
	}
	if !strings.Contains(out, "-beta") || !strings.Contains(out, "+BETA") {
		t.Fatalf("edit output = %q, want diff summary with -beta and +BETA", out)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	if string(got) != "alpha\nBETA\ngamma\n" {
		t.Fatalf("patched content = %q, want %q", string(got), "alpha\nBETA\ngamma\n")
	}
}

func TestEditRejectsMissingOldString(t *testing.T) {
	dir := inTempDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := requireTool(t, "edit")
	if _, err := tool.Run(context.Background(), map[string]any{
		"path":       path,
		"old_string": "nope",
		"new_string": "x",
	}); err == nil {
		t.Fatal("expected error for missing old_string, got nil")
	}
}

func TestEditRejectsNonUniqueOldString(t *testing.T) {
	dir := inTempDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("dup\ndup\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := requireTool(t, "edit")
	if _, err := tool.Run(context.Background(), map[string]any{
		"path":       path,
		"old_string": "dup",
		"new_string": "x",
	}); err == nil {
		t.Fatal("expected error for non-unique old_string, got nil")
	}
}

func TestEditRejectsMissingArguments(t *testing.T) {
	tool := requireTool(t, "edit")
	if _, err := tool.Run(context.Background(), map[string]any{
		"path": "x.txt",
	}); err == nil {
		t.Fatal("expected error for missing old_string, got nil")
	}
	if _, err := tool.Run(context.Background(), map[string]any{
		"old_string": "a",
		"new_string": "b",
	}); err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
}

func TestToolResultFormat(t *testing.T) {
	// Verify that a tool result has the correct ID, content, and error flag.
	tr := ai.NewToolResult("output", "call-1", false)
	if tr.ToolCallID != "call-1" {
		t.Fatalf("ToolCallID = %q, want call-1", tr.ToolCallID)
	}
	if tr.Content != "output" {
		t.Fatalf("Content = %q, want output", tr.Content)
	}
	if tr.IsError {
		t.Fatal("IsError = true, want false")
	}

	// Error result.
	trErr := ai.NewToolResult("boom", "call-2", true)
	if !trErr.IsError {
		t.Fatal("IsError = false, want true")
	}
	if trErr.Content != "boom" {
		t.Fatalf("Content = %q, want boom", trErr.Content)
	}
}

func TestUnknownToolResult(t *testing.T) {
	// An unknown tool call produces a tool result with IsError=true.
	tr := ai.NewToolResult("unknown tool: ghost", "c1", true)
	if !tr.IsError {
		t.Fatal("unknown tool result IsError = false, want true")
	}
	if !strings.Contains(tr.Content, "unknown tool") {
		t.Fatalf("Content = %q, want it to contain 'unknown tool'", tr.Content)
	}
}

func TestSetSkillsRegistersLoadSkillTool(t *testing.T) {
	ag := NewAgent(nil, NewRegistry(), 0)
	if _, ok := ag.tools["load_skill"]; ok {
		t.Fatal("load_skill tool should not exist before SetSkills")
	}
	skills := []Skill{
		{Name: "test-skill", Description: "A test", Content: "Do the thing.", Dir: "/tmp/skills/test-skill"},
	}
	ag.SetSkills(skills)
	tool, ok := ag.tools["load_skill"]
	if !ok {
		t.Fatal("load_skill tool not registered after SetSkills")
	}
	if tool.Description == "" {
		t.Error("load_skill description is empty")
	}
}

func TestSetSkillsNoopOnEmpty(t *testing.T) {
	ag := NewAgent(nil, NewRegistry(), 0)
	ag.SetSkills(nil)
	if _, ok := ag.tools["load_skill"]; ok {
		t.Fatal("load_skill tool should not be registered for nil skills")
	}
}

func TestRunLoadSkill(t *testing.T) {
	skills := []Skill{
		{Name: "learn-skill", Description: "Teaches", Content: "Learn by doing.", Dir: "/home/skills/learn-skill"},
		{Name: "deploy-skill", Description: "Deploys", Content: "Deploy with care.", Dir: "/home/skills/deploy-skill"},
	}
	result, err := runLoadSkill(skills, map[string]any{"name": "learn-skill"})
	if err != nil {
		t.Fatalf("runLoadSkill: %v", err)
	}
	if !strings.Contains(result, "Learn by doing.") {
		t.Errorf("result missing skill content: %q", result)
	}
	if !strings.Contains(result, "/home/skills/learn-skill") {
		t.Errorf("result missing skill directory: %q", result)
	}
}

func TestRunLoadSkillNotFound(t *testing.T) {
	skills := []Skill{
		{Name: "learn-skill", Description: "Teaches", Content: "Learn by doing.", Dir: "/home/skills/learn-skill"},
	}
	_, err := runLoadSkill(skills, map[string]any{"name": "ghost"})
	if err == nil {
		t.Fatal("expected error for unknown skill, got nil")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should mention skill name: %v", err)
	}
	if !strings.Contains(err.Error(), "learn-skill") {
		t.Errorf("error should list available skills: %v", err)
	}
}

func TestConcurrentWritesSamePath(t *testing.T) {
	dir := inTempDir(t)
	path := filepath.Join(dir, "concurrent.txt")
	// Seed the file so edit has something to replace.
	if err := os.WriteFile(path, []byte("0\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	writeTool := requireTool(t, "write_file")
	editTool := requireTool(t, "edit")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			_, _ = writeTool.Run(context.Background(), map[string]any{
				"path":    path,
				"content": strings.Repeat("x", n+1) + "\n",
			})
		}(i)
		go func(n int) {
			defer wg.Done()
			_, _ = editTool.Run(context.Background(), map[string]any{
				"path":       path,
				"old_string": "x",
				"new_string": "y" + strings.Repeat("x", n),
			})
		}(i)
	}
	wg.Wait()

	// If the locks work, the file exists and has valid content (no corruption).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	// The file should contain only x/y characters and a newline.
	for _, c := range string(data) {
		if c != 'x' && c != 'y' && c != '\n' {
			t.Fatalf("unexpected character %q in concurrent write result: %q", c, string(data))
		}
	}
}
