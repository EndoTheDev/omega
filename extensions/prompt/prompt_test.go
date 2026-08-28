package prompt

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// TestPromptBuilderImplementsInterface verifies the PromptBuilder
// satisfies agent.PromptBuilder at compile time.
func TestPromptBuilderImplementsInterface(t *testing.T) {
	var _ agent.PromptBuilder = (*PromptBuilder)(nil)
}

// TestPluginImplementsInterface verifies the Plugin satisfies
// agent.Plugin at compile time.
func TestPluginImplementsInterface(t *testing.T) {
	var _ agent.Plugin = (*Plugin)(nil)
}

// TestPluginMetadata checks Name/Provides/Requires return expected values.
func TestPluginMetadata(t *testing.T) {
	p := NewPlugin("")
	if p.Name() != "prompt" {
		t.Errorf("Name() = %q, want %q", p.Name(), "prompt")
	}
	provides := p.Provides()
	if len(provides) != 1 || provides[0] != "prompt_builder" {
		t.Errorf("Provides() = %v, want [prompt_builder]", provides)
	}
	req := p.Requires()
	if len(req) != 1 || req[0] != "memory" {
		t.Errorf("Requires() = %v, want [memory]", req)
	}
}

// TestMountSetsPromptBuilder verifies Mount populates ctx.PromptBuilder.
func TestMountSetsPromptBuilder(t *testing.T) {
	p := NewPlugin("")
	ctx := &agent.Context{}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if ctx.PromptBuilder == nil {
		t.Fatal("ctx.PromptBuilder is nil after Mount")
	}
}

// TestBuildPrompt verifies the basic structure of the assembled prompt.
func TestBuildPrompt(t *testing.T) {
	b := NewPromptBuilder("", nil) // no skills dir → no skills section
	opts := agent.PromptBuildOptions{
		CWD:            "/test/cwd",
		ProjectContext: "This is a test project.",
		Custom:         "Be extra careful.",
		Append:         []string{"Extra line 1."},
		Extensions: []agent.ExtensionInfo{
			{
				Name: "tools",
				ToolList: []agent.ToolInfo{
					{Name: "shell", Description: "Run a shell command.\nSecond line ignored."},
				},
			},
		},
	}

	prompt, ok := b.BuildPrompt(context.Background(), opts)
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}

	// Core sections present.
	for _, want := range []string{
		"You are an AI coding agent",
		"## Guidelines",
		"- Use tools to read files",
		"## Project Context",
		"This is a test project.",
		"## Tools",
		"### tools",
		"- shell: Run a shell command.",
		"## Environment",
		"CWD: /test/cwd",
		"Be extra careful.",
		"Extra line 1.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", want, prompt)
		}
	}

	// firstLine should truncate the tool description to the first line.
	if strings.Contains(prompt, "Second line ignored") {
		t.Error("prompt contains second line of tool description — firstLine not working")
	}

	// No skills section when skillsDir is empty.
	if strings.Contains(prompt, "## Available Skills") {
		t.Error("prompt contains skills section with no skills dir")
	}
}

// TestGuidelines returns the expected guideline lines.
func TestGuidelines(t *testing.T) {
	b := NewPromptBuilder("", nil)
	g := b.Guidelines()
	if len(g) != 5 {
		t.Fatalf("Guidelines() returned %d lines, want 5", len(g))
	}
	for i, want := range []string{
		"Use tools to read files and run commands before making assumptions.",
		"Prefer the simplest solution that works. Avoid unnecessary abstraction.",
		"When editing files, match the existing style and conventions.",
		"Report what you did concisely. Do not repeat file contents back.",
		"If something fails, report the error honestly rather than guessing.",
	} {
		if g[i] != want {
			t.Errorf("Guidelines()[%d] = %q, want %q", i, g[i], want)
		}
	}
}
