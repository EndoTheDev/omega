// Package observability is the read-only observability surface: a
// bounded in-memory ring of STRUCTURED run events (fed from the run
// event spine) plus a bounded tail of the on-disk log - the two reads
// behind the webui logging tab (card t_8de82cff, the engine half of the
// observability arc; the tab itself is the child card).
//
// Two sources, deliberately. The ring is what the RUNNING process
// knows: turn boundaries, tool calls with their durations, stream and
// run errors, session-attributed. The log tail is what the process
// WROTE: history across restarts, and the only view of a run whose
// process is gone. The ring is the primary view; the tail is the
// secondary one behind a toggle (ruling 1, 2026-11-10).
//
// Nothing leaves the process: no OTLP, no exporter, no telemetry
// dependency. OTel is a FUTURE concern (ruling 2) and
// TestNoTelemetryDependency holds that door shut.
package observability

import (
	"slices"
	"sync"
	"time"
)

// RingCap bounds the live ring: 1024 records. Sizing rationale - the
// recorder drops token-level stream chunks, so one busiest-case turn
// contributes roughly forty rows (turn start + end, a call + result per
// tool, one stream end per provider call): the cap holds about twenty
// five full turns of the loudest run, or several ordinary runs, which
// is what a tab opened mid-session needs to reconstruct the picture.
// At ~200 bytes per record that is a ~200 KiB ceiling on a process that
// runs for weeks - a live view, never a log. The disk file is the
// durable side of the same picture.
const RingCap = 1024

// The level vocabulary. Three names, closed: a record's level is
// DERIVED, never configured - an error field or an IsError result makes
// an error, a run that ended without a clean stop (cancelled, max
// turns) makes a warning, everything else is info.
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// PageMax caps one page read (?limit=). The ring itself is bounded at
// RingCap, so a page larger than this is a client asking to re-render
// the whole history in one frame - refuse the shape rather than serve a
// megabyte to a browser tab.
const PageMax = 1000

// Event is one observability record: the structured row the tab renders.
// Everything here is derived from an existing spine event - no new event
// type was invented for this surface (the probe report on the card).
type Event struct {
	// Seq is the ring's monotonic insertion counter (1-based, never
	// reused), the client's stable ordering key across refetches.
	Seq uint64 `json:"seq"`
	// Time is RFC3339Nano UTC, stamped at observation: the spine's
	// events carry no timestamp of their own.
	Time string `json:"time"`
	// Session is the run's conversation id; empty for a sessionless
	// run (one-shot CLI, tests).
	Session string `json:"session,omitempty"`
	// Type is the spine vocabulary, lowercased: agent_start,
	// turn_start, turn_end, tool_call, tool_result, stream_end,
	// agent_end.
	Type string `json:"type"`
	// Level is one of info/warn/error.
	Level string `json:"level"`
	// Tool names the tool for tool_call/tool_result rows.
	Tool string `json:"tool,omitempty"`
	// Turn is the loop's turn number where the event carries one.
	Turn int `json:"turn,omitempty"`
	// DurationMs is the derived elapsed time: a turn's start->end, or a
	// tool call's call->result. Absent when nothing could be paired.
	DurationMs int64 `json:"duration_ms,omitempty"`
	// Summary is the one-line human text the row shows.
	Summary string `json:"summary"`
	// Error carries the failure text when the level says error.
	Error string `json:"error,omitempty"`
}

// Ring is the bounded, concurrency-safe record buffer. One write path
// (the recorder, on the run's forward goroutine), many read paths (the
// HTTP route, on request goroutines) - hence the mutex rather than an
// atomic index.
type Ring struct {
	mu   sync.Mutex
	buf  []Event
	next int    // next write slot
	seq  uint64 // last stamped sequence number
}

// NewRing returns a ring of the given capacity (RingCap when <= 0).
func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = RingCap
	}
	return &Ring{buf: make([]Event, capacity)}
}

// Capacity reports the ring's fixed size.
func (r *Ring) Capacity() int { return len(r.buf) }

// Len reports how many records are buffered right now.
func (r *Ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int(min(r.seq, uint64(len(r.buf))))
}

// Append stores one record, stamping its sequence number and time when
// the caller left them zero. A full ring overwrites its OLDEST slot:
// the newest records always win, which is the only sane policy for a
// live view.
func (r *Ring) Append(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	e.Seq = r.seq
	if e.Time == "" {
		e.Time = time.Now().UTC().Format(time.RFC3339Nano)
	}
	r.buf[r.next] = e
	r.next = (r.next + 1) % len(r.buf)
}

// snapshot returns the buffered records oldest-first.
func (r *Ring) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.buf)
	count := int(min(r.seq, uint64(n)))
	out := make([]Event, 0, count)
	start := (r.next - count + n) % n
	for i := 0; i < count; i++ {
		out = append(out, r.buf[(start+i)%n])
	}
	return out
}

// Page returns up to limit records matching the filters, oldest-first:
// the NEWEST limit matches are selected, then ordered for reading, so a
// bounded page always ends at the most recent record.
//
// session scopes to one conversation ("" = every session); level is one
// of "", "all", "info", "warn" (warn + error) or "error" (errors only).
// An unrecognized level matches nothing - the route validates the
// parameter first, so a silent empty page can only come from a caller
// that bypassed it.
func (r *Ring) Page(session, level string, limit int) []Event {
	if limit <= 0 || limit > PageMax {
		limit = PageMax
	}
	all := r.snapshot()
	out := make([]Event, 0, min(limit, len(all)))
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		if session != "" && all[i].Session != session {
			continue
		}
		if !levelMatches(level, all[i].Level) {
			continue
		}
		out = append(out, all[i])
	}
	slices.Reverse(out)
	return out
}

// levelMatches applies the level filter. "warn" is a floor (warn and
// above), which is what an errors-plus-warnings view means; "info" and
// "all" are the unfiltered names.
func levelMatches(want, got string) bool {
	switch want {
	case "", "all", "info":
		return true
	case "warn":
		return got == LevelWarn || got == LevelError
	case "error":
		return got == LevelError
	}
	return false
}
