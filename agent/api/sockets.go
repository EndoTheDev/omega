package api

// The push channel's hub.

import (
	"encoding/json"
	"reflect"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// The topic vocabulary: one closed set, six names.
const (
	// TopicChat carries run event deltas.
	TopicChat = "chat"
	// TopicChips carries the pending chip's live tail while a tool runs and the fill.
	TopicChips = "chips"
	// TopicCanvas carries canvas ViewModelDeltas. One topic, not one per module: the panel
	// descriptor names itself, so the shell never learns a capsule noun (the binary-purity
	// law).
	TopicCanvas = "canvas"
	// TopicRuns carries the live-run truth (session + active).
	TopicRuns = "runs"
	// TopicHealth carries the server heartbeat.
	TopicHealth = "health"
	// TopicLogs carries the log tail's DELTA: only the lines appended to the server's own
	// log file since the previous frame on this topic, never the 200-line snapshot (the
	// pane's first read stays GET /logs; the socket carries what comes after it).
	TopicLogs = "logs"
)

// Topics is the closed topic set, in wire order. A subscriber starts subscribed to all
// of them and may narrow the set; an unknown name is ignored, never invented.
var Topics = []string{TopicChat, TopicChips, TopicCanvas, TopicRuns, TopicHealth, TopicLogs}

// SocketMessage is one pushed frame.
type SocketMessage struct {
	Topic string `json:"topic"`
	Session string `json:"session,omitempty"`
	Event   string `json:"event,omitempty"`
	Seq     uint64 `json:"seq"`
	Data json.RawMessage `json:"data,omitempty"`
}

// ChipFrame is the chips topic's payload.
type ChipFrame struct {
	CallID string `json:"call_id"`
	Tool   string `json:"tool"`
	Fill   bool   `json:"fill,omitempty"`
	Text    string `json:"text,omitempty"`
	Content string `json:"content,omitempty"`
	Truncated bool  `json:"truncated,omitempty"`
	IsError   bool  `json:"is_error,omitempty"`
	Timeout   bool  `json:"timeout,omitempty"`
	ElapsedMs int64 `json:"elapsed_ms,omitempty"`
}

// RunState is the runs topic's payload: one session's live-run truth.
type RunState struct {
	Active bool `json:"active"`
}

// LogLines is the logs topic's payload: the lines the log file gained since the
// previous frame on the topic - a delta, never the tail. Each element is one raw line
// exactly as the file holds it (timestamp included), so a pushed line and the same line
// read back through GET /logs are byte-identical.
type LogLines struct {
	Lines []string `json:"lines"`
}

// HealthState is the health topic's payload - the same body GET /health serves, so the
// reconnect fetch and the pushed frames agree byte for byte.
type HealthState struct {
	Status        string `json:"status"`
	UptimeSeconds int    `json:"uptime_seconds"`
	Version       string `json:"version,omitempty"`
	Provider *HealthProvider `json:"provider,omitempty"`
	Database *HealthDatabase `json:"database,omitempty"`
	LastError *HealthError `json:"last_error,omitempty"`
}

// HealthProvider is the provider-reachability fact.
type HealthProvider struct {
	Reachable bool   `json:"reachable"`
	CheckedAt int64  `json:"checked_at"` // unix seconds of the probe
	Detail    string `json:"detail,omitempty"`
}

// HealthDatabase is the store-health fact: whether the store's database answered a
// round trip on this very health read (it is probed inline, not cached, so the row is
// never stale).
type HealthDatabase struct {
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail,omitempty"`
}

// HealthError is the last-error fact.
type HealthError struct {
	Message string `json:"message"`
	At      int64  `json:"at,omitempty"`
}

// socketSubBuffer bounds one connection's frame queue. The chat topic
// emits one frame per token delta, so a burst between two writes stays
// comfortably inside this; a client that cannot keep up is cut.
const socketSubBuffer = 64

// SocketHub is the process-side fan-out. Safe for concurrent use.
type SocketHub struct {
	mu   sync.Mutex
	subs map[*SocketSub]struct{}
	seq  uint64
}

// NewSocketHub returns an empty hub.
func NewSocketHub() *SocketHub {
	return &SocketHub{subs: map[*SocketSub]struct{}{}}
}

// SocketSub is one attached connection: its frame channel, its topic set, and the
// closed flag the hub uses to cut it exactly once.
type SocketSub struct {
	hub    *SocketHub
	ch     chan []byte
	topics map[string]bool
	closed bool
}

// Attach registers one connection.
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

// Close detaches the connection and closes its frame channel.
// Idempotent: the read side and the write side both close on their way out.
func (s *SocketSub) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	s.hub.drop(s)
}

// Messages is the frame stream the transport writes, one JSON message per element.
func (s *SocketSub) Messages() <-chan []byte { return s.ch }

