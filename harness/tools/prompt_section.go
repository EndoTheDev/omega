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
// block per extension that owns at least one action-bearing tool. The
// section renders the DELTA only - a plain tool (no structured Actions)
// contributes nothing, because its name and full description already
// reach the model in the provider's JSON tool schema. A tool with
// structured Actions still renders the split: the domain line once,
// then each action labeled with its one-liner, the one thing a schema
// enum cannot carry (card t_4f2c7b2e, measured: the pre-trim section
// restated the schema descriptions verbatim - 33/33 first lines were
// verbatim schema-description prefixes, 5,799 of the 7,943 prompt
// bytes - and carried zero production action annotations, since no
// shipped tool declared Actions).
//
// A disabled entry (ToolInfo.Disabled, the registry's enable flag,
// card t_441deb04) never renders: the section is the model's tool
// list, and advertising a tool the wire does not carry is a lie the
// model acts on. An extension whose every tool is off disappears
// entirely, and a home with no action-bearing tools left contributes
// no section at all.
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
