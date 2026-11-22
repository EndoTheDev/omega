package core

import "sort"

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

	DBSizeBytes  int64 `json:"db_size_bytes,omitempty"`
	WALSizeBytes int64 `json:"wal_size_bytes,omitempty"`
}

// ExtensionCommand is a slash command registered by an extension.
type ExtensionCommand struct {
	Name        string `json:"name"` // includes leading slash, e.g. "/mycmd"
	Description string `json:"description"`
	Args []string `json:"args,omitempty"`
}

// CommandResult is what an extension command returns.
type CommandResult struct {
	Text    string      // what to display (may be empty)
	Actions []CmdAction // optional host actions
}

// CmdAction tells the host to do something after a command runs.
type CmdAction struct {
	Type  string   // "set_model", "refresh_title", "fetch_model_info", CmdRunCompact, "set_model_list", "set_tool_display", "set_thinking"
	Value string   // primary value (model name, session ID, label text)
	List  []string // list values (model names)
}

// CmdRunCompact is the /compact intent. The capsule declares it - it owns the summarizer
// and no store - and every host that holds a session runs the compact itself through
// agent.CompactSession, so the intent has an executor on every surface a human can type on.
const CmdRunCompact = "run_compact"

// ToolInfo is a tool name and description pair, for display in the system prompt.
// The flag is stamped where the fold is known (StampDisabledTools), never stored on the
// mount inventory - a live toggle changes it without a remount.
type ToolInfo struct {
	Name        string
	Description string
	Actions     []ToolAction
	Disabled    bool
}

// StampDisabledTools returns infos with every ToolInfo's Disabled flag set.
// The input is never mutated: callers hold the mount inventory across live toggles, so
// the fold rides a copy stamped per read.
func StampDisabledTools(infos []ExtensionInfo, disabled []string) []ExtensionInfo {
	if len(disabled) == 0 || len(infos) == 0 {
		return infos
	}
	fold := make(map[string]bool, len(disabled))
	for _, name := range disabled {
		fold[name] = true
	}
	out := make([]ExtensionInfo, len(infos))
	for i, info := range infos {
		if len(info.ToolList) == 0 {
			out[i] = info
			continue
		}
		list := make([]ToolInfo, len(info.ToolList))
		copy(list, info.ToolList)
		for j := range list {
			list[j].Disabled = fold[list[j].Name]
		}
		info.ToolList = list
		out[i] = info
	}
	return out
}

// ToolNames returns every tool name the inventory carries, sorted, never nil. It is the
// fold that means "no tools at all" - a toolless run passes this list as its fold.
func ToolNames(infos []ExtensionInfo) []string {
	var out []string
	for _, info := range infos {
		for _, t := range info.ToolList {
			if t.Name != "" {
				out = append(out, t.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ExtensionInfo is metadata about a loaded extension, for display.
type ExtensionInfo struct {
	Name        string
	Description string // one-line purpose, from the optional Describer seam
	Tools       int
	Commands    int
	Seams       []string   // declared seam types ("prompt_builder", "compactor", etc.)
	ToolList    []ToolInfo // tools provided by this extension (name + description)
	CommandNames []string
}

// PromptBuildOptions carries context for extension-built system prompts.
type PromptBuildOptions struct {
	CWD            string
	Messages       []Message
	Extensions     []ExtensionInfo
	ProjectContext string   // AGENTS.md contents, already trust-gated by Go
	Custom         string   // user-supplied prompt from config, may be empty
	Append         []string // extra prompts from --append-system-prompt, may be nil
	// DisabledTools is the run's fold: the tools the wire does not carry. The Tools
	// section already reads the stamped Disabled flags; every other contributor (a
	// capsule's prose section, the guidelines, the opening posture) must consult this
	// set too - a prompt that describes a capability the run cannot execute is a lie
	// the model acts on (agent/agent.go:206).
	DisabledTools []string
}

// ToolEnabled reports whether the run offers name to the model - the tool is on the wire.
// A tool is on the wire when the run's inventory carries it and the fold does not name it.
// An inventory the caller did not supply is unknown, never empty, so a name is then judged
// by the fold alone (the caller-less builders keep the prompt they had).
func (o PromptBuildOptions) ToolEnabled(name string) bool {
	if name == "" {
		return true
	}
	for _, n := range o.DisabledTools {
		if n == name {
			return false
		}
	}
	if len(o.Extensions) == 0 {
		return true
	}
	for _, ext := range o.Extensions {
		for _, t := range ext.ToolList {
			if t.Name == name {
				return true
			}
		}
	}
	return false
}

// ToolsEnabled reports whether at least one of names is offered to the model - the gate
// for a section that needs any one of a capability family rather than a named tool.
func (o PromptBuildOptions) ToolsEnabled(names ...string) bool {
	for _, n := range names {
		if o.ToolEnabled(n) {
			return true
		}
	}
	return false
}

// HasTools reports whether the run's inventory carries at least one callable tool. An
// inventory the caller did not supply (no Extensions) answers true: a missing inventory
// is not evidence of missing tools, and the opening posture is a claim about the run.
func (o PromptBuildOptions) HasTools() bool {
	if len(o.Extensions) == 0 {
		return true
	}
	for _, ext := range o.Extensions {
		for _, t := range ext.ToolList {
			if !t.Disabled {
				return true
			}
		}
	}
	return false
}

// PromptSection is one titled block of system prompt content, contributed.
// Sections render between Project Context and Environment, sorted by Order (stable
// sort; equal orders keep plugin mount order).
type PromptSection struct {
	Title string // rendered as "\n## <Title>\n"; empty = untitled block
	Body  string // section content, lines carry their own newlines
	Order int    // sort key; lower renders earlier
}
