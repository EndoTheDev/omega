package verdict

import (
	"reflect"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// parseTUIAnswer is the /verdict command's typed-answer parser (the
// live busy-Enter path in the TUI shell uses the mirror helper in
// modules/tui). Both must agree on the option-number / comma-multi /
// y-n / skip / free-text contract, so this pins the shared shape.
func TestParseTUIAnswer(t *testing.T) {
	card := agent.VerdictPrompt{ID: "c1", Title: "Pick", Options: vo("yes", "no")}

	cases := []struct {
		name    string
		card    agent.VerdictPrompt
		tokens  []string
		wantErr string
		want    agent.Verdict
	}{
		{"pick by number", card, []string{"2"}, "", agent.Verdict{OptionIndex: 2, Option: "no"}},
		{"y maps to option 1", card, []string{"y"}, "", agent.Verdict{OptionIndex: 1, Option: "yes"}},
		{"n maps to option 2", card, []string{"n"}, "", agent.Verdict{OptionIndex: 2, Option: "no"}},
		{"skip", card, []string{"skip"}, "", agent.Verdict{Skipped: true}},
		{"out of range", card, []string{"3"}, "out of range", agent.Verdict{}},
		{"free text rejected on pick card", card, []string{"hello"}, "option number", agent.Verdict{}},
		{"empty", card, []string{}, "empty answer", agent.Verdict{}},
		{"free text allowed", agent.VerdictPrompt{ID: "c2", Title: "Name", AllowText: true}, []string{"omega"}, "", agent.Verdict{Text: "omega"}},
		{"free text rejoins tokens", agent.VerdictPrompt{ID: "c2", Title: "Name", AllowText: true}, []string{"two", "words"}, "", agent.Verdict{Text: "two words"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTUIAnswer(tc.card, tc.tokens)
			if tc.wantErr != "" {
				if err == nil || !containsStr(err.Error(), tc.wantErr) {
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

func TestParseTUIAnswerYNWithNoOptions(t *testing.T) {
	card := agent.VerdictPrompt{ID: "c1", Title: "Name", AllowText: true}
	if _, err := parseTUIAnswer(card, []string{"y"}); err == nil {
		t.Fatal("y on a no-options card must error")
	}
}

func TestParseTUIAnswerMulti(t *testing.T) {
	card := agent.VerdictPrompt{ID: "cm", Title: "Multi", Options: vo("a", "b", "c"), Multi: true}
	cases := []struct {
		name    string
		tokens  []string
		wantErr string
		want    agent.Verdict
	}{
		{"comma list", []string{"1,3"}, "", agent.Verdict{OptionIndexes: []int{1, 3}, Options: []string{"a", "c"}}},
		{"lone number is a one-row multi pick", []string{"2"}, "", agent.Verdict{OptionIndexes: []int{2}, Options: []string{"b"}}},
		{"out of range member", []string{"1,9"}, "out of range", agent.Verdict{}},
		{"y is ambiguous on multi", []string{"y"}, "multi-select", agent.Verdict{}},
		{"n is ambiguous on multi", []string{"n"}, "multi-select", agent.Verdict{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTUIAnswer(card, tc.tokens)
			if tc.wantErr != "" {
				if err == nil || !containsStr(err.Error(), tc.wantErr) {
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
