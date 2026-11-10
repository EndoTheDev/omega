package tools

import (
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// TestPromptSectionsRendersOnlyTheDelta pins the trim (card
// t_4f2c7b2e): a plain tool renders nothing - its name and description
// already ride the provider's JSON schema - while an action-bearing
// tool renders the domain line plus its sub-verb one-liners, the part
// a schema enum cannot carry. An extension with no action-bearing tool
// gets no block at all.
func TestPromptSectionsRendersOnlyTheDelta(t *testing.T) {
	p := NewPlugin()
	secs := p.PromptSections(core.PromptBuildOptions{
		Extensions: []core.ExtensionInfo{
			{
				Name: "memory",
				ToolList: []core.ToolInfo{
					{
						Name:        "memory.batch",
						Description: "Atomic multi-change.\nSecond line ignored.",
						Actions: []core.ToolAction{
							{Name: "add", Description: "append an entry"},
							{Name: "remove", Description: "delete an entry"},
						},
					},
					{Name: "memory.replace", Description: "plain tool the schema already carries"},
				},
			},
			{Name: "files", ToolList: []core.ToolInfo{
				{Name: "files.read", Description: "read a file"},
			}},
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
	want := "### memory\n- memory.batch: Atomic multi-change.\n  .add: append an entry\n  .remove: delete an entry\n\n"
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
// prompt effect (card t_441deb04): the section is the model's tool
// list, so an entry the fold switched off must not be advertised -
// the model would call a tool the wire no longer carries.
func TestPromptSectionsSkipsDisabledTools(t *testing.T) {
	p := NewPlugin()
	secs := p.PromptSections(core.PromptBuildOptions{
		Extensions: []core.ExtensionInfo{
			{
				Name: "kanban",
				ToolList: []core.ToolInfo{
					{Name: "kanban.batch", Description: "run several ops", Actions: []core.ToolAction{
						{Name: "create", Description: "create a task"},
					}},
					{Name: "kanban.update", Description: "move a task", Disabled: true, Actions: []core.ToolAction{
						{Name: "move", Description: "move status"},
					}},
				},
			},
			{
				Name: "shell",
				ToolList: []core.ToolInfo{
					{Name: "shell.run", Description: "run a command", Disabled: true, Actions: []core.ToolAction{
						{Name: "run", Description: "run it"},
					}},
				},
			},
		},
	})
	if len(secs) != 1 {
		t.Fatalf("expected 1 section, got %d", len(secs))
	}
	body := secs[0].Body
	if !strings.Contains(body, "kanban.batch") {
		t.Fatalf("enabled action tool missing from the section: %q", body)
	}
	if strings.Contains(body, "kanban.update") {
		t.Fatalf("disabled tool advertised to the model: %q", body)
	}
	if strings.Contains(body, "shell") {
		t.Fatalf("an all-disabled extension still rendered a block: %q", body)
	}
}

// TestPromptSectionsEmptyCasesContributeNothing pins the empty cases: a
// home whose tools carry no action annotations (the shipped set, card
// t_4f2c7b2e) and a home with every tool off both contribute no
// section - never a dangling "## Tools" heading.
func TestPromptSectionsEmptyCasesContributeNothing(t *testing.T) {
	p := NewPlugin()
	plain := p.PromptSections(core.PromptBuildOptions{
		Extensions: []core.ExtensionInfo{
			{Name: "files", ToolList: []core.ToolInfo{
				{Name: "files.read", Description: "read a file"},
				{Name: "files.write", Description: "write a file"},
			}},
		},
	})
	if len(plain) != 0 {
		t.Fatalf("plain tools rendered a section: %#v", plain)
	}
	disabled := p.PromptSections(core.PromptBuildOptions{
		Extensions: []core.ExtensionInfo{
			{Name: "kanban", ToolList: []core.ToolInfo{
				{Name: "kanban.batch", Description: "run several ops", Disabled: true, Actions: []core.ToolAction{
					{Name: "create", Description: "create a task"},
				}},
			}},
		},
	})
	if len(disabled) != 0 {
		t.Fatalf("all-disabled set rendered a section: %#v", disabled)
	}
}
