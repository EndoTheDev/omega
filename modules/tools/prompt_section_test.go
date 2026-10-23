package tools

import (
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