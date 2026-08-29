package http_channel

import (
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
)

// Plugin implements agent.Plugin for the HTTP channel. It reads the
// port from config (server.port, unchanged — no breaking config moves)
// and appends an HTTPChannel to ctx.Channels.
type Plugin struct{}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string       { return "http_channel" }
func (p *Plugin) Provides() []string { return []string{"channel"} }
func (p *Plugin) Requires() []string { return nil }

// Mount creates an HTTPChannel from config and appends it to
// ctx.Channels. The host starts it (and any other mounted channels)
// after MountAll completes.
func (p *Plugin) Mount(ctx *agent.Context) error {
	cfg, ok := ctx.Config.(gateway.Config)
	if !ok {
		cfg = gateway.DefaultConfig()
	}
	port := cfg.Server.Port
	if port <= 0 {
		port = 8099
	}
	ctx.Channels = append(ctx.Channels, &HTTPChannel{
		addr: fmt.Sprintf(":%d", port),
	})
	return nil
}

// NewPlugin returns an HTTP channel Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }