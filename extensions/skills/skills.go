// Package skills implements the skills seam: scanning a directory
// for <name>/<name>.md skill files, parsing YAML frontmatter, and
// providing the skills.read tool and /skills slash command.
package skills

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"
)

// SkillsProvider implements agent.SkillsProvider by scanning a
// directory for skill files.
type SkillsProvider struct {
	Dir string
}

// LoadSkills scans dir for <name>/<name>.md skill files and returns
// them. A missing directory returns an empty slice, not an error.
func (sp *SkillsProvider) LoadSkills(dir string) ([]agent.Skill, error) {
	return scanSkills(dir)
}

// Tools returns the skills.read tool.
func (sp *SkillsProvider) Tools() map[string]agent.Tool {
	return map[string]agent.Tool{
		"skills.read": {
			Description: "Load a skill's full content by name. Returns the skill's markdown body and the directory path where its files (scripts, references, templates) live.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "The skill name (from the Available Skills list)"},
				},
				"required": []string{"name"},
			},
			Run: sp.runRead,
		},
	}
}

// runRead is the tool handler for skills.read.
func (sp *SkillsProvider) runRead(ctx context.Context, args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return "", fmt.Errorf(`missing required argument "name"`)
	}
	dir := sp.Dir
	if dir == "" {
		return "", fmt.Errorf("skills directory not configured")
	}
	skillFile := filepath.Join(dir, name, name+".md")
	s, err := loadSkill(skillFile)
	if err != nil {
		if os.IsNotExist(err) {
			entries, _ := os.ReadDir(dir)
			var names []string
			for _, e := range entries {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					names = append(names, e.Name())
				}
			}
			return "", fmt.Errorf("skill %q not found. Available: %s", name, strings.Join(names, ", "))
		}
		return "", err
	}
	s.Name = name
	s.Dir = filepath.Join(dir, name)
	return formatSkill(s), nil
}

// HandleCommand processes the /skills slash command.
func (sp *SkillsProvider) HandleCommand(ctx context.Context, name, args string) (string, error) {
	if name != "/skills" {
		return "", fmt.Errorf("unknown command: %s", name)
	}
	skills, err := scanSkills(sp.Dir)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if len(skills) == 0 {
		sb.WriteString("[no skills loaded]")
	} else {
		nameWidth := 12
		for _, s := range skills {
			if len(s.Name) > nameWidth {
				nameWidth = len(s.Name)
			}
		}
		fmt.Fprintf(&sb, "%-*s  %s\n", nameWidth, "NAME", "DESCRIPTION")
		for _, s := range skills {
			fmt.Fprintf(&sb, "%-*s  %s\n", nameWidth, s.Name, s.Description)
		}
	}
	return sb.String(), nil
}

// Commands returns the slash command definitions.
func (sp *SkillsProvider) Commands() []agent.ExtensionCommand {
	return []agent.ExtensionCommand{
		{Name: "/skills", Description: "List loaded skills with name and description"},
	}
}

// scanSkills scans a directory for <name>/<name>.md skill files.
func scanSkills(dir string) ([]agent.Skill, error) {
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
		s, err := loadSkill(skillFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		s.Dir = filepath.Join(dir, entry.Name())
		skills = append(skills, s)
	}
	return skills, nil
}

// loadSkill reads a single skill .md file and parses its YAML
// frontmatter. The frontmatter is delimited by --- lines.
func loadSkill(path string) (agent.Skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return agent.Skill{}, err
	}
	defer f.Close()

	var s agent.Skill
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		s.Content = scanner.Text() + "\n" + readRemaining(scanner)
		return s, nil
	}
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
			s.Name = val
		case "description":
			s.Description = val
		}
	}
	s.Content = readRemaining(scanner)
	return s, scanner.Err()
}

func formatSkill(s agent.Skill) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Skill: %s\n", s.Name)
	fmt.Fprintf(&sb, "Directory: %s\n\n", s.Dir)
	sb.WriteString(s.Content)
	return sb.String()
}

func readRemaining(scanner *bufio.Scanner) string {
	var sb strings.Builder
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}