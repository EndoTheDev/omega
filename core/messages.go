package core

// Session is a persisted conversation with optional parent linking
// for branching.
type Session struct {
	ID        string `json:"id"`
	ParentID  string `json:"parent_id,omitempty"`
	Label     string `json:"label,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	// Real provider-reported usage accumulated per session. Sessions
	// saved before this data was recorded keep 0 totals.
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`

	// Pinned is the user's keep-at-top flag. Old sessions default to
	// unpinned; baked data is never rewritten.
	Pinned bool `json:"pinned"`

	// Archived is the user's hide-from-list flag. Old sessions default
	// to unarchived; baked data is never rewritten.
	Archived bool `json:"archived"`

	// LastReadAt is the time the session's output was last seen by the
	// user (empty on rows created before the column existed - the
	// migration backfills it to updated_at so nothing old turns unread).
	LastReadAt string `json:"last_read_at,omitempty"`

	// TitleState tracks generated-title maturity: 0 untitled, 1
	// provisional (user-message-only), 2 final (first exchange done).
	// Sessions that already carried a label when the column was added
	// were migrated straight to 2 - a manual label is never retitled.
	TitleState int `json:"title_state"`

	// Agent is the profile this session is bound to ("default" for
	// sessions created before/without the agent surface). Bound at
	// creation - a session never switches agents mid-conversation.
	Agent string `json:"agent,omitempty"`

	// LastRun is the status bar's last-known run telemetry, computed at
	// run end (real provider usage over the run's streaming wall clock)
	// and persisted on the session row. Nil until a run with reported
	// usage completes - a fresh session never carries phantom rates.
	LastRun *LastRunStats `json:"last_run,omitempty"`

	// Model is the session's pinned model_name (empty = no pin - the
	// channel's default model serves the session). Written when the
	// user picks a model mid-session and read by every send: reopening
	// the session restores the model it last used. Sessions saved
	// before this data was recorded stay empty - today's behavior.
	Model string `json:"model,omitempty"`

	// Workspace is the session's working folder (empty = no root -
	// relative file operations resolve against the process working
	// directory, exactly as before). Set operations root file tools
	// here, and the folder's trust.yaml + AGENTS.md govern the
	// session's prompt context. Absolute path; validated to be an
	// existing directory when written.
	Workspace string `json:"workspace,omitempty"`
}

// LastRunStats is one completed run's average rates, written with the
// run's final token persist and shown by the webui status bar while no
// stream is live. Rates round to whole units, matching the live meters.
type LastRunStats struct {
	TokensPerSecond int `json:"tokens_per_second"`
	ChunksPerSecond int `json:"chunks_per_second"`
}

// SessionCounts is per-session activity computed at read time (SQL
// COUNT over the messages table, no schema change or maintained
// counters). message_count covers all stored messages; tool_count is
// the tool-role subset (tool results).
type SessionCounts struct {
	MessageCount int `json:"message_count"`
	ToolCount    int `json:"tool_count"`
}

// SessionNode is one node in the session tree returned by GetSessionTree.
type SessionNode struct {
	Session
	Children []*SessionNode
}

// StoredMessage pairs a message with the store-level time it was
// written (created_at), for consumers that surface message times. ID is
// the store row id - the message's identity on the conversation tree
// (parent pointers reference it); the transcript wire exposes it as a
// string so clients can address branch points.
type StoredMessage struct {
	ID        int64
	Message   Message
	CreatedAt string
}

// SearchResult is a single search hit from SearchMessages.
type SearchResult struct {
	SessionID string `json:"session_id"`
	Snippet   string `json:"snippet"`
	// Title marks a hit that matched the session's label rather than
	// message content (SearchSessions). When set, Snippet holds the
	// label itself.
	Title bool `json:"title"`
}

// InjectedMessage is a message injected by an extension (e.g. a
// subagent result) that re-enters the conversation as a new user
// message, triggering a new turn.
type InjectedMessage struct {
	Text   string // the message content
	Source string // "delegate:<task_id>" - for display
}
