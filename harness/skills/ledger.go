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

// Curator audit ledger: every curator mutation appends one row with the before/after.
// Full-folder copies ride the sidecar backups (keep 5) - the ledger stays small, the
// restores stay byte-exact.

const (
	LedgerArchive  = "archive"
	LedgerMerge    = "merge"
	LedgerDistill  = "distill"
	LedgerRemove   = "remove"
	LedgerRollback = "rollback"
)

type LedgerEntry struct {
	ID     string `json:"id"`
	TS     string `json:"ts"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Skill  string `json:"skill"`
	Evidence map[string]any `json:"evidence,omitempty"`
	Files    []LedgerFile   `json:"files"`
}

type LedgerFile struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}

const (
	maxLedgerEntries = 200
	backupKeep       = 5
)

const (
	curatorDirName    = ".curator"
	ledgerFileName    = "ledger.jsonl"
	backupsDirName    = "backups"
	legacyLedgerName  = ".curator_ledger.jsonl"
	legacyBackupsName = ".curator_backups"
)

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

// migrateLegacyCuratorState adopts the pre-.curator layout. The call is best-effort and
// idempotent. A home that already holds the new path keeps it and the stale legacy path
// is LEFT on disk - nothing is merged, because the on-disk copy is the record; a rename
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

func (sp *SkillsProvider) ledgerFile() (string, error) {
	root, err := sp.curatorRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ledgerFileName), nil
}

var ledgerMu sync.Mutex

// readLedger returns every ledger row, oldest first.
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

// AppendLedger appends one audit row and returns its id. The before contents are
// snapped from disk by the caller; this function only records. Errors surface - the
// caller must not mutate anything when the ledger refuses.
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

func (sp *SkillsProvider) backupDir() (string, error) {
	root, err := sp.curatorRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, backupsDirName), nil
}

func (sp *SkillsProvider) archiveDir() (string, error) {
	if sp.Dir == "" {
		return "", fmt.Errorf("skills directory not configured")
	}
	return filepath.Join(sp.Dir, ".archive"), nil
}

func (sp *SkillsProvider) BackupSkill(name, action string) (string, error) {
	root, err := sp.backupDir()
	if err != nil {
		return "", err
	}
	return sp.backupFrom(filepath.Join(sp.Dir, name), root, name, action)
}

// backupFrom copies one skill folder (src) whole-tree. The source is the caller's
// resolved folder so a shared-dir winner is backed up from where it lives.
func (sp *SkillsProvider) backupFrom(src, root, name, action string) (string, error) {
	dst := filepath.Join(root, fmt.Sprintf("%s-%s-%s", time.Now().UTC().Format("20060102-150405"), action, name))
	if err := copyDir(src, dst); err != nil {
		return "", fmt.Errorf("backup skill %s: %w", name, err)
	}
	sp.pruneBackups(root)
	return dst, nil
}

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
			// The archived copy itself must match the recorded before bytes.
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
			// The folder came back whole; skip the recorded files it carried (writing them again
			// could clobber the payload copy with a stale ledger line).
			continue
		}
		// Refuse when the file changed since the row: the single-edit restore would clobber.
		cur, rerr := os.ReadFile(f.Path)
		if rerr == nil && string(cur) != f.After {
			return "", fmt.Errorf("rollback: %s changed since entry %s - restore it by hand", f.Path, id)
		}
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
			return "", fmt.Errorf("rollback: %w", err)
		}
		switch target.Action {
		case LedgerArchive:
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
	if target.Action == LedgerArchive || target.Action == LedgerMerge {
		arch, err := sp.archiveDir()
		if err == nil {
			_ = os.RemoveAll(filepath.Join(arch, target.Skill))
		}
	}
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
	// Merge/distill leave both files restored from `before` - byte-exact by construction.
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

func randID() string {
	var raw [6]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}
