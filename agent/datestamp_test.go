package agent

import (
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// stampHistory builds the fold a run sees: earlier user messages, then the arriving one.
func stampHistory(contents ...string) []core.Message {
	out := make([]core.Message, 0, len(contents))
	for _, c := range contents {
		out = append(out, core.NewUser(c))
	}
	return out
}

// TestDateStampOncePerDay pins the ledger's core rule: the day's first user message
// carries the stamp, every later message that day rides unstamped, and a session that
// crosses midnight stamps the next day's first message.
func TestDateStampOncePerDay(t *testing.T) {
	d := NewDateStamper()
	first := core.NewUser("first")
	if got := d.Stamp("s1", []core.Message{first}, first, "2026-11-24"); got.Content != "[2026-11-24] first" {
		t.Fatalf("first message = %q, want the day-1 stamp", got.Content)
	}

	history := stampHistory("[2026-11-24] first", "second")
	second := history[len(history)-1].(core.User)
	if got := d.Stamp("s1", history, second, "2026-11-24"); got.Content != "second" {
		t.Errorf("second message the same day = %q, want it unstamped", got.Content)
	}

	history = stampHistory("[2026-11-24] first", "second", "third")
	third := history[len(history)-1].(core.User)
	if got := d.Stamp("s1", history, third, "2026-11-25"); got.Content != "[2026-11-25] third" {
		t.Errorf("first message of the next day = %q, want the next day's stamp", got.Content)
	}
}

// TestDateStampSeedsFromHistory pins the restart law: a ledger that has never seen the
// session reads the fold, so a day already stamped on the wire is not stamped twice.
func TestDateStampSeedsFromHistory(t *testing.T) {
	d := NewDateStamper()
	history := stampHistory("[2026-11-24] first", "second")
	second := history[len(history)-1].(core.User)
	if got := d.Stamp("s2", history, second, "2026-11-24"); got.Content != "second" {
		t.Errorf("seeded stamp = %q, want it left alone", got.Content)
	}
	history = stampHistory("[2026-11-24] first", "second", "third")
	third := history[len(history)-1].(core.User)
	if got := d.Stamp("s2", history, third, "2026-11-25"); got.Content != "[2026-11-25] third" {
		t.Errorf("next day's message = %q, want the next day's stamp", got.Content)
	}
}

// TestDateStampNeverRewritesHistory pins the append-only law: a message that already
// carries a stamp is returned untouched, whatever day it is now.
func TestDateStampNeverRewritesHistory(t *testing.T) {
	d := NewDateStamper()
	stamped := core.NewUser("[2026-11-24] already stamped")
	if got := d.Stamp("s3", nil, stamped, "2026-11-25"); got.Content != stamped.Content {
		t.Errorf("stamped message rewritten to %q", got.Content)
	}
}

// TestDateStampSessionlessReadsTheFold pins the sessionless rule: a run with no session
// keeps no ledger, so its own fold decides - a fold already carrying today's stamp is
// left alone.
func TestDateStampSessionlessReadsTheFold(t *testing.T) {
	d := NewDateStamper()
	history := stampHistory("[2026-11-24] first", "second")
	second := history[len(history)-1].(core.User)
	if got := d.Stamp("", history, second, "2026-11-24"); got.Content != "second" {
		t.Errorf("sessionless same-day message = %q, want it unstamped", got.Content)
	}
	fresh := core.NewUser("hello")
	if got := d.Stamp("", []core.Message{fresh}, fresh, "2026-11-24"); got.Content != "[2026-11-24] hello" {
		t.Errorf("sessionless fresh message = %q, want the day's stamp", got.Content)
	}
}

// TestUserDateStampOnlyReadsAStamp pins the parse: prose that opens with a bracket is not
// a stamp, so a message is never mistaken for history that is already stamped.
func TestUserDateStampOnlyReadsAStamp(t *testing.T) {
	for _, tc := range []struct {
		content string
		day     string
		ok      bool
	}{
		{"[2026-11-24] hi", "2026-11-24", true},
		{"[2026-11-24]", "2026-11-24", true},
		{"[not a date] hi", "", false},
		{"[2026-13-40] hi", "", false},
		{"no stamp", "", false},
		{"", "", false},
	} {
		day, ok := userDateStamp(core.User{Content: tc.content})
		if ok != tc.ok || day != tc.day {
			t.Errorf("userDateStamp(%q) = (%q, %v), want (%q, %v)", tc.content, day, ok, tc.day, tc.ok)
		}
	}
}
