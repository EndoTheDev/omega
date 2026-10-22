package tui

import (
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// verdictAnswerFromInput is the TUI shell's live busy-Enter parser (the
// mirror of the verdict module's /verdict command parser). It must agree
// on the option-number / y-n / skip / free-text contract.
func TestVerdictAnswerFromInput(t *testing.T) {
	card := agent.VerdictPrompt{ID: "c1", Title: "Pick", Options: []string{"yes", "no"}}

	cases := []struct {
		name    string
		card    agent.VerdictPrompt
		input   string
		wantErr string
		want    agent.Verdict
	}{
		{"number", card, "2", "", agent.Verdict{OptionIndex: 2, Option: "no"}},
		{"y", card, "y", "", agent.Verdict{OptionIndex: 1, Option: "yes"}},
		{"yes", card, "YES", "", agent.Verdict{OptionIndex: 1, Option: "yes"}},
		{"n", card, "n", "", agent.Verdict{OptionIndex: 2, Option: "no"}},
		{"skip", card, "skip", "", agent.Verdict{Skipped: true}},
		{"dismiss alias", card, "dismiss", "", agent.Verdict{Skipped: true}},
		{"s alias", card, "s", "", agent.Verdict{Skipped: true}},
		{"out of range", card, "9", "option number", agent.Verdict{}},
		{"free text rejected", card, "hello", "option number", agent.Verdict{}},
		{"free text allowed", agent.VerdictPrompt{ID: "c2", Title: "Name", FreeText: true}, "omega", "", agent.Verdict{Text: "omega"}},
		{"empty on freetext card", agent.VerdictPrompt{ID: "c2", Title: "Name", FreeText: true}, "  ", "empty answer", agent.Verdict{}},
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
			if got != tc.want {
				t.Fatalf("verdict = %+v, want %+v", got, tc.want)
			}
		})
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