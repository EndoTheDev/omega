package tools

import (
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// promptSectionOrder places the tools list last among the contributed sections, after.
const promptSectionOrder = 30

// PromptSections renders the "## Tools" section. A tool with structured Actions still
// renders the split: the domain line once, then each action labeled with its one-liner,
// the one thing a schema enum cannot carry. A disabled entry never renders: the section
// is the model's tool list, and advertising a tool the wire does not carry is a lie the
// model acts on.
func (p *Plugin) PromptSections(opts core.PromptBuildOptions) []core.PromptSection {
	var sb strings.Builder
	for _, ext := range opts.Extensions {
		var block strings.Builder
		for _, t := range ext.ToolList {
			if t.Disabled || len(t.Actions) == 0 {
				continue
			}
			fmt.Fprintf(&block, "- %s: %s\n", t.Name, firstLine(t.Description))
			for _, a := range t.Actions {
				fmt.Fprintf(&block, "  .%s: %s\n", a.Name, a.Description)
			}
		}
		if block.Len() == 0 {
			continue
		}
		fmt.Fprintf(&sb, "### %s\n", ext.Name)
		sb.WriteString(block.String())
		sb.WriteString("\n")
	}
	if sb.Len() == 0 {
		return nil
	}
	return []core.PromptSection{
		{Title: "Tools", Body: sb.String(), Order: promptSectionOrder},
	}
}

// firstLine returns the first non-empty line of s, or s itself if it has no newlines.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return s
}
