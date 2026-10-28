// Package skills implements the skills seam: scanning a directory
// for <name>/SKILL.md skill files (one folder per skill, the file
// always named SKILL.md, the folder name is the skill name),
// parsing YAML frontmatter, and providing the skills.read tool and
// /skills slash command.
package skills

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// SkillsProvider implements core.SkillsProvider by scanning a
// directory for <name>/<name>.md skill files. SharedDir (optional) is
// scanned before Dir and is every agent's shared skill home; Dir is
// the agent's own. The merge is by skill NAME in scan order - first
// match wins, so shared entries win name collisions (the card's
// resolution order is literal: shared first).
type SkillsProvider struct {
	Dir       string
	SharedDir string
	// Disabled is the config.yaml disabled.skills fold (v0.14.1): the
	// names a prompt build never advertises. Unset by default. The
	// disableMu guards it: prompt builds read while the toggle seam
	// swaps the list live.
	Disabled []string

	disableMu sync.RWMutex
}

// SetDisabled swaps the disable list live (the host's toggle seam and
// hot config reload take this one path).
func (sp *SkillsProvider) SetDisabled(names []string) {
	sp.disableMu.Lock()
	defer sp.disableMu.Unlock()
	sp.Disabled = names
}

// disabledNames returns the live disable list snapshot (nil when none).
func (sp *SkillsProvider) disabledNames() []string {
	sp.disableMu.RLock()
	defer sp.disableMu.RUnlock()
	return sp.Disabled
}

// scanDirs returns the ordered directory list to scan: SharedDir
// (set) first, then dir (the explicit argument; a missing dir yields
// an empty slice, not an error).
func (sp *SkillsProvider) scanDirs(dir string) []string {
	var dirs []string
	if sp.SharedDir != "" {
		dirs = append(dirs, sp.SharedDir)
	}
	if dir != "" {
		dirs = append(dirs, dir)
	}
	return dirs
}

// LoadSkills scans the configured directories (shared first, then
// dir) for <name>/SKILL.md skill files and returns them, deduped by
// name with the first (shared) occurrence winning. Missing
// directories return an empty slice, not an error. If dir is empty,
// uses the provider's configured Dir.
func (sp *SkillsProvider) LoadSkills(dir string) ([]core.Skill, error) {
	if dir == "" {
		dir = sp.Dir
	}
	dirs := sp.scanDirs(dir)
	if len(dirs) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []core.Skill
	for _, d := range dirs {
		skills, err := scanSkills(d)
		if err != nil {
			return nil, err
		}
		for _, s := range skills {
			if seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			out = append(out, s)
		}
	}
	return out, nil
}

