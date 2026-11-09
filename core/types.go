package core

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

	// DBSizeBytes / WALSizeBytes are the store's on-disk file sizes at
	// compute time (0 on stores without a file path, e.g. ":memory:").
	// The hygiene pass's vacuum report reads these - the insights
	// surface is where db size lives (card t_329b9312, no duplicate
	// surface).
	DBSizeBytes  int64 `json:"db_size_bytes,omitempty"`
	WALSizeBytes int64 `json:"wal_size_bytes,omitempty"`
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
// to the transcript. Actions are host directives the host interprets.
type CommandResult struct {
	Text    string      // what to display (may be empty)
	Actions []CmdAction // optional host actions
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
// Disabled is the registry entry's enable flag (card t_441deb04):
// false is ON, so the zero value keeps every existing entry enabled
// and a home needs no config change to stay where it is. The flag is
// stamped where the fold is known (StampDisabledTools), never stored
// on the mount inventory - a live toggle changes it without a remount.
type ToolInfo struct {
	Name        string
	Description string
	Actions     []ToolAction
	Disabled    bool
}

// StampDisabledTools returns infos with every ToolInfo's Disabled flag
// set from the disabled name list (the config.yaml disabled.tools fold,
// or the live Context copy of it). Entries not named keep Disabled
// false. The input is never mutated: callers hold the mount inventory
// across live toggles, so the fold rides a copy stamped per read.
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

// ExtensionInfo is metadata about a loaded extension, for display.
type ExtensionInfo struct {
	Name        string
	Description string // one-line purpose, from the optional Describer seam
	Tools       int
	Commands    int
	Seams       []string   // declared seam types ("prompt_builder", "compactor", etc.)
	ToolList    []ToolInfo // tools provided by this extension (name + description)
	// CommandNames lists the slash commands this extension registered
	// (registration order, leading slash included) - the webui
	// extensions-tab expansion renders these verbatim, so the count and
	// the names stay one fact, not two.
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
