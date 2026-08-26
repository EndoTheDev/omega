package main

import (
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{12, "12"},
		{123, "123"},
		{1234, "1,234"},
		{12345, "12,345"},
		{123456, "123,456"},
		{1234567, "1,234,567"},
		{1000000, "1,000,000"},
		{-1, "-1"},
		{-1234, "-1,234"},
	}
	for _, tt := range tests {
		if got := formatNumber(tt.n); got != tt.want {
			t.Errorf("formatNumber(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFormatInsights(t *testing.T) {
	in := &agent.Insights{
		Period:         "30 days",
		PeriodStart:    "2026-08-01",
		PeriodEnd:      "2026-08-31",
		Sessions:       42,
		Messages:       500,
		UserMessages:   200,
		ToolCalls:      300,
		TotalTokens:    1234567,
		AvgSessionMsgs: 11.9,
		Tools: []agent.ToolStat{
			{Name: "bash", Count: 150},
			{Name: "read_file", Count: 100},
		},
		Daily: [7]agent.DayStat{
			{Day: "Mon", Bar: "████", Count: 10},
			{Day: "Tue", Bar: "██", Count: 5},
		},
		NotableMsgs:   agent.NotableStat{Value: 99, Detail: "2026-08-15"},
		NotableTokens: agent.NotableStat{Value: 50000, Detail: "2026-08-20"},
		NotableTools:  agent.NotableStat{Value: 30, Detail: "2026-08-10"},
	}

	out := formatInsights(in)

	// Period header with range.
	if !strings.Contains(out, "omega insights — 30 days") {
		t.Errorf("missing period header in output:\n%s", out)
	}
	if !strings.Contains(out, "Period: 2026-08-01 — 2026-08-31") {
		t.Errorf("missing period range in output:\n%s", out)
	}

	// Overview numbers.
	if !strings.Contains(out, "Sessions:          42") {
		t.Errorf("missing session count:\n%s", out)
	}
	if !strings.Contains(out, "Tokens (est.):     1,234,567") {
		t.Errorf("missing formatted token count:\n%s", out)
	}
	if !strings.Contains(out, "Avg msgs/session:  11.9") {
		t.Errorf("missing avg session msgs:\n%s", out)
	}

	// Top Tools section.
	if !strings.Contains(out, "Top Tools") {
		t.Errorf("missing Top Tools section:\n%s", out)
	}
	if !strings.Contains(out, "bash") || !strings.Contains(out, "read_file") {
		t.Errorf("missing tool names:\n%s", out)
	}

	// Activity by Day.
	if !strings.Contains(out, "Activity by Day") {
		t.Errorf("missing Activity by Day section:\n%s", out)
	}
	if !strings.Contains(out, "Mon") || !strings.Contains(out, "Tue") {
		t.Errorf("missing day labels:\n%s", out)
	}

	// Notable Sessions.
	if !strings.Contains(out, "Notable Sessions") {
		t.Errorf("missing Notable Sessions section:\n%s", out)
	}
	if !strings.Contains(out, "Most messages") || !strings.Contains(out, "99 msgs") {
		t.Errorf("missing notable msgs:\n%s", out)
	}
	if !strings.Contains(out, "Most tokens") || !strings.Contains(out, "50,000") {
		t.Errorf("missing notable tokens:\n%s", out)
	}
	if !strings.Contains(out, "Most tool calls") || !strings.Contains(out, "30") {
		t.Errorf("missing notable tools:\n%s", out)
	}
}

func TestFormatInsightsThroughMode(t *testing.T) {
	// PeriodStart empty → "Through:" line instead of range.
	in := &agent.Insights{
		Period:      "all-time",
		PeriodEnd:   "2026-08-31",
		Sessions:    1,
		Messages:    1,
		TotalTokens: 100,
		Daily:       [7]agent.DayStat{{Day: "Wed", Bar: "█", Count: 1}},
	}

	out := formatInsights(in)

	if !strings.Contains(out, "Through: 2026-08-31") {
		t.Errorf("missing Through line:\n%s", out)
	}
	if strings.Contains(out, "Period:") {
		t.Errorf("should not have Period range when PeriodStart is empty:\n%s", out)
	}
}

func TestFormatInsightsNoNotable(t *testing.T) {
	// All NotableStats zero → section omitted.
	in := &agent.Insights{
		Period:    "7 days",
		Sessions:  5,
		Messages:  10,
	}

	out := formatInsights(in)

	if strings.Contains(out, "Notable Sessions") {
		t.Errorf("Notable Sessions should be omitted when all zero:\n%s", out)
	}
}

func TestFormatInsightsNoTools(t *testing.T) {
	// No tools → Top Tools section omitted.
	in := &agent.Insights{
		Period:    "7 days",
		Sessions:  5,
		Messages:  10,
	}

	out := formatInsights(in)

	if strings.Contains(out, "Top Tools") {
		t.Errorf("Top Tools should be omitted when empty:\n%s", out)
	}
}