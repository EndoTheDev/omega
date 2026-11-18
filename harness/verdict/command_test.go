package verdict

// The /verdict command's name law. The command seam's chain-on-miss wrapper falls
// through only on an error, so a nil error for a foreign name swallows every command
// registered below this capsule in the chain.

import (
	"context"
	"strings"
	"testing"
)

func TestVerdictCommandAnswersOnlyItsOwnName(t *testing.T) {
	h := verdictHandler(NewPlugin())

	for _, foreign := range []string{"/models", "/cron", "/logs", "/board"} {
		res, err := h(context.Background(), foreign, "")
		if err == nil {
			t.Fatalf("%s answered %q with a nil error - the chain swallow", foreign, res.Text)
		}
		if strings.Contains(err.Error(), "verdicts seam") {
			t.Fatalf("%s was read as a /verdict request (%v) - the name check must come first", foreign, err)
		}
	}

	// /verdict with no hub is the capsule's own refusal: an error, and
	// its own error, never a miss.
	if _, err := h(context.Background(), "/verdict", ""); err == nil ||
		!strings.Contains(err.Error(), "verdicts seam") {
		t.Fatalf("/verdict = %v, want the unmounted-seam refusal", err)
	}
}
