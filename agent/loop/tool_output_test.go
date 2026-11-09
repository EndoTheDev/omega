package loop

// The tool-output bound's self-check (card t_a54ef2ce, the phantom
// results probe): the truncation note is the only thing a model has to
// judge whether a tool payload it received is complete. A note that
// misstates the dropped volume turns a full answer into an apparent
// fragment - the prod incident read "5336 bytes total" on a ~2.7 KB
// payload and confabulated the missing rows.

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTruncateToolOutputNoteNamesKeptAndTotal: the note must carry
// BOTH numbers so the model can size the loss, and the kept prefix
// must be exactly the bytes the note claims.
func TestTruncateToolOutputNoteNamesKeptAndTotal(t *testing.T) {
	content := strings.Repeat("a", 100)
	got := truncateToolOutput(content, 10)
	if !strings.HasPrefix(got, "aaaaaaaaaa") {
		t.Fatalf("kept prefix = %q, want the first 10 bytes", got[:12])
	}
	if want := "kept the first 10 bytes of 100"; !strings.Contains(got, want) {
		t.Errorf("note = %q, want it to contain %q", got, want)
	}
	if !strings.Contains(got, "the tail was dropped") {
		t.Errorf("note = %q, want it to say the tail was dropped", got)
	}
}

// TestTruncateToolOutputShortContentUntouched: a payload inside the
// bound rides byte-identical - the bound never rewrites a small
// result.
func TestTruncateToolOutputShortContentUntouched(t *testing.T) {
	for _, max := range []int{0, -1, 10, 1000} {
		if got := truncateToolOutput("short", max); got != "short" {
			t.Errorf("max=%d rewrote a short payload: %q", max, got)
		}
	}
}

// TestTruncateToolOutputRuneBoundary: the cut backs off a multi-byte
// rune instead of slicing through it. A byte cut in a path reaches the
// provider as U+FFFD in the middle of a filename.
func TestTruncateToolOutputRuneBoundary(t *testing.T) {
	// Each "→" is 3 bytes: cutting at any byte that is not a rune
	// start must step back to one.
	content := strings.Repeat("→", 40) // 120 bytes
	for max := 1; max < 60; max++ {
		got := truncateToolOutput(content, max)
		body := got[:strings.Index(got, "\n... [")]
		if !utf8.ValidString(body) {
			t.Fatalf("max=%d produced invalid UTF-8: %q", max, body)
		}
		if len(body) > max {
			t.Fatalf("max=%d kept %d bytes", max, len(body))
		}
		if want := len(body)/3*3; want != len(body) {
			t.Fatalf("max=%d kept a partial rune (%d bytes)", max, len(body))
		}
	}
}
