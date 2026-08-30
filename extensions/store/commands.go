package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// HandleSessionsCommand lists all sessions with message counts.
// Returns a text table for the TUI to display.
func HandleSessionsCommand(ctx context.Context, store agent.StoreProvider, args string) (agent.CommandResult, error) {
	if store == nil {
		return agent.CommandResult{}, fmt.Errorf("no store available")
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return agent.CommandResult{}, fmt.Errorf("list sessions: %w", err)
	}
	if len(sessions) == 0 {
		return agent.CommandResult{Text: "[no sessions yet]"}, nil
	}
	type row struct {
		name  string
		count int
		id    string
	}
	rows := make([]row, len(sessions))
	maxName := 4
	maxCount := 4
	for i, s := range sessions {
		count, _ := store.CountMessages(ctx, s.ID)
		name := sessionDisplayName(s.Label, s.ID)
		rows[i] = row{name: name, count: count, id: s.ID}
		if len(name) > maxName {
			maxName = len(name)
		}
		countStr := fmt.Sprintf("%d", count)
		if len(countStr) > maxCount {
			maxCount = len(countStr)
		}
	}
	var sb strings.Builder
	sb.WriteString("\n")
	header := fmt.Sprintf("  %-3s  %-*s  %*s  %s", "#", maxName, "NAME", maxCount, "MSGS", "SESSION ID")
	sb.WriteString(header)
	sb.WriteString("\n")
	for i, r := range rows {
		fmt.Fprintf(&sb, "  %-3d  %-*s  %*d  %s\n", i+1, maxName, r.name, maxCount, r.count, r.id)
	}
	return agent.CommandResult{Text: sb.String()}, nil
}

// HandleTreeCommand renders the session tree.
func HandleTreeCommand(ctx context.Context, store agent.StoreProvider) (agent.CommandResult, error) {
	if store == nil {
		return agent.CommandResult{}, fmt.Errorf("no store available")
	}
	roots, err := store.GetSessionTree(ctx)
	if err != nil {
		return agent.CommandResult{}, fmt.Errorf("tree: %w", err)
	}
	if len(roots) == 0 {
		return agent.CommandResult{Text: "[no sessions yet]"}, nil
	}
	type row struct {
		name  string
		count int
		id    string
		glyph string
	}
	var rows []row
	var flatten func(node *agent.SessionNode, depth int, last bool)
	flatten = func(node *agent.SessionNode, depth int, last bool) {
		count, _ := store.CountMessages(ctx, node.ID)
		name := sessionDisplayName(node.Label, node.ID)
		glyph := ""
		if depth > 0 {
			if last {
				glyph = "└─ "
			} else {
				glyph = "├─ "
			}
			glyph = strings.Repeat("  ", depth-1) + glyph
		}
		rows = append(rows, row{name: name, count: count, id: node.ID, glyph: glyph})
		for i, child := range node.Children {
			flatten(child, depth+1, i == len(node.Children)-1)
		}
	}
	for _, root := range roots {
		flatten(root, 0, false)
	}
	maxName := 4
	maxCount := 4
	for _, r := range rows {
		if len(r.glyph)+len(r.name) > maxName {
			maxName = len(r.glyph) + len(r.name)
		}
		count := fmt.Sprint(r.count)
		if len(count) > maxCount {
			maxCount = len(count)
		}
	}
	var sb strings.Builder
	sb.WriteString("\n")
	header := fmt.Sprintf("%s %-*s %*s  %s", "", maxName, "NAME", maxCount, "MSGS", "SESSION ID")
	sb.WriteString(header)
	sb.WriteString("\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "  %-*s %*d  %s\n", maxName, r.glyph+r.name, maxCount, r.count, r.id)
	}
	return agent.CommandResult{Text: sb.String()}, nil
}

