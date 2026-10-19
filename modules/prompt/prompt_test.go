package prompt

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// Compile-time interface checks.
var (
	_ agent.PromptBuilder = (*PromptBuilder)(nil)
	_ agent.Plugin        = (*Plugin)(nil)
)

// stubSectioner contributes canned sections; used to pin the
// assembler's section rules without pulling in other modules.
type stubSectioner struct {
	sections []agent.PromptSection
}

// PromptSections returns the canned sections.
func (s *stubSectioner) PromptSections(opts agent.PromptBuildOptions) []agent.PromptSection {
	return s.sections
}

// TestPluginMetadata checks Name/Provides/Requires return expected values.
func TestPluginMetadata(t *testing.T) {
	p := NewPlugin()
	if p.Name() != "prompt" {
		t.Errorf("Name() = %q, want %q", p.Name(), "prompt")
	}
	provides := p.Provides()
	if len(provides) != 1 || provides[0] != "prompt_builder" {
		t.Errorf("Provides() = %v, want [prompt_builder]", provides)
	}
	if req := p.Requires(); len(req) != 0 {
		t.Errorf("Requires() = %v, want none (sections are read at build time)", req)
	}
}

// TestMountSetsPromptBuilder verifies Mount populates ctx.PromptBuilder.
func TestMountSetsPromptBuilder(t *testing.T) {
	p := NewPlugin()
	ctx := &agent.Context{}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if ctx.PromptBuilder == nil {
		t.Fatal("ctx.PromptBuilder is nil after Mount")
	}
}

// TestBuildPrompt verifies the core structure of the assembled prompt:
// identity, guidelines, project context, environment, custom, append.
func TestBuildPrompt(t *testing.T) {
	b := NewPromptBuilder(&agent.Context{})
	opts := agent.PromptBuildOptions{
		CWD:            "/test/cwd",
		ProjectContext: "This is a test project.",
		Custom:         "Be extra careful.",
		Append:         []string{"Extra line 1."},
	}

	prompt, ok := b.BuildPrompt(context.Background(), opts)
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}

	// Core sections present, in order.
	for _, want := range []string{
		"You are an AI coding agent",
		"## Guidelines",
		"- Use tools to read files",
		"## Project Context",
		"This is a test project.",
		"## Environment",
		"CWD: /test/cwd",
		"Be extra careful.",
		"Extra line 1.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", want, prompt)
		}
	}
}

// TestBuildPromptNoSections verifies no "## Tools" heading leaks from
// the assembler when no module contributes one.
func TestBuildPromptNoSections(t *testing.T) {
	b := NewPromptBuilder(&agent.Context{})
	prompt, _ := b.BuildPrompt(context.Background(), agent.PromptBuildOptions{
		CWD:        "/test/cwd",
		Extensions: []agent.ExtensionInfo{{Name: "tools", ToolList: []agent.ToolInfo{{Name: "shell", Description: "Run it.\nMore."}}}},
	})
	if strings.Contains(prompt, "## Tools") {
		t.Errorf("prompt renders Tools with no contributing module:\n%s", prompt)
	}
}

// TestBuildPromptSections pins the section rendering rules: titled
// sections render under "\n## <Title>\n", untitled ones as raw blocks,
// and sections sort by Order with the contributing order as tiebreak.
func TestBuildPromptSections(t *testing.T) {
	ctx := &agent.Context{
		PromptSectioners: []agent.PromptSectioner{
			&stubSectioner{sections: []agent.PromptSection{
				{Title: "Second", Body: "second body\n", Order: 20},
				{Title: "Third", Body: "third body\n", Order: 20},
			}},
			&stubSectioner{sections: []agent.PromptSection{
				{Body: "raw block\n", Order: 10},
			}},
		},
	}
	b := NewPromptBuilder(ctx)
	prompt, ok := b.BuildPrompt(context.Background(), agent.PromptBuildOptions{CWD: "/test/cwd"})
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}

	for _, want := range []string{
		"\nraw block\n\n",
		"\n## Second\nsecond body\n",
		"\n## Third\nthird body\n",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", want, prompt)
		}
	}

	rawIdx := strings.Index(prompt, "raw block")
	secondIdx := strings.Index(prompt, "## Second")
	thirdIdx := strings.Index(prompt, "## Third")
	envIdx := strings.Index(prompt, "## Environment")
	if !(rawIdx < secondIdx && secondIdx < thirdIdx && thirdIdx < envIdx) {
		t.Errorf("section order wrong:\n%s", prompt)
	}
}

// TestBuildPromptNilContext verifies the builder tolerates a nil
// Context: core sections render, nothing else.
func TestBuildPromptNilContext(t *testing.T) {
	b := &PromptBuilder{now: func() time.Time { return time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) }}
	prompt, ok := b.BuildPrompt(context.Background(), agent.PromptBuildOptions{CWD: "/test/cwd"})
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}
	if !strings.Contains(prompt, "## Guidelines") {
		t.Error("prompt missing Guidelines section")
	}
	if strings.Contains(prompt, "## Tools") {
		t.Error("prompt renders Tools with nil context")
	}
}

// TestGuidelines returns the expected guideline lines.
func TestGuidelines(t *testing.T) {
	b := NewPromptBuilder(&agent.Context{})
	g := b.Guidelines()
	if len(g) != 6 {
		t.Fatalf("Guidelines() returned %d lines, want 6", len(g))
	}
	for i, want := range []string{
		"Use tools to read files and run commands before making assumptions.",
		"Prefer the simplest solution that works. Avoid unnecessary abstraction.",
		"When editing files, match the existing style and conventions.",
		"Never open omega.db directly - the store tools are the interface to the database.",
		"Report what you did concisely. Do not repeat file contents back.",
		"If something fails, report the error honestly rather than guessing.",
	} {
		if g[i] != want {
			t.Errorf("Guidelines()[%d] = %q, want %q", i, g[i], want)
		}
	}
}