// Package http_channel provides the HTTP/SSE delivery transport as a
// Channel extension. It wraps gateway.Server — the HTTP logic stays in
// the gateway package; this extension adapts it to the Channel seam.
//
// Seam: channel (additive — multiple channels can run simultaneously).
package http_channel

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
)

// HTTPChannel adapts the gateway HTTP server to the agent.Channel
// interface. It creates one agent from the Context and reuses it for
// all requests (current gateway behavior).
type HTTPChannel struct {
	addr string
	srv  *gateway.Server
}

// Start creates an agent from deps.Ctx and serves HTTP/SSE until the
// context is cancelled. Blocks.
func (c *HTTPChannel) Start(ctx context.Context, deps agent.ChannelDeps) error {
	if deps.Ctx == nil || deps.Ctx.Provider == nil {
		return fmt.Errorf("http_channel: no provider configured")
	}
	ag := agent.NewFromContext(deps.Ctx, agent.AgentOptions{})
	c.srv = gateway.NewServer(ag, nil, deps.Store)
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