package approval

import (
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// TestPromptSectionObeysTheFold pins the prose law: the Approval Gate section describes
// shell.run's gate, so it renders while the shell is on the wire and vanishes when the
// fold switches the shell off - otherwise the prompt advertises a gate over a tool the
// model cannot call.
func TestPromptSectionObeysTheFold(t *testing.T) {
	p := NewPlugin()

	on := p.PromptSections(core.PromptBuildOptions{})
	if len(on) != 1 || on[0].Title != "Approval Gate" {
		t.Fatalf("unfolded: sections = %+v, want one Approval Gate", on)
	}

	off := p.PromptSections(core.PromptBuildOptions{DisabledTools: []string{"shell.run"}})
	if len(off) != 0 {
		t.Fatalf("shell folded: sections = %+v, want none (nothing to gate)", off)
	}

	// A fold that names other tools leaves the section alone.
	other := p.PromptSections(core.PromptBuildOptions{DisabledTools: []string{"files.read", "verdict.request"}})
	if len(other) != 1 || other[0].Title != "Approval Gate" {
		t.Fatalf("other tools folded: sections = %+v, want the Approval Gate kept", other)
	}
}