// HandleSearchCommand runs an FTS5 search across session messages.
func HandleSearchCommand(ctx context.Context, store agent.StoreProvider, args string) (agent.CommandResult, error) {
	if store == nil {
		return agent.CommandResult{}, fmt.Errorf("no session store available")
	}
	query := strings.TrimSpace(args)
	if query == "" {
		return agent.CommandResult{}, fmt.Errorf("usage: /search <query>")
	}
	results, err := store.SearchMessages(ctx, query)
	if err != nil {
		return agent.CommandResult{}, fmt.Errorf("search: %w", err)
	}
	if len(results) == 0 {
		return agent.CommandResult{Text: "[no results]"}, nil
	}
	var sb strings.Builder
	sb.WriteString("\n")
	for _, r := range results {
		label := r.SessionID
		if sess, err := store.GetSession(ctx, r.SessionID); err == nil && sess.Label != "" {
			label = sess.Label
		}
		sb.WriteString(label)
		sb.WriteString(": ")
		sb.WriteString(r.Snippet)
		sb.WriteString("\n")
	}
	return agent.CommandResult{Text: sb.String()}, nil
}

// HandleInsightsCommand computes cross-session usage analytics.
func HandleInsightsCommand(ctx context.Context, store agent.StoreProvider, args string) (agent.CommandResult, error) {
	if store == nil {
		return agent.CommandResult{}, fmt.Errorf("no session store available")
	}
	days := 30
	if d, err := strconv.Atoi(strings.TrimSpace(args)); err == nil && d >= 0 {
		days = d
	}
	stats, err := store.ComputeInsights(ctx, days)
	if err != nil {
		return agent.CommandResult{}, fmt.Errorf("insights: %w", err)
	}
	if stats.Sessions == 0 {
		return agent.CommandResult{Text: fmt.Sprintf("[no sessions in the last %d days]", days)}, nil
	}
	return agent.CommandResult{Text: agent.FormatInsights(stats)}, nil
}

// sessionDisplayName returns the label, or a truncated ID if no label.
func sessionDisplayName(label, id string) string {
	if label != "" {
		return label
	}
	if len(id) > 12 {
		return id[:12] + "..."
	}
	return id
}

// HandleNewCommand returns a new_session action. The TUI resets state.
func HandleNewCommand(args string) (agent.CommandResult, error) {
	ephemeral := ""
	if strings.TrimSpace(args) == "--ephemeral" {
		ephemeral = "--ephemeral"
	}
	return agent.CommandResult{
		Actions: []agent.CmdAction{{Type: "new_session", Value: ephemeral}},
	}, nil
}

// HandleResumeCommand resolves the argument to a session ID using the
// store, then returns a load_session action for the TUI to load.
func HandleResumeCommand(ctx context.Context, store agent.StoreProvider, args string) (agent.CommandResult, error) {
	if store == nil {
		return agent.CommandResult{}, fmt.Errorf("no store available")
	}
	arg := strings.TrimSpace(args)
	if arg == "" {
		return agent.CommandResult{}, fmt.Errorf("usage: /resume <# | id | label>")
	}
	// Build a session list for number-based resolution.
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return agent.CommandResult{}, fmt.Errorf("list sessions: %w", err)
	}
	// Strip leading # for line numbers.
	numStr := strings.TrimPrefix(arg, "#")
	if n, err := strconv.Atoi(numStr); err == nil && n >= 1 && n <= len(sessions) {
		return agent.CommandResult{
			Actions: []agent.CmdAction{{Type: "load_session", Value: sessions[n-1].ID}},
		}, nil
	}
	// Try exact ID match.
	for _, s := range sessions {
		if s.ID == arg {
			return agent.CommandResult{
				Actions: []agent.CmdAction{{Type: "load_session", Value: s.ID}},
			}, nil
		}
	}
	// Try case-insensitive label prefix match.
	lower := strings.ToLower(arg)
	for _, s := range sessions {
		if s.Label != "" && strings.HasPrefix(strings.ToLower(s.Label), lower) {
			return agent.CommandResult{
				Actions: []agent.CmdAction{{Type: "load_session", Value: s.ID}},
			}, nil
		}
	}
	// Fallback: try the store directly.
	if _, err := store.GetSession(ctx, arg); err == nil {
		return agent.CommandResult{
			Actions: []agent.CmdAction{{Type: "load_session", Value: arg}},
		}, nil
	}
	return agent.CommandResult{}, fmt.Errorf("session not found: %s", arg)
}

