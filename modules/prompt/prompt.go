// Package prompt builds the omega system prompt.
//
// It owns the prompt template and assembly logic: guidelines,
// project context, skills, tools, environment, and user-supplied
// custom/append sections. It implements agent.PromptBuilder.
package prompt

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// PromptBuilder is the in-process system prompt builder.
type PromptBuilder struct {
	skills agent.SkillsProvider
	memory agent.MemoryProvider
}

// NewPromptBuilder creates a PromptBuilder. skills is the
// SkillsProvider for listing available skills; pass nil if no skills
// extension is loaded. memory is the MemoryProvider for snapshot
// injection; pass nil if no memory extension is loaded.
func NewPromptBuilder(skills agent.SkillsProvider, memory agent.MemoryProvider) *PromptBuilder {
	return &PromptBuilder{skills: skills, memory: memory}
}

// BuildPrompt assembles the full system prompt from the build options.
// Returns (prompt, true) on success. It always returns true — the
// prompt builder is the default source of the system prompt.
func (b *PromptBuilder) BuildPrompt(_ context.Context, opts agent.PromptBuildOptions) (string, bool) {
	var skills []agent.Skill
	if b.skills != nil {
		skills, _ = b.skills.LoadSkills("")
	}

	var sb strings.Builder
	sb.WriteString("You are an AI coding agent with access to tools.\n")

	sb.WriteString("\n## Guidelines\n")
	for _, g := range b.Guidelines() {
		fmt.Fprintf(&sb, "- %s\n", g)
	}

	if opts.ProjectContext != "" {
		sb.WriteString("\n## Project Context\n")
		sb.WriteString(opts.ProjectContext)
		sb.WriteString("\n")
	}

	// Memory snapshot (read fresh from disk).
	if b.memory != nil {
		if snap := b.memory.Snapshot(); snap != "" {
			sb.WriteString("\n")
			sb.WriteString(snap)
			sb.WriteString("\n")
		}
	}

	if len(skills) > 0 {
		sb.WriteString("\n## Available Skills\n")
		sb.WriteString("Call the skills.read tool with a skill name to read its full content.\n")
		for _, skill := range skills {
			fmt.Fprintf(&sb, "- %s: %s\n", skill.Name, skill.Description)
		}
	}

	sb.WriteString("\n## Tools\n")
	writeTools(&sb, opts.Extensions)

	sb.WriteString("\n## Environment\n")
	fmt.Fprintf(&sb, "CWD: %s\n", opts.CWD)
	fmt.Fprintf(&sb, "OS: %s\n", runtime.GOOS)
	if runtime.GOOS == "windows" {
		sb.WriteString("Shell: cmd.exe\n")
	} else {
		sb.WriteString("Shell: bash\n")
	}
	fmt.Fprintf(&sb, "Date: %s\n", time.Now().Format("2006-01-02"))

	if opts.Custom != "" {
		sb.WriteString("\n")
		sb.WriteString(opts.Custom)
		sb.WriteString("\n")
	}
	for _, extra := range opts.Append {
		sb.WriteString("\n")
		sb.WriteString(extra)
		sb.WriteString("\n")
	}

	return sb.String(), true
}

// writeTools renders the "## Tools" section body: one "### <extension>"
// block per extension that has tools.
func writeTools(sb *strings.Builder, extensions []agent.ExtensionInfo) {
	for _, ext := range extensions {
		if len(ext.ToolList) == 0 {
			continue
		}
		fmt.Fprintf(sb, "### %s\n", ext.Name)
		for _, t := range ext.ToolList {
			fmt.Fprintf(sb, "- %s: %s\n", t.Name, firstLine(t.Description))
		}
		sb.WriteString("\n")
	}
}

// Guidelines returns the default guideline lines appended under
// "## Guidelines" in the system prompt.
func (b *PromptBuilder) Guidelines() []string {
	return []string{
		"Use tools to read files and run commands before making assumptions.",
		"Prefer the simplest solution that works. Avoid unnecessary abstraction.",
		"When editing files, match the existing style and conventions.",
		"Report what you did concisely. Do not repeat file contents back.",
		"If something fails, report the error honestly rather than guessing.",
	}
}

// firstLine returns the first non-empty line of s, or s itself if it
// has no newlines. Used to keep tool descriptions short in the system
// prompt — full descriptions go to the LLM via the provider's JSON
// tool schemas.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return s
}
