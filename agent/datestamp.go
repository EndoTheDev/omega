package agent

import (
	"context"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// The day stamp: the date rides message CONTENT, not the system prompt. A prompt that
// carries "today" is a fresh prefix root every midnight, which costs every session its
// warm prefix cache exactly once a day; a stamp on the day's first user message is
// append-only history instead, so the prompt stays a pure function of the fold.

const (
	// dateStampLayout is the wire form of the stamp's date.
	dateStampLayout  = "2006-01-02"
	dateStampDateLen = len("2006-01-02")
	// dateStampPrefixLen is the shortest content a stamped message carries: "[date]".
	dateStampPrefixLen = dateStampDateLen + 2
	// dateStampLedgerCap bounds the per-session ledger. Past it, entries from previous
	// days drop: only today's date can suppress a stamp (a stale entry's day is stamped
	// on the next message regardless).
	dateStampLedgerCap = 4096
)

// dayStamps is the process's per-session stamp ledger. It is process state on purpose: a
// session can be served by more than one Agent in one process, and the ledger is a
// property of the SESSION, not of the object that happened to run it.
var dayStamps = NewDateStamper()

// DateStamper tracks, per session, the last date a user message was stamped with, so a
// session stamps once a day and every later message rides unstamped. The zero value is
// ready to use.
type DateStamper struct {
	mu   sync.Mutex
	last map[string]string
}

// NewDateStamper returns an empty ledger.
func NewDateStamper() *DateStamper {
	return &DateStamper{last: make(map[string]string)}
}

// Stamp returns msg carrying the day's [YYYY-MM-DD] prefix when sessionID has not stamped
// a user message on day yet, and msg unchanged otherwise. history is the fold the message
// rides: its newest stamp seeds a ledger that has never seen this session (a fresh
// process, a replayed transcript), so a day already stamped on the wire is not stamped a
// second time. A message that already carries a stamp is history being replayed (retry,
// fork, wake re-entry) and is never rewritten.
func (d *DateStamper) Stamp(sessionID string, history []core.Message, msg core.User, day string) core.User {
	if _, ok := userDateStamp(msg); ok {
		return msg
	}
	if sessionID == "" {
		// A sessionless run keeps no ledger - its fold is the whole state.
		if last, ok := latestStamp(history); ok && last == day {
			return msg
		}
		return stampMessage(msg, day)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.last == nil {
		d.last = make(map[string]string)
	}
	last, seen := d.last[sessionID]
	if !seen {
		last, _ = latestStamp(history)
	}
	if last == day {
		d.last[sessionID] = last
		return msg
	}
	d.last[sessionID] = day
	d.pruneLocked(day)
	return stampMessage(msg, day)
}

// pruneLocked drops every entry from a previous day once the ledger outgrows its cap.
func (d *DateStamper) pruneLocked(day string) {
	if len(d.last) <= dateStampLedgerCap {
		return
	}
	for id, stamped := range d.last {
		if stamped != day {
			delete(d.last, id)
		}
	}
}

// WithDateStampClock overrides the clock the day stamp reads. The stamp is the one
// self-moving byte a run writes, so tests (and replay tools) pin it per run instead of
// racing the wall clock.
func WithDateStampClock(ctx context.Context, now func() time.Time) context.Context {
	if now == nil {
		return ctx
	}
	return context.WithValue(ctx, dateStampClockKey{}, now)
}

// StampUserDay returns the user message to send AND persist: msg carrying the day's
// [YYYY-MM-DD] prefix when this session has not stamped a user message today, msg
// unchanged otherwise. wire is the conversation the message arrives into; it seeds the
// ledger on first sight of the session.
func StampUserDay(ctx context.Context, sessionID string, wire []core.Message, msg core.User) core.User {
	now := time.Now
	if c, ok := ctx.Value(dateStampClockKey{}).(func() time.Time); ok && c != nil {
		now = c
	}
	return dayStamps.Stamp(sessionID, wire, msg, now().Format(dateStampLayout))
}

// userDateStamp reports the date a user message carries at its head, if any. The head
// must parse as the stamp layout, so prose that happens to open with a bracket is not
// mistaken for a stamp.
func userDateStamp(u core.User) (string, bool) {
	if len(u.Content) < dateStampPrefixLen || u.Content[0] != '[' {
		return "", false
	}
	day := u.Content[1 : 1+dateStampDateLen]
	if _, err := time.Parse(dateStampLayout, day); err != nil {
		return "", false
	}
	rest := u.Content[1+dateStampDateLen:]
	if rest != "" && rest[0] != ']' {
		return "", false
	}
	return day, true
}

// stampMessage prefixes msg's content with the day.
func stampMessage(msg core.User, day string) core.User {
	head := "[" + day + "]"
	if msg.Content != "" {
		head += " "
	}
	msg.Content = head + msg.Content
	return msg
}

// latestStamp returns the date the newest stamped user message in history carries.
func latestStamp(history []core.Message) (string, bool) {
	for i := len(history) - 1; i >= 0; i-- {
		u, ok := history[i].(core.User)
		if !ok {
			continue
		}
		if day, ok := userDateStamp(u); ok {
			return day, true
		}
	}
	return "", false
}

// WithDayStampOff marks a run whose user messages are synthetic instruction text rather
// than conversation - a title request, say. The day stamp never lands on such a run.
func WithDayStampOff(ctx context.Context) context.Context {
	return context.WithValue(ctx, dayStampOffKey{}, true)
}

// DayStampOff reports whether the run is marked as not conversational.
func DayStampOff(ctx context.Context) bool {
	v, _ := ctx.Value(dayStampOffKey{}).(bool)
	return v
}

type dateStampClockKey struct{}

type dayStampOffKey struct{}
