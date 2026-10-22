// Package http_channel provides the HTTP/SSE delivery transport as a
// Channel extension. The HTTP server implementation (server.go) lives
// in this package; the adapter wires it to the Channel seam.
//
// Seam: channel (additive — multiple channels can run simultaneously).
package http_channel

import (
	"context"
	"fmt"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// HTTPChannel adapts the HTTP server to the agent.Channel interface.
// It creates one agent from the Context and reuses it for all requests.
type HTTPChannel struct {
	addr       string
	runTimeout time.Duration // detached-run wall-clock cap from config
	titleModel string        // title_model knob from config
	srv        *Server
}

// Start creates an agent from deps.Ctx and serves HTTP/SSE until the
// context is cancelled. Blocks.
func (c *HTTPChannel) Start(ctx context.Context, deps agent.ChannelDeps) error {
	if deps.Ctx == nil || deps.Ctx.Provider == nil {
		return fmt.Errorf("http_channel: no provider configured")
	}
	ag := agent.NewFromContext(deps.Ctx, agent.AgentOptions{})
	c.srv = NewServer(ag, nil, deps.Store, deps.Settings)
	c.srv.Kanban = deps.Ctx.Kanban
	// Inspector seams (card t_db430fea): read-only views for the
	// /cron/jobs, /skills and /logs endpoints. All optional - the
	// handlers degrade to empty lists / 404 when a module is absent.
	c.srv.Skills = deps.Ctx.Skills
	c.srv.Cron = deps.Ctx.Cron
	// card t_5d9032d3: the finished-things seam (bell list/read + live
	// toast stream). Optional - nil degrades to 501s.
	c.srv.Notifications = deps.Ctx.Notifications
	// card t_2ecf9c51: the verdicts seam (answer POSTs + pending list).
	c.srv.Verdicts = deps.Ctx.Verdicts
	c.srv.logger = deps.Ctx.Logger
	c.srv.runTimeout = c.runTimeout
	c.srv.titleModel = c.titleModel
	c.srv.version = deps.Version
	return c.srv.Serve(ctx, c.addr)
}

// Stop shuts the HTTP server down. The host calls this after the
// context is cancelled; Serve returns on cancellation, so Stop is a
// no-op safety net.
func (c *HTTPChannel) Stop() error {
	return nil
}

// Compile-time assertion that HTTPChannel implements agent.Channel.
var _ agent.Channel = (*HTTPChannel)(nil)
