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
// byte-exact. Rows are JSON lines at <skillsdir>/.curator/ledger.jsonl:
// every non-skill file the curator owns (usage.json, ledger.jsonl,
// backups/) lives under the one ignored .curator dir, so a dev
// checkout sees no untracked skills noise (finding C2, t_a48be895).

// Ledger actions.
const (
	LedgerArchive  = "archive"
	LedgerMerge    = "merge"
	LedgerDistill  = "distill"
	LedgerRemove   = "remove"
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

// Curator state paths, all under <skillsdir>/.curator (the one ignored
// directory beside the skills themselves). The legacy names are the
// pre-t_a48be895 layout, which sat BESIDE .curator and leaked untracked
// files around its own ignore rule.
const (
	curatorDirName    = ".curator"
	ledgerFileName    = "ledger.jsonl"
	backupsDirName    = "backups"
	legacyLedgerName  = ".curator_ledger.jsonl"
	legacyBackupsName = ".curator_backups"
)

// curatorRoot returns the curator's state root (<skillsdir>/.curator)
// and creates it. Creating it is deliberate: the ledger and the backup
// root both need the parent, the migration below needs a rename
// target, and an empty dot dir is invisible to the scanner (dot dirs
// are skipped), so nothing about discovery changes.
func (sp *SkillsProvider) curatorRoot() (string, error) {
	if sp.Dir == "" {
		return "", fmt.Errorf("skills directory not configured")
	}
	root := filepath.Join(sp.Dir, curatorDirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create curator dir: %w", err)
	}
	migrateLegacyCuratorState(sp.Dir, root)
	return root, nil
}

// migrateLegacyCuratorState adopts the pre-t_a48be895 layout: the
// ledger and the backup root used to sit beside .curator
// (<dir>/.curator_ledger.jsonl, <dir>/.curator_backups). Both are
// renamed in on first touch, so an existing audit history keeps its
// rollback ids and the keep-5 backup window survives the move.
//
// The call is best-effort and idempotent. A home that already holds
// the new path keeps it and the stale legacy path is LEFT on disk -
// nothing is merged, because the on-disk copy is the record; a rename
// that fails (permissions, a lock) never blocks the ledger write.
func migrateLegacyCuratorState(dir, root string) {
	move := func(old, dst string) {
		if _, err := os.Stat(dst); err == nil {
			return // the new home is populated: never overwrite it
		}
		if _, err := os.Stat(old); err != nil {
			return // nothing to adopt
		}
		_ = os.Rename(old, dst)
	}
	move(filepath.Join(dir, legacyLedgerName), filepath.Join(root, ledgerFileName))
	move(filepath.Join(dir, legacyBackupsName), filepath.Join(root, backupsDirName))
}

// ledgerFile returns the ledger path for the provider's write home:
// <skillsdir>/.curator/ledger.jsonl.
func (sp *SkillsProvider) ledgerFile() (string, error) {
	root, err := sp.curatorRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ledgerFileName), nil
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

// AppendLedger appends one audit row and returns its id. The actor is
// the writer's class (the curator's passes; ActorWebui for the tab
// editor's own writes) - the audit trail names its writer. The before
// contents are snapped from disk by the caller; this function only
// records. Errors surface - the caller must not mutate anything when
// the ledger refuses (the audit row IS the mutation's receipt).
func (sp *SkillsProvider) AppendLedger(actor, action, skill, reason string, files []LedgerFile, extra map[string]any) (string, error) {
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
		Actor:    actor,
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

// backupDir returns the sidecar backup root:
// <skillsdir>/.curator/backups (inside the ignored curator dir).
func (sp *SkillsProvider) backupDir() (string, error) {
	root, err := sp.curatorRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, backupsDirName), nil
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

// BackupSkill copies a skill folder into .curator/backups by NAME in
// the own skills dir (the curator's callers; the editor's delete backs
// up from the merge-winner's folder via backupFrom). Returns the
// backup path.
func (sp *SkillsProvider) BackupSkill(name, action string) (string, error) {
	root, err := sp.backupDir()
	if err != nil {
		return "", err
	}
	return sp.backupFrom(filepath.Join(sp.Dir, name), root, name, action)
}

// backupFrom copies one skill folder (src, the whole tree: SKILL.md
// plus references/scripts) into the backup root named <ts>-<action>-
// <skill>, pruned to the keep set. The source is the caller's resolved
// folder so a shared-dir winner is backed up from where it lives.
func (sp *SkillsProvider) backupFrom(src, root, name, action string) (string, error) {
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
// archive (or a merge's absorbed source) moves the whole folder back
// from .archive, so a multi-file skill's scripts/ and references/
// survive the undo (card t_c8f31a8c). The rollback itself is
// ledgered, so a reverted curator pass leaves two rows, not none.
// Returns a report.
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
	// Archive and merge rollbacks move the retired folder back whole
	// (card t_c8f31a8c): the ledger records SKILL.md bytes only, so
	// restoring just the recorded files would drop a multi-file
	// skill's scripts/ and references/ payloads.
	arched, aerr := sp.archiveDir()
	if aerr != nil {
		return "", aerr
	}
	moved := false
	if target.Action == LedgerArchive || target.Action == LedgerMerge {
		archived := filepath.Join(arched, target.Skill)
		live := filepath.Join(sp.Dir, target.Skill)
		if _, serr := os.Stat(archived); serr == nil {
			if _, lerr := os.Stat(live); lerr == nil {
				return "", fmt.Errorf("rollback: %s exists live and in .archive - resolve by hand", live)
			}
			// The archived copy itself must match the recorded before
			// bytes - a hand-tampered folder is not this entry's undo.
			for _, f := range target.Files {
				if f.Path != filepath.Join(sp.Dir, target.Skill, "SKILL.md") {
					continue
				}
				if b, rerr := os.ReadFile(filepath.Join(archived, "SKILL.md")); rerr == nil && string(b) != f.Before {
					return "", fmt.Errorf("rollback: %s changed since entry %s - restore it by hand", f.Path, id)
				}
			}
			if err := moveDir(archived, live); err != nil {
				return "", fmt.Errorf("rollback: move back %s: %w", target.Skill, err)
			}
			report.WriteString("restored " + live + "\n")
			moved = true
		} else if !os.IsNotExist(serr) {
			return "", fmt.Errorf("rollback: %w", serr)
		}
	}
	movedRoot := filepath.Join(sp.Dir, target.Skill) + string(os.PathSeparator)
	for _, f := range target.Files {
		if moved && strings.HasPrefix(f.Path, movedRoot) {
			// The folder came back whole; skip the recorded files it
			// carried (writing them again could clobber the payload
			// copy with a stale ledger line).
			continue
		}
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
			// Degraded fallback: the archived folder is gone, so the
			// recorded files are all that is left to restore.
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
