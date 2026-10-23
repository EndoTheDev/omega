package tui

import (
	"reflect"
	"testing"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"

	"github.com/charmbracelet/lipgloss"
)

// vo builds pick rows from labels: the test shorthand for
// []ai.VerdictOption (labels only, no description).
func vo(labels ...string) []ai.VerdictOption {
	out := make([]ai.VerdictOption, len(labels))
	for i, l := range labels {
		out[i] = ai.VerdictOption{Label: l}
	}
	return out
}

// testVerdictTheme is the banner test's styles: lipgloss renderers with
// no color set keep the assertions on plain text.
func testVerdictTheme() Theme {
	return Theme{
		Name:     "test",
		User:     lipgloss.NewStyle(),
		Thinking: lipgloss.NewStyle(),
		Tool:     lipgloss.NewStyle(),
		Info:     lipgloss.NewStyle(),
		Status:   lipgloss.NewStyle(),
		Match:    lipgloss.NewStyle(),
		Error:    lipgloss.NewStyle(),
	}
}

// verdictAnswerFromInput is the TUI shell's live busy-Enter parser (the
// mirror of the verdict module's /verdict command parser). It must agree
// on the option-number / comma-multi / y-n / skip / typed-text contract.
func TestVerdictAnswerFromInput(t *testing.T) {
	card := core.VerdictPrompt{ID: "c1", Title: "Pick", Options: vo("yes", "no")}

	cases := []struct {
		name    string
		card    core.VerdictPrompt
		input   string
		wantErr string
		want    core.Verdict
	}{
		{"number", card, "2", "", core.Verdict{OptionIndex: 2, Option: "no"}},
		{"y", card, "y", "", core.Verdict{OptionIndex: 1, Option: "yes"}},
		{"yes", card, "YES", "", core.Verdict{OptionIndex: 1, Option: "yes"}},
		{"n", card, "n", "", core.Verdict{OptionIndex: 2, Option: "no"}},
		{"skip", card, "skip", "", core.Verdict{Skipped: true}},
		{"dismiss alias", card, "dismiss", "", core.Verdict{Skipped: true}},
		{"s alias", card, "s", "", core.Verdict{Skipped: true}},
		{"out of range", card, "9", "option number", core.Verdict{}},
		{"free text rejected", card, "hello", "option number", core.Verdict{}},
		{"free text allowed", core.VerdictPrompt{ID: "c2", Title: "Name", AllowText: true}, "omega", "", core.Verdict{Text: "omega"}},
		{"empty on allowtext card", core.VerdictPrompt{ID: "c2", Title: "Name", AllowText: true}, "  ", "empty answer", core.Verdict{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := verdictAnswerFromInput(tc.card, tc.input)
			if tc.wantErr != "" {
				if err == nil || !containsSub(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("verdict = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestVerdictAnswerFromInputMulti(t *testing.T) {
	card := core.VerdictPrompt{ID: "cm", Title: "Multi", Options: vo("a", "b", "c"), Multi: true}
	cases := []struct {
		name    string
		input   string
		wantErr string
		want    core.Verdict
	}{
		{"comma list", "1,3", "", core.Verdict{OptionIndexes: []int{1, 3}, Options: []string{"a", "c"}}},
		{"lone number", "2", "", core.Verdict{OptionIndexes: []int{2}, Options: []string{"b"}}},
		{"out of range member", "1,9", "out of range", core.Verdict{}},
		{"y rejected", "y", "multi-select", core.Verdict{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := verdictAnswerFromInput(card, tc.input)
			if tc.wantErr != "" {
				if err == nil || !containsSub(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("verdict = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// verdictBanner parses the tool args (the card) for display; multi and
// allow_text cards show their contract line, described options carry
// their description lines.
func TestVerdictBannerShapes(t *testing.T) {
	args := map[string]any{
		"title": "Deploy?",
		"body":  "pick lanes",
		"options": []any{
			map[string]any{"label": "Ship", "description": "merge now"},
			map[string]any{"label": "Hold"},
		},
		"multi":      true,
		"allow_text": true,
	}
	out := verdictBanner(args, 80, testVerdictTheme())
	for _, want := range []string{
		"[question] Deploy?",
		"1. Ship",
		"merge now",
		"2. Hold",
		"multi-select",
		"answer:",
	} {
		if !containsSub(out, want) {
			t.Fatalf("banner missing %q:\n%s", want, out)
		}
	}

	single := verdictBanner(map[string]any{
		"title":      "ok?",
		"options":    []any{"yes", "no"},
		"allow_text": true,
	}, 80, testVerdictTheme())
	if containsSub(single, "multi-select") {
		t.Fatalf("single card banner shows multi-select:\n%s", single)
	}
	if !containsSub(single, "type your own answer") {
		t.Fatalf("allow_text banner missing the typed answer hint:\n%s", single)
	}
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
