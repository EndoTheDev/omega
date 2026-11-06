package skills

import (
	"github.com/EndoTheDev/omega/agent"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

)

// The weekly curator pass (card t_9972dfd0, ported from Hermes
// agent/curator.py). Division of labor is the card's law:
//
//   - Deterministic parts are CODE: staleness by last_used (stale at
//     14d, archived at 30d), archive moves, whole-folder backups,
//     ledger rows.
//   - The LLM only READS and PROPOSES: one call proposes merge
//     candidates (a narrow skill absorbed into an umbrella) and a
//     distill list (oversized bodies); per accepted candidate, one
//     call drafts the distilled text. Code validates and applies
//     everything with backup + ledger.
//
// Scope law (Hermes provenance port): the LLM passes touch only
// agent-created skills (author: agent frontmatter, stamped by
// skills.write). Hand-written skills age through staleness like
// everything else but are never rewritten by a proposal. Pinned
// skills are untouchable.

// DistillMinChars is the body size a skill must exceed before the
// curator may distill it (~24k, the Hermes threshold).
const DistillMinChars = 24000

// proposal caps keep one weekly pass bounded.
const (
	maxMergesPerPass   = 5
	maxDistillsPerPass = 3
)

// CuratorReport is the pass's outcome, one row per applied mutation.
type CuratorReport struct {
	MarkedStale   int      `json:"marked_stale"`
	Reactivated   int      `json:"reactivated"`
	Archived      int      `json:"archived"`
	Merges        int      `json:"merges"`
	Distills      int      `json:"distills"`
	Skipped       []string `json:"skipped,omitempty"`
	LedgerIDs     []string `json:"ledger_ids"`
	ProposalsSeen string   `json:"proposals_seen,omitempty"`
}

// RunCurator executes one pass against the provider's own skills dir.
// prov may be nil - the deterministic staleness pass still runs; the
// LLM proposal steps are skipped and the report says so. curatorModel
// is the per-subsystem slot (card t_fc0b2f2d): "instance:model" rides
// the named provider instance (ResolveModelSlot), a bare name routes
// through prov.WithModel (empty = the instance's own model).
func (sp *SkillsProvider) RunCurator(ctx context.Context, prov agent.Provider, curatorModel string) (CuratorReport, error) {
	if sp.Dir == "" {
		return CuratorReport{}, fmt.Errorf("curator: skills directory not configured")
	}
	rep := CuratorReport{}
	if err := sp.applyStaleness(&rep); err != nil {
		return rep, err
	}
	if prov == nil {
		rep.Skipped = append(rep.Skipped, "no provider mounted: LLM proposals skipped")
		return rep, nil
	}
	prov, curatorModel = agent.ResolveModelSlot(prov, curatorModel)
	if curatorModel != "" {
		prov = prov.WithModel(curatorModel)
	}
	mergeCands, distillCands, seen, err := sp.proposeMutations(ctx, prov)
	rep.ProposalsSeen = seen
	if err != nil {
		rep.Skipped = append(rep.Skipped, "proposal consult failed: "+err.Error())
		return rep, nil
	}
	for _, m := range mergeCands {
		if rep.Merges >= maxMergesPerPass {
			break
		}
		id, err := sp.applyMerge(ctx, prov, m)
		if err != nil {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("merge %s->%s: %v", m.Source, m.Into, err))
			continue
		}
		rep.Merges++
		rep.LedgerIDs = append(rep.LedgerIDs, id)
	}
	for _, d := range distillCands {
		if rep.Distills >= maxDistillsPerPass {
			break
		}
		id, err := sp.applyDistill(ctx, prov, d)
		if err != nil {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("distill %s: %v", d.Name, err))
			continue
		}
		rep.Distills++
		rep.LedgerIDs = append(rep.LedgerIDs, id)
	}
	return rep, nil
}

// applyStaleness is the deterministic transition pass: every skill on
// disk (usage-recorded or not) freshens or retires by its last real
// activity. A skill with no record gets one on first sight (created
// now), so never-used skills anchor on creation and cannot self-
// archive by sitting unread for one archive window (the Hermes law).
func (sp *SkillsProvider) applyStaleness(rep *CuratorReport) error {
	usageMu.Lock()
	defer usageMu.Unlock()
	usage := readUsageLocked(sp.Dir)
	now := time.Now().UTC()
	touched := false
	staleCutoff := now.AddDate(0, 0, -StaleAfterDays)
	archiveCutoff := now.AddDate(0, 0, -ArchiveAfterDays)

	names, err := sp.onDiskSkillNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		rec := usage[name]
		fresh := rec == UsageRecord{}
		if fresh {
			rec = UsageRecord{CreatedAt: now.Format(time.RFC3339), State: StateActive}
		}
		if rec.State == "" {
			// A seeded record without a phase rides as active - the
			// staleness ladder's entry state (TouchUsage normalizes
			// the same way).
			rec.State = StateActive
		}
		anchor := anchorTime(rec, now)
		// never-used: absence of evidence is not staleness (Hermes);
		// only usage or an old-enough creation anchors the clock.
		if rec.UseCount+rec.PatchCount == 0 && anchor.After(staleCutoff) {
			usage[name] = rec
			touched = true
			continue
		}
		if !anchor.After(archiveCutoff) && rec.State != StateArchived && !rec.Pinned {
			id, err := sp.archiveOne(name, rec, usage)
			if err != nil {
				return err
			}
			rep.LedgerIDs = append(rep.LedgerIDs, id)
			touched = true
			rep.Archived++
			continue
		}
		if !anchor.After(staleCutoff) && rec.State == StateActive && !rec.Pinned {
			rec.State = StateStale
			usage[name] = rec
			touched = true
			rep.MarkedStale++
			continue
		}
		if anchor.After(staleCutoff) && rec.State == StateStale {
			rec.State = StateActive
			usage[name] = rec
			touched = true
			rep.Reactivated++
			continue
		}
		usage[name] = rec
		touched = true
	}
	if touched {
		writeUsageLocked(sp.Dir, usage)
	}
	return nil
}

