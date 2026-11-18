package core

// ContextFoldPrefix and ContextFoldSuffix wrap the summary that heads a folded.
const (
	ContextFoldPrefix = "[compacted: "
	ContextFoldSuffix = "]"
)

// Session is a persisted conversation with optional parent linking for branching.
type Session struct {
	ID string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	// DelegateParent is the DELEGATE lineage. It is metadata for lineage display only - a
	// delegate child never inherits its parent's history.
	DelegateParent string `json:"delegate_parent,omitempty"`
	Label          string `json:"label,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`

	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`

	// Pinned is the user's keep-at-top flag. Old sessions default to
	// unpinned; baked data is never rewritten.
	Pinned bool `json:"pinned"`

	// Archived is the user's hide-from-list flag. Old sessions default
	// to unarchived; baked data is never rewritten.
	Archived bool `json:"archived"`

	// MachineOwned marks a conversation the MACHINERY owns rather than the human. Such a
	// row opens from its owner's surface and never appears in the human's own session
	// list, and a /new on it folds the conversation in place instead of forking a new one
	// - a bot keeps one conversation, its context is what restarts.
	MachineOwned bool `json:"machine_owned,omitempty"`

	ContextStart string `json:"context_start,omitempty"`

	// ContextSummary is what survives a fold. Empty on a conversation that was never
	// folded, or folded too small for a summary to be worth a model call.
	ContextSummary string `json:"context_summary,omitempty"`
	LastReadAt string `json:"last_read_at,omitempty"`

	// TitleState tracks generated-title maturity. Sessions that already carried a label
	// when the column was added were migrated straight to 2 - a manual label is never
	// retitled.
	TitleState int `json:"title_state"`

	// Agent is the profile this session is bound to ("default" for sessions created. Bound
	// at creation - a session never switches agents mid-conversation.
	Agent string `json:"agent,omitempty"`

	// LastRun is the status bar's last-known run telemetry, computed at run end (real. Nil
	// until a run with reported usage completes - a fresh session never carries phantom
	// rates.
	LastRun *LastRunStats `json:"last_run,omitempty"`

	Model string `json:"model,omitempty"`

	// ProviderName is the session's pinned provider INSTANCE id. Resolution is
	// config-driven per run: a name that is gone from config.yaml providers: fails the run
	// loudly - a removed instance never strands a session into a silent fallback.
	ProviderName string `json:"provider,omitempty"`

	// Workspace is the session's working folder (empty = no root - relative file
	// operations resolve against the process working directory, exactly as before).
	Workspace string `json:"workspace,omitempty"`

	// Summary is the hygiene pass's LLM-written one-paragraph digest of the session. A
	// summary is generated prose, not activity: writing one never touches updated_at.
	Summary string `json:"summary,omitempty"`
}

// LastRunStats is one completed run's average rates, written with the run's final.
type LastRunStats struct {
	TokensPerSecond int `json:"tokens_per_second"`
	ChunksPerSecond int `json:"chunks_per_second"`
}

// SessionCounts is per-session activity computed at read time.
type SessionCounts struct {
	MessageCount int `json:"message_count"`
	ToolCount    int `json:"tool_count"`
}

// SessionNode is one node in the session tree returned by GetSessionTree.
type SessionNode struct {
	Session
	Children []*SessionNode
}

// StoredMessage pairs a message with the store-level time it was written.
type StoredMessage struct {
	ID        int64
	Message   Message
	CreatedAt string
}

// SearchMatch is one matched-term span inside a SearchResult.Snippet: byte offsets.
// Matches ride structured metadata, never in-band marker characters.
type SearchMatch struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// SearchResult is a single search hit from SearchMessages.
type SearchResult struct {
	SessionID string `json:"session_id"`

	// Snippet is a verbatim excerpt of the stored content: a contiguous substring,
	// byte-for-byte what the store holds - no highlight markers are inserted.
	Snippet string `json:"snippet"`
	// Matches carries the matched-term spans (byte offsets into Snippet) when any matched
	// term lands in the window; nil when it does not (label hits) or when the excerpt
	// window itself holds a marker-sentinel byte and the span parse would be ambiguous.
	Matches []SearchMatch `json:"matches,omitempty"`

	Title bool `json:"title"`
	Summary bool `json:"summary"`
}
