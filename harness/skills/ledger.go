package skills

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Curator audit ledger (card t_9972dfd0, ported from Hermes
// tools/skill_ledger.py): every curator mutation appends one row with
// the before/after of the primary artifact (SKILL.md content), so a
// single row is a single-edit revert. Full-folder copies ride the
// sidecar backups (keep 5) - the ledger stays small, the restores stay
// byte-exact. Rows are JSON lines at <skillsdir>/.curator_ledger.jsonl
// (the Hermes path shape, inside the skills home).

// Ledger actions.
const (
	LedgerArchive  = "archive"
	LedgerMerge    = "merge"
	LedgerDistill  = "distill"
	LedgerRollback = "rollback"
)

// LedgerEntry is one audit row. Files carries one entry per touched
// SKILL.md: before = the content before the edit, after = the content
// after ("" for an archive's removed file). FromPath/ToPath record an
// archive/undo move so rollback knows where the folder went.
type LedgerEntry struct {
	ID     string `json:"id"`
	TS     string `json:"ts"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Skill  string `json:"skill"`
	// Evidence is the why (the LLM's one-line reason) plus any
	// structured provenance the pass recorded.
	Evidence map[string]any `json:"evidence,omitempty"`
	Files    []LedgerFile   `json:"files"`
}

// LedgerFile is one touched SKILL.md with its before/after bytes.
type LedgerFile struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}

const (
	maxLedgerEntries = 200
	backupKeep       = 5
)

// ledgerFile returns the ledger path for the provider's write home.
func (sp *SkillsProvider) ledgerFile() (string, error) {
	if sp.Dir == "" {
		return "", fmt.Errorf("skills directory not configured")
	}
	return filepath.Join(sp.Dir, ".curator_ledger.jsonl"), nil
}

// ledgerMu serializes ledger appends and rollbacks - the file is the
// audit record; a torn line is worse than a wait.
var ledgerMu sync.Mutex

// readLedger returns every ledger row, oldest first. A missing file
// is an empty history.
func (sp *SkillsProvider) readLedger() ([]LedgerEntry, error) {
	path, err := sp.ledgerFile()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read curator ledger: %w", err)
	}
	var out []LedgerEntry
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e LedgerEntry
		if json.Unmarshal([]byte(line), &e) == nil && e.ID != "" {
			out = append(out, e)
		}
	}
	return out, nil
}

// AppendLedger appends one audit row and returns its id. The before
// contents are snapped from disk by the caller; this function only
// records. Errors surface - the caller must not mutate anything when
// the ledger refuses (the audit row IS the mutation's receipt).
func (sp *SkillsProvider) AppendLedger(action, skill, reason string, files []LedgerFile, extra map[string]any) (string, error) {
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	entries, err := sp.readLedger()
	if err != nil {
		return "", err
	}
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("ledger id: %w", err)
	}
	id := hex.EncodeToString(raw[:])
	e := LedgerEntry{
		ID:       id,
		TS:       time.Now().UTC().Format(time.RFC3339),
		Actor:    "curator",
		Action:   action,
		Skill:    skill,
		Evidence: map[string]any{"reason": reason},
		Files:    files,
	}
	for k, v := range extra {
		e.Evidence[k] = v
	}
	entries = append(entries, e)
	if len(entries) > maxLedgerEntries {
		entries = entries[len(entries)-maxLedgerEntries:]
	}
	return id, writeLedger(sp, entries)
}

func writeLedger(sp *SkillsProvider, entries []LedgerEntry) error {
	path, err := sp.ledgerFile()
	if err != nil {
		return err
	}
	var sb strings.Builder
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("ledger encode: %w", err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// backupDir returns the sidecar backup root: <skillsdir>/.curator_backups.
func (sp *SkillsProvider) backupDir() (string, error) {
	if sp.Dir == "" {
		return "", fmt.Errorf("skills directory not configured")
	}
	return filepath.Join(sp.Dir, ".curator_backups"), nil
}

// archiveDir returns the retired-skills root: <skillsdir>/.archive
// (the Hermes path shape). The scanner skips dot dirs, so archived
// skills leave discovery but stay one mv away from recovery.
func (sp *SkillsProvider) archiveDir() (string, error) {
	if sp.Dir == "" {
		return "", fmt.Errorf("skills directory not configured")
	}
	return filepath.Join(sp.Dir, ".archive"), nil
}

// BackupSkill copies a skill's whole folder (SKILL.md plus any
// references/scripts) into .curator_backups/<ts>-<action>-<skill>/ and
// prunes the oldest copies past backupKeep. Returns the backup path.
func (sp *SkillsProvider) BackupSkill(name, action string) (string, error) {
	root, err := sp.backupDir()
	if err != nil {
		return "", err
	}
	src := filepath.Join(sp.Dir, name)
	dst := filepath.Join(root, fmt.Sprintf("%s-%s-%s", time.Now().UTC().Format("20060102-150405"), action, name))
	if err := copyDir(src, dst); err != nil {
		return "", fmt.Errorf("backup skill %s: %w", name, err)
	}
	sp.pruneBackups(root)
	return dst, nil
}

// pruneBackups keeps the newest backupKeep backup dirs, oldest first out.
func (sp *SkillsProvider) pruneBackups(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) <= backupKeep {
		return
	}
	sort.Strings(names) // timestamped names sort chronologically
	for _, n := range names[:len(names)-backupKeep] {
		_ = os.RemoveAll(filepath.Join(root, n))
	}
}

// copyDir is a plain recursive copy (small trees: skills folders).
func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(s)
		if err != nil {
			return err
		}
		if err := os.WriteFile(d, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Rollback reverts one ledger row: the single-edit revert (card law).
// A merge/distill restores every recorded file's before bytes; an
// archive moves the folder back from .archive (and restores SKILL.md
// bytes exactly as recorded). The rollback itself is ledgered, so a
// reverted curator pass leaves two rows, not none. Returns a report.
func (sp *SkillsProvider) Rollback(id string) (string, error) {
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	entries, err := sp.readLedger()
	if err != nil {
		return "", err
	}
	var target *LedgerEntry
	for i := range entries {
		if entries[i].ID == id {
			target = &entries[i]
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("rollback: no ledger entry %q", id)
	}
	if target.Action == LedgerRollback {
		return "", fmt.Errorf("rollback: entry %s is already a rollback", id)
	}
	var report strings.Builder
	for _, f := range target.Files {
		// Refuse when the file changed since the row: the single-edit
		// restore would clobber newer work.
		cur, rerr := os.ReadFile(f.Path)
		if rerr == nil && string(cur) != f.After {
			return "", fmt.Errorf("rollback: %s changed since entry %s - restore it by hand", f.Path, id)
		}
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
			return "", fmt.Errorf("rollback: %w", err)
		}
		switch target.Action {
		case LedgerArchive:
			// The folder moved to .archive; every recorded file goes
			// back to its before bytes and the archived copy goes away.
			if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
				return "", fmt.Errorf("rollback: %w", err)
			}
			if err := os.WriteFile(f.Path, []byte(f.Before), 0o644); err != nil {
				return "", fmt.Errorf("rollback: %w", err)
			}
			report.WriteString("restored " + f.Path + "\n")
		case LedgerMerge, LedgerDistill:
			if err := os.WriteFile(f.Path, []byte(f.Before), 0o644); err != nil {
				return "", fmt.Errorf("rollback: %w", err)
			}
			report.WriteString("restored " + f.Path + "\n")
		}
	}
	// Merge rollbacks un-archive too: the source moved to .archive as
	// part of the absorb, so the undo takes the folder back the same
	// way an archive undo does.
	if target.Action == LedgerArchive || target.Action == LedgerMerge {
		arch, err := sp.archiveDir()
		if err == nil {
			_ = os.RemoveAll(filepath.Join(arch, target.Skill))
		}
	}
	// Reactivate the record the archive pass retired.
	if target.Action == LedgerArchive {
		usageMu.Lock()
		m := readUsageLocked(sp.Dir)
		rec := m[target.Skill]
		rec.State = StateActive
		rec.ArchivedAt = ""
		m[target.Skill] = rec
		writeUsageLocked(sp.Dir, m)
		usageMu.Unlock()
	}
	// Archive-folder rollback: the archived folder vanishes above, so
	// the ledger row's undo is complete. Merge/distill leave both files
	// restored from `before` - byte-exact by construction.
	undo := LedgerEntry{
		ID:     randID(),
		TS:     time.Now().UTC().Format(time.RFC3339),
		Actor:  "curator",
		Action: LedgerRollback,
		Skill:  target.Skill,
		Evidence: map[string]any{
			"reason":  fmt.Sprintf("rollback of %s (%s)", id, target.Action),
			"reverts": id,
		},
		Files: target.Files,
	}
	entries = append(entries, undo)
	if len(entries) > maxLedgerEntries {
		entries = entries[len(entries)-maxLedgerEntries:]
	}
	if err := writeLedger(sp, entries); err != nil {
		return "", err
	}
	return report.String(), nil
}

// randID is the shared hex id helper (ledger rows and rollback rows).
func randID() string {
	var raw [6]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}
