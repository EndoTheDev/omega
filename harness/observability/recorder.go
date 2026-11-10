package observability

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// Derivations, stated plainly because the spine does not carry them
// (the card's probe report):
//
//   - TIME: no spine event carries a timestamp. The recorder stamps
//     each record at observation.
//   - DURATION: derived by PAIRING, never measured. A turn's duration is
//     TurnStart(now) -> TurnEnd for the same (session, turn); a tool
//     call's duration is StreamEvent{ToolCallEvent}(now) ->
//     ToolResultEvent for the same (session, tool_call_id). A pair whose
//     start was missed (a ring started mid-run, a result the loop
//     emitted without the matching call) yields the event with no
//     duration rather than a fabricated one.
//   - LEVEL: derived from what already exists. StreamEnd.Error and
//     AgentEnd.Error make an error; a ToolResult.IsError makes an error;
//     a run that ended without a clean stop (cancelled, max_turns) and a
//     stream cut by the token ceiling (length) make a warning.
//   - SESSION: the one thing the event rail genuinely lacks. The plain
//     observer carries no session (the id rides the run context), so
//     this capsule subscribes through core.SessionEventObserver, the
//     additive session-attributed shape the agent's forward goroutine
//     feeds (card t_8de82cff, core/events.go).
//
// Token-level stream chunks (ThinkingChunk, ResponseChunk) are dropped
// on purpose: one record per delta would drown every structural row the
// tab exists to show, and the chat transcript already renders them.

// callTrackCap / callTrackTTL bound the in-flight pairing tables. A tool
// result normally arrives within the call's own lifetime; an entry that
// never pairs (a cancelled run, a crashing tool) must not accumulate
// forever, so the sweep drops stale entries once the table outgrows the
// cap. Losing a duration is acceptable; losing an event is not.
const (
	callTrackCap = 4096
	callTrackTTL = 15 * time.Minute
)

// summaryRunes bounds a record's one-line summary; errorRunes bounds the
// error excerpt a row carries. A tool result can be a megabyte - the
// ring holds the shape of the failure, never the payload (the disk log
// and the session store keep the content).
const (
	summaryRunes = 160
	errorRunes   = 400
)

// callStart is one in-flight tool call: the name (the result message
// does not carry it) and the moment the call was observed.
type callStart struct {
	tool string
	at   time.Time
}

// Recorder converts spine events into ring records. It is the capsule's
// only write path into the ring, driven by the agent's forward
// goroutine - so every method here must stay cheap (map writes, no I/O,
// never a network call: the spine contract).
type Recorder struct {
	ring *Ring
	// active gates recording. A dismounted capsule's observer stays
	// registered on the Context (the observer set is not part of the
	// mount journal) - the lifecycle Stop flips this flag, so a dead
	// recorder costs one atomic read per event and writes nothing.
	active atomic.Bool

	mu    sync.Mutex
	turns map[string]time.Time
	calls map[string]callStart

	// now is the clock seam (tests pin deterministic durations).
	now func() time.Time
}

// NewRecorder returns a recorder writing into the ring. Recording starts
// ON: a host that never arms lifecycle handles still gets its ring.
func NewRecorder(ring *Ring) *Recorder {
	r := &Recorder{
		ring:  ring,
		turns: map[string]time.Time{},
		calls: map[string]callStart{},
		now:   time.Now,
	}
	r.active.Store(true)
	return r
}

// Start arms recording (the lifecycle handle's Start).
func (r *Recorder) Start() { r.active.Store(true) }

// Stop disarms recording (the lifecycle handle's Stop, and the
// capsule's Dismount): the ring keeps what it has, nothing new lands.
func (r *Recorder) Stop() { r.active.Store(false) }

// Observe is the spine subscription: one structured record per
// structural event, attributed to the run's session.
func (r *Recorder) Observe(session string, ev core.Event) {
	if !r.active.Load() {
		return
	}
	now := r.now()
	switch e := ev.(type) {
	case core.AgentStart:
		summary := "run started"
		if e.ModelName != "" {
			summary += " (model " + e.ModelName + ")"
		}
		r.add(session, Event{Type: "agent_start", Level: LevelInfo, Summary: summary})

	case core.TurnStart:
		r.rememberTurn(session, e.Turn, now)
		r.add(session, Event{
			Type:    "turn_start",
			Level:   LevelInfo,
			Turn:    e.Turn,
			Summary: fmt.Sprintf("turn %d started", e.Turn),
		})

	case core.TurnEnd:
		summary := fmt.Sprintf("turn %d ended with %d tool call(s)", e.Turn, e.ToolCalls)
		if e.PendingCalls > 0 {
			// The async loop's yield-class calls: dispatched, not yet
			// executed in this turn - the wake path owns their re-entry.
			summary += fmt.Sprintf(", %d pending", e.PendingCalls)
		}
		r.add(session, Event{
			Type:       "turn_end",
			Level:      LevelInfo,
			Turn:       e.Turn,
			DurationMs: r.takeTurn(session, e.Turn, now),
			Summary:    summary,
		})

	case core.StreamEvent:
		r.observeStream(session, e, now)

	case core.ToolResultEvent:
		call := r.takeCall(session, e.Message.ToolCallID)
		tool := call.tool
		if tool == "" {
			// No matching call observed (a ring that started mid-run, or
			// a result the loop emitted without its call). The RESULT is
			// still a fact worth showing - name it honestly.
			tool = "unknown"
		}
		level := LevelInfo
		summary := tool + " returned"
		record := Event{
			Type:       "tool_result",
			Level:      level,
			Tool:       tool,
			DurationMs: sinceMs(call.at, now),
			Summary:    summary,
		}
		if e.Message.IsError {
			record.Level = LevelError
			record.Summary = tool + " failed"
			record.Error = shorten(e.Message.Content, errorRunes)
		}
		r.add(session, record)

	case core.AgentEnd:
		level := LevelInfo
		switch e.FinishReason {
		case "cancelled", "max_turns":
			level = LevelWarn
		}
		summary := fmt.Sprintf("run finished (%s, %d turn(s))", finishReason(e.FinishReason), e.Turns)
		record := Event{Type: "agent_end", Level: level, Summary: summary}
		if e.Error != "" {
			record.Level = LevelError
			record.Error = shorten(e.Error, errorRunes)
			record.Summary = "run failed: " + shorten(e.Error, summaryRunes)
		}
		r.add(session, record)

	case core.AssistantMessageEvent:
		// Dropped on purpose: the stream already reports this turn's
		// content, and the turn_end row carries the shape the tab shows.
		return
	}
}

// observeStream handles the wrapped provider events: tool calls (the
// only place a call's NAME exists) and stream ends (the only place a
// provider failure surfaces).
func (r *Recorder) observeStream(session string, e core.StreamEvent, now time.Time) {
	switch s := e.Event.(type) {
	case core.ToolCallEvent:
		r.rememberCall(session, s.ToolCall.ID, s.ToolCall.Name, now)
		r.add(session, Event{
			Type:    "tool_call",
			Level:   LevelInfo,
			Tool:    s.ToolCall.Name,
			Summary: "calling " + s.ToolCall.Name,
		})
	case core.StreamEnd:
		record := Event{Type: "stream_end", Level: LevelInfo, Summary: "stream finished"}
		switch {
		case s.Error != "":
			record.Level = LevelError
			record.Error = shorten(s.Error, errorRunes)
			record.Summary = "stream error: " + shorten(s.Error, summaryRunes)
		case s.FinishReason != "":
			record.Summary = "stream finished (" + s.FinishReason + ")"
			if s.FinishReason == "length" {
				// The provider cut the answer at its token ceiling: not a
				// failure, but not a clean stop either.
				record.Level = LevelWarn
			}
		}
		if s.PromptEvalCount != nil || s.EvalCount != nil {
			var parts []string
			if s.PromptEvalCount != nil {
				parts = append(parts, fmt.Sprintf("%d prompt", *s.PromptEvalCount))
			}
			if s.EvalCount != nil {
				parts = append(parts, fmt.Sprintf("%d eval", *s.EvalCount))
			}
			record.Summary += " - " + strings.Join(parts, ", ") + " tokens"
		}
		r.add(session, record)
	}
	// ThinkingChunk / ResponseChunk: dropped (one record per token delta
	// would bury every structural row - see the header).
}

// add stamps the session and appends to the ring.
func (r *Recorder) add(session string, e Event) {
	e.Session = session
	r.ring.Append(e)
}

// rememberTurn / takeTurn pair a turn's boundaries per (session, turn).
// One live run per session is the hub's law, so a session key is
// unambiguous; a sessionless run shares the "" key, where two concurrent
// runs can only lose a duration, never an event.
func (r *Recorder) rememberTurn(session string, turn int, now time.Time) {
	key := fmt.Sprintf("%s#%d", session, turn)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns[key] = now
}

func (r *Recorder) takeTurn(session string, turn int, now time.Time) int64 {
	key := fmt.Sprintf("%s#%d", session, turn)
	r.mu.Lock()
	start, ok := r.turns[key]
	delete(r.turns, key)
	r.mu.Unlock()
	if !ok {
		return 0
	}
	return sinceMs(start, now)
}

// rememberCall / takeCall pair a tool call with its result by the
// provider's call id.
func (r *Recorder) rememberCall(session, callID, tool string, now time.Time) {
	if callID == "" {
		return
	}
	key := session + "\x00" + callID
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls[key] = callStart{tool: tool, at: now}
	r.sweepCallsLocked(now)
}

func (r *Recorder) takeCall(session, callID string) callStart {
	if callID == "" {
		return callStart{}
	}
	key := session + "\x00" + callID
	r.mu.Lock()
	defer r.mu.Unlock()
	call := r.calls[key]
	delete(r.calls, key)
	return call
}

// sweepCallsLocked drops entries too old to pair, once the table has
// outgrown its cap (the bounded-state rule: an unpaired call is a lost
// duration, not a leak).
func (r *Recorder) sweepCallsLocked(now time.Time) {
	if len(r.calls) <= callTrackCap {
		return
	}
	for k, v := range r.calls {
		if now.Sub(v.at) > callTrackTTL {
			delete(r.calls, k)
		}
	}
}

// sinceMs is the elapsed milliseconds between two observations, floored
// at zero; a zero start (no pair) yields zero, which the wire omits.
func sinceMs(start, now time.Time) int64 {
	if start.IsZero() {
		return 0
	}
	if d := now.Sub(start); d > 0 {
		return d.Milliseconds()
	}
	return 0
}

// finishReason names an empty finish reason honestly.
func finishReason(fr string) string {
	if fr == "" {
		return "no finish reason"
	}
	return fr
}

// shorten collapses whitespace and cuts s to n runes with a plain
// trailing marker - one line, no embedded newlines (a record row is one
// line on the wire, always).
func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 3 {
		return string(runes[:n])
	}
	return string(runes[:n-3]) + "..."
}
