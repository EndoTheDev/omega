package skills

import (
	"github.com/EndoTheDev/omega/agent"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// writeAgentSkill creates an agent-authored skill folder by hand (so
// tests control the bytes exactly, no tool-call ceremony).
func writeAgentSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ndescription: test skill " + name + "\nauthor: agent\n---\n" + body
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeHandSkill is the hand-written twin: no author stamp - the
// curator's LLM passes must refuse it.
func writeHandSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ndescription: hand skill\n---\n" + body
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// daysAgo is an RFC3339 stamp N days ago.
func daysAgo(n int) string {
	return time.Now().UTC().Add(-time.Duration(n) * 24 * time.Hour).Format(time.RFC3339)
}

// writeUsageAt seeds the usage state file with exact records.
func writeUsageAt(dir string, m map[string]UsageRecord) error {
	if err := os.MkdirAll(filepath.Join(dir, ".curator"), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(usageFile(dir), data, 0o644)
}

// chat is one scripted completion: a text answer.
func chat(text string) []core.ProviderStreamEvent {
	return []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response", Content: text},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
}

// TestCuratorStalenessArchives: the deterministic pass - a 40d-idle
// skill lands in .archive with backup + ledger row; 20d-idle skills
// are only marked stale (never-used ones included: absence of usage
// inside the stale window still ages them out); fresh skills stay.
func TestCuratorStalenessArchives(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	writeAgentSkill(t, dir, "ancient", "old body")
	writeAgentSkill(t, dir, "mid", "mid body")
	writeAgentSkill(t, dir, "brandnew", "unused body")
	writeAgentSkill(t, dir, "newborn", "unused body")
	if err := writeUsageAt(dir, map[string]UsageRecord{
		"ancient":  {CreatedAt: daysAgo(100), LastUsed: daysAgo(40), UseCount: 5},
		"mid":      {CreatedAt: daysAgo(100), LastUsed: daysAgo(20), UseCount: 2},
		"brandnew": {CreatedAt: daysAgo(3)},
		"newborn":  {CreatedAt: daysAgo(20)},
	}); err != nil {
		t.Fatal(err)
	}

	rep, err := sp.RunCurator(context.Background(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Archived != 1 {
		t.Fatalf("archived %d, want 1 (report %+v)", rep.Archived, rep)
	}
	if rep.MarkedStale != 2 {
		t.Fatalf("marked stale %d, want 2 (report %+v)", rep.MarkedStale, rep)
	}
	// archive: the folder moved, the backup exists, the ledger row exists.
	if _, err := os.Stat(filepath.Join(dir, "ancient", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("ancient skill still on disk after archive")
	}
	if _, err := os.Stat(filepath.Join(dir, ".archive", "ancient", "SKILL.md")); err != nil {
		t.Fatalf("archived skill missing: %v", err)
	}
	entries, err := sp.readLedger()
	if err != nil || len(entries) != 1 || entries[0].Action != LedgerArchive || entries[0].Skill != "ancient" {
		t.Fatalf("ledger after pass: %+v (err %v)", entries, err)
	}
	if entries[0].Files[0].Before == "" {
		t.Fatal("archive ledger row carries no before content")
	}
	backups, _ := sp.backupDir()
	bs, _ := os.ReadDir(backups)
	if len(bs) != 1 || !strings.Contains(bs[0].Name(), "archive-ancient") {
		t.Fatalf("backup dir after archive: %v (%s)", bs, backups)
	}
	m := readUsage(dir)
	if m["ancient"].State != StateArchived || m["ancient"].ArchivedAt == "" {
		t.Fatalf("ancient record not archived: %+v", m["ancient"])
	}
	if m["mid"].State != StateStale || m["newborn"].State != StateStale {
		t.Fatalf("stale marks wrong: mid=%+v newborn=%+v", m["mid"], m["newborn"])
	}
	if _, err := os.Stat(filepath.Join(dir, "brandnew", "SKILL.md")); err != nil {
		t.Fatalf("brandnew vanished: %v", err)
	}
}

// TestCuratorStaleListSurfaces: StaleSkillNames reflects the state file.
func TestCuratorStaleListSurfaces(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	writeAgentSkill(t, dir, "sleepy", "body")
	if err := writeUsageAt(dir, map[string]UsageRecord{
		"sleepy": {CreatedAt: "2026-01-01T00:00:00Z", State: StateStale},
	}); err != nil {
		t.Fatal(err)
	}
	got := sp.StaleSkillNames()
	if len(got) != 1 || got[0] != "sleepy" {
		t.Fatalf("stale list: %v", got)
	}
}

// TestCuratorMergeByteExactRollback: an LLM merge proposal absorbs a
// source into an umbrella - the target gains the section, the source
// is archived, one ledger row covers both files; the rollback
// restores both byte-exact and un-archives the source.
func TestCuratorMergeByteExactRollback(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	writeAgentSkill(t, dir, "umbrella", "# umbrella\n\nexisting rules")
	writeAgentSkill(t, dir, "narrow", "narrow insight one")
	srcBefore, _ := os.ReadFile(filepath.Join(dir, "narrow", "SKILL.md"))
	tgtBefore, _ := os.ReadFile(filepath.Join(dir, "umbrella", "SKILL.md"))

	prov := agent.NewFakeProviderScripts("glm-test",
		chat(`{"merges":[{"source":"narrow","into":"umbrella","reason":"same job, one umbrella"}],"distills":[]}`),
		chat(`{"title":"Narrow rules","body":"- rule one\n- rule two"}`),
	)
	rep, err := sp.RunCurator(context.Background(), prov, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Merges != 1 {
		t.Fatalf("merges %d, want 1 (%+v)", rep.Merges, rep)
	}
	tgtAfter, _ := os.ReadFile(filepath.Join(dir, "umbrella", "SKILL.md"))
	if !strings.Contains(string(tgtAfter), "(absorbed from narrow)") {
		t.Fatalf("umbrella lacks the absorbed section:\n%s", tgtAfter)
	}
	if _, err := os.Stat(filepath.Join(dir, "narrow", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("source still on disk after merge")
	}
	if _, err := os.Stat(filepath.Join(dir, ".archive", "narrow", "SKILL.md")); err != nil {
		t.Fatal("source missing from .archive after merge")
	}
	entries, _ := sp.readLedger()
	if len(entries) != 1 || entries[0].Action != LedgerMerge || entries[0].Skill != "narrow" {
		t.Fatalf("ledger: %+v", entries)
	}
	if entries[0].Evidence["absorbed_into"] != "umbrella" {
		t.Fatalf("merge row lacks absorbed_into: %+v", entries[0].Evidence)
	}

	out, err := sp.Rollback(entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "restored") {
		t.Fatalf("rollback report: %q", out)
	}
	tgtNow, _ := os.ReadFile(filepath.Join(dir, "umbrella", "SKILL.md"))
	srcNow, _ := os.ReadFile(filepath.Join(dir, "narrow", "SKILL.md"))
	if string(tgtNow) != string(tgtBefore) {
		t.Fatal("umbrella rollback is not byte-exact")
	}
	if string(srcNow) != string(srcBefore) {
		t.Fatal("source rollback is not byte-exact")
	}
	if _, err := os.Stat(filepath.Join(dir, ".archive", "narrow")); !os.IsNotExist(err) {
		t.Fatal("archived copy survived the rollback")
	}
	entries, _ = sp.readLedger()
	if len(entries) != 2 || entries[1].Action != LedgerRollback {
		t.Fatalf("rollback ledger row missing: %+v", entries)
	}
}

// TestCuratorArchiveRollbackByteExact: the staleness archive undoes
// through the same ledger row.
func TestCuratorArchiveRollbackByteExact(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	writeAgentSkill(t, dir, "oldone", "precise original body")
	before, _ := os.ReadFile(filepath.Join(dir, "oldone", "SKILL.md"))
	if err := writeUsageAt(dir, map[string]UsageRecord{
		"oldone": {CreatedAt: daysAgo(100), LastUsed: daysAgo(40), UseCount: 1},
	}); err != nil {
		t.Fatal(err)
	}
	rep, err := sp.RunCurator(context.Background(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Archived != 1 {
		t.Fatalf("archive pass: %+v", rep)
	}
	entries, _ := sp.readLedger()
	if _, err := sp.Rollback(entries[0].ID); err != nil {
		t.Fatal(err)
	}
	now, _ := os.ReadFile(filepath.Join(dir, "oldone", "SKILL.md"))
	if string(now) != string(before) {
		t.Fatal("archive rollback is not byte-exact")
	}
	if _, err := os.Stat(filepath.Join(dir, ".archive", "oldone")); !os.IsNotExist(err) {
		t.Fatal("archived copy survived rollback")
	}
	m := readUsage(dir)
	if m["oldone"].State == StateArchived {
		t.Fatal("usage record still archived after rollback")
	}
}

// TestCuratorRefusesHandAuthoredMerge: the LLM proposes it, the code
// refuses - agent-created class only.
func TestCuratorRefusesHandAuthoredMerge(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	writeHandSkill(t, dir, "handone", "hand body")
	writeAgentSkill(t, dir, "umbrella2", "body")
	prov := agent.NewFakeProvider("glm-test", chat(`{"merges":[{"source":"handone","into":"umbrella2","reason":"x"}]}`)...)
	rep, err := sp.RunCurator(context.Background(), prov, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Merges != 0 || len(rep.Skipped) == 0 {
		t.Fatalf("hand-authored merge was not skipped: %+v", rep)
	}
	if body, _ := os.ReadFile(filepath.Join(dir, "handone", "SKILL.md")); !strings.Contains(string(body), "hand body") {
		t.Fatal("hand-authored skill was mutated")
	}
	entries, _ := sp.readLedger()
	if len(entries) != 0 {
		t.Fatalf("refused merge left ledger rows: %+v", entries)
	}
}

// TestCuratorDistillOnlyOverThreshold: distill applies only to bodies
// past the 24k floor; the under-floor candidate is refused in code,
// the over-floor one is applied with frontmatter intact and rolls
// back byte-exact.
func TestCuratorDistillOnlyOverThreshold(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	big := strings.Repeat("- procedural step with detail\n", 1200) // ~30k
	writeAgentSkill(t, dir, "bigskill", big)
	writeAgentSkill(t, dir, "smallskill", "tiny body")
	bigBefore, _ := os.ReadFile(filepath.Join(dir, "bigskill", "SKILL.md"))

	prov := agent.NewFakeProviderScripts("glm-test",
		chat(`{"merges":[],"distills":[{"name":"bigskill","reason":"oversize"},{"name":"smallskill","reason":"oversize?"}]}`),
		chat("Concise procedures rewritten tighter with no narration."),
	)
	rep, err := sp.RunCurator(context.Background(), prov, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Distills != 1 {
		t.Fatalf("distills %d, want 1 (%+v)", rep.Distills, rep)
	}
	if len(rep.Skipped) == 0 || !strings.Contains(strings.Join(rep.Skipped, ";"), "smallskill") {
		t.Fatalf("under-floor distill was not refused: %+v", rep)
	}
	smallAfter, _ := os.ReadFile(filepath.Join(dir, "smallskill", "SKILL.md"))
	if string(smallAfter) != "---\ndescription: test skill smallskill\nauthor: agent\n---\ntiny body" {
		t.Fatal("small skill mutated")
	}
	bigAfter, _ := os.ReadFile(filepath.Join(dir, "bigskill", "SKILL.md"))
	if !strings.HasPrefix(string(bigAfter), "---\ndescription: test skill bigskill\nauthor: agent\n---\n") {
		t.Fatal("frontmatter lost in distill")
	}
	if len(bigAfter) >= len(bigBefore) {
		t.Fatal("distill did not shrink the file")
	}
	entries, _ := sp.readLedger()
	if len(entries) != 1 || entries[0].Action != LedgerDistill {
		t.Fatalf("distill ledger: %+v", entries)
	}
	if _, err := sp.Rollback(entries[0].ID); err != nil {
		t.Fatal(err)
	}
	now, _ := os.ReadFile(filepath.Join(dir, "bigskill", "SKILL.md"))
	if string(now) != string(bigBefore) {
		t.Fatal("distill rollback is not byte-exact")
	}
}

// TestCuratorModelOverride: the configured model wins for the
// consult (WithModel copy, receiver untouched).
func TestCuratorModelOverride(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	writeAgentSkill(t, dir, "solo", "body")
	prov := agent.NewFakeProvider("glm-test", chat(`{}`)...)
	if _, err := sp.RunCurator(context.Background(), prov, "curator-special"); err != nil {
		t.Fatal(err)
	}
	if prov.ModelName() != "glm-test" {
		t.Fatal("provider model mutated by the override")
	}
}

// TestCuratorBackupKeepFive: the sidecar backup root prunes to 5.
func TestCuratorBackupKeepFive(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	writeAgentSkill(t, dir, "backed", "body")
	for i := range 7 {
		if i < 6 {
			root, err := sp.backupDir()
			if err != nil {
				t.Fatal(err)
			}
			name := "20250101-00000" + string(rune('0'+i)) + "-archive-oldbackfill"
			if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := sp.BackupSkill("backed", "archive"); err != nil {
			t.Fatal(err)
		}
	}
	root, _ := sp.backupDir()
	entries, _ := os.ReadDir(root)
	if len(entries) != 5 {
		t.Fatalf("backups kept %d, want 5", len(entries))
	}
}

// TestRunCuratorNilProviderStillStaleRun: no provider - the
// deterministic pass still runs and the report says what was skipped.
func TestRunCuratorNilProviderStillStaleRun(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	writeAgentSkill(t, dir, "staleonly", "body")
	if err := writeUsageAt(dir, map[string]UsageRecord{
		"staleonly": {CreatedAt: daysAgo(100), LastUsed: daysAgo(40), UseCount: 3},
	}); err != nil {
		t.Fatal(err)
	}
	rep, err := sp.RunCurator(context.Background(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Archived != 1 || !strings.Contains(strings.Join(rep.Skipped, ";"), "no provider") {
		t.Fatalf("nil-provider pass wrong: %+v", rep)
	}
}
