package agent

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill is a loaded skill from a skill directory. The YAML frontmatter
// in the skill file provides name and description; the markdown body is
// the skill content injected into the system prompt when invoked. Dir
// is the path to the skill's directory, so the skill can reference its
// own files (scripts, references, templates) by relative path.
type Skill struct {
	Name        string
	Description string
	Content     string
	Dir         string
}

// LoadSkills scans dir for subdirectories, each containing a skill file
// named <dirname>.md. It parses the YAML frontmatter and returns the
// loaded skills. An empty or missing dir returns an empty slice with
// no error.
func LoadSkills(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var skills []Skill
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
func loadSkill(path string) (Skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return Skill{}, err
	}
	defer f.Close()

	var skill Skill
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
