package prompt

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

var (
	_ agent.PromptBuilder = (*PromptBuilder)(nil)
	_ agent.Plugin       = (*Plugin)(nil)
)

// stubSectioner contributes canned sections.
type stubSectioner struct {
	sections []core.PromptSection
}

func (s *stubSectioner) PromptSections(opts core.PromptBuildOptions) []core.PromptSection {
	return s.sections
}

// TestPluginMetadata checks Name/Provides/Requires return expected values.
func TestPluginMetadata(t *testing.T) {
	p := NewPlugin()
	if p.Name() != "prompt" {
		t.Errorf("Name() = %q, want %q", p.Name(), "prompt")
	}
	provides := p.Provides()
	if len(provides) != 1 || provides[0] != "prompt" {
		t.Errorf("Provides() = %v, want [prompt]", provides)
	}
	if req := p.Requires(); len(req) != 0 {
		t.Errorf("Requires() = %v, want none (sections are read at build time)", req)
	}
}

// TestMountSetsPromptBuilder verifies Mount populates ctx.PromptBuilder.
func TestMountSetsPromptBuilder(t *testing.T) {
	p := NewPlugin()
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if ctx.Services().Resolve("prompt") == nil {
		t.Fatal("ctx.PromptBuilder is nil after Mount")
	}
}

// TestBuildPrompt verifies the core structure of the assembled prompt.
func TestBuildPrompt(t *testing.T) {
	b := NewPromptBuilder(&core.Context{})
	opts := core.PromptBuildOptions{
		CWD:            "/test/cwd",
		ProjectContext: "This is a test project.",
		Custom:         "Be extra careful.",
		Append:         []string{"Extra line 1."},
	}

	prompt, ok := b.BuildPrompt(context.Background(), opts)
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}

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

// TestBuildPromptNoSections verifies no "## Tools" heading leaks from the assembler.
func TestBuildPromptNoSections(t *testing.T) {
	b := NewPromptBuilder(&core.Context{})
	prompt, _ := b.BuildPrompt(context.Background(), core.PromptBuildOptions{
		CWD:        "/test/cwd",
		Extensions: []core.ExtensionInfo{{Name: "tools", ToolList: []core.ToolInfo{{Name: "shell", Description: "Run it.\nMore."}}}},
	})
	if strings.Contains(prompt, "## Tools") {
		t.Errorf("prompt renders Tools with no contributing module:\n%s", prompt)
	}
}

// TestBuildPromptSections pins the section rendering rules: titled
// sections render under "\n## <Title>\n", untitled ones as raw blocks,
// and sections sort by Order with the contributing order as tiebreak.
func TestBuildPromptSections(t *testing.T) {
	ctx := &core.Context{
		PromptSectioners: []core.PromptSectioner{
			&stubSectioner{sections: []core.PromptSection{
				{Title: "Second", Body: "second body\n", Order: 20},
				{Title: "Third", Body: "third body\n", Order: 20},
			}},
			&stubSectioner{sections: []core.PromptSection{
				{Body: "raw block\n", Order: 10},
			}},
		},
	}
	b := NewPromptBuilder(ctx)
	prompt, ok := b.BuildPrompt(context.Background(), core.PromptBuildOptions{CWD: "/test/cwd"})
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

// TestBuildPromptNilContext verifies the builder tolerates a nil Context.
func TestBuildPromptNilContext(t *testing.T) {
	b := &PromptBuilder{now: func() time.Time { return time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) }}
	prompt, ok := b.BuildPrompt(context.Background(), core.PromptBuildOptions{CWD: "/test/cwd"})
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

// TestBuildPromptFoldedGuidelines pins the guideline half of the prose law: a line that
// names a folded capability never renders, and the opening posture stops claiming tools
// once the inventory carries none.
func TestBuildPromptFoldedGuidelines(t *testing.T) {
	b := NewPromptBuilder(&core.Context{})
	exts := []core.ExtensionInfo{{
		Name: "tools",
		ToolList: []core.ToolInfo{
			{Name: "files.read", Description: "Read a file."},
			{Name: "files.edit", Description: "Edit a file."},
			{Name: "shell.run", Description: "Run a command."},
			{Name: "sessions.search", Description: "Search sessions."},
		},
	}}
	fold := []string{"files.read", "files.grep", "files.glob", "files.write", "files.edit", "files.batch", "shell.run", "sessions.search", "sessions.hygiene"}
	prompt, ok := b.BuildPrompt(context.Background(), core.PromptBuildOptions{
		CWD:           "/test/cwd",
		Extensions:    core.StampDisabledTools(exts, fold),
		DisabledTools: fold,
	})
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}
	for _, banned := range []string{"access to tools", "Use tools to read files", "When editing files", "store tools"} {
		if strings.Contains(prompt, banned) {
			t.Errorf("fully folded prompt still claims %q:\n%s", banned, prompt)
		}
	}
	for _, want := range []string{"No tools are enabled for this session", "- Prefer the simplest solution that works.", "## Environment"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("fully folded prompt missing %q:\n%s", want, prompt)
		}
	}

	// A partial fold drops only the lines that name the folded family: the whole files
	// write family and the store go, the file readers and the shell stay.
	partial := []string{"files.write", "files.edit", "files.batch", "sessions.search", "sessions.hygiene"}
	prompt, _ = b.BuildPrompt(context.Background(), core.PromptBuildOptions{
		CWD:           "/test/cwd",
		Extensions:    core.StampDisabledTools(exts, partial),
		DisabledTools: partial,
	})
	if strings.Contains(prompt, "When editing files") {
		t.Error("files tools folded but the editing guideline still renders")
	}
	if strings.Contains(prompt, "store tools") {
		t.Error("store tools folded but the omega.db guideline still renders")
	}
	if !strings.Contains(prompt, "Use tools to read files and run commands") {
		t.Error("shell.run is still on the wire, want the read/run guideline kept")
	}
}

// TestGuidelines returns the expected guideline lines on an unfolded run.
func TestGuidelines(t *testing.T) {
	b := NewPromptBuilder(&core.Context{})
	g := b.Guidelines(core.PromptBuildOptions{})
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
