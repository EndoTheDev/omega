package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/agent"
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
	return agent.CommandResult{Text: formatInsights(stats)}, nil
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

// formatInsights formats an Insights struct as a text report.
func formatInsights(in *agent.Insights) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("  omega insights — %s\n", in.Period))
	if in.PeriodStart != "" {
		sb.WriteString(fmt.Sprintf("  Period: %s — %s\n\n", in.PeriodStart, in.PeriodEnd))
	} else {
		sb.WriteString(fmt.Sprintf("  Through: %s\n\n", in.PeriodEnd))
	}

	sb.WriteString("  Overview\n")
	sb.WriteString("  ─────────────────────────────────────\n")
	sb.WriteString(fmt.Sprintf("  Sessions:          %d\n", in.Sessions))
	sb.WriteString(fmt.Sprintf("  Messages:          %d\n", in.Messages))
	sb.WriteString(fmt.Sprintf("  User messages:     %d\n", in.UserMessages))
	sb.WriteString(fmt.Sprintf("  Tool calls:        %d\n", in.ToolCalls))
	sb.WriteString(fmt.Sprintf("  Tokens (est.):     %s\n", formatNumber(in.TotalTokens)))
	sb.WriteString(fmt.Sprintf("  Avg msgs/session:  %.1f\n\n", in.AvgSessionMsgs))

	if len(in.Tools) > 0 {
		sb.WriteString("  Top Tools\n")
		sb.WriteString("  ─────────────────────────────────────\n")
		for _, t := range in.Tools {
			pct := 0.0
			if in.ToolCalls > 0 {
				pct = float64(t.Count) / float64(in.ToolCalls) * 100
			}
			sb.WriteString(fmt.Sprintf("  %-24s %5d  %5.1f%%\n", t.Name, t.Count, pct))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("  Activity by Day\n")
	sb.WriteString("  ─────────────────────────────────────\n")
	for _, d := range in.Daily {
		sb.WriteString(fmt.Sprintf("  %s  %-14s %d\n", d.Day, d.Bar, d.Count))
	}
	sb.WriteString("\n")

	if in.NotableMsgs.Value > 0 || in.NotableTokens.Value > 0 || in.NotableTools.Value > 0 {
		sb.WriteString("  Notable Sessions\n")
		sb.WriteString("  ─────────────────────────────────────\n")
		if in.NotableMsgs.Value > 0 {
			sb.WriteString(fmt.Sprintf("  Most messages      %d msgs     (%s)\n", in.NotableMsgs.Value, in.NotableMsgs.Detail))
		}
		if in.NotableTokens.Value > 0 {
			sb.WriteString(fmt.Sprintf("  Most tokens        %s  (%s)\n", formatNumber(in.NotableTokens.Value), in.NotableTokens.Detail))
		}
		if in.NotableTools.Value > 0 {
			sb.WriteString(fmt.Sprintf("  Most tool calls    %d          (%s)\n", in.NotableTools.Value, in.NotableTools.Detail))
		}
	}

	return sb.String()
}

// formatNumber adds thousands separators to an integer.
func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var sb strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			sb.WriteString(",")
		}
		sb.WriteRune(c)
	}
	return sb.String()
}