// Subscribe replaces the connection's topic set and returns the accepted names.
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

// Publish marshals one payload and fans it out to every subscriber that wants the topic.
// payload marshals once (never per subscriber) and not at all when nobody wants the
// topic - a shell command with no browser attached pays one map scan per chunk.
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

// Wants reports whether any live subscriber holds the topic.
func (h *SocketHub) Wants(topic string) bool { return h.wanted(topic) }

// SubscriberCount is the number of attached connections.
func (h *SocketHub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// publish stamps the sequence and delivers to every subscriber under the one lock.
func (h *SocketHub) publish(msg SocketMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	msg.Seq = h.seq
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	// The chips topic is the one LOSSY topic. Every authoritative topic cuts the stalled
	// subscriber instead - a silent frame loss there would look like state that never
	// changed.
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

// drop detaches one subscriber and ends its frame stream.
func (h *SocketHub) drop(sub *SocketSub) {
	if sub.closed {
		return
	}
	sub.closed = true
	delete(h.subs, sub)
	close(sub.ch)
}

// the broadcast hooks (the publish side of the serve seam) ---------

// emitRunFrame is the ONE run broadcast seam.
func (s *Server) emitRunFrame(run *activeRun, f runFrame) {
	s.runs.broadcast(run, f)
	s.sockets.PublishEvent(TopicChat, run.id, f.event, f.data)
}

// publishRunState pushes one session's live-run truth (the runs topic).
func (s *Server) publishRunState(sessionID string, active bool) {
	s.sockets.Publish(TopicRuns, sessionID, RunState{Active: active})
}

// publishChipFill pushes one settled pending call onto the chips topic.
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

// chipTap is the run's output tap (core.RunOutputTap).
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

// the verdict issuance push ----------------------

// verdictIssuedEvent is the chat-topic event name a card's ISSUANCE rides.
const verdictIssuedEvent = "verdict_issued"

// verdictCardSource is the consumer-side face of the verdict capsule's issuance.
type verdictCardSource interface {
	OnCardRegistered(fn func(core.QuestionPrompt))
}

// verdictIssuedFrame is the issuance hint's payload: MINIMAL on purpose. The frame is a
// HINT, state is truth - the webui re-reads GET /verdicts on it, so the card's
// options/body/expiry come from the REST read and never need duplicating here.
type verdictIssuedFrame struct {
	CardID    string `json:"card_id"`
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
}

// subscribeVerdictCards wires the issuance push at serve boot and after every live. Nil
// safe - a host without the verdict capsule (disabled, or never mounted) subscribes
// nothing and boots clean.
func (s *Server) subscribeVerdictCards(ctx *core.Context) {
	if ctx == nil {
		return
	}
	svc := ctx.Services().Resolve("verdicts")
	if svc == nil {
		return
	}
	src, ok := svc.(verdictCardSource)
	if !ok {
		return
	}
	if !s.claimVerdictSource(src) {
		return // already subscribed to this very object (a rewire)
	}
	src.OnCardRegistered(s.publishVerdictIssued)
}

// claimVerdictSource records the source this server subscribed to and reports whether it
// changed - re-claiming the same source is a no-op.
func (s *Server) claimVerdictSource(src verdictCardSource) bool {
	s.verdictMu.Lock()
	defer s.verdictMu.Unlock()
	if sameVerdictSource(s.verdictSrc, src) {
		return false
	}
	s.verdictSrc = src
	return true
}

// sameVerdictSource reports whether two offers name the same object. Interface equality
// would be the obvious form and would PANIC on a non-comparable dynamic type, so
// compare pointer-shaped values by address and treat anything else as a new source (one
// extra subscription is harmless; a panic at boot is not).
func sameVerdictSource(a, b verdictCardSource) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	if va.Kind() != vb.Kind() {
		return false
	}
	switch va.Kind() {
	case reflect.Ptr, reflect.Chan, reflect.Map, reflect.Func, reflect.UnsafePointer:
		return va.Pointer() == vb.Pointer()
	}
	return false
}

// publishVerdictIssued pushes one issued card onto the chat topic under its session id.
// It runs on the issuing goroutine, outside the hub's lock, and it is deliberately
// fire-and-forget: with no subscriber the hub does no work and nothing is buffered -
// the GET routes stay the truth.
func (s *Server) publishVerdictIssued(card core.QuestionPrompt) {
	if s.sockets == nil {
		return
	}
	data, err := json.Marshal(verdictIssuedFrame{CardID: card.ID, SessionID: card.SessionID, Title: card.Title})
	if err != nil {
		return
	}
	s.sockets.PublishEvent(TopicChat, card.SessionID, verdictIssuedEvent, data)
}
