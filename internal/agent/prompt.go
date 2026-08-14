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
	Custom         string   // user-supplied prompt from config, may be empty
	Append         []string // extra prompts from --append-system-prompt, may be nil
}

// BuildSystemPrompt constructs the agent's system prompt from the
// project context, skills, environment, and any custom prompt. Tool
// descriptions are not included here; the provider receives them as
// structured JSON schemas alongside the system prompt. Empty sections
// are omitted.
func BuildSystemPrompt(opts PromptOptions) string {
	var b strings.Builder
	b.WriteString("You are an AI coding agent with access to tools.\n")

	b.WriteString("\n## Guidelines\n")
	b.WriteString("- Use tools to read files and run commands before making assumptions.\n")
	b.WriteString("- Prefer the simplest solution that works. Avoid unnecessary abstraction.\n")
	b.WriteString("- When editing files, match the existing style and conventions.\n")
	b.WriteString("- Report what you did concisely. Do not repeat file contents back.\n")
	b.WriteString("- If something fails, report the error honestly rather than guessing.\n")

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
	for _, extra := range opts.Append {
		b.WriteString("\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}
	return b.String()
}
