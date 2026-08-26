package agent

import (
	"github.com/EndoTheDev/omega/ai"
)

// Session is a persisted conversation with optional parent linking
// for branching.
type Session struct {
	ID        string `json:"id"`
	ParentID  string `json:"parent_id,omitempty"`
	Label     string `json:"label,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// SessionNode is one node in the session tree returned by GetSessionTree.
type SessionNode struct {
	Session
	Children []*SessionNode
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
	Type  string // "set_model", "refresh_title", "fetch_model_info", "run_compact"
	Value string // the value (model name)
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
