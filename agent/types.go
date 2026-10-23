package agent

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// Compatibility aliases: these wire/session message shapes now live in
// core (arc 1a, t_8c9841ac). Every module import of agent.* keeps
// compiling; the aliases disappear when modules rewire in phase 2.
//
// DO NOT add new types here - new session shapes belong in core.

type (
	// Session is a persisted conversation with optional parent linking
	// for branching.
	Session = core.Session
	// LastRunStats is one completed run's average rates.
	LastRunStats = core.LastRunStats
	// SessionCounts is per-session activity computed at read time.
	SessionCounts = core.SessionCounts
	// SessionNode is one node in the session tree returned by GetSessionTree.
	SessionNode = core.SessionNode
	// StoredMessage pairs a message with the store-level time it was written.
	StoredMessage = core.StoredMessage
	// SearchResult is a single search hit from SearchMessages.
	SearchResult = core.SearchResult
	// InjectedMessage is a message injected by an extension (e.g. a
	// subagent result) that re-enters the conversation as a new user message.
	InjectedMessage = core.InjectedMessage
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

// ExtensionCommand is a slash command registered by an extension.
type ExtensionCommand struct {
	Name        string `json:"name"` // includes leading slash, e.g. "/mycmd"
	Description string `json:"description"`
	// Args optionally lists first-argument completions the serving
	// frontend's autocomplete offers once the command itself is typed.
	Args []string `json:"args,omitempty"`
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
// system prompt and /tools listing. Actions carries the sub-action
// metadata of a domain tool so renderers can show the split
// (domain line + labeled sub-actions) without re-parsing schemas.
type ToolInfo struct {
	Name        string
	Description string
	Actions     []ToolAction
}

// ExtensionInfo is metadata about a loaded extension, for display.
type ExtensionInfo struct {
	Name        string
	Description string     // one-line purpose, from the optional Describer seam
	Tools       int
	Commands    int
	Seams       []string   // declared seam types ("prompt_builder", "compactor", etc.)
	ToolList    []ToolInfo // tools provided by this extension (name + description)
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