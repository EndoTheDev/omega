package approval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The audit log (design Q6): every verdict appends ONE JSONL record
// to the omega home. The log IS the eval data source (Q9's corpus
// companion) - it records the parsed command, all six answers, the
// ladder step fired, the policy hash and the mode. Append-only,
// best-effort: a logging failure never changes a gate decision.

// AuditLog appends decision records to <home>/approval-decisions.jsonl.
// Mode 0600 (the rows carry reviewed commands), rotation at 4 MB
// keeping one previous generation.
const (
	auditFileName = "approval-decisions.jsonl"
	auditMaxBytes = 4 << 20
)

// AuditRow is one decision record. The six answers carry zero-values
// when the deciding pass was a card (manual mode) or a replay
// (cached: the original's answers repeat in the row).
type AuditRow struct {
	TS             string   `json:"ts"`
	Mode           string   `json:"mode"`
	Origin         string   `json:"origin"` // interactive | cron | delegate
	Session        string   `json:"session,omitempty"`
	Tool           string   `json:"tool"`
	Command        string   `json:"command"` // the canonical parsed form
	Rules          []string `json:"rules,omitempty"`
	Verdict        string   `json:"verdict"`
	RawVerdict     string   `json:"raw_verdict,omitempty"` // the model's own verdict before the ladder
	Step           string   `json:"step"`
	Reason         string   `json:"reason,omitempty"`
	Confidence     float64  `json:"confidence"`
	PolicyAllows   float64  `json:"policy_allows"`
	BlastRadius    float64  `json:"blast_radius"`
	SelfAdvocating float64  `json:"self_advocating"`
	ReadsSecrets   float64  `json:"reads_secrets"`
	SendsOutbound  float64  `json:"sends_outbound"`
	Model          string   `json:"model,omitempty"`
	HasPolicy      bool     `json:"has_policy"`
	PolicyHash     string   `json:"policy_hash,omitempty"` // the operator policy fingerprint
	PolicyFP       string   `json:"policy_fp,omitempty"`   // the questions fingerprint
	LadderVersion  string   `json:"ladder_version,omitempty"` // the threshold set that decided
	DecidedBy      string   `json:"decided_by"`            // reviewer | human_card | manual | cache
	CacheKey       string   `json:"cache_key,omitempty"`
	Cached         bool     `json:"cached"`
	CachedIn       string   `json:"cached_in,omitempty"` // the model that decided the cached original
	Redacted       bool     `json:"redacted"`
	Truncated      bool     `json:"truncated"`
	Executed       bool     `json:"executed"` // false for deny/escalate/unreachable
	Error          string   `json:"error,omitempty"`
	LatencyMs      int64    `json:"latency_ms"`
}

// Audit writes the log. path is resolved once at mount; Append never
// returns an error to the gate's callers.
type Audit struct {
	path string
}

func NewAudit(home string) *Audit {
	return &Audit{path: filepath.Join(home, auditFileName)}
}

// Append writes one record. Never fails the caller: an error is
// dropped (with an fmt.Fprintln to stderr so a dead audit disk still
// leaves a trace) - the gate's decision stands either way.
func (a *Audit) Append(row AuditRow) {
	if a == nil || a.path == "" {
		return
	}
	if row.TS == "" {
		row.TS = time.Now().Format(time.RFC3339Nano)
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		return
	}
	encoded = append(encoded, '\n')
	if err := a.append(encoded); err != nil {
		fmt.Fprintln(os.Stderr, "approval: audit append failed:", err)
	}
}

// append is the write seam: single O_APPEND|O_CREATE|O_NOFOLLOW open
// (where the platform defines O_NOFOLLOW), 0600, rotation keeping one
// generation. Best effort - mirror of the reference's persistence
// boundary.
func (a *Audit) append(encoded []byte) error {
	dir := filepath.Dir(a.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Stat(a.path); err == nil && info.Size()+int64(len(encoded)) > auditMaxBytes {
		next := a.path + ".1"
		_ = os.Remove(next)
		_ = os.Rename(a.path, next)
	}
	f, err := os.OpenFile(a.path, appendFlags(), 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(encoded)
	return err
}
