package agent

import (
	"fmt"
	"strings"
	"time"
)

// PromptOptions configures the system prompt builder.
type PromptOptions struct {
	ProjectContext string          // AGENTS.md contents, may be empty
	Skills         []Skill         // loaded skills, may be empty
	CWD            string
	Custom         string // user-supplied prompt from config, may be empty
}

// BuildSystemPrompt constructs the agent's system prompt from the
// project context, skills, environment, and any custom prompt. Tool
// descriptions are not included here; the provider receives them as
// structured JSON schemas alongside the system prompt. Empty sections
// are omitted.
func BuildSystemPrompt(opts PromptOptions) string {
	var b strings.Builder
	b.WriteString("You are an AI coding agent with access to tools.\n")

	if opts.ProjectContext != "" {
		b.WriteString("\n## Project Context\n")
		b.WriteString(opts.ProjectContext)
		b.WriteString("\n")
	}

	if len(opts.Skills) > 0 {
		b.WriteString("\n## Available Skills\n")
		b.WriteString("Call the load_skill tool with a skill name to read its full content.\n")
		for _, skill := range opts.Skills {
			fmt.Fprintf(&b, "- %s: %s\n", skill.Name, skill.Description)
		}
	}

	b.WriteString("\n## Environment\n")
	fmt.Fprintf(&b, "CWD: %s\n", opts.CWD)
	fmt.Fprintf(&b, "Date: %s\n", time.Now().Format("2006-01-02"))

	if opts.Custom != "" {
		b.WriteString("\n")
		b.WriteString(opts.Custom)
		b.WriteString("\n")
	}
	return b.String()
}
