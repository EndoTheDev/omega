package http_channel

import (
	"fmt"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin implements agent.Plugin for the HTTP channel. It reads the
// port from config and appends an HTTPChannel to ctx.Channels.
type Plugin struct {
	// chat is the conversation's canvas capsule (chat-last 1/3): one
	// instance per plugin, registered with the canvas seam below and
	// shared by every bind's channel start (multi-bind: the last
	// started server serves it - one frontend is live per process).
	chat *chatCapsule
}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string { return "http_channel" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "HTTP channel: the web UI API server - chat streaming, sessions, inspector endpoints"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "http_channel" }

func (p *Plugin) Provides() []string { return []string{"channel"} }
func (p *Plugin) Requires() []string { return nil }

// Mount creates an HTTPChannel from config and appends it to
// ctx.Channels. The host starts it (and any other mounted channels)
// after MountAll completes.
func (p *Plugin) Mount(ctx *core.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["http_channel"].(Config); ok {
		cfg = c
	}
	port := cfg.Port
	if port <= 0 {
		port = 8099
	}
	// Chat panel (chat-last 1/3, card t_ec45b670): the conversation's
	// canvas surface rides the same registration seam - one capsule,
	// appended here, bound to its server at channel start (the Server
	// exists from then on, and newCanvas runs after the handoff).
	p.chat = newChatCapsule()
	ctx.PanelProviders = append(ctx.PanelProviders, p.chat)
	// Chat panel (chat-last 1/3, card t_ec45b670): the conversation's
	// canvas surface rides the same registration seam. The capsule is
	// declared at Mount and appended to ctx.PanelProviders; the channel
	// carries it so Start's handoff binds it (see below).

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
			chat:       p.chat,
		})
	}
	// Chat panel (chat-last 1/3, card t_ec45b670): the conversation's
	// canvas surface rides the same registration seam. The capsule is
	// declared HERE (one instance) and bound to its server at channel
	// start, which is when the Server exists - newCanvas runs after
	// the handoff, so the snapshot and the action pipe route to it.
	return nil
}

// NewPlugin returns an HTTP channel Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }
