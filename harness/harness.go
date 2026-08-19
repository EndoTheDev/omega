package harness

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// PromptOptions configures the system prompt builder.
type PromptOptions struct {
	ProjectContext string              // AGENTS.md contents, may be empty
	Skills         []agent.Skill       // loaded skills, may be empty
	Extensions     []agent.ExtensionInfo // loaded extensions, may be nil
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
		b.WriteString("Call the skills.read tool with a skill name to read its full content.\n")
		for _, skill := range opts.Skills {
			fmt.Fprintf(&b, "- %s: %s\n", skill.Name, skill.Description)
		}
	}

	// Tools section: list native tools and extension-provided tools
	// so the agent knows where each tool comes from.
	b.WriteString("\n## Tools\n")
	b.WriteString("### Native\n")
	b.WriteString("- shell.run: Run a shell command\n")
	b.WriteString("- files.read: Read a file\n")
	b.WriteString("- files.write: Write a file\n")
	b.WriteString("- files.edit: Apply a targeted find-and-replace patch\n")
	b.WriteString("- skills.read: Load a skill's full content\n")
	if len(opts.Extensions) > 0 {
		b.WriteString("\n### Extensions\n")
		for _, ext := range opts.Extensions {
			if len(ext.ToolList) > 0 {
				var tools []string
				for _, t := range ext.ToolList {
					tools = append(tools, t.Name+" ("+t.Description+")")
				}
				fmt.Fprintf(&b, "- %s: %s\n", ext.Name, strings.Join(tools, ", "))
			}
		}
	}

	b.WriteString("\n## Environment\n")
	fmt.Fprintf(&b, "CWD: %s\n", opts.CWD)
	fmt.Fprintf(&b, "OS: %s\n", runtime.GOOS)
	if runtime.GOOS == "windows" {
		b.WriteString("Shell: cmd.exe\n")
	} else {
		b.WriteString("Shell: bash\n")
	}
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

// LoadSkills scans dir for subdirectories, each containing a skill file
// named <dirname>.md. It parses the YAML frontmatter and returns the
// loaded skills. An empty or missing dir returns an empty slice with
// no error.
func LoadSkills(dir string) ([]agent.Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var skills []agent.Skill
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		skillFile := filepath.Join(dir, entry.Name(), entry.Name()+".md")
		skill, err := loadSkill(skillFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue // no <name>.md in this directory, skip
			}
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		skill.Dir = filepath.Join(dir, entry.Name())
		skills = append(skills, skill)
	}
	return skills, nil
}

// loadSkill reads a single skill .md file and parses its YAML
// frontmatter. The frontmatter is delimited by --- lines. The body is
// everything after the closing ---.
func loadSkill(path string) (agent.Skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return agent.Skill{}, err
	}
	defer f.Close()

	var skill agent.Skill
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		// No frontmatter — treat the whole file as content.
		// The first line was already consumed by Scan; prepend it.
		first := scanner.Text()
		skill.Content = first + "\n" + readRemaining(scanner, "")
		return skill, nil
	}

	// Parse YAML frontmatter (simple key: value lines).
	for scanner.Scan() {
		line := scanner.Text()
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
			skill.Name = val
		case "description":
			skill.Description = val
		}
	}

	// Read the markdown body.
	skill.Content = readRemaining(scanner, "")
	return skill, scanner.Err()
}

// readRemaining reads the rest of the scanner into a string, prepending
// prefix to each line.
func readRemaining(scanner *bufio.Scanner, prefix string) string {
	var sb strings.Builder
	for scanner.Scan() {
		sb.WriteString(prefix)
		sb.WriteString(scanner.Text())
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ProjectRoot returns the nearest directory (walking up from dir) that
// contains an AGENTS.md, or "" if none exists. This is the trust unit:
// a project is trusted by its root directory, not by individual files.
func ProjectRoot(dir string) string {
	visited := map[string]bool{}
	for {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return ""
		}
		if visited[abs] {
			return ""
		}
		visited[abs] = true

		if _, err := os.Stat(filepath.Join(abs, "AGENTS.md")); err == nil {
			return abs
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			return ""
		}
		dir = parent
	}
}

// LoadProjectContext walks from dir up to the filesystem root,
// collecting AGENTS.md files at each level. Results are concatenated
// in root-to-leaf order (outermost project first, nearest last) so
// the nearest context has the most influence. Non-existent files are
// silently skipped. Read errors (permission denied, etc.) produce a
// warning line instead of being silently dropped.
func LoadProjectContext(dir string) string {
	var parts []string
	visited := map[string]bool{}

	for {
		abs, err := filepath.Abs(dir)
		if err != nil {
			break
		}
		if visited[abs] {
			break
		}
		visited[abs] = true

		path := filepath.Join(abs, "AGENTS.md")
		data, err := os.ReadFile(path)
		if err == nil {
			parts = append(parts, string(data))
		} else if !os.IsNotExist(err) {
			parts = append(parts, fmt.Sprintf("[warning: could not read %s: %v]", path, err))
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		dir = parent
	}

	// Reverse so root is first, CWD is last.
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "\n\n")
}