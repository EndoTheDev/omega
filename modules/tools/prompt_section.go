package tools

import (
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
)

// promptSectionOrder places the tools list last among the contributed
// sections, after the memory snapshot (10) and skills (20).
const promptSectionOrder = 30

// PromptSections renders the "## Tools" section: one "### <extension>"
// block per extension that has tools. Owned here because the
// firstLine policy for tool descriptions is tools-seam knowledge - the
// full descriptions already reach the LLM via the provider's JSON
// tool schemas.
func (p *Plugin) PromptSections(opts agent.PromptBuildOptions) []agent.PromptSection {
	var sb strings.Builder
	for _, ext := range opts.Extensions {
		if len(ext.ToolList) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "### %s\n", ext.Name)
		for _, t := range ext.ToolList {
			fmt.Fprintf(&sb, "- %s: %s\n", t.Name, firstLine(t.Description))
		}
		sb.WriteString("\n")
	}
	return []agent.PromptSection{
		{Title: "Tools", Body: sb.String(), Order: promptSectionOrder},
	}
}

// firstLine returns the first non-empty line of s, or s itself if it
// has no newlines. Used to keep tool descriptions short in the system
// prompt.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return s
}