package agent

import (
	"fmt"
	"strings"
)

// FormatInsights renders an Insights struct as a plain-text report.
// Shared by the CLI (omega insights, stdout) and the TUI (/insights
// slash command, transcript).
func FormatInsights(in *Insights) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("  omega insights — %s\n", in.Period))
	if in.PeriodStart != "" {
		sb.WriteString(fmt.Sprintf("  Period: %s — %s\n\n", in.PeriodStart, in.PeriodEnd))
	} else {
		sb.WriteString(fmt.Sprintf("  Through: %s\n\n", in.PeriodEnd))
	}

	// Overview
	sb.WriteString("  Overview\n")
	sb.WriteString("  ─────────────────────────────────────\n")
	sb.WriteString(fmt.Sprintf("  Sessions:          %d\n", in.Sessions))
	sb.WriteString(fmt.Sprintf("  Messages:          %d\n", in.Messages))
	sb.WriteString(fmt.Sprintf("  User messages:     %d\n", in.UserMessages))
	sb.WriteString(fmt.Sprintf("  Tool calls:        %d\n", in.ToolCalls))
	sb.WriteString(fmt.Sprintf("  Tokens (est.):     %s\n", FormatNumber(in.TotalTokens)))
	sb.WriteString(fmt.Sprintf("  Avg msgs/session:  %.1f\n\n", in.AvgSessionMsgs))

	// Top Tools
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

	// Activity by Day
	sb.WriteString("  Activity by Day\n")
	sb.WriteString("  ─────────────────────────────────────\n")
	for _, d := range in.Daily {
		sb.WriteString(fmt.Sprintf("  %s  %-14s %d\n", d.Day, d.Bar, d.Count))
	}
	sb.WriteString("\n")

	// Notable Sessions
	if in.NotableMsgs.Value > 0 || in.NotableTokens.Value > 0 || in.NotableTools.Value > 0 {
		sb.WriteString("  Notable Sessions\n")
		sb.WriteString("  ─────────────────────────────────────\n")
		if in.NotableMsgs.Value > 0 {
			sb.WriteString(fmt.Sprintf("  Most messages      %d msgs     (%s)\n", in.NotableMsgs.Value, in.NotableMsgs.Detail))
		}
		if in.NotableTokens.Value > 0 {
			sb.WriteString(fmt.Sprintf("  Most tokens        %s  (%s)\n", FormatNumber(in.NotableTokens.Value), in.NotableTokens.Detail))
		}
		if in.NotableTools.Value > 0 {
			sb.WriteString(fmt.Sprintf("  Most tool calls    %d          (%s)\n", in.NotableTools.Value, in.NotableTools.Detail))
		}
	}

	return sb.String()
}

// FormatNumber adds thousands separators to an integer.
func FormatNumber(n int) string {
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