// anchorTime returns the staleness anchor: the freshest real activity,
// created_at when never used. A missing timestamp collapses to now -
// an unparseable anchor must not instantly archive anything.
func anchorTime(rec UsageRecord, now time.Time) time.Time {
	for _, s := range []string{rec.LastUsed, rec.CreatedAt} {
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return now
}

// onDiskSkillNames lists the skill folders in the write home, sorted.
func (sp *SkillsProvider) onDiskSkillNames() ([]string, error) {
	entries, err := os.ReadDir(sp.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan skills dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// archiveOne moves <dir>/<name> to .archive/<name> with backup +
// ledger, inside the usage lock (the deterministic pass holds it).
// The usage record the caller carries is already archived-state.
func (sp *SkillsProvider) archiveOne(name string, rec UsageRecord, usage map[string]UsageRecord) (string, error) {
	skillFile := filepath.Join(sp.Dir, name, "SKILL.md")
	before := ""
	if b, err := os.ReadFile(skillFile); err == nil {
		before = string(b)
	}
	if _, err := sp.BackupSkill(name, "archive"); err != nil {
		return "", err
	}
	arch, err := sp.archiveDir()
	if err != nil {
		return "", err
	}
	if err := moveDir(filepath.Join(sp.Dir, name), filepath.Join(arch, name)); err != nil {
		return "", fmt.Errorf("archive %s: %w", name, err)
	}
	rec.State = StateArchived
	rec.ArchivedAt = time.Now().UTC().Format(time.RFC3339)
	usage[name] = rec
	id, err := sp.AppendLedger(LedgerArchive, name, "stale-only skill: no activity in "+fmt.Sprint(ArchiveAfterDays)+" days",
		[]LedgerFile{{Path: skillFile, Before: before, After: ""}}, nil)
	if err != nil {
		return "", err
	}
	// The pass-level report reads rep.Archived; the id rides the ledger.
	_ = id
	return id, nil
}

// mergeProposal is one LLM-proposed consolidation.
type mergeProposal struct {
	Source string `json:"source"`
	Into   string `json:"into"`
	Reason string `json:"reason"`
}

// distillProposal is one LLM-proposed oversized body.
type distillProposal struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// proposalSet is the LLM's one-call answer.
type proposalSet struct {
	Merges   []mergeProposal   `json:"merges"`
	Distills []distillProposal `json:"distills"`
}

// proposeMutations runs the single proposal call: skill inventory in,
// JSON proposals out. The candidates are NOT applied here - they are
// validated and re-verified per skill in the apply steps.
func (sp *SkillsProvider) proposeMutations(ctx context.Context, prov agent.Provider) ([]mergeProposal, []distillProposal, string, error) {
	usage := readUsage(sp.Dir)
	var sb strings.Builder
	sb.WriteString("You are the skills curator for an AI agent. Below is the skill inventory. Propose consolidations.\n\n")
	sb.WriteString("Rules:\n")
	sb.WriteString("- Only propose skills with author=agent (agent-created class). Hand-written skills are off limits.\n")
	sb.WriteString("- A merge ABSORBS a narrow skill into a broader umbrella: content becomes rules inside the umbrella; the source is archived. Only merge skills that genuinely cover one job.\n")
	sb.WriteString("- A distill candidate is a skill whose body exceeds 24000 chars; the pass will ask you to rewrite it tighter, later.\n")
	sb.WriteString("- Never propose pinning, deleting, or skills that are pinned.\n")
	sb.WriteString(`- Answer with ONLY a JSON object: {"merges":[{"source":"name","into":"umbrella-name","reason":"one sentence"}],"distills":[{"name":"name","reason":"one sentence"}]}. Empty lists are a valid answer.` + "\n\n")
	sb.WriteString("Inventory (name | author | state | last_used | uses | body_chars | description):\n")
	names, _ := sp.onDiskSkillNames()
	for _, n := range names {
		s, err := loadSkill(filepath.Join(sp.Dir, n, "SKILL.md"))
		if err != nil {
			continue
		}
		author := s.Author
		if author == "" {
			author = "hand"
		}
		rec := usage[n]
		last := rec.LastUsed
		if last == "" {
			last = rec.CreatedAt
		}
		if last == "" {
			last = "never"
		}
		fmt.Fprintf(&sb, "- %s | %s | %s | %s | %d | %d | %s\n",
			n, author, stateOrActive(rec, n), last, rec.UseCount+rec.PatchCount, len(s.Content), oneLine(s.Description, 120))
	}
	out, err := complete(ctx, prov, sb.String())
	if err != nil {
		return nil, nil, out, err
	}
	var set proposalSet
	if err := json.Unmarshal([]byte(extractJSON(out)), &set); err != nil {
		return nil, nil, out, fmt.Errorf("curator proposals: not JSON: %w", err)
	}
	return set.Merges, set.Distills, out, nil
}

func stateOrActive(rec UsageRecord, _ string) string {
	if rec.State == "" {
		return StateActive
	}
	return rec.State
}

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > max {
		s = s[:max]
	}
	return s
}

// applyMerge validates one merge candidate and applies it: distilled
// section drafted by the LLM, appended to the umbrella body; the
// source is backed up and archived. One ledger row covers both files.
func (sp *SkillsProvider) applyMerge(ctx context.Context, prov agent.Provider, m mergeProposal) (string, error) {
	m.Source, m.Into = strings.TrimSpace(m.Source), strings.TrimSpace(m.Into)
	if m.Source == "" || m.Into == "" || m.Source == m.Into {
		return "", fmt.Errorf("empty or self-merge proposal")
	}
	if reason := m.Reason; len(reason) > 500 {
		m.Reason = reason[:500]
	}
	srcFile, err := sp.agentAuthoredSkillFile(m.Source)
	if err != nil {
		return "", err
	}
	tgtFile, err := sp.agentAuthoredSkillFile(m.Into)
	if err != nil {
		return "", err
	}
	if sp.isPinned(m.Source) || sp.isPinned(m.Into) {
		return "", fmt.Errorf("pinned skill is untouchable")
	}
	srcRaw, _ := os.ReadFile(srcFile)
	tgtRaw, _ := os.ReadFile(tgtFile)
	section, err := sp.draftAbsorption(ctx, prov, m.Into, string(tgtRaw), m.Source, string(srcRaw))
	if err != nil {
		return "", err
	}
	title, body := section.title, section.body
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("absorption draft was empty")
	}
	patched := mergeIntoBody(string(tgtRaw), title, m.Source, body)
	if _, err := loadSkillFromContent(m.Into, patched); err != nil {
		return "", fmt.Errorf("merge draft failed the loader's parse: %w", err)
	}
	if _, err := sp.BackupSkill(m.Source, "merge-source"); err != nil {
		return "", err
	}
	if _, err := sp.BackupSkill(m.Into, "merge-target"); err != nil {
		return "", err
	}
	arch, err := sp.archiveDir()
	if err != nil {
		return "", err
	}
	// Apply in write-order: umbrella first (the source must still
	// exist on disk for the archive ledger's before content, and the
	// umbrella write is atomic), then the move.
	if err := os.WriteFile(tgtFile, []byte(patched), 0o644); err != nil {
		return "", fmt.Errorf("write umbrella: %w", err)
	}
	if err := moveDir(filepath.Join(sp.Dir, m.Source), filepath.Join(arch, m.Source)); err != nil {
		return "", fmt.Errorf("archive source: %w", err)
	}
	// Usage: source retired, target gains the patches.
	usageMu.Lock()
	usage := readUsageLocked(sp.Dir)
	rec := usage[m.Source]
	rec.State = StateArchived
	rec.ArchivedAt = time.Now().UTC().Format(time.RFC3339)
	usage[m.Source] = rec
	t := usage[m.Into]
	t.PatchCount++
	t.LastUsed = time.Now().UTC().Format(time.RFC3339)
	if t.State == StateStale || t.State == "" {
		t.State = StateActive
	}
	usage[m.Into] = t
	writeUsageLocked(sp.Dir, usage)
	usageMu.Unlock()
	id, err := sp.AppendLedger(LedgerMerge, m.Source, m.Reason,
		[]LedgerFile{
			{Path: tgtFile, Before: string(tgtRaw), After: patched},
			{Path: srcFile, Before: string(srcRaw), After: ""},
		},
		map[string]any{"absorbed_into": m.Into})
	return id, err
}

// applyDistill validates one distill candidate (over the threshold
// only) and applies it: the LLM drafts a tighter body, frontmatter is
// kept byte-for-byte, backup + ledger ride along.
func (sp *SkillsProvider) applyDistill(ctx context.Context, prov agent.Provider, d distillProposal) (string, error) {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return "", fmt.Errorf("empty distill proposal")
	}
	reason := d.Reason
	if len(reason) > 500 {
		reason = reason[:500]
	}
	skillFile, err := sp.agentAuthoredSkillFile(name)
	if err != nil {
		return "", err
	}
	if sp.isPinned(name) {
		return "", fmt.Errorf("pinned skill is untouchable")
	}
	raw, _ := os.ReadFile(skillFile)
	content := string(raw)
	front, body := splitFrontmatter(content)
	if len([]rune(body)) <= DistillMinChars {
		return "", fmt.Errorf("body is %d chars, at or under the %d-char floor - distill refused", len([]rune(body)), DistillMinChars)
	}
	distilled, err := sp.draftDistill(ctx, prov, name, body)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(distilled) == "" {
		return "", fmt.Errorf("distill draft was empty")
	}
	if len([]rune(distilled)) >= len([]rune(body)) {
		return "", fmt.Errorf("distill draft (%d) did not shrink the body (%d) - refused", len([]rune(distilled)), len([]rune(body)))
	}
	patched := front + distilled
	if _, err := loadSkillFromContent(name, patched); err != nil {
		return "", fmt.Errorf("distill draft failed the loader's parse: %w", err)
	}
	if _, err := sp.BackupSkill(name, "distill"); err != nil {
		return "", err
	}
	if err := os.WriteFile(skillFile, []byte(patched), 0o644); err != nil {
		return "", fmt.Errorf("write distilled skill: %w", err)
	}
	usageMu.Lock()
	usage := readUsageLocked(sp.Dir)
	rec := usage[name]
	rec.PatchCount++
	rec.LastUsed = time.Now().UTC().Format(time.RFC3339)
	usage[name] = rec
	writeUsageLocked(sp.Dir, usage)
	usageMu.Unlock()
	return sp.AppendLedger(LedgerDistill, name, reason,
		[]LedgerFile{{Path: skillFile, Before: content, After: patched}},
		map[string]any{"before_chars": len([]rune(body)), "after_chars": len([]rune(distilled))})
}

