// Package skills implements the skills seam: scanning a directory
// for <name>/SKILL.md skill files (one folder per skill, the file
// always named SKILL.md, the folder name is the skill name),
// parsing YAML frontmatter, and providing the skills domain tool
// (read/write/update) and
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

// PromptSections renders the Available Skills section: the read
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
	sb.WriteString("Call the skills tool (action read) with a skill name to read its full content; actions write and update create and patch skills.\n")
	for _, s := range skills {
		fmt.Fprintf(&sb, "- %s: %s\n", s.Name, s.Description)
	}
	return []core.PromptSection{
		{Title: "Available Skills", Body: sb.String(), Order: promptSectionOrder},
	}
}

// skillsActions is the structured sub-action metadata for the skills
// domain tool (one source of truth for the schema enum, the prompt,
// and the /tools inventory - the one-domain tool law, t_e06b38bc).
var skillsActions = []core.ToolAction{
	{Name: "read", Description: "Load a skill's full content by name. Returns the skill's markdown body and the directory path where its files (scripts, references, templates) live."},
	{Name: "write", Description: "Create a new skill: a <dir>/<name>/SKILL.md folder with YAML frontmatter (description) over the body. The frontmatter shape must parse exactly the way the loader parses it."},
	{Name: "update", Description: "Patch an existing skill: find it by name, replace one exact occurrence in its SKILL.md."},
}

// skillsActionNames returns the action names from the metadata.
func skillsActionNames() []string {
	names := make([]string, len(skillsActions))
	for i, a := range skillsActions {
		names[i] = a.Name
	}
	return names
}

// Tools returns the skills domain tool. The one-domain tool law
// (t_e06b38bc): the domain tool names ITSELF (formerly the flat
// read tool) and the actions list carries read/write/update.
func (sp *SkillsProvider) Tools() map[string]core.Tool {
	return map[string]core.Tool{
		"skills": {
			Description: "Skills (one SKILL.md folder per skill) the agent loads on demand: read a skill's full content, create a new skill, or patch an existing one. The prompt's Available Skills list is the name + description inventory.",
			Actions:     skillsActions,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action":      map[string]any{"type": "string", "description": "read, write, or update."},
					"name":        map[string]any{"type": "string", "description": "The skill name (read); the new skill's folder name (write); the skill to patch (update)."},
					"description": map[string]any{"type": "string", "description": "One-line frontmatter description (write; becomes the Available Skills line)."},
					"body":        map[string]any{"type": "string", "description": "Full markdown body under the frontmatter (write; do not repeat the --- fences)."},
					"old_text":    map[string]any{"type": "string", "description": "Exact text to find in SKILL.md, occurring exactly once (update)."},
					"new_text":    map[string]any{"type": "string", "description": "Replacement text (update)."},
				},
				"required": []string{"action", "name"},
			},
			Run: sp.runTool,
		},
	}
}

// runTool dispatches the skills domain tool's actions.
func (sp *SkillsProvider) runTool(ctx context.Context, args map[string]any) (string, error) {
	switch action, _ := args["action"].(string); action {
	case "read":
		return sp.runRead(ctx, args)
	case "write":
		return sp.runWrite(ctx, args)
	case "update":
		return sp.runUpdate(ctx, args)
	default:
		return "", fmt.Errorf("action is required: one of %v", skillsActionNames())
	}
}

// validSkillName enforces the loader's folder-name law (card
// t_5baad951): the folder name IS the skill name, so it must be a
// single directory component - no separators, no hidden dot dirs, no
// padding whitespace.
func validSkillName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	if strings.HasPrefix(name, ".") || strings.TrimSpace(name) != name {
		return false
	}
	return true
}

