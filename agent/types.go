package agent

import (
	"crypto/rand"
	"encoding/hex"
	"time"

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

// Skill is a loaded skill from a skill directory (moved to core,
// arc 1b - SkillsProvider, SkillsLister and FrontendOptions reference it).
type Skill = core.Skill

// ToolStat is one row in the tool breakdown.
type ToolStat = core.ToolStat

// DayStat is one row in the daily activity breakdown.
type DayStat = core.DayStat

// NotableStat holds the most extreme session for a given metric.
type NotableStat = core.NotableStat

// Insights is the aggregated cross-session analytics result
// (moved to core, arc 1b - StoreProvider.ComputeInsights returns it).
type Insights = core.Insights

// ExtensionCommand is a slash command registered by an extension
// (moved to core, arc 1b - Context.CommandHandler's neighborhood).
type ExtensionCommand = core.ExtensionCommand

// CommandResult is what an extension command returns. Text is printed
// to the transcript. Actions are TUI directives the host interprets.
type CommandResult = core.CommandResult

// CmdAction tells the host to do something after a command runs.
type CmdAction = core.CmdAction

// ToolInfo is a tool name and description pair (moved to core, arc 1b).
type ToolInfo = core.ToolInfo

// ExtensionInfo is metadata about a loaded extension
// (moved to core, arc 1b - LoopOptions and Context.Infos reference it).
type ExtensionInfo = core.ExtensionInfo

// PromptBuildOptions carries context for extension-built system prompts
// (moved to core, arc 1b).
type PromptBuildOptions = core.PromptBuildOptions

// PromptSection is one titled block of system prompt content
// (moved to core, arc 1b).
type PromptSection = core.PromptSection