package api

// The push channel's transport.

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	agentapi "github.com/EndoTheDev/omega/agent/api"
)

// socketUpgrader upgrades /ws.
var socketUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 8192,
}

// socketWriteTimeout bounds one frame write.
const socketWriteTimeout = 10 * time.Second

// socketClientMessage is the only message a client may send.
type socketClientMessage struct {
	Type   string   `json:"type"`
	Topics []string `json:"topics"`
}

// handleSocket serves GET /ws. Both exit paths detach the subscriber, whose Close is
// idempotent.
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
		// The client closed, or the read failed.
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

// healthPushInterval is the health topic's cadence. The heartbeat is pushed, never
// polled: the SPA's 30s /health poll retires with it, and a reconnect still does one
// plain GET /health (the same body) before trusting the socket again.
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

// healthState is the /health body, shared by the GET route and the health topic so. The
// three fact objects are optional and additive: each is nil when this process has no
// source for it, and the body then stays exactly the shape it was before the facts
// existed.
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
