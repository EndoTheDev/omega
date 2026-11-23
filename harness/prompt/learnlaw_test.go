package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/memory"
)

// TestLearningLawRidesThePrompt pins the learning loop at its two decision points, because
// nothing in Go counts tool calls to enforce it any more: the skills section carries the
// scan-and-load law and the finish-gate, and the memory tool's description carries the
// routing law that sends a procedure learned mid-task to the skill, not to memory.
func TestLearningLawRidesThePrompt(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "probe-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: probes a thing\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := wireContext(t, dir, memory.Config{Enabled: false})
	pb, ok := ctx.Services().Resolve("prompt").(agent.PromptBuilder)
	if !ok {
		t.Fatal("prompt offer missing")
	}
	prompt, ok := pb.BuildPrompt(context.Background(), core.PromptBuildOptions{})
	if !ok {
		t.Fatal("BuildPrompt returned false")
	}

	for _, law := range []struct{ name, line string }{
		{"scan-and-load law", "Before acting, consult this registry: load any skill whose description matches the task"},
		{"finish-gate", "update it before you finish the turn"},
		{"save-after-iteration law", "offer to save a new skill"},
	} {
		if !strings.Contains(prompt, law.line) {
			t.Errorf("the assembled prompt is missing the %s (%q)", law.name, law.line)
		}
	}
	if !strings.Contains(prompt, "probe-skill") {
		t.Fatal("the prompt does not carry the skill inventory the law points at")
	}

	desc := memoryAddDescription(t, ctx)
	for _, want := range []string{"skills.write", "skills.update", "narrow exception"} {
		if !strings.Contains(desc, want) {
			t.Errorf("memory.add description is missing the routing law token %q: %q", want, desc)
		}
	}
}

// memoryAddDescription reads the registered memory.add tool back off the live context.
func memoryAddDescription(t *testing.T, ctx *core.Context) string {
	t.Helper()
	for _, tp := range ctx.ToolProviders {
		if tp == nil {
			continue
		}
		if tool, ok := tp.Tools()["memory.add"]; ok {
			return tool.Description
		}
	}
	t.Fatal("memory.add is not on the registered tool providers")
	return ""
}
