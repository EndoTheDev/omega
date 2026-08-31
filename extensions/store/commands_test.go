package store

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// --- sessionDisplayName ---

func TestSessionDisplayName(t *testing.T) {
	cases := []struct {
		name  string
		label string
		id    string
		want  string
	}{
		{"label set returns label", "my-label", "abc123", "my-label"},
		{"empty label long id truncated", "", "abcdefghijklmnop", "abcdefghijkl..."},
		{"empty label short id returned as-is", "", "short", "short"},
		{"empty label 12-char id returned as-is (boundary)", "", "123456789012", "123456789012"},
		{"empty label 13-char id truncated (boundary)", "", "1234567890123", "123456789012..."},
		{"both empty returns empty", "", "", ""},
		{"label wins over long id", "winner", "abcdefghijklmnopqrstuvwxyz", "winner"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sessionDisplayName(c.label, c.id)
			if got != c.want {
				t.Fatalf("sessionDisplayName(%q, %q) = %q, want %q", c.label, c.id, got, c.want)
			}
		})
	}
}

// --- agent.FormatNumber ---

func TestFormatNumber(t *testing.T) {
	cases := []struct {
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
		{-1234, "-1,234"},
		{-1234567, "-1,234,567"},
		{1000000, "1,000,000"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			got := agent.FormatNumber(c.n)
			if got != c.want {
				t.Fatalf("agent.FormatNumber(%d) = %q, want %q", c.n, got, c.want)
			}
		})
	}
}

// --- newSessionID ---

func TestNewSessionID(t *testing.T) {
	id, err := agent.NewSessionID()
	if err != nil {
		t.Fatalf("newSessionID error: %v", err)
	}
	// 16 bytes -> 32 hex chars.
	if len(id) != 32 {
		t.Fatalf("len = %d, want 32", len(id))
	}
	// Must be valid lowercase hex.
	if _, err := hex.DecodeString(id); err != nil {
		t.Fatalf("not valid hex: %q (%v)", id, err)
	}

	// Uniqueness: two calls should not collide.
	id2, err := agent.NewSessionID()
	if err != nil {
		t.Fatalf("second newSessionID error: %v", err)
	}
	if id == id2 {
		t.Fatalf("two IDs collided: %q", id)
	}
}

// --- messageRole ---

func TestMessageRole(t *testing.T) {
	cases := []struct {
		name string
		m    ai.Message
		want string
	}{
		{"user", ai.User{Content: "hi"}, "user"},
		{"assistant", ai.Assistant{Content: "hi"}, "assistant"},
		{"system", ai.System{Content: "sys"}, "system"},
		{"tool result", ai.ToolResult{Content: "ok"}, "tool"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ai.MessageRole(c.m)
			if got != c.want {
				t.Fatalf("ai.MessageRole(%T) = %q, want %q", c.m, got, c.want)
			}
		})
	}

	// Unknown message type returns "unknown".
	t.Run("unknown", func(t *testing.T) {
		got := ai.MessageRole(ai.ModelChange{Model: "x"})
		if got != "unknown" {
			t.Fatalf("ai.MessageRole(unknown) = %q, want unknown", got)
		}
	})
}

// --- messageContent ---

func TestMessageContent(t *testing.T) {
	cases := []struct {
		name string
		m    ai.Message
		want string
	}{
		{"user", ai.User{Content: "hello"}, "hello"},
		{"assistant", ai.Assistant{Content: "world"}, "world"},
		{"system", ai.System{Content: "prompt"}, "prompt"},
		{"tool result", ai.ToolResult{Content: "result"}, "result"},
		{"empty user content", ai.User{Content: ""}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := agent.MessageText(c.m)
			if got != c.want {
				t.Fatalf("agent.MessageText(%T) = %q, want %q", c.m, got, c.want)
			}
		})
	}

	// Unknown message type returns empty string.
	t.Run("unknown", func(t *testing.T) {
		got := agent.MessageText(ai.ModelChange{Model: "x"})
		if got != "" {
			t.Fatalf("agent.MessageText(unknown) = %q, want empty", got)
		}
	})
}

// --- HandleNewCommand ---

func TestHandleNewCommand(t *testing.T) {
	t.Run("default no args", func(t *testing.T) {
		res, err := HandleNewCommand("")
		if err != nil {
			t.Fatalf("HandleNewCommand(\"\") error: %v", err)
		}
		if len(res.Actions) != 1 {
			t.Fatalf("actions len = %d, want 1", len(res.Actions))
		}
		act := res.Actions[0]
		if act.Type != "new_session" {
			t.Fatalf("action type = %q, want new_session", act.Type)
		}
		if act.Value != "" {
			t.Fatalf("action value = %q, want empty", act.Value)
		}
	})

	t.Run("ephemeral flag", func(t *testing.T) {
		res, err := HandleNewCommand("--ephemeral")
		if err != nil {
			t.Fatalf("HandleNewCommand(\"--ephemeral\") error: %v", err)
		}
		if len(res.Actions) != 1 {
			t.Fatalf("actions len = %d, want 1", len(res.Actions))
		}
		act := res.Actions[0]
		if act.Type != "new_session" {
			t.Fatalf("action type = %q, want new_session", act.Type)
		}
		if act.Value != "--ephemeral" {
			t.Fatalf("action value = %q, want --ephemeral", act.Value)
		}
	})

	t.Run("ephemeral with surrounding whitespace", func(t *testing.T) {
		res, err := HandleNewCommand("  --ephemeral  ")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if res.Actions[0].Value != "--ephemeral" {
			t.Fatalf("value = %q, want --ephemeral", res.Actions[0].Value)
		}
	})

	t.Run("non-ephemeral args ignored", func(t *testing.T) {
		res, err := HandleNewCommand("some other args")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if res.Actions[0].Value != "" {
			t.Fatalf("value = %q, want empty for non-ephemeral args", res.Actions[0].Value)
		}
	})
}