// agentAuthoredSkillFile resolves a SKILL.md and refuses any skill
// outside the agent-created class (the provenance rule the LLM passes
// enforce; the LLM could mislabel, so code re-checks on every apply).
func (sp *SkillsProvider) agentAuthoredSkillFile(name string) (string, error) {
	if !validSkillName(name) {
		return "", fmt.Errorf("invalid skill name %q", name)
	}
	dir := filepath.Join(sp.Dir, name)
	f := filepath.Join(dir, "SKILL.md")
	s, err := loadSkill(f)
	if err != nil {
		return "", fmt.Errorf("skill %q: %w", name, err)
	}
	if s.Author != "agent" {
		return "", fmt.Errorf("skill %q is %q-authored - the curator only rewrites agent-created skills (author: agent)", name, orHand(s.Author))
	}
	return f, nil
}

func orHand(author string) string {
	if author == "" {
		return "hand"
	}
	return author
}

func (sp *SkillsProvider) isPinned(name string) bool {
	usageMu.Lock()
	defer usageMu.Unlock()
	return readUsageLocked(sp.Dir)[name].Pinned
}

// mergeIntoBody appends one absorbed section to the umbrella body.
func mergeIntoBody(content, title, source, body string) string {
	if title == "" {
		title = source
	}
	sep := ""
	if !strings.HasSuffix(content, "\n") {
		sep = "\n"
	}
	return content + sep + "\n## " + title + " (absorbed from " + source + ")\n\n" + strings.TrimRight(body, "\n") + "\n"
}

// splitFrontmatter divides a SKILL.md into (frontmatter-with-fences,
// body). No frontmatter = ("", content).
func splitFrontmatter(content string) (front, body string) {
	lines := strings.SplitAfterN(content, "\n", -1)
	if len(lines) == 0 || strings.TrimRight(lines[0], "\n\r") != "---" {
		return "", content
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\n\r") == "---" {
			return strings.Join(lines[:i+1], ""), strings.Join(lines[i+1:], "")
		}
	}
	return "", content
}

// loadSkillFromContent parses a composed SKILL.md the way loadSkill
// parses a file (the shape gate for curator-composed content).
func loadSkillFromContent(name, content string) (Skill, error) {
	tmp, err := os.CreateTemp("", name+"-*.md")
	if err != nil {
		return Skill{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return Skill{}, err
	}
	tmp.Close()
	s, err := loadSkill(tmp.Name())
	s.Name = name
	return s, err
}

// moveDir renames a folder, falling back to copy+delete across
// filesystems.
func moveDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyDir(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}
