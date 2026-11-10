package api

// Tests for the push channel's transport (card t_7624bab1): a real
// websocket dial against the served handler, the typed frame envelope,
// the subscribe message, the non-upgrade refusals, and reconnect (drop,
// detach, dial again, frames resume - the handler-level half of the
// client's backoff).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	agentapi "github.com/EndoTheDev/omega/agent/api"
)

// dialSocket connects a websocket client to the test server's /ws.
func dialSocket(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial /ws: %v (status %d)", err, status)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// readFrame reads one pushed frame off the wire, failing on timeout.
func readFrame(t *testing.T, conn *websocket.Conn) agentapi.SocketMessage {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	var msg agentapi.SocketMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("decode frame: %v (%s)", err, raw)
	}
	return msg
}

// TestSocketPushesTypedFrames: the connection attaches to the hub, a
// subscribe message narrows the set, and a published frame arrives typed
// with its topic, session, event and payload.
func TestSocketPushesTypedFrames(t *testing.T) {
	s := newTestServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	conn := dialSocket(t, ts)
	// the frame only lands if the connection attached before it was
	// published - so publish after a first round trip the server answers
	if err := conn.WriteJSON(map[string]any{"type": "subscribe", "topics": []string{"chat"}}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// the subscribe message is processed by the read loop; the hub's
	// default set is every topic, so a chat frame arrives either way
	s.Sockets().PublishEvent(agentapi.TopicChat, "sess-1", "response_chunk", []byte(`{"type":"response_chunk","content":"hi"}`))

	msg := readFrame(t, conn)
	if msg.Topic != agentapi.TopicChat || msg.Session != "sess-1" || msg.Event != "response_chunk" {
		t.Fatalf("frame = %+v, want chat/sess-1/response_chunk", msg)
	}
	var chunk struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(msg.Data, &chunk); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if chunk.Content != "hi" {
		t.Fatalf("content = %q, want hi", chunk.Content)
	}
}

// TestSocketHealthAndChipTopics: the other two hooks reach a live
// connection over the real wire - the heartbeat and the chip fill.
func TestSocketHealthAndChipTopics(t *testing.T) {
	s := newTestServer()
	s.version = "v0.14.1-test"
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	conn := dialSocket(t, ts)
	if got := waitSubscribers(t, s, 1); !got {
		t.Fatal("connection never attached to the hub")
	}

	s.Sockets().Publish(agentapi.TopicHealth, "", s.healthState())

	msg := readFrame(t, conn)
	if msg.Topic != agentapi.TopicHealth {
		t.Fatalf("topic = %q, want health", msg.Topic)
	}
	var state agentapi.HealthState
	if err := json.Unmarshal(msg.Data, &state); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if state.Status != "ok" || state.Version != "v0.14.1-test" {
		t.Fatalf("health = %+v, want ok + the build tag", state)
	}
}

// TestSocketReconnect: a dropped connection detaches from the hub, and a
// fresh dial attaches again and receives frames - the socket's reconnect
// half (catch-up itself is the client's refetch, ruling 4).
func TestSocketReconnect(t *testing.T) {
	s := newTestServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	first := dialSocket(t, ts)
	if !waitSubscribers(t, s, 1) {
		t.Fatal("first connection never attached")
	}
	first.Close()
	if !waitSubscribers(t, s, 0) {
		t.Fatalf("dropped connection still attached: %d", s.Sockets().SubscriberCount())
	}

	second := dialSocket(t, ts)
	if !waitSubscribers(t, s, 1) {
		t.Fatal("reconnect never attached")
	}
	s.Sockets().Publish(agentapi.TopicRuns, "sess-2", agentapi.RunState{Active: true})
	msg := readFrame(t, second)
	if msg.Topic != agentapi.TopicRuns || msg.Session != "sess-2" {
		t.Fatalf("frame after reconnect = %+v, want runs/sess-2", msg)
	}
}

// waitSubscribers polls the hub until it holds count connections (the
// attach/detach happen on the server's own goroutines).
func waitSubscribers(t *testing.T, s *Server, count int) bool {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for time.Now().Before(deadline) {
		if s.Sockets().SubscriberCount() == count {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// TestSocketRequiresUpgrade: /ws is a GET-only upgrade endpoint - the
// route answers a method error and a plain GET a 400, never a body.
func TestSocketRequiresUpgrade(t *testing.T) {
	s := newTestServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/ws", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("post /ws: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /ws status = %d, want 405", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/ws")
	if err != nil {
		t.Fatalf("get /ws: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("plain GET /ws status = %d, want 400 (no upgrade headers)", resp.StatusCode)
	}
}

// waitWants polls the hub until its wanted() answer for the topic equals
// want (subscribe messages are processed on the read loop's goroutine).
func waitWants(t *testing.T, s *Server, topic string, want bool) bool {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for time.Now().Before(deadline) {
		if s.Sockets().Wants(topic) == want {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// TestSocketLogsTopicGated: `logs` is the sixth topic and the ONLY
// subscription-gated one - a connection that never asked for it gets
// nothing when a log delta is published, and a connection that asked
// gets the typed delta. The gate is what keeps a high-frequency tail
// from fanning out to every open pane.
func TestSocketLogsTopicGated(t *testing.T) {
	s := newTestServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// the silent half: narrowed to health, so a logs publish must not
	// reach it
	quiet := dialSocket(t, ts)
	if err := quiet.WriteJSON(map[string]any{"type": "subscribe", "topics": []string{"health"}}); err != nil {
		t.Fatalf("subscribe health: %v", err)
	}
	if !waitWants(t, s, agentapi.TopicLogs, false) {
		t.Fatal("the health-only connection still holds the logs topic")
	}

	// the listening half: asks for logs explicitly
	live := dialSocket(t, ts)
	if err := live.WriteJSON(map[string]any{"type": "subscribe", "topics": []string{"logs"}}); err != nil {
		t.Fatalf("subscribe logs: %v", err)
	}
	if !waitWants(t, s, agentapi.TopicLogs, true) {
		t.Fatal("the logs connection never took the topic")
	}

	s.Sockets().Publish(agentapi.TopicLogs, "", agentapi.LogLines{Lines: []string{"2026/11/11 10:00:00 hello", "2026/11/11 10:00:01 world"}})

	msg := readFrame(t, live)
	if msg.Topic != agentapi.TopicLogs || msg.Session != "" || msg.Event != "" {
		t.Fatalf("frame = %+v, want a session-less, event-less logs frame", msg)
	}
	var delta agentapi.LogLines
	if err := json.Unmarshal(msg.Data, &delta); err != nil {
		t.Fatalf("decode logs payload: %v", err)
	}
	if len(delta.Lines) != 2 || delta.Lines[0] != "2026/11/11 10:00:00 hello" {
		t.Fatalf("delta = %v, want the two appended lines verbatim", delta.Lines)
	}

	// the quiet connection saw none of it (a read error is the ONLY
	// correct outcome: gorilla marks a connection failed after a read
	// timeout, so this is the last thing asked of it)
	_ = quiet.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, raw, err := quiet.ReadMessage(); err == nil {
		t.Fatalf("a health-only connection received a logs frame: %s", raw)
	}
}
