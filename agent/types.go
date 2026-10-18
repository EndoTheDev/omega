package agent

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/EndoTheDev/omega/ai"
)

// NewSessionID generates a random 32-character hex session ID.
// Used by the store, TUI, and HTTP channel to avoid duplicating
// the crypto/rand boilerplate.
func NewSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

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

	// LastReadAt is the time the session's output was last seen by the
	// user (empty on rows created before the column existed - the
	// migration backfills it to updated_at so nothing old turns unread).
	LastReadAt string `json:"last_read_at,omitempty"`

	// TitleState tracks generated-title maturity: 0 untitled, 1
	// provisional (user-message-only), 2 final (first exchange done).
	// Sessions that already carried a label when the column was added
	// were migrated straight to 2 - a manual label is never retitled.
	TitleState int `json:"title_state"`

	// LastRun is the status bar's last-known run telemetry, computed at
	// run end (real provider usage over the run's streaming wall clock)
	// and persisted on the session row. Nil until a run with reported
	// usage completes - a fresh session never carries phantom rates.
	LastRun *LastRunStats `json:"last_run,omitempty"`
}

// LastRunStats is one completed run's average rates, written with the
// run's final token persist and shown by the webui status bar while no
// stream is live. Rates round to whole units, matching the live meters.
type LastRunStats struct {
	TokensPerSecond int `json:"tokens_per_second"`
	ChunksPerSecond int `json:"chunks_per_second"`
}

// SessionUnread reports whether a session has output newer than its last
// read stamp: updated_at after last_read_at. Computed at read time - no
// counters, no schema writes. Empty stamps read as seen, and the session
// being compared must have been persisted with RFC 3339 times.
func SessionUnread(s Session) bool {
	if s.LastReadAt == "" {
		return false
	}
	updated, err := time.Parse(time.RFC3339, s.UpdatedAt)
	if err != nil {
		return s.UpdatedAt > s.LastReadAt
	}
	read, err := time.Parse(time.RFC3339, s.LastReadAt)
	if err != nil {
		return s.UpdatedAt > s.LastReadAt
	}
	return updated.After(read)
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
// written (created_at), for consumers that surface message times.
type StoredMessage struct {
	Message   ai.Message
	CreatedAt string
}

// SearchResult is a single search hit from SearchMessages.
type SearchResult struct {
	SessionID string `json:"session_id"`
	Snippet   string `json:"snippet"`
}

// Skill is a loaded skill from a skill directory. The YAML frontmatter
// in the skill file provides name and description; the markdown body is
// the skill content injected into the system prompt when invoked. Dir
// is the path to the skill's directory, so the skill can reference its
// own files (scripts, references, templates) by relative path.
type Skill struct {
	Name        string
	Description string
	Content     string
	Dir         string
}

// ToolStat is one row in the tool breakdown.
type ToolStat struct {
	Name  string
	Count int
}

// DayStat is one row in the daily activity breakdown.
type DayStat struct {
	Day   string // "Mon", "Tue", etc.
	Count int
	Bar   string // visual bar string
}

// NotableStat holds the most extreme session for a given metric.
type NotableStat struct {
	Value  int
	Detail string // date or session label
}

// Insights is the aggregated cross-session analytics result.
type Insights struct {
	Period         string
	PeriodStart    string
	PeriodEnd      string
	Days           int
	Sessions       int
	Messages       int
	UserMessages   int
	ToolCalls      int
	TotalTokens    int
	AvgSessionMsgs float64
	Tools          []ToolStat
	Daily          [7]DayStat
	NotableMsgs    NotableStat
	NotableTokens  NotableStat
	NotableTools   NotableStat
}

// InjectedMessage is a message injected by an extension (e.g. a
// subagent result) that re-enters the conversation as a new user
// message, triggering a new turn.
type InjectedMessage struct {
	Text   string // the message content
	Source string // "delegate:<task_id>" — for display
}

// ExtensionCommand is a slash command registered by an extension.
type ExtensionCommand struct {
	Name        string `json:"name"` // includes leading slash, e.g. "/mycmd"
	Description string `json:"description"`
}

// CommandResult is what an extension command returns. Text is printed
// to the transcript. Actions are TUI directives the host interprets.
type CommandResult struct {
	Text    string      // what to display (may be empty)
	Actions []CmdAction // optional TUI actions
}

// CmdAction tells the host to do something after a command runs.
// Extensions declare intent; the host interprets.
type CmdAction struct {
	Type  string   // "set_model", "refresh_title", "fetch_model_info", "run_compact", "set_model_list", "set_tool_display", "set_thinking", "new_session", "load_session", "branch_session", "set_label"
	Value string   // primary value (model name, session ID, label text)
	List  []string // list values (model names)
}

// ToolInfo is a tool name and description pair, for display in the
// system prompt and /tools listing.
type ToolInfo struct {
	Name        string
	Description string
}

// ExtensionInfo is metadata about a loaded extension, for display.
type ExtensionInfo struct {
	Name     string
	Tools    int
	Commands int
	Seams    []string   // declared seam types ("prompt_builder", "compactor", etc.)
	ToolList []ToolInfo // tools provided by this extension (name + description)
}

// PromptBuildOptions carries context for extension-built system prompts.
type PromptBuildOptions struct {
	CWD            string
	Messages       []ai.Message
	Extensions     []ExtensionInfo
	ProjectContext string   // AGENTS.md contents, already trust-gated by Go
	Custom         string   // user-supplied prompt from config, may be empty
	Append         []string // extra prompts from --append-system-prompt, may be nil
}

// PromptSection is one titled block of system prompt content,
// contributed by the module that owns it. Sections render between
// Project Context and Environment, sorted by Order (stable sort; equal
// orders keep plugin mount order). Current layout: memory 10, skills
// 20, tools 30.
type PromptSection struct {
	Title string // rendered as "\n## <Title>\n"; empty = untitled block
	Body  string // section content, lines carry their own newlines
	Order int    // sort key; lower renders earlier
}
