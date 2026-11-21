package store

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// sessionDisplayName

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

// agent.FormatNumber

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

// newSessionID

func TestNewSessionID(t *testing.T) {
	id, err := agent.NewSessionID()
	if err != nil {
		t.Fatalf("newSessionID error: %v", err)
	}
	if len(id) != 32 {
		t.Fatalf("len = %d, want 32", len(id))
	}
	// Must be valid lowercase hex.
	if _, err := hex.DecodeString(id); err != nil {
		t.Fatalf("not valid hex: %q (%v)", id, err)
	}

	id2, err := agent.NewSessionID()
	if err != nil {
		t.Fatalf("second newSessionID error: %v", err)
	}
	if id == id2 {
		t.Fatalf("two IDs collided: %q", id)
	}
}

// messageRole

func TestMessageRole(t *testing.T) {
	cases := []struct {
		name string
		m    core.Message
		want string
	}{
		{"user", core.User{Content: "hi"}, "user"},
		{"assistant", core.Assistant{Content: "hi"}, "assistant"},
		{"system", core.System{Content: "sys"}, "system"},
		{"tool result", core.ToolResult{Content: "ok"}, "tool"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := core.MessageRole(c.m)
			if got != c.want {
				t.Fatalf("core.MessageRole(%T) = %q, want %q", c.m, got, c.want)
			}
		})
	}

	t.Run("unknown", func(t *testing.T) {
		got := core.MessageRole(core.ModelChange{Model: "x"})
		if got != "unknown" {
			t.Fatalf("core.MessageRole(unknown) = %q, want unknown", got)
		}
	})
}

// messageContent

func TestMessageContent(t *testing.T) {
	cases := []struct {
		name string
		m    core.Message
		want string
	}{
		{"user", core.User{Content: "hello"}, "hello"},
		{"assistant", core.Assistant{Content: "world"}, "world"},
		{"system", core.System{Content: "prompt"}, "prompt"},
		{"tool result", core.ToolResult{Content: "result"}, "result"},
		{"empty user content", core.User{Content: ""}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := agent.MessageText(c.m)
			if got != c.want {
				t.Fatalf("agent.MessageText(%T) = %q, want %q", c.m, got, c.want)
			}
		})
	}

	t.Run("unknown", func(t *testing.T) {
		got := agent.MessageText(core.ModelChange{Model: "x"})
		if got != "" {
			t.Fatalf("agent.MessageText(unknown) = %q, want empty", got)
		}
	})
}

// Session commands answer TEXT and declare no actions
//
// t_146d3133: new_session, load_session, branch_session and set_label had no executor on
// any surface - the dispatch relayed them, the webui's wire had no field for them, and a
// command whose Text was empty read as a silent no-op. The intents are gone; the store
// answers the real result, and this pin fails the day one comes back without an executor.
func TestSessionCommandsShipNoActions(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.CreateSession(ctx, "pin-1", "", "pin label"); err != nil {
		t.Fatalf("create: %v", err)
	}
	bound := core.WithCommandSession(ctx, "pin-1")
	cases := []struct{ name, args string }{
		{"/resume", "#1"},
		{"/resume", "pin-1"},
		{"/resume", "PIN"},
		{"/branch", ""},
		{"/label", "from the palette"},
		{"/label", ""},
	}
	for _, c := range cases {
		res, err := sessionCommandsThrough(t, bound, s, c.name, c.args)
		if err != nil {
			t.Fatalf("%s %q: %v", c.name, c.args, err)
		}
		if len(res.Actions) != 0 {
			t.Fatalf("%s %q declares %+v - nothing in this repo executes those types", c.name, c.args, res.Actions)
		}
		if strings.TrimSpace(res.Text) == "" {
			t.Fatalf("%s %q answered no text: the client has nothing to show", c.name, c.args)
		}
	}
}

// agent.FormatInsights

func TestFormatInsightsPopulated(t *testing.T) {
	in := &core.Insights{
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
		Tools: []core.ToolStat{
			{Name: "shell.run", Count: 40},
			{Name: "files.read", Count: 20},
		},
		Daily: [7]core.DayStat{
			{Day: "Mon", Count: 10, Bar: "█"},
			{Day: "Tue", Count: 5, Bar: "▌"},
		},
		NotableMsgs:   core.NotableStat{Value: 99, Detail: "2026-08-15"},
		NotableTokens: core.NotableStat{Value: 50000, Detail: "2026-08-15"},
		NotableTools:  core.NotableStat{Value: 30, Detail: "2026-08-20"},
	}

	got := agent.FormatInsights(in)

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
	in := &core.Insights{}

	got := agent.FormatInsights(in)

	// Empty PeriodStart path uses the "Through:" line.
	if !strings.Contains(got, "Through: ") {
		t.Errorf("empty insights missing 'Through:' line\ngot:\n%s", got)
	}
	if !strings.Contains(got, "Sessions:          0") {
		t.Errorf("empty insights missing Sessions: 0\ngot:\n%s", got)
	}
	if strings.Contains(got, "Top Tools") {
		t.Errorf("empty insights should not contain Top Tools\ngot:\n%s", got)
	}
	if strings.Contains(got, "Notable Sessions") {
		t.Errorf("empty insights should not contain Notable Sessions\ngot:\n%s", got)
	}
	if !strings.Contains(got, "Activity by Day") {
		t.Errorf("empty insights missing Activity by Day\ngot:\n%s", got)
	}
}

func TestFormatInsightsPeriodStartEmptyUsesThrough(t *testing.T) {
	in := &core.Insights{
		Period:    "all time",
		PeriodEnd: "2026-08-30",
		Sessions:  1,
		Messages:  1,
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
	in := &core.Insights{
		Period:    "30 days",
		PeriodEnd: "2026-08-30",
		ToolCalls: 80,
		Tools: []core.ToolStat{
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
	in := &core.Insights{
		Period:    "30 days",
		PeriodEnd: "2026-08-30",
		ToolCalls: 0,
		Tools: []core.ToolStat{
			{Name: "shell.run", Count: 5},
		},
	}
	got := agent.FormatInsights(in)
	if !strings.Contains(got, "0.0%") {
		t.Errorf("missing 0.0%% when ToolCalls=0\ngot:\n%s", got)
	}
}