// runWrite is the write-action handler: create <dir>/<name>/SKILL.md
// with the loader's frontmatter shape (--- / description: / ---). The
// description must stay one line (the loader reads a single scalar
// line) and the body must not open with a --- fence (the parser would
// eat it as the frontmatter close). A re-parse through loadSkill
// pins the composed shape before the confirmation returns.
func (sp *SkillsProvider) runWrite(ctx context.Context, args map[string]any) (string, error) {
	name := strings.TrimSpace(strArg(args["name"]))
	if !validSkillName(name) {
		return "", fmt.Errorf("write: name must be a single folder component (no separators, no leading dot), got %q", name)
	}
	description := strings.TrimSpace(strArg(args["description"]))
	if description == "" || strings.ContainsAny(description, "\n\r") {
		return "", fmt.Errorf("write: description is required and must be one line")
	}
	body := strArg(args["body"])
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("write: body is required")
	}
	if strings.HasPrefix(strings.TrimLeft(body, " \t"), "---") {
		return "", fmt.Errorf(`write: body must not start with a "---" fence - the frontmatter is composed for you`)
	}
	dir := sp.Dir
	if dir == "" {
		return "", fmt.Errorf("skills directory not configured")
	}
	skillDir := filepath.Join(dir, name)
	if _, err := os.Stat(skillDir); err == nil {
		return "", fmt.Errorf("skill %q already exists in %s - use the update action", name, skillDir)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err == nil {
		return "", fmt.Errorf("skill %q already exists in %s - use the update action", name, skillDir)
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	content := fmt.Sprintf("---\ndescription: %s\n---\n%s", description, body)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("create skill dir: %w", err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write SKILL.md: %w", err)
	}
	// the loader's parse of what we just wrote is the shape proof
	s, err := loadSkill(skillFile)
	if err != nil || s.Description != description {
		return "", fmt.Errorf("write: composed SKILL.md failed the loader's frontmatter parse (description %q)", description)
	}
	return "created skill " + name + " at " + skillDir, nil
}

// runUpdate is the update-action handler: resolve the skill exactly
// the way runRead does (shared dir first, own dir second - the winner
// of the merge law), then an exact-once targeted replace in its
// SKILL.md file. Frontmatter must survive: a file that opened with
// --- must still open with ---.
func (sp *SkillsProvider) runUpdate(ctx context.Context, args map[string]any) (string, error) {
	name := strings.TrimSpace(strArg(args["name"]))
	if !validSkillName(name) {
		return "", fmt.Errorf("update: invalid skill name %q", name)
	}
	oldText := strArg(args["old_text"])
	newText := strArg(args["new_text"])
	if oldText == "" || newText == "" {
		return "", fmt.Errorf(`update: "old_text" (exact text to replace, occurring once) and "new_text" are required`)
	}
	skillFile, err := sp.resolveSkillFile(name)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(skillFile)
	if err != nil {
		return "", fmt.Errorf("read SKILL.md: %w", err)
	}
	content := string(raw)
	switch n := strings.Count(content, oldText); n {
	case 1:
	case 0:
		return "", fmt.Errorf("update: old_text not found in %s - the text must match exactly", skillFile)
	default:
		return "", fmt.Errorf("update: old_text occurs %d times in %s - it must be unique", n, skillFile)
	}
	hadFrontmatter := strings.HasPrefix(content, "---\n") || strings.HasPrefix(content, "---\r\n")
	patched := strings.Replace(content, oldText, newText, 1)
	if hadFrontmatter && !strings.HasPrefix(patched, "---\n") {
		return "", fmt.Errorf("update: replacement would break the frontmatter (the file must keep its leading --- fence)")
	}
	skillFile, err = sp.writePatched(skillFile, patched)
	if err != nil {
		return "", err
	}
	return "updated skill " + name + " (" + skillFile + "): replaced one occurrence", nil
}

// writePatched writes the patched content back and re-parses it
// through the loader - the same shape gate runWrite applies.
func (sp *SkillsProvider) writePatched(path, content string) (string, error) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write SKILL.md: %w", err)
	}
	if _, err := loadSkill(path); err != nil {
		return "", fmt.Errorf("update: patched SKILL.md failed the loader's parse: %w", err)
	}
	return path, nil
}

// resolveSkillFile finds a skill's SKILL.md by name in the scan order
// LoadSkills merges by (shared first, then own): the winner of a name
// collision is the file the update action patches.
func (sp *SkillsProvider) resolveSkillFile(name string) (string, error) {
	dirs := sp.scanDirs(sp.Dir)
	if len(dirs) == 0 {
		return "", fmt.Errorf("skills directory not configured")
	}
	for _, dir := range dirs {
		candidate := filepath.Join(dir, name, "SKILL.md")
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	skills, _ := sp.LoadSkills("")
	var names []string
	for _, s := range skills {
		names = append(names, s.Name)
	}
	return "", fmt.Errorf("skill %q not found. Available: %s", name, strings.Join(names, ", "))
}

// strArg is the plain string read every action does on an untyped
// tool-args map.
func strArg(v any) string {
	s, _ := v.(string)
	return s
}

// runRead is the read-action handler.
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
