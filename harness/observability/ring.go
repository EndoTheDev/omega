// Package observability is the read-only observability surface.
package observability

import (
	"slices"
	"sync"
	"time"
)

// RingCap bounds the live ring: 1024 records. At ~200 bytes per record that is a ~200
// KiB ceiling on a process that runs for weeks - a live view, never a log.
const RingCap = 1024

// The level vocabulary. Three names, closed: a record's level is DERIVED, never
// configured - an error field or an IsError result makes an error, a run that ended
// without a clean stop (cancelled, max turns) makes a warning, everything else is info.
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

const PageMax = 1000

type Event struct {
	// Seq is the ring's monotonic insertion counter (1-based, never
	// reused), the client's stable ordering key across refetches.
	Seq uint64 `json:"seq"`
	Time string `json:"time"`
	Session string `json:"session,omitempty"`
	Type string `json:"type"`
	Level string `json:"level"`
	Tool string `json:"tool,omitempty"`
	Turn int `json:"turn,omitempty"`
	DurationMs int64 `json:"duration_ms,omitempty"`
	Summary string `json:"summary"`
	Error string `json:"error,omitempty"`
}

// Ring is the bounded, concurrency-safe record buffer. One write path (the recorder, on
// the run's forward goroutine), many read paths (the HTTP route, on request goroutines)
// - hence the mutex rather than an atomic index.
type Ring struct {
	mu   sync.Mutex
	buf  []Event
	next int    // next write slot
	seq  uint64 // last stamped sequence number
}

func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = RingCap
	}
	return &Ring{buf: make([]Event, capacity)}
}

func (r *Ring) Capacity() int { return len(r.buf) }

func (r *Ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int(min(r.seq, uint64(len(r.buf))))
}

// Append stores one record, stamping its sequence number and time when the caller. A
// full ring overwrites its OLDEST slot: the newest records always win, which is the
// only sane policy for a live view.
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

// Page returns up to limit records matching the filters, oldest-first. An unrecognized
// level matches nothing - the route validates the parameter first, so a silent empty
// page can only come from a caller that bypassed it.
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
