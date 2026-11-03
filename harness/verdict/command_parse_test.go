package verdict

import (
	"reflect"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// parseVerdictAnswer is the /verdict command's typed-answer parser (the
// live busy-Enter path in the TUI shell uses the mirror helper in
// tui). Both must agree on the option-number / comma-multi /
// y-n / skip / free-text contract, so this pins the shared shape.
func TestParseTUIAnswer(t *testing.T) {
	card := core.QuestionPrompt{ID: "c1", Title: "Pick", Options: vo("yes", "no")}

	cases := []struct {
		name    string
		card    core.QuestionPrompt
		tokens  []string
		wantErr string
		want    core.QuestionAnswer
	}{
		{"pick by number", card, []string{"2"}, "", core.QuestionAnswer{OptionIndex: 2, Option: "no"}},
		{"y maps to option 1", card, []string{"y"}, "", core.QuestionAnswer{OptionIndex: 1, Option: "yes"}},
		{"n maps to option 2", card, []string{"n"}, "", core.QuestionAnswer{OptionIndex: 2, Option: "no"}},
		{"skip", card, []string{"skip"}, "", core.QuestionAnswer{Skipped: true}},
		{"out of range", card, []string{"3"}, "out of range", core.QuestionAnswer{}},
		{"free text rejected on pick card", card, []string{"hello"}, "option number", core.QuestionAnswer{}},
		{"empty", card, []string{}, "empty answer", core.QuestionAnswer{}},
		{"free text allowed", core.QuestionPrompt{ID: "c2", Title: "Name", AllowText: true}, []string{"omega"}, "", core.QuestionAnswer{Text: "omega"}},
		{"free text rejoins tokens", core.QuestionPrompt{ID: "c2", Title: "Name", AllowText: true}, []string{"two", "words"}, "", core.QuestionAnswer{Text: "two words"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVerdictAnswer(tc.card, tc.tokens)
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
	card := core.QuestionPrompt{ID: "c1", Title: "Name", AllowText: true}
	if _, err := parseVerdictAnswer(card, []string{"y"}); err == nil {
		t.Fatal("y on a no-options card must error")
	}
}

func TestParseTUIAnswerMulti(t *testing.T) {
	card := core.QuestionPrompt{ID: "cm", Title: "Multi", Options: vo("a", "b", "c"), Multi: true}
	cases := []struct {
		name    string
		tokens  []string
		wantErr string
		want    core.QuestionAnswer
	}{
		{"comma list", []string{"1,3"}, "", core.QuestionAnswer{OptionIndexes: []int{1, 3}, Options: []string{"a", "c"}}},
		{"lone number is a one-row multi pick", []string{"2"}, "", core.QuestionAnswer{OptionIndexes: []int{2}, Options: []string{"b"}}},
		{"out of range member", []string{"1,9"}, "out of range", core.QuestionAnswer{}},
		{"y is ambiguous on multi", []string{"y"}, "multi-select", core.QuestionAnswer{}},
		{"n is ambiguous on multi", []string{"n"}, "multi-select", core.QuestionAnswer{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVerdictAnswer(card, tc.tokens)
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
