package core

// ContextCompactPrefix and ContextCompactSuffix wrap a legacy compacted context's summary
// head - the pre-block shape, kept so old sessions read as they always did.
const (
	ContextCompactPrefix = "[compacted: "
	ContextCompactSuffix = "]"
)

// CompressToolName is the tool whose call/result pair carries a compact's summary at the
// TAIL of the session, so nothing the provider already saw changes bytes.
const CompressToolName = "compress"

// ContextBlock is one compact, recorded by the store: the span it took out of the live
// context (From/To row ids), the first retained tail row (Keep, 0 = none), the carrier rows
// holding its summary (Call/Carrier), and the lower-tier blocks it folds (Sources). Blocks
// are the context's tier structure, never retired by their timestamp; the wire is derived
// from them.
type ContextBlock struct {
	ID      string   `json:"id"`
	Tier    int      `json:"tier"`
	From    int64    `json:"from"`
	To      int64    `json:"to"`
	Keep    int64    `json:"keep,omitempty"`
	Call    int64    `json:"call,omitempty"`
	Carrier int64    `json:"carrier,omitempty"`
	Sources []string `json:"sources,omitempty"`
	At      string   `json:"at,omitempty"`
}

type CompactWrite struct {
	Summary string
	From    int64
	To      int64
	Keep    int64
	Tier    int
	Sources []string
}

type Session struct {
	ID string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	// DelegateParent is the DELEGATE lineage - metadata for lineage display only; a delegate
	// child never inherits its parent's history.
	DelegateParent string `json:"delegate_parent,omitempty"`
	Label          string `json:"label,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`

	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`

	// Pinned is the user's keep-at-top flag; baked rows are never rewritten.
	Pinned bool `json:"pinned"`

	// Archived is the user's hide-from-list flag; baked rows are never rewritten.
	Archived bool `json:"archived"`

	// MachineOwned marks a conversation the MACHINERY owns rather than the human: such a row
	// opens from its owner's surface, never from the human's session list, and a /new on it
	// compacts the conversation in place instead of forking - a bot keeps one conversation.
	MachineOwned bool `json:"machine_owned,omitempty"`

	ContextStart string `json:"context_start,omitempty"`

	ContextSummary string `json:"context_summary,omitempty"`
	LastReadAt string `json:"last_read_at,omitempty"`

	// TitleState tracks generated-title maturity: rows that already carried a label were
	// migrated straight to 2, because a manual label is never retitled.
	TitleState int `json:"title_state"`

	// Agent is the profile this session is bound to, fixed at creation: a session never
	// switches agents mid-conversation.
	Agent string `json:"agent,omitempty"`

	// LastRun is the status bar's run telemetry, computed at run end from reported usage; nil
	// until such a run completes, so a fresh session never carries phantom rates.
	LastRun *LastRunStats `json:"last_run,omitempty"`

	Model string `json:"model,omitempty"`

	// ProviderName is the session's pinned provider INSTANCE id. A name gone from config.yaml
	// fails the run loudly - a removed instance never strands a session into a silent
	// fallback.
	ProviderName string `json:"provider,omitempty"`

	// Workspace is the session's working folder; empty = the process working directory.
	Workspace string `json:"workspace,omitempty"`

	// Summary is the hygiene pass's LLM-written digest. It is generated prose, not activity:
	// writing one never touches updated_at.
	Summary string `json:"summary,omitempty"`
}

// LastRunStats is one completed run's average rates, written with the run's final usage.
type LastRunStats struct {
	TokensPerSecond int `json:"tokens_per_second"`
	ChunksPerSecond int `json:"chunks_per_second"`
}

type SessionCounts struct {
	MessageCount int `json:"message_count"`
	ToolCount    int `json:"tool_count"`
}

type SessionNode struct {
	Session
	Children []*SessionNode
}

type StoredMessage struct {
	ID        int64
	Message   Message
	CreatedAt string
}

type SearchMatch struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type SearchResult struct {
	SessionID string `json:"session_id"`

	Snippet string `json:"snippet"`
	// Matches carries the matched-term spans when any matched term lands in the window; nil
	// for label hits, or when the window holds a marker-sentinel byte and the span parse would
	// be ambiguous.
	Matches []SearchMatch `json:"matches,omitempty"`

	Title bool `json:"title"`
	Summary bool `json:"summary"`
}
