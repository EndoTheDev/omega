package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// HandleSessionsCommand lists all sessions with message counts.
func HandleSessionsCommand(ctx context.Context, store agent.StoreProvider, args string) (core.CommandResult, error) {
	if store == nil {
		return core.CommandResult{}, fmt.Errorf("no store available")
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return core.CommandResult{}, fmt.Errorf("list sessions: %w", err)
	}
	if len(sessions) == 0 {
		return core.CommandResult{Text: "[no sessions yet]"}, nil
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
	return core.CommandResult{Text: sb.String()}, nil
}

// HandleTreeCommand renders the session tree.
func HandleTreeCommand(ctx context.Context, store agent.StoreProvider) (core.CommandResult, error) {
	if store == nil {
		return core.CommandResult{}, fmt.Errorf("no store available")
	}
	roots, err := store.GetSessionTree(ctx)
	if err != nil {
		return core.CommandResult{}, fmt.Errorf("tree: %w", err)
	}
	if len(roots) == 0 {
		return core.CommandResult{Text: "[no sessions yet]"}, nil
	}
	type row struct {
		name  string
		count int
		id    string
		glyph string
	}
	var rows []row
	var flatten func(node *core.SessionNode, depth int, last bool)
	flatten = func(node *core.SessionNode, depth int, last bool) {
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
	return core.CommandResult{Text: sb.String()}, nil
}

// HandleSearchCommand runs an FTS5 search across session messages.
func HandleSearchCommand(ctx context.Context, store agent.StoreProvider, args string) (core.CommandResult, error) {
	if store == nil {
		return core.CommandResult{}, fmt.Errorf("no session store available")
	}
	query := strings.TrimSpace(args)
	if query == "" {
		return core.CommandResult{}, fmt.Errorf("usage: /search <query>")
	}
	results, err := store.SearchMessages(ctx, query)
	if err != nil {
		return core.CommandResult{}, fmt.Errorf("search: %w", err)
	}
	if len(results) == 0 {
		return core.CommandResult{Text: "[no results]"}, nil
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
	return core.CommandResult{Text: sb.String()}, nil
}

// HandleInsightsCommand computes cross-session usage analytics.
func HandleInsightsCommand(ctx context.Context, store agent.StoreProvider, args string) (core.CommandResult, error) {
	if store == nil {
		return core.CommandResult{}, fmt.Errorf("no session store available")
	}
	days := 30
	if d, err := strconv.Atoi(strings.TrimSpace(args)); err == nil && d >= 0 {
		days = d
	}
	stats, err := store.ComputeInsights(ctx, days)
	if err != nil {
		return core.CommandResult{}, fmt.Errorf("insights: %w", err)
	}
	if stats.Sessions == 0 {
		return core.CommandResult{Text: fmt.Sprintf("[no sessions in the last %d days]", days)}, nil
	}
	return core.CommandResult{Text: agent.FormatInsights(stats)}, nil
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

// HandleResumeCommand resolves the argument to a session ID using the store.
//
// The answer is TEXT, not a CmdAction: `load_session` had no executor on any surface
// (the webui opens a tab itself, the terminal loop switches with /session), so the wire
// shipped an intent nobody ran and the command read as a no-op. The resolve is the real
// work, and the line names what it resolved.
func HandleResumeCommand(ctx context.Context, store agent.StoreProvider, args string) (core.CommandResult, error) {
	if store == nil {
		return core.CommandResult{}, fmt.Errorf("no store available")
	}
	arg := strings.TrimSpace(args)
	if arg == "" {
		return core.CommandResult{}, fmt.Errorf("usage: /resume <# | id | label>")
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return core.CommandResult{}, fmt.Errorf("list sessions: %w", err)
	}
	resolved := func(s core.Session) (core.CommandResult, error) {
		name := s.ID
		if s.Label != "" {
			name = fmt.Sprintf("%s (%s)", s.Label, s.ID)
		}
		return core.CommandResult{Text: "[resume: " + name + "]"}, nil
	}
	numStr := strings.TrimPrefix(arg, "#")
	if n, err := strconv.Atoi(numStr); err == nil && n >= 1 && n <= len(sessions) {
		return resolved(sessions[n-1])
	}
	// Try exact ID match.
	for _, s := range sessions {
		if s.ID == arg {
			return resolved(s)
		}
	}
	lower := strings.ToLower(arg)
	for _, s := range sessions {
		if s.Label != "" && strings.HasPrefix(strings.ToLower(s.Label), lower) {
			return resolved(s)
		}
	}
	if s, err := store.GetSession(ctx, arg); err == nil {
		return resolved(s)
	}
	return core.CommandResult{}, fmt.Errorf("session not found: %s", arg)
}

// HandleBranchCommand creates a child session and answers with what it created.
//
// Same law as /resume: `branch_session` had no executor, so it is gone. The child row is
// the real work and the line names both ends - a surface that can open a tab (the webui)
// opens it through its own REST parity POST /sessions/{id}/branch instead.
func HandleBranchCommand(ctx context.Context, store agent.StoreProvider, parentID, args string) (core.CommandResult, error) {
	if store == nil {
		return core.CommandResult{}, fmt.Errorf("no store available")
	}
	if parentID == "" {
		return core.CommandResult{}, fmt.Errorf("no current session to branch; /resume <id> first or pass one: /branch <id>")
	}
	if _, err := store.GetSession(ctx, parentID); err != nil {
		return core.CommandResult{}, fmt.Errorf("branch: %w", err)
	}
	// Generate a new session ID.
	id, err := agent.NewSessionID()
	if err != nil {
		return core.CommandResult{}, fmt.Errorf("branch: %w", err)
	}
	if err := store.CreateSession(ctx, id, parentID, ""); err != nil {
		return core.CommandResult{}, fmt.Errorf("branch: %w", err)
	}
	return core.CommandResult{
		Text: fmt.Sprintf("[branched %s from %s]", id, parentID),
	}, nil
}

// HandleLabelCommand updates the session label in the store and answers with what it set.
//
// `set_label` had no executor (the webui writes the row through PUT /sessions/{id}), so
// the action is gone and the store's own write is the whole result.
func HandleLabelCommand(ctx context.Context, store agent.StoreProvider, sessionID, args string) (core.CommandResult, error) {
	if store == nil {
		return core.CommandResult{}, fmt.Errorf("no store available")
	}
	if sessionID == "" {
		return core.CommandResult{}, fmt.Errorf("no current session to label")
	}
	label := strings.TrimSpace(args)
	if err := store.UpdateSession(ctx, sessionID, label); err != nil {
		return core.CommandResult{}, fmt.Errorf("label: %w", err)
	}
	if label == "" {
		return core.CommandResult{Text: "[label cleared]"}, nil
	}
	return core.CommandResult{Text: "[label: " + label + "]"}, nil
}

// HandleExportCommand reads messages from the store and writes them as JSONL.
func HandleExportCommand(ctx context.Context, store agent.StoreProvider, sessionID, args string) (core.CommandResult, error) {
	if store == nil || sessionID == "" {
		return core.CommandResult{}, fmt.Errorf("no active session to export")
	}
	messages, err := store.GetMessages(ctx, sessionID)
	if err != nil {
		return core.CommandResult{}, fmt.Errorf("export: %w", err)
	}
	path := sessionID + ".jsonl"
	if p := strings.TrimSpace(args); p != "" {
		path = p
	}
	f, err := os.Create(path)
	if err != nil {
		return core.CommandResult{}, fmt.Errorf("export: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, msg := range messages {
		role := core.MessageRole(msg)
		content := agent.MessageText(msg)
		if err := enc.Encode(map[string]string{"role": role, "content": content}); err != nil {
			return core.CommandResult{}, fmt.Errorf("export: %w", err)
		}
	}
	return core.CommandResult{Text: fmt.Sprintf("[exported %d messages to %s]", len(messages), path)}, nil
}
