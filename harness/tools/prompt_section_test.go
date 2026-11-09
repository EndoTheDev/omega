package tools

import (
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// TestPromptSectionsRendersTools verifies the Tools section rendering:
// one "### <extension>" block per extension with tools, descriptions
// truncated by firstLine.
func TestPromptSectionsRendersTools(t *testing.T) {
	p := NewPlugin()
	secs := p.PromptSections(core.PromptBuildOptions{
		Extensions: []core.ExtensionInfo{
			{
				Name: "webui",
				ToolList: []core.ToolInfo{
					{Name: "ui.click", Description: "Run a shell command.\nSecond line ignored."},
				},
			},
			{Name: "empty", ToolList: nil},
		},
	})
	if len(secs) != 1 {
		t.Fatalf("expected 1 section, got %d", len(secs))
	}
	s := secs[0]
	if s.Title != "Tools" {
		t.Errorf("Title = %q, want %q", s.Title, "Tools")
	}
	if s.Order != promptSectionOrder {
		t.Errorf("Order = %d, want %d", s.Order, promptSectionOrder)
	}
	want := "### webui\n- ui.click: Run a shell command.\n\n"
	if s.Body != want {
		t.Errorf("Body = %q, want %q", s.Body, want)
	}
}

// TestFirstLine verifies firstLine semantics: first non-empty line,
// trimmed; a whitespace-only string falls back to the input.
func TestFirstLine(t *testing.T) {
	for in, want := range map[string]string{
		"one\ntwo":           "one",
		"\n\n  spaced \ntwo": "spaced",
		"plain":              "plain",
		"   ":                "   ",
	} {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPromptSectionsImplementsSeam verifies the plugin satisfies the
// prompt-section seam.
func TestPromptSectionsImplementsSeam(t *testing.T) {
	var _ core.PromptSectioner = (*Plugin)(nil)
}

// TestPromptSectionsSkipsDisabledTools pins the registry enable flag's
// prompt effect (card t_441deb04): the Tools section is the model's
// tool list, so an entry the fold switched off must not be advertised -
// the model would call a tool the wire no longer carries.
func TestPromptSectionsSkipsDisabledTools(t *testing.T) {
	p := NewPlugin()
	secs := p.PromptSections(core.PromptBuildOptions{
		Extensions: []core.ExtensionInfo{
			{
				Name: "files",
				ToolList: []core.ToolInfo{
					{Name: "files.read", Description: "read a file"},
					{Name: "files.write", Description: "write a file", Disabled: true},
				},
			},
			{
				Name: "shell",
				ToolList: []core.ToolInfo{
					{Name: "shell.run", Description: "run a command", Disabled: true},
				},
			},
		},
	})
	if len(secs) != 1 {
		t.Fatalf("expected 1 section, got %d", len(secs))
	}
	body := secs[0].Body
	if !strings.Contains(body, "files.read") {
		t.Fatalf("enabled tool missing from the section: %q", body)
	}
	if strings.Contains(body, "files.write") {
		t.Fatalf("disabled tool advertised to the model: %q", body)
	}
	if strings.Contains(body, "shell") {
		t.Fatalf("an all-disabled extension still rendered a block: %q", body)
	}
}

// TestPromptSectionsAllDisabledContributesNothing pins the empty case: a
// home with every tool off renders no Tools heading at all, never a
// dangling "## Tools" with nothing under it.
func TestPromptSectionsAllDisabledContributesNothing(t *testing.T) {
	p := NewPlugin()
	secs := p.PromptSections(core.PromptBuildOptions{
		Extensions: []core.ExtensionInfo{
			{Name: "files", ToolList: []core.ToolInfo{
				{Name: "files.read", Description: "read a file", Disabled: true},
			}},
		},
	})
	if len(secs) != 0 {
		t.Fatalf("expected no section, got %#v", secs)
	}
}
