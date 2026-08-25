// Package prompt builds the omega system prompt.
//
// It owns the prompt template and assembly logic: guidelines,
// project context, skills, tools, environment, and user-supplied
// custom/append sections. It implements agent.PromptBuilder.
package prompt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// PromptBuilder is the in-process system prompt builder. It replaces
// the former core-prompt stdio extension.
type PromptBuilder struct {
	skillsDir string // OMEGA_SKILLS_DIR, empty = no skills
}

// NewPromptBuilder creates a PromptBuilder. skillsDir overrides the
// OMEGA_SKILLS_DIR env var; pass "" to use the env var (the common
// case when the host wires this via Mount).
func NewPromptBuilder(skillsDir string) *PromptBuilder {
	if skillsDir == "" {
		skillsDir = os.Getenv("OMEGA_SKILLS_DIR")
	}
	return &PromptBuilder{skillsDir: skillsDir}
}

// BuildPrompt assembles the full system prompt from the build options.
// Returns (prompt, true) on success. It always returns true — the
// prompt builder is the default source of the system prompt.
func (b *PromptBuilder) BuildPrompt(_ context.Context, opts agent.PromptBuildOptions) (string, bool) {
	skills := b.loadSkills()

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

	if len(skills) > 0 {
		sb.WriteString("\n## Available Skills\n")
		sb.WriteString("Call the skills.read tool with a skill name to read its full content.\n")
		for _, skill := range skills {
			fmt.Fprintf(&sb, "- %s: %s\n", skill.Name, skill.Description)
		}
	}

	sb.WriteString("\n## Tools\n")
	if len(opts.Extensions) > 0 {
		for _, ext := range opts.Extensions {
			if len(ext.ToolList) > 0 {
				fmt.Fprintf(&sb, "### %s\n", ext.Name)
				for _, t := range ext.ToolList {
					fmt.Fprintf(&sb, "- %s: %s\n", t.Name, firstLine(t.Description))
				}
				sb.WriteString("\n")
			}
		}
	}

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

// loadSkills reads skills from the configured skills directory. Returns
// nil if the directory is not set or missing.
func (b *PromptBuilder) loadSkills() []agent.Skill {
	if b.skillsDir == "" {
		return nil
	}
	entries, err := os.ReadDir(b.skillsDir)
	if err != nil {
		return nil
	}
	var skills []agent.Skill
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		skillFile := filepath.Join(b.skillsDir, entry.Name(), entry.Name()+".md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}
		s := agent.Skill{Name: entry.Name(), Dir: filepath.Join(b.skillsDir, entry.Name())}
		// Parse simple YAML frontmatter (name, description).
		lines := strings.Split(string(data), "\n")
		if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
			for _, line := range lines[1:] {
				if strings.TrimSpace(line) == "---" {
					break
				}
				parts := strings.SplitN(line, ":", 2)
				if len(parts) != 2 {
					continue
				}
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				switch key {
				case "name":
					s.Name = val
				case "description":
					s.Description = val
				}
			}
		}
		skills = append(skills, s)
	}
	return skills
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