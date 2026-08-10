package agent

import (
	"fmt"
	"strings"
	"time"
)

// PromptOptions configures the system prompt builder.
type PromptOptions struct {
	ProjectContext string       // AGENTS.md contents, may be empty
	Tools          map[string]Tool // available tools, may be empty
	CWD            string
	Custom         string // user-supplied prompt from config, may be empty
}

// BuildSystemPrompt constructs the agent's system prompt from the
// project context, available tools, environment, and any custom prompt.
// Empty sections are omitted.
func BuildSystemPrompt(opts PromptOptions) string {
	var b strings.Builder
	b.WriteString("You are an AI coding agent with access to tools.\n")

	if opts.ProjectContext != "" {
		b.WriteString("\n## Project Context\n")
		b.WriteString(opts.ProjectContext)
		b.WriteString("\n")
	}

	if len(opts.Tools) > 0 {
		b.WriteString("\n## Available Tools\n")
		for name, tool := range opts.Tools {
			fmt.Fprintf(&b, "- %s: %s\n", name, tool.Description)
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
