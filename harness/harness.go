package harness

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"
)

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
		skill.Content = first + "\n" + readRemaining(scanner)
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
	skill.Content = readRemaining(scanner)
	return skill, scanner.Err()
}

// readRemaining reads the rest of the scanner into a string.
func readRemaining(scanner *bufio.Scanner) string {
	var sb strings.Builder
	for scanner.Scan() {
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