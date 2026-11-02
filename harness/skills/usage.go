package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Usage state for the learning loop (card t_9972dfd0): one JSON file
// per skills directory - <dir>/.curator/usage.json - recording the
// activity the staleness pass anchors on. Shape follows Hermes'
// skill_usage records: created_at anchors a never-used skill so it
// cannot self-archive by sitting unread, last_used_at is the freshest
// real activity, state is the staleness phase the deterministic pass
// moved the skill to.
//
// The file lives in a dot-dir the scanner skips, so usage tracking is
// invisible to discovery. All records ride skills; hand-written and
// agent-authored skills age the same here - the AUTHOR frontmatter
// field (stamped by skills.write) is the curator's agent-created
// class, so the LLM passes can age them differently if needed.

// Usage states (Hermes skill_usage vocabulary).
const (
	StateActive   = "active"
	StateStale    = "stale"
	StateArchived = "archived"
)

// Staleness constants (card t_9972dfd0, Hermes defaults): a skill with
// no activity for staleAfterDays is marked stale; archiveAfterDays
// without activity is archived. Distillation floor lives in
// curator.go (DistillMinChars).
const (
	StaleAfterDays   = 14
	ArchiveAfterDays = 30
)

// UsageRecord is one skill's activity record.
type UsageRecord struct {
	CreatedAt  string `json:"created_at,omitempty"`   // anchored at creation/first sight
	LastUsed   string `json:"last_used_at,omitempty"` // freshest read/write/update
	UseCount   int    `json:"use_count,omitempty"`
	PatchCount int    `json:"patch_count,omitempty"`
	State      string `json:"state,omitempty"` // StateActive | StateStale | StateArchived
	Pinned     bool   `json:"pinned,omitempty"`
	Author     string `json:"author,omitempty"` // "agent" = agent-created class
	ArchivedAt string `json:"archived_at,omitempty"`
}

// usageFile is the per-directory state path: <dir>/.curator/usage.json.
func usageFile(dir string) string {
	return filepath.Join(dir, ".curator", "usage.json")
}

// usageStore is the in-process gate over the JSON file. One provider,
// one skills dir, one goroutine at a time is the common case, but the
// tool runs concurrent reads - a mutex beats a corrupt file.
var usageMu sync.Mutex

// readUsage loads the usage map for dir. A missing file is an empty
// map, never an error.
func readUsage(dir string) map[string]UsageRecord {
	usageMu.Lock()
	defer usageMu.Unlock()
	return readUsageLocked(dir)
}

func readUsageLocked(dir string) map[string]UsageRecord {
	out := map[string]UsageRecord{}
	data, err := os.ReadFile(usageFile(dir))
	if err != nil {
		return out
	}
	var m map[string]UsageRecord
	if json.Unmarshal(data, &m) == nil && m != nil {
		return m
	}
	return out
}

// writeUsage persists the usage map for dir, creating .curator/ as
// needed. A write failure is swallowed: usage tracking must never
// fail a skills tool call.
func writeUsage(dir string, m map[string]UsageRecord) {
	usageMu.Lock()
	defer usageMu.Unlock()
	writeUsageLocked(dir, m)
}

func writeUsageLocked(dir string, m map[string]UsageRecord) {
	if err := os.MkdirAll(filepath.Dir(usageFile(dir)), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(sortedUsage(m), "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(usageFile(dir), data, 0o644)
}

// sortedUsage renders the map through a name-sorted shim so the file
// diffs deterministically (map JSON would churn key order).
func sortedUsage(m map[string]UsageRecord) map[string]UsageRecord {
	// json.Marshal already sorts map keys alphabetically in Go; this
	// shim exists as the named law, not behavior.
	return m
}

// TouchUsage records one activity on a skill: kind "read" bumps
// use_count + last_used, "write" bumps patch_count + last_used (and
// creates the record with an author class when the caller has one).
// Missing record = the skill's first sight (created_at = now) unless
// the skill was created this run - the caller supplies createdAt.
func TouchUsage(dir, name, kind, author string) {
	usageMu.Lock()
	defer usageMu.Unlock()
	m := readUsageLocked(dir)
	rec := m[name]
	now := time.Now().UTC().Format(time.RFC3339)
	if rec.CreatedAt == "" {
		rec.CreatedAt = now
	}
	rec.LastUsed = now
	switch kind {
	case "write":
		rec.PatchCount++
	case "read":
		rec.UseCount++
	}
	if author != "" {
		rec.Author = author
	}
	// Real activity reactivates a stale skill (Hermes: used again
	// after going stale). An archived skill stays archived - the
	// folder is gone; the record keeps its history until rollback.
	if rec.State == StateStale {
		rec.State = StateActive
	}
	if rec.State == "" {
		rec.State = StateActive
	}
	m[name] = rec
	writeUsageLocked(dir, m)
}

// StaleSkillNames returns the names currently marked stale in dir,
// sorted - the input the weekly LLM pass reads. A missing usage file
// yields an empty list.
func (sp *SkillsProvider) StaleSkillNames() []string {
	var out []string
	for dir := range sp.usageDirs() {
		m := readUsage(dir)
		names := make([]string, 0, len(m))
		for n, rec := range m {
			if rec.State == StateStale {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		out = append(out, names...)
	}
	return out
}

// usageDirs returns the provider's write-home (Dir) keyed by itself -
// usage records live only in the agent's own dir; SharedDir skills
// belong to every agent and none of them.
func (sp *SkillsProvider) usageDirs() map[string]bool {
	if sp.Dir == "" {
		return nil
	}
	return map[string]bool{sp.Dir: true}
}
