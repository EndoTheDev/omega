package agent

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
