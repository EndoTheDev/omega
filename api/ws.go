package api

// The push channel's transport (card t_7624bab1; design of record
// Workshop/omega/websocket-channel.md). ONE websocket per browser
// connection: GET /ws upgrades, the connection attaches to the base
// server's hub (agent/api/sockets.go - the fan-out lives with the agent
// loop, the socket library lives here in the shell because the
// importability law keeps external modules out of agent/api), and every
// frame is a topic-typed JSON message.
//
// Client -> server traffic is deliberately tiny: a subscribe message
// replacing the connection's topic set. Everything else the client
// needs, it reads through the GET API - events are hints, state is
// truth (ruling 4). There is no server-side buffering and no replay, so
// a reconnect is a refetch of the visible panes plus one health fetch.
//
// Loopback bind until the gateway arc owns auth (the host guard's law
// wraps this handler too: the upgrader's default origin check refuses a
// cross-origin page, which is the write-side half of the same rule).

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	agentapi "github.com/EndoTheDev/omega/agent/api"
)

// socketUpgrader upgrades /ws. Buffers are small on purpose: the hub's
// per-connection queue is the real buffer, this one only spans one
// frame. CheckOrigin stays gorilla's default (an Origin whose host is
// not the request's Host is refused) - the same-host law the guard
// applies to every other route.
var socketUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 8192,
}

// socketWriteTimeout bounds one frame write: a client that stops
// reading is cut rather than pinning the write loop forever.
const socketWriteTimeout = 10 * time.Second

// socketClientMessage is the only message a client may send:
// {"type":"subscribe","topics":["chat","chips"]}. Unknown types are
// ignored (forward compatibility costs one no-op).
type socketClientMessage struct {
	Type   string   `json:"type"`
	Topics []string `json:"topics"`
}

// handleSocket serves GET /ws. The write loop owns the write side of
// the connection (one writer, gorilla's rule); the read loop runs beside
// it for subscribe messages and close detection. Both exit paths detach
// the subscriber, whose Close is idempotent.
func (s *Server) handleSocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	conn, err := socketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote the HTTP error.
		return
	}
	// The write loop below can exit on a write error without the client
	// ever closing: closing the conn here unblocks the reader's
	// ReadJSON, so neither goroutine outlives the handler.
	defer conn.Close()
	sub := s.Server.Sockets().Attach()
	defer sub.Close()
	go func() {
		// The client closed, or the read failed: end the subscriber so
		// the write loop below stops waiting on frames that nobody will
		// ever read.
		defer sub.Close()
		defer conn.Close()
		for {
			var msg socketClientMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg.Type == "subscribe" {
				sub.Subscribe(msg.Topics)
			}
		}
	}()
	for frame := range sub.Messages() {
		_ = conn.SetWriteDeadline(time.Now().Add(socketWriteTimeout))
		if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
			return
		}
	}
}

// healthPushInterval is the health topic's cadence. The heartbeat is
// pushed, never polled: the SPA's 30s /health poll retires with it, and
// a reconnect still does one plain GET /health (the same body) before
// trusting the socket again.
const healthPushInterval = 5 * time.Second

// pushHealth pushes the health heartbeat until the server context ends.
// Started by Serve, after the listener is up - the goroutine fence's
// law (mount never starts background work; the host does).
func (s *Server) pushHealth(ctx context.Context) {
	s.Server.Sockets().Publish(agentapi.TopicHealth, "", s.healthState())
	ticker := time.NewTicker(healthPushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Server.Sockets().Publish(agentapi.TopicHealth, "", s.healthState())
		}
	}
}

// healthState is the /health body, shared by the GET route and the
// health topic so the reconnect fetch and the pushed frames agree. The
// three fact objects are optional and additive (card t_5e2f3095): each
// is nil when this process has no source for it, and the body then stays
// exactly the shape it was before the facts existed.
func (s *Server) healthState() agentapi.HealthState {
	return agentapi.HealthState{
		Status:        "ok",
		UptimeSeconds: int(time.Since(s.startedAt).Seconds()),
		Version:       s.version,
		Provider:      s.providerHealth(),
		Database:      s.databaseHealth(),
		LastError:     s.lastErrorFact(),
	}
}