// HandleBranchCommand creates a child session and returns a branch_session
// action with the child ID. The TUI loads the ancestor messages.
func HandleBranchCommand(ctx context.Context, store agent.StoreProvider, parentID, args string) (agent.CommandResult, error) {
	if store == nil {
		return agent.CommandResult{}, fmt.Errorf("no store available")
	}
	if parentID == "" {
		return agent.CommandResult{}, fmt.Errorf("no current session to branch; /resume <id> first or pass one: /branch <id>")
	}
	if _, err := store.GetSession(ctx, parentID); err != nil {
		return agent.CommandResult{}, fmt.Errorf("branch: %w", err)
	}
	// Generate a new session ID.
	id, err := newSessionID()
	if err != nil {
		return agent.CommandResult{}, fmt.Errorf("branch: %w", err)
	}
	if err := store.CreateSession(ctx, id, parentID, ""); err != nil {
		return agent.CommandResult{}, fmt.Errorf("branch: %w", err)
	}
	return agent.CommandResult{
		Actions: []agent.CmdAction{{Type: "branch_session", Value: id}},
	}, nil
}

// HandleLabelCommand updates the session label in the store and returns
// a set_label action for the TUI to update its display.
func HandleLabelCommand(ctx context.Context, store agent.StoreProvider, sessionID, args string) (agent.CommandResult, error) {
	if store == nil {
		return agent.CommandResult{}, fmt.Errorf("no store available")
	}
	if sessionID == "" {
		return agent.CommandResult{}, fmt.Errorf("no current session to label")
	}
	label := strings.TrimSpace(args)
	if err := store.UpdateSession(ctx, sessionID, label); err != nil {
		return agent.CommandResult{}, fmt.Errorf("label: %w", err)
	}
	if label == "" {
		return agent.CommandResult{
			Text:    "[label cleared]",
			Actions: []agent.CmdAction{{Type: "set_label", Value: ""}},
		}, nil
	}
	return agent.CommandResult{
		Text:    "[label: " + label + "]",
		Actions: []agent.CmdAction{{Type: "set_label", Value: label}},
	}, nil
}

// HandleExportCommand reads messages from the store and writes them as
// JSONL to the given path. No TUI state needed.
func HandleExportCommand(ctx context.Context, store agent.StoreProvider, sessionID, args string) (agent.CommandResult, error) {
	if store == nil || sessionID == "" {
		return agent.CommandResult{}, fmt.Errorf("no active session to export")
	}
	messages, err := store.GetMessages(ctx, sessionID)
	if err != nil {
		return agent.CommandResult{}, fmt.Errorf("export: %w", err)
	}
	path := sessionID + ".jsonl"
	if p := strings.TrimSpace(args); p != "" {
		path = p
	}
	f, err := os.Create(path)
	if err != nil {
		return agent.CommandResult{}, fmt.Errorf("export: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, msg := range messages {
		role := messageRole(msg)
		content := messageContent(msg)
		if err := enc.Encode(map[string]string{"role": role, "content": content}); err != nil {
			return agent.CommandResult{}, fmt.Errorf("export: %w", err)
		}
	}
	return agent.CommandResult{Text: fmt.Sprintf("[exported %d messages to %s]", len(messages), path)}, nil
}

// newSessionID generates a 16-byte random hex session ID.
func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// messageRole returns the role string for a message.
func messageRole(m ai.Message) string {
	switch m.(type) {
	case ai.User:
		return "user"
	case ai.Assistant:
		return "assistant"
	case ai.System:
		return "system"
	case ai.ToolResult:
		return "tool"
	default:
		return "unknown"
	}
}

// messageContent extracts the text content from a message.
func messageContent(m ai.Message) string {
	switch v := m.(type) {
	case ai.User:
		return v.Content
	case ai.Assistant:
		return v.Content
	case ai.System:
		return v.Content
	case ai.ToolResult:
		return v.Content
	default:
		return ""
	}
}