// ListSkills is the read-only inspector snapshot (core.SkillsLister,
// card t_db430fea): a fresh scan of the configured directory, sorted by
// name. The directory is the live source of truth - a re-scan on every
// listing is the point; there is no cached loader state to invalidate.
// A missing directory is an empty inventory, not an error.
func (sp *SkillsProvider) ListSkills() ([]core.Skill, error) {
	skills, err := sp.LoadSkills("")
	if err != nil {
		return nil, err
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// promptSectionOrder places the Available Skills section behind the
// memory snapshot (10) and ahead of the tools list (30).
const promptSectionOrder = 20

// PromptSections renders the Available Skills section: the skills.read
// usage instruction plus one line per skill. Empty when no skills are
// installed. Owned here - the usage instruction describes this module's
// tool, not the prompt builder's.
func (sp *SkillsProvider) PromptSections(opts core.PromptBuildOptions) []core.PromptSection {
	skills, _ := sp.LoadSkills("")
	// The disabled.skills fold (v0.14.1): a disabled skill stays in the
	// registry (the tab lists it, the reader tool reads it) and leaves
	// the prompt - exclusion is prompt-build business, nothing else.
	if off := sp.disabledNames(); len(off) > 0 {
		offSet := make(map[string]bool, len(off))
		for _, n := range off {
			offSet[n] = true
		}
		enabled := make([]core.Skill, 0, len(skills))
		for _, s := range skills {
			if !offSet[s.Name] {
				enabled = append(enabled, s)
			}
		}
		skills = enabled
	}
	if len(skills) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("Call the skills.read tool with a skill name to read its full content.\n")
	for _, s := range skills {
		fmt.Fprintf(&sb, "- %s: %s\n", s.Name, s.Description)
	}
	return []core.PromptSection{
		{Title: "Available Skills", Body: sb.String(), Order: promptSectionOrder},
	}
}

// Tools returns the skills.read tool.
func (sp *SkillsProvider) Tools() map[string]core.Tool {
	return map[string]core.Tool{
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
	dirs := sp.scanDirs(sp.Dir)
	if len(dirs) == 0 {
		return "", fmt.Errorf("skills directory not configured")
	}
	// Read resolution follows the LoadSkills merge order: shared is
	// scanned first, then the agent's own dir - so a name present in
	// both resolves to the same winner the prompt listing shows.
	var lastIsNotExist bool
	for _, dir := range dirs {
		skillFile := filepath.Join(dir, name, "SKILL.md")
		s, err := loadSkill(skillFile)
		if err != nil {
			lastIsNotExist = os.IsNotExist(err)
			continue
		}
		s.Name = name
		s.Dir = filepath.Join(dir, name)
		return formatSkill(s), nil
	}
	if lastIsNotExist {
		skills, _ := sp.LoadSkills("")
		var names []string
		for _, s := range skills {
			names = append(names, s.Name)
		}
		return "", fmt.Errorf("skill %q not found. Available: %s", name, strings.Join(names, ", "))
	}
	return "", fmt.Errorf("skill %q could not be read", name)
}

// HandleCommand processes the /skills slash command.
func (sp *SkillsProvider) HandleCommand(ctx context.Context, name, args string) (string, error) {
	if name != "/skills" {
		return "", fmt.Errorf("unknown command: %s", name)
	}
	skills, err := sp.LoadSkills("")
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
func (sp *SkillsProvider) Commands() []core.ExtensionCommand {
	return []core.ExtensionCommand{
		{Name: "/skills", Description: "List loaded skills with name and description"},
	}
}

// scanSkills scans a directory for <name>/SKILL.md skill files. The
// folder name is the skill name (card t_5baad951): frontmatter cannot
// rename a skill, so the parsed name field (retired) is overridden.
func scanSkills(dir string) ([]core.Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var skills []core.Skill
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		skillFile := filepath.Join(dir, entry.Name(), "SKILL.md")
		s, err := loadSkill(skillFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		s.Name = entry.Name()
		s.Dir = filepath.Join(dir, entry.Name())
		skills = append(skills, s)
	}
	return skills, nil
}

// WarnLegacyShape reports skill folders still in the retired format:
// a directory holding <name>.md with no SKILL.md. Such a folder is
// invisible to discovery (discovery reads SKILL.md only), and silent
// loss is how the old <name>.md bug hid - so every legacy folder gets
// one explicit boot warning naming the skill. The warning goes to
// stderr unconditionally (logging may be disabled, a NopLogger
// discards everything) and into the log when a logger is mounted.
// Boot-only: runtime scans skip the legacy shape, this once-per-boot
// scan is the visibility contract (card t_5baad951). Returns the
// warned skill names. No auto-migration - a rename is the fix.
func (sp *SkillsProvider) WarnLegacyShape(logger core.LoggerProvider) []string {
	var warned []string
	for _, dir := range sp.scanDirs(sp.Dir) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// Best-effort boot diagnostic; the runtime scan surfaces
			// real read errors (LoadSkills returns them).
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if fileExists(filepath.Join(dir, entry.Name(), "SKILL.md")) {
				continue
			}
			legacy := filepath.Join(dir, entry.Name(), entry.Name()+".md")
			if !fileExists(legacy) {
				continue
			}
			msg := fmt.Sprintf("skills: skill %q uses the retired format (%s) - rename the file to SKILL.md (the folder name is the skill name)",
				entry.Name(), legacy)
			fmt.Fprintln(os.Stderr, msg)
			if logger != nil {
				logger.Printf("%s", msg)
			}
			warned = append(warned, entry.Name())
		}
	}
	return warned
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// loadSkill reads a single SKILL.md file and parses its YAML
// frontmatter. The frontmatter is delimited by --- lines; only the
// description is read from it - the skill name is the folder name.
func loadSkill(path string) (core.Skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return core.Skill{}, err
	}
	defer f.Close()

	var s core.Skill
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
		if key == "description" {
			s.Description = val
		}
	}
	s.Content = readRemaining(scanner)
	return s, scanner.Err()
}

func formatSkill(s core.Skill) string {
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