// --- agent.FormatInsights ---

func TestFormatInsightsPopulated(t *testing.T) {
	in := &agent.Insights{
		Period:         "30 days",
		PeriodStart:    "2026-08-01",
		PeriodEnd:      "2026-08-30",
		Days:           30,
		Sessions:       12,
		Messages:       345,
		UserMessages:   120,
		ToolCalls:      80,
		TotalTokens:    1234567,
		AvgSessionMsgs: 28.75,
		Tools: []agent.ToolStat{
			{Name: "shell.run", Count: 40},
			{Name: "files.read", Count: 20},
		},
		Daily: [7]agent.DayStat{
			{Day: "Mon", Count: 10, Bar: "█"},
			{Day: "Tue", Count: 5, Bar: "▌"},
		},
		NotableMsgs:   agent.NotableStat{Value: 99, Detail: "2026-08-15"},
		NotableTokens: agent.NotableStat{Value: 50000, Detail: "2026-08-15"},
		NotableTools:  agent.NotableStat{Value: 30, Detail: "2026-08-20"},
	}

	got := agent.FormatInsights(in)

	// Verify key elements are present.
	checks := []string{
		"omega insights — 30 days",
		"Period: 2026-08-01 — 2026-08-30",
		"Sessions:          12",
		"Messages:          345",
		"User messages:     120",
		"Tool calls:        80",
		"Tokens (est.):     1,234,567",
		"Avg msgs/session:  28.8",
		"Top Tools",
		"shell.run",
		"files.read",
		"Activity by Day",
		"Mon",
		"Tue",
		"Notable Sessions",
		"Most messages      99 msgs",
		"Most tokens        50,000",
		"Most tool calls    30",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("agent.FormatInsights output missing %q\ngot:\n%s", want, got)
		}
	}
}

func TestFormatInsightsEmpty(t *testing.T) {
	// Zero-value Insights: no tools, no notable sessions, no period start.
	in := &agent.Insights{}

	got := agent.FormatInsights(in)

	// Empty PeriodStart path uses the "Through:" line.
	if !strings.Contains(got, "Through: ") {
		t.Errorf("empty insights missing 'Through:' line\ngot:\n%s", got)
	}
	// Overview section present with zeros.
	if !strings.Contains(got, "Sessions:          0") {
		t.Errorf("empty insights missing Sessions: 0\ngot:\n%s", got)
	}
	// No Top Tools section when Tools is empty.
	if strings.Contains(got, "Top Tools") {
		t.Errorf("empty insights should not contain Top Tools\ngot:\n%s", got)
	}
	// No Notable Sessions section when all notable values are 0.
	if strings.Contains(got, "Notable Sessions") {
		t.Errorf("empty insights should not contain Notable Sessions\ngot:\n%s", got)
	}
	// Activity by Day is always present (renders 7 zero rows).
	if !strings.Contains(got, "Activity by Day") {
		t.Errorf("empty insights missing Activity by Day\ngot:\n%s", got)
	}
}

func TestFormatInsightsPeriodStartEmptyUsesThrough(t *testing.T) {
	in := &agent.Insights{
		Period:      "all time",
		PeriodEnd:   "2026-08-30",
		Sessions:    1,
		Messages:    1,
	}
	got := agent.FormatInsights(in)
	if !strings.Contains(got, "Through: 2026-08-30") {
		t.Errorf("missing 'Through: 2026-08-30' when PeriodStart empty\ngot:\n%s", got)
	}
	if strings.Contains(got, "Period:") {
		t.Errorf("should not contain 'Period:' line when PeriodStart empty\ngot:\n%s", got)
	}
}

func TestFormatInsightsToolPercentage(t *testing.T) {
	// 40 of 80 total tool calls -> 50.0%.
	in := &agent.Insights{
		Period:    "30 days",
		PeriodEnd: "2026-08-30",
		ToolCalls: 80,
		Tools: []agent.ToolStat{
			{Name: "shell.run", Count: 40},
		},
	}
	got := agent.FormatInsights(in)
	if !strings.Contains(got, "50.0%") {
		t.Errorf("missing 50.0%% tool percentage\ngot:\n%s", got)
	}
}

func TestFormatInsightsToolPercentageZeroToolCalls(t *testing.T) {
	// ToolCalls=0 should not divide-by-zero; percentage stays 0.0%.
	in := &agent.Insights{
		Period:    "30 days",
		PeriodEnd: "2026-08-30",
		ToolCalls: 0,
		Tools: []agent.ToolStat{
			{Name: "shell.run", Count: 5},
		},
	}
	got := agent.FormatInsights(in)
	if !strings.Contains(got, "0.0%") {
		t.Errorf("missing 0.0%% when ToolCalls=0\ngot:\n%s", got)
	}
}