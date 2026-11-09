package tools

import (
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// promptSectionOrder places the tools list last among the contributed
// sections, after the memory snapshot (10) and skills (20).
const promptSectionOrder = 30

// PromptSections renders the "## Tools" section: one "### <extension>"
// block per extension that has tools. Each tool renders as a domain
// line; a tool with structured Actions renders the split - the domain
// context once, then each action labeled with its one-liner. The old
// blob-style multi-sentence description is gone in both renderers
// (prompt + webui) because both draw from the same metadata. Owned
// here because the firstLine policy for tool descriptions is
// tools-seam knowledge - the full descriptions already reach the LLM
// via the provider's JSON tool schemas.
//
// A disabled entry (ToolInfo.Disabled, the registry's enable flag,
// card t_441deb04) never renders: the section is the model's tool list,
// and advertising a tool the wire does not carry is a lie the model
// acts on. An extension whose every tool is off disappears entirely,
// and a home with no tools left contributes no section at all.
func (p *Plugin) PromptSections(opts core.PromptBuildOptions) []core.PromptSection {
	var sb strings.Builder
	for _, ext := range opts.Extensions {
		enabled := make([]core.ToolInfo, 0, len(ext.ToolList))
		for _, t := range ext.ToolList {
			if t.Disabled {
				continue
			}
			enabled = append(enabled, t)
		}
		if len(enabled) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "### %s\n", ext.Name)
		for _, t := range enabled {
			fmt.Fprintf(&sb, "- %s: %s\n", t.Name, firstLine(t.Description))
			for _, a := range t.Actions {
				fmt.Fprintf(&sb, "  .%s: %s\n", a.Name, a.Description)
			}
		}
		sb.WriteString("\n")
	}
	if sb.Len() == 0 {
		return nil
	}
	return []core.PromptSection{
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
