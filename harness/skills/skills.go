// Package skills implements the skills seam.
package skills

import (
	"bufio"
	"context"
	"fmt"
	"github.com/EndoTheDev/omega/agent"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

type SkillsProvider struct {
	Dir       string
	SharedDir string
	// Disabled is the config.yaml disabled.skills fold: the names a prompt build
	// never advertises.
	Disabled []string

	Allow    []string
	AllowSet bool

	providerFn   func() agent.Provider
	curatorModel string

	disableMu sync.RWMutex
}

func (sp *SkillsProvider) SetCuratorModel(model string) {
	sp.disableMu.Lock()
	defer sp.disableMu.Unlock()
	sp.curatorModel = model
}

func (sp *SkillsProvider) curatorSlot() string {
	sp.disableMu.RLock()
	defer sp.disableMu.RUnlock()
	return sp.curatorModel
}

func (sp *SkillsProvider) SetDisabled(names []string) {
	sp.disableMu.Lock()
	defer sp.disableMu.Unlock()
	sp.Disabled = names
}

func (sp *SkillsProvider) SetProviderFn(fn func() agent.Provider) { sp.providerFn = fn }

// provider returns the consult provider for one pass, nil-safe.
func (sp *SkillsProvider) provider() agent.Provider {
	if sp.providerFn == nil {
		return nil
	}
	return sp.providerFn()
}

// disabledNames returns the live disable list snapshot (nil when none).
func (sp *SkillsProvider) disabledNames() []string {
	sp.disableMu.RLock()
	defer sp.disableMu.RUnlock()
	return sp.Disabled
}

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

// LoadSkills scans the configured directories (shared first, then dir).
func (sp *SkillsProvider) LoadSkills(dir string) ([]Skill, error) {
	if dir == "" {
		dir = sp.Dir
	}
	dirs := sp.scanDirs(dir)
	if len(dirs) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []Skill
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
	if sp.AllowSet {
		allowed := make(map[string]bool, len(sp.Allow))
		for _, name := range sp.Allow {
			allowed[name] = true
		}
		var keep []Skill
		for _, s := range out {
			if allowed[s.Name] {
				keep = append(keep, s)
			}
		}
		return keep, nil
	}
	return out, nil
}

// ListSkills is the read-only inspector snapshot: a fresh scan of the configured
// directory, sorted by name.
func (sp *SkillsProvider) ListSkills() ([]Skill, error) {
	skills, err := sp.LoadSkills("")
	if err != nil {
		return nil, err
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// SkillsEntries is the std-typed inventory view consumers read through the universal.
func (sp *SkillsProvider) SkillsEntries() (index map[string]string, contents map[string]string, err error) {
	skills, err := sp.LoadSkills("")
	if err != nil {
		return nil, nil, err
	}
	index = make(map[string]string, len(skills))
	contents = make(map[string]string, len(skills))
	for _, s := range skills {
		index[s.Name] = s.Description
		contents[s.Name] = s.Content
	}
	return index, contents, nil
}

// SkillRead reads one skill by name through the merge law's winner rule - the std-typed
// face of ReadSkill for consumers that cannot name the capsule type.
func (sp *SkillsProvider) SkillRead(name string) (description, content string, err error) {
	s, err := sp.ReadSkill(name)
	if err != nil {
		return "", "", err
	}
	return s.Description, s.Content, nil
}

// promptSectionOrder places the Available Skills section behind the memory snapshot.
const promptSectionOrder = 20

func (sp *SkillsProvider) PromptSections(opts core.PromptBuildOptions) []core.PromptSection {
	skills, _ := sp.LoadSkills("")
	if off := sp.disabledNames(); len(off) > 0 {
		offSet := make(map[string]bool, len(off))
		for _, n := range off {
			offSet[n] = true
		}
		enabled := make([]Skill, 0, len(skills))
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
	// The lead sentence names the skills verbs, so it is built from the verbs the fold
	// left on the wire: an inventory whose only loader is disabled is dead weight, and
	// naming a disabled verb is the same lie as listing a disabled tool.
	var verbs []string
	if opts.ToolEnabled("skills.read") {
		verbs = append(verbs, "Call skills.read with a skill name to load its full content")
	}
	if opts.ToolEnabled("skills.write") {
		verbs = append(verbs, "skills.write creates a new skill")
	}
	if opts.ToolEnabled("skills.update") {
		verbs = append(verbs, "skills.update patches an existing one")
	}
	if len(verbs) == 0 {
		return nil
	}
	var sb strings.Builder
	// Clauses join the way the original prose did: "a; b and c" - the all-on case stays
	// byte-identical to the pre-fold sentence.
	switch len(verbs) {
	case 1:
		sb.WriteString(verbs[0])
	default:
		sb.WriteString(strings.Join(verbs[:len(verbs)-1], "; "))
		sb.WriteString(" and ")
		sb.WriteString(verbs[len(verbs)-1])
	}
	sb.WriteString(".\n")
	for _, s := range skills {
		fmt.Fprintf(&sb, "- %s: %s\n", s.Name, s.Description)
	}
	return []core.PromptSection{
		{Title: "Available Skills", Body: sb.String(), Order: promptSectionOrder},
	}
}

func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

// object assembles the tool's JSON object schema. required names the args THIS verb
// cannot run without - its own list, never another verb's; a verb with no required args
// (curate - the bare call is the default pass) omits the key entirely.
func object(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func (sp *SkillsProvider) Tools() map[string]core.Tool {
	return map[string]core.Tool{
		"skills.read": {
			Description: "Load a skill's full content by name. Returns the skill's markdown body and the directory path where its files (scripts, references, templates) live. The prompt's Available Skills list is the name + description inventory.",
			Parameters: object(map[string]any{
				"name": prop("string", "The skill name to load."),
			}, "name"),
			Run: sp.runRead,
		},
		"skills.write": {
			Description: "Create a new skill: a <dir>/<name>/SKILL.md folder with YAML frontmatter (description, author: agent) over the body. The frontmatter shape must parse exactly the way the loader parses it.",
			Parameters: object(map[string]any{
				"name":        prop("string", "The new skill's folder name (the skill name)."),
				"description": prop("string", "One-line frontmatter description (becomes the Available Skills line)."),
				"body":        prop("string", "Full markdown body under the frontmatter (do not repeat the --- fences)."),
			}, "name", "description", "body"),
			Run: sp.runWrite,
		},
		"skills.update": {
			Description: "Patch an existing skill: find it by name, replace one exact occurrence in its SKILL.md.",
			Parameters: object(map[string]any{
				"name":     prop("string", "The skill to patch."),
				"old_text": prop("string", "Exact text to find in SKILL.md, occurring exactly once."),
				"new_text": prop("string", "Replacement text."),
			}, "name", "old_text", "new_text"),
			Run: sp.runUpdate,
		},
		"skills.curate": {
			Description: "Run one learning-loop curator pass: the deterministic staleness pass (stale at 14d, archive at 30d) plus LLM-proposed merges/distills applied with backup and audit ledger. Returns the report and ledger ids.",
			Parameters: object(map[string]any{
				"verb": prop("string", "Sub-verb: run (default), rollback <ledger-id>, stale, ledger (recent rows)."),
			}),
			Run: sp.runCurate,
		},
	}
}

func (sp *SkillsProvider) runCurate(ctx context.Context, args map[string]any) (string, error) {
	out, err := sp.HandleCommand(ctx, "/curator", strArg(args["verb"]))
	if out == "" && err == nil {
		return "", fmt.Errorf("curate: nothing to do")
	}
	return out, err
}

// validSkillName enforces the loader's folder-name law: the folder name IS the skill
// name, so it must be a single directory component - no separators, no hidden dot dirs,
// no padding whitespace.
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

// runWrite is the write-action handler. The description must stay one line (the loader
// reads a single scalar line) and the body must not open with a --- fence (the parser
// would eat it as the frontmatter close).
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
	content := fmt.Sprintf("---\ndescription: %s\nauthor: agent\n---\n%s", description, body)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("create skill dir: %w", err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write SKILL.md: %w", err)
	}
	s, err := loadSkill(skillFile)
	if err != nil || s.Description != description {
		return "", fmt.Errorf("write: composed SKILL.md failed the loader's frontmatter parse (description %q)", description)
	}
	TouchUsage(dir, name, "write", "agent")
	return "created skill " + name + " at " + skillDir, nil
}

// runUpdate is the update-action handler: resolve the skill exactly the way runRead.
// Frontmatter must survive: a file that opened with --- must still open with ---.
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
	// The persisted lesson resets the nudge counter (ObserveToolRun) and ages the record.
	TouchUsage(filepath.Dir(filepath.Dir(skillFile)), name, "write", "")
	return "updated skill " + name + " (" + skillFile + "): replaced one occurrence", nil
}

func (sp *SkillsProvider) writePatched(path, content string) (string, error) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write SKILL.md: %w", err)
	}
	if _, err := loadSkill(path); err != nil {
		return "", fmt.Errorf("update: patched SKILL.md failed the loader's parse: %w", err)
	}
	return path, nil
}

// ReadSkill is the single-content read seam.
func (sp *SkillsProvider) ReadSkill(name string) (Skill, error) {
	// The explicit allowlist is the agent's whole registry.
	if sp.AllowSet && !sp.allowed(name) {
		return Skill{}, fmt.Errorf("skill %q is not enabled for this agent (its allowlist names %s)", name, sp.allowList())
	}
	skillFile, err := sp.resolveSkillFile(name)
	if err != nil {
		return Skill{}, err
	}
	s, err := loadSkill(skillFile)
	if err != nil {
		return Skill{}, fmt.Errorf("read SKILL.md: %w", err)
	}
	s.Name = name
	s.Dir = filepath.Dir(skillFile)
	return s, nil
}

func (sp *SkillsProvider) allowed(name string) bool {
	for _, a := range sp.Allow {
		if a == name {
			return true
		}
	}
	return false
}

func (sp *SkillsProvider) allowList() string {
	if len(sp.Allow) == 0 {
		return "none"
	}
	return strings.Join(sp.Allow, ", ")
}

// resolveSkillFile finds a skill's SKILL.md by name in the scan order LoadSkills.
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

func strArg(v any) string {
	s, _ := v.(string)
	return s
}

func (sp *SkillsProvider) runRead(ctx context.Context, args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return "", fmt.Errorf(`missing required argument "name"`)
	}
	dirs := sp.scanDirs(sp.Dir)
	if len(dirs) == 0 {
		return "", fmt.Errorf("skills directory not configured")
	}
	// Read resolution follows the LoadSkills merge order.
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
		TouchUsage(dir, name, "read", s.Author)
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

func (sp *SkillsProvider) HandleCommand(ctx context.Context, name, args string) (string, error) {
	switch name {
	case "/skills":
		return sp.handleSkillsCommand()
	case "/curator":
		return sp.handleCuratorCommand(ctx, args)
	}
	return "", fmt.Errorf("unknown command: %s", name)
}

func (sp *SkillsProvider) handleSkillsCommand() (string, error) {
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

func (sp *SkillsProvider) handleCuratorCommand(ctx context.Context, args string) (string, error) {
	verb, rest := strings.TrimSpace(args), ""
	if i := strings.IndexAny(verb, " 	"); i >= 0 {
		rest = strings.TrimSpace(verb[i:])
		verb = verb[:i]
	}
	switch verb {
	case "", "run":
		rep, err := sp.RunCurator(ctx, sp.provider(), sp.curatorSlot())
		if err != nil {
			return formatReport(rep), err
		}
		return formatReport(rep), nil
	case "rollback":
		if rest == "" {
			return "", fmt.Errorf("rollback needs a ledger id - /curator ledger lists them")
		}
		out, err := sp.Rollback(rest)
		if err != nil {
			return "", err
		}
		return "rolled back " + rest + "\n" + out, nil
	case "stale":
		names := sp.StaleSkillNames()
		if len(names) == 0 {
			return "no stale skills", nil
		}
		return "stale: " + strings.Join(names, ", "), nil
	case "ledger":
		entries, err := sp.readLedger()
		if err != nil {
			return "", err
		}
		if len(entries) == 0 {
			return "ledger is empty", nil
		}
		var sb strings.Builder
		for _, e := range entries {
			fmt.Fprintf(&sb, "%s  %s  %-8s %-16s %s\n", e.ID, e.TS, e.Action, e.Skill, e.Evidence["reason"])
		}
		return strings.TrimRight(sb.String(), "\n"), nil
	default:
		return "", fmt.Errorf("unknown /curator verb %q - run | rollback <id> | stale | ledger", verb)
	}
}

func formatReport(rep CuratorReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "curator pass: %d marked stale, %d reactivated, %d archived, %d merges, %d distills",
		rep.MarkedStale, rep.Reactivated, rep.Archived, rep.Merges, rep.Distills)
	if len(rep.LedgerIDs) > 0 {
		fmt.Fprintf(&sb, "\nledger: %s", strings.Join(rep.LedgerIDs, ", "))
	} else {
		sb.WriteString("\nledger: (no mutations)")
	}
	for _, s := range rep.Skipped {
		sb.WriteString("\nskipped: " + s)
	}
	if rep.ProposalsSeen != "" {
		fmt.Fprintf(&sb, "\nproposals: %d chars consulted", len(rep.ProposalsSeen))
	}
	return sb.String()
}

func (sp *SkillsProvider) Commands() []core.ExtensionCommand {
	return []core.ExtensionCommand{
		{Name: "/skills", Description: "List loaded skills with name and description"},
		{Name: "/curator", Description: "Run the weekly skills curator pass (or: rollback <id> | stale | ledger)"},
	}
}

// scanSkills scans a directory for <name>/SKILL.md skill files. The folder name is the
// skill name: frontmatter cannot rename a skill, so the parsed name field (retired) is
// overridden.
func scanSkills(dir string) ([]Skill, error) {
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

// WarnLegacyShape reports skill folders still in the retired format. Such a folder is
// invisible to discovery (discovery reads SKILL.md only), and silent loss is how the
// old <name>.md bug hid - so every legacy folder gets one explicit boot warning naming
// the skill. The warning goes to stderr unconditionally and into the log when a logger
// is mounted. Boot-only: runtime scans skip the legacy shape, this once-per-boot scan
// is the visibility contract.
func (sp *SkillsProvider) WarnLegacyShape(logger agent.LoggerProvider) []string {
	var warned []string
	for _, dir := range sp.scanDirs(sp.Dir) {
		entries, err := os.ReadDir(dir)
		if err != nil {
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

func loadSkill(path string) (Skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return Skill{}, err
	}
	defer f.Close()

	var s Skill
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
		if key == "author" {
			s.Author = val
		}
	}
	s.Content = readRemaining(scanner)
	return s, scanner.Err()
}

func formatSkill(s Skill) string {
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
