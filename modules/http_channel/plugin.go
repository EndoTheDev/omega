package http_channel

import (
	"fmt"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// Plugin implements agent.Plugin for the HTTP channel. It reads the
// port from config and appends an HTTPChannel to ctx.Channels.
type Plugin struct{}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string       { return "http_channel" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "HTTP channel: the web UI API server - chat streaming, sessions, inspector endpoints"
}
func (p *Plugin) Provides() []string { return []string{"channel"} }
func (p *Plugin) Requires() []string { return nil }

// Mount creates an HTTPChannel from config and appends it to
// ctx.Channels. The host starts it (and any other mounted channels)
// after MountAll completes.
func (p *Plugin) Mount(ctx *agent.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["http_channel"].(Config); ok {
		cfg = c
	}
	port := cfg.Port
	if port <= 0 {
		port = 8099
	}
	// Bind loopback only by default: the API is unauthenticated and /chat
	// can execute shell commands via the tool registry — it must not be
	// reachable from the LAN (a Content-Type-blind JSON decoder also makes
	// cross-origin no-cors POSTs exploitable from any LAN browser).
	// Additional addresses (e.g. the Tailscale IP for Traefik) are an
	// explicit config opt-in. Upgrade path when omega.endothe.dev deploys:
	// Pocket ID wall in front of every path.
	binds := cfg.Bind
	if len(binds) == 0 {
		binds = []string{"127.0.0.1"}
	}
	for _, b := range binds {
		ctx.Channels = append(ctx.Channels, &HTTPChannel{
			addr:       fmt.Sprintf("%s:%d", b, port),
			runTimeout: time.Duration(cfg.RunTimeoutSeconds()) * time.Second,
			titleModel: cfg.TitleModel,
		})
	}
	return nil
}

// NewPlugin returns an HTTP channel Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }
