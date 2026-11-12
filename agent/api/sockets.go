package api

// The push channel's hub (card t_7624bab1; design of record
// Workshop/omega/websocket-channel.md). ONE websocket per browser
// connection, every update a typed topic on it - and this file is the
// fan-out, deliberately transport-blind: the importability law keeps a
// websocket library out of agent/api, so the serving shell (api/ws.go)
// upgrades the request, attaches a subscriber here and writes its
// frames.
//
// Events are hints, state is truth (ruling 4). The hub buffers NOTHING,
// replays NOTHING and keeps no per-session queue: a client that falls
// behind is cut (or, on the lossy chips topic, loses frames) and
// reconnects; catch-up is a refetch of the visible panes through the GET
// API, which stays complete and authoritative (ruling: the GET API stays
// authoritative, the socket never becomes the only way to read state).
//
// The broadcast hooks live at the one seam the loop already fans out
// through - see emitRunFrame / CanvasEmit / prepareRun / wakeHome.

import (
	"encoding/json"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// The topic vocabulary: one closed set, six names (five live on every
// connection; `logs` is subscription-gated).
const (
	// TopicChat carries run event deltas - the exact frames the SSE
	// stream emits (event name + verbatim payload), so the SPA's
	// existing frame dispatch works unchanged.
	TopicChat = "chat"
	// TopicChips carries the pending chip's live tail while a tool runs
	// and the fill when the call settles.
	TopicChips = "chips"
	// TopicCanvas carries canvas ViewModelDeltas - panel state from
	// every mounted capsule, notifications toasts and board changes
	// included. One topic, not one per module: the panel descriptor
	// names itself, so the shell never learns a capsule noun (the
	// binary-purity law).
	TopicCanvas = "canvas"
	// TopicRuns carries the live-run truth (session + active) - the
	// fact the SPA used to poll GET /sessions for.
	TopicRuns = "runs"
	// TopicHealth carries the server heartbeat - the fact the SPA used
	// to poll GET /health for.
	TopicHealth = "health"
	// TopicLogs carries the log tail's DELTA: only the lines appended to
	// the server's own log file since the previous frame on this topic,
	// never the 200-line snapshot (the pane's first read stays
	// GET /logs; the socket carries what comes after it). Subscription-
	// gated by design - a log tail is high-frequency, so a connection
	// that did not ask for `logs` receives nothing (card t_0e7bc0bb,
	// ruling: overloading `runs`/`canvas` would make the vocabulary
	// lie).
	TopicLogs = "logs"
)

// Topics is the closed topic set, in wire order. A subscriber starts
// subscribed to all of them and may narrow the set; an unknown name is
// ignored, never invented.
var Topics = []string{TopicChat, TopicChips, TopicCanvas, TopicRuns, TopicHealth, TopicLogs}

// SocketMessage is one pushed frame. The envelope is topic-typed: Topic
// says what it is, Event carries the chat topic's SSE event name (empty
// on the other topics - their Data is a typed payload and the topic IS
// the type), and Seq is the hub's monotonic counter so a client can
// notice a gap and refetch.
type SocketMessage struct {
	Topic string `json:"topic"`
	// Session scopes the frame to one conversation ("" = global: canvas
	// and health).
	Session string `json:"session,omitempty"`
	Event   string `json:"event,omitempty"`
	Seq     uint64 `json:"seq"`
	// Data is the frame's payload: the chat topic's SSE bytes verbatim,
	// a typed struct for the other topics.
	Data json.RawMessage `json:"data,omitempty"`
}

// ChipFrame is the chips topic's payload: the pending chip's live tail
// while the call runs (Fill false, Text growing) and its fill when it
// settles (Fill true, Content the result).
type ChipFrame struct {
	CallID string `json:"call_id"`
	Tool   string `json:"tool"`
	Fill   bool   `json:"fill,omitempty"`
	// Text is one live output chunk (streaming half); Content is the
	// settled result text (fill half).
	Text    string `json:"text,omitempty"`
	Content string `json:"content,omitempty"`
	// Truncated marks the streaming path's own cap (the truncation
	// marker law) - the tail the chip holds is not the whole output.
	Truncated bool  `json:"truncated,omitempty"`
	IsError   bool  `json:"is_error,omitempty"`
	Timeout   bool  `json:"timeout,omitempty"`
	ElapsedMs int64 `json:"elapsed_ms,omitempty"`
}

// RunState is the runs topic's payload: one session's live-run truth.
type RunState struct {
	Active bool `json:"active"`
}

// LogLines is the logs topic's payload: the lines the log file gained
// since the previous frame on the topic - a delta, never the tail. Each
// element is one raw line exactly as the file holds it (timestamp
// included), so a pushed line and the same line read back through
// GET /logs are byte-identical.
type LogLines struct {
	Lines []string `json:"lines"`
}

// HealthState is the health topic's payload - the same body GET /health
// serves, so the reconnect fetch and the pushed frames agree byte for
// byte. The three fact objects below are OPTIONAL and additive: an
// absent one means this build (or this process, before the first probe
// landed) has no source for that fact, and the popover renders "not
// reported" rather than an invented value. A sourced fact always rides
// as an object, so "none recorded" (an error object with an empty
// message) stays distinguishable from "no source".
type HealthState struct {
	Status        string `json:"status"`
	UptimeSeconds int    `json:"uptime_seconds"`
	Version       string `json:"version,omitempty"`
	// Provider is the cached model-endpoint probe's verdict.
	Provider *HealthProvider `json:"provider,omitempty"`
	// Database is the store seam's liveness ping.
	Database *HealthDatabase `json:"database,omitempty"`
	// LastError is the newest error the process logger recorded.
	LastError *HealthError `json:"last_error,omitempty"`
}

// HealthProvider is the provider-reachability fact: the last probe's
// verdict and when it was taken. Detail carries the probe's error text
// when it failed (empty on success) - the popover's tooltip.
type HealthProvider struct {
	Reachable bool   `json:"reachable"`
	CheckedAt int64  `json:"checked_at"` // unix seconds of the probe
	Detail    string `json:"detail,omitempty"`
}

// HealthDatabase is the store-health fact: whether the store's database
// answered a round trip on this very health read (it is probed inline,
// not cached, so the row is never stale). Detail carries the ping's
// error text when it failed.
type HealthDatabase struct {
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail,omitempty"`
}

// HealthError is the last-error fact. Message empty = the logger has
// recorded no error since the process started (a sourced "none"); At is
// the entry's unix seconds and stays 0 in that case.
type HealthError struct {
	Message string `json:"message"`
	At      int64  `json:"at,omitempty"`
}

// socketSubBuffer bounds one connection's frame queue. The chat topic
// emits one frame per token delta, so a burst between two writes stays
// comfortably inside this; a client that cannot keep up is cut.
const socketSubBuffer = 64

// SocketHub is the process-side fan-out. Attach on connect, detach on
// close; Publish from every hook. Safe for concurrent use.
type SocketHub struct {
	mu   sync.Mutex
	subs map[*SocketSub]struct{}
	seq  uint64
}

// NewSocketHub returns an empty hub.
func NewSocketHub() *SocketHub {
	return &SocketHub{subs: map[*SocketSub]struct{}{}}
}

// SocketSub is one attached connection: its frame channel, its topic
// set, and the closed flag the hub uses to cut it exactly once. Every
// field except ch is guarded by the hub's mutex - one lock for the whole
// fan-out keeps publish a single pass.
type SocketSub struct {
	hub    *SocketHub
	ch     chan []byte
	topics map[string]bool
	closed bool
}

// Attach registers one connection. It starts subscribed to every topic;
// Subscribe narrows the set.
func (h *SocketHub) Attach() *SocketSub {
	sub := &SocketSub{hub: h, ch: make(chan []byte, socketSubBuffer), topics: map[string]bool{}}
	for _, t := range Topics {
		sub.topics[t] = true
	}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

// Close detaches the connection and closes its frame channel (ending the
// transport's write loop). Idempotent: the read side and the write side
// both close on their way out.
func (s *SocketSub) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	s.hub.drop(s)
}

// Messages is the frame stream the transport writes, one JSON message
// per element. A closed channel means the connection is done.
func (s *SocketSub) Messages() <-chan []byte { return s.ch }

// Subscribe replaces the connection's topic set and returns the accepted
// names. Unknown names are dropped (the set is closed); an empty list
// keeps the current set - "subscribed to nothing" is not a state the
// wire needs.
func (s *SocketSub) Subscribe(topics []string) []string {
	if len(topics) == 0 {
		return Topics
	}
	next := map[string]bool{}
	accepted := make([]string, 0, len(topics))
	for _, t := range topics {
		for _, known := range Topics {
			if t == known {
				next[t] = true
				accepted = append(accepted, t)
				break
			}
		}
	}
	if len(next) == 0 {
		return Topics
	}
	s.hub.mu.Lock()
	s.topics = next
	s.hub.mu.Unlock()
	return accepted
}

// Publish marshals one payload and fans it out to every subscriber that
// wants the topic. The payload marshals once (never per subscriber) and
// not at all when nobody wants the topic - a shell command with no
// browser attached pays one map scan per chunk.
func (h *SocketHub) Publish(topic, session string, payload any) {
	if !h.wanted(topic) {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.publish(SocketMessage{Topic: topic, Session: session, Data: data})
}

// PublishEvent is Publish for a frame whose payload is already JSON -
// the chat topic re-uses the SSE encoder's bytes verbatim, so the webui
// dispatches a socket frame exactly like a stream frame.
func (h *SocketHub) PublishEvent(topic, session, event string, data []byte) {
	if !h.wanted(topic) {
		return
	}
	h.publish(SocketMessage{Topic: topic, Session: session, Event: event, Data: data})
}

// wanted reports whether any live subscriber holds the topic.
func (h *SocketHub) wanted(topic string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		if !sub.closed && sub.topics[topic] {
			return true
		}
	}
	return false
}

// Wants reports whether any live subscriber holds the topic. It is the
// read a publisher whose payload comes from OUTSIDE the hub uses to
// skip the work entirely - the log follower (api/server_inspector.go)
// only reads the file when a pane actually asked for `logs`.
func (h *SocketHub) Wants(topic string) bool { return h.wanted(topic) }

// SubscriberCount is the number of attached connections - the read the
// reconnect test and any future "is anyone listening" inspection needs.
func (h *SocketHub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// publish stamps the sequence and delivers to every subscriber under the
// one lock. Marshaling happens inside the lock so frame order IS state
// order (the same law the canvas emitter holds).
func (h *SocketHub) publish(msg SocketMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	msg.Seq = h.seq
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	// The chips topic is the one LOSSY topic: a chunk or a fill that
	// arrives too late is worth less than the wire itself (the tool
	// result and the transcript remain the truth), so a full queue drops
	// the frame. Every authoritative topic cuts the stalled subscriber
	// instead - a silent frame loss there would look like state that
	// never changed.
	lossy := msg.Topic == TopicChips
	for sub := range h.subs {
		if sub.closed || !sub.topics[msg.Topic] {
			continue
		}
		select {
		case sub.ch <- b:
		default:
			if !lossy {
				h.drop(sub)
			}
		}
	}
}

// drop detaches one subscriber and ends its frame stream. Called with
// the hub's mutex held.
func (h *SocketHub) drop(sub *SocketSub) {
	if sub.closed {
		return
	}
	sub.closed = true
	delete(h.subs, sub)
	close(sub.ch)
}

// --- the broadcast hooks (the publish side of the serve seam) ------------

// emitRunFrame is the ONE run broadcast seam: every frame the SSE
// subscribers see also rides the chat topic. launchRun and persistRun
// call this instead of the hub directly, so a wake turn (which launches
// through launchRun like any other) fans out on the same path with zero
// extra wiring.
func (s *Server) emitRunFrame(run *activeRun, f runFrame) {
	s.runs.broadcast(run, f)
	s.sockets.PublishEvent(TopicChat, run.id, f.event, f.data)
}

// publishRunState pushes one session's live-run truth (the runs topic):
// taken by prepareRun (the single run-slot truth, human sends and wake
// launches alike) and released when launchRun's finish hook runs.
func (s *Server) publishRunState(sessionID string, active bool) {
	s.sockets.Publish(TopicRuns, sessionID, RunState{Active: active})
}

// publishChipFill pushes one settled pending call onto the chips topic -
// the pending chip's fill. wakeHome is the only settle entry point
// (immediate-yield dispatches and budget escalations alike), so this is
// the fill's one source.
func (s *Server) publishChipFill(sessionID string, res core.YieldResult) {
	frame := ChipFrame{
		CallID:  res.ToolCallID,
		Tool:    res.ToolName,
		Fill:    true,
		Content: res.Content,
		IsError: res.IsError,
		Timeout: res.Timeout,
	}
	if res.Timing != nil {
		frame.ElapsedMs = res.Timing.ElapsedMs
	}
	s.sockets.Publish(TopicChips, sessionID, frame)
}

// chipTap is the run's output tap (core.RunOutputTap): a tool that
// streams (the shell's log tail, tapLog in harness/terminal) hands its
// live tail here and it lands on the chips topic straight away. Nothing
// accumulates: the hub holds no state, so a chip missed on a dead
// socket costs one refetch.
func (s *Server) chipTap(sessionID string) core.RunOutputTap {
	return func(chunk core.ToolOutputChunk) {
		s.sockets.Publish(TopicChips, sessionID, ChipFrame{
			CallID:    chunk.CallID,
			Tool:      chunk.Tool,
			Text:      chunk.Text,
			Truncated: chunk.Truncated,
		})
	}
}
