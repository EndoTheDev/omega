package api

import (
	"fmt"
	"time"

	"github.com/EndoTheDev/omega/agent"
	agentapi "github.com/EndoTheDev/omega/agent/api"
	"github.com/EndoTheDev/omega/core"
)

// Plugin implements agent.Plugin for the HTTP channel. It reads the
// port from config and appends an HTTPChannel to ctx.Channels.
type Plugin struct {
	// hub is the process-wide run registry (t_c6f13a82): one hub, shared
	// by every bind's channel start. Multi-bind mirrors serve one
	// logical frontend - a run started on any mirror must be visible to
	// the reads and reattach on all of them.
	hub *agentapi.RunHub
	// chat is the conversation's canvas capsule (chat-last 1/3): one
	// instance per plugin, registered with the canvas seam below and
	// shared by every bind's channel start (multi-bind: the last
	// started server serves it - one frontend is live per process).
	chat *agentapi.ChatCapsule
	// canvas is the per-process canvas builder (t_1c9793a8): one
	// canvas behind every bind's server - the panel state the snapshot
	// GET serves, the action pipe routes and the deltas carry must be
	// the same on all mirrors.
	canvas *sharedCanvas
	// api is the mount-time registration surface, stashed for the
	// channels' wire (Endo ruling 2026-11-04): the chat-runs offer
	// rides the proper api.Offer path at wire time - claimed on the
	// journal, named in the census - instead of the raw core
	// primitive.
	api *agent.ExtensionAPI
	// mounted carries the channels Mount appended - the value Dismount
	// removes by identity (the E1 dismount proof).
	mounted []core.Channel
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

// MountWithAPI creates an HTTPChannel from config and registers it on
// ctx.Channels. The host starts it (and any other mounted channels)
// after the mount completes.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["http_channel"].(Config); ok {
		cfg = c
	}
	port := cfg.Port
	if port <= 0 {
		port = 8099
	}
	// Chat panel (chat-last 1/3, chat-last 2b ruling): the conversation's
	// canvas surface. The capsule is created here (one instance, carried
	// on every bind) but NOT registered with the canvas seam at Mount -
	// registration is gated on the frontend actually starting
	// (HTTPChannel.Start, below). One chat panel per live frontend: a
	// serve-less process never reaches Start, so its canvas stays free of
	// the dead "chat channel not started" duplicate.
	p.chat = agentapi.NewChatCapsule()

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
	// One run registry per process (t_c6f13a82): every mirror joins it
	// at channel start, so action-pipe launches, reattach reads and the
	// SSE attach agree on what is live regardless of which bind speaks.
	p.hub = &agentapi.RunHub{}
	p.canvas = &sharedCanvas{}
	p.api = api
	for _, b := range binds {
		ch := &HTTPChannel{
			addr:       fmt.Sprintf("%s:%d", b, port),
			runTimeout: time.Duration(cfg.RunTimeoutSeconds()) * time.Second,
			titleModel: cfg.TitleModel,
			chat:       p.chat,
			hub:        p.hub,
			canvas:     p.canvas,
			ext:        p.api,
		}
		p.mounted = append(p.mounted, ch)
		if err := api.RegisterChannel(ch); err != nil {
			return err
		}
	}
	// Web tools (v0.13 shell law 1, card t_f76ecdbe): the former
	// modules/web capsule dissolved into the channel - serving and
	// these tools are the channel's job, one place, one word. The
	// pair is shell vocabulary: always mounted with the channel at
	// its natural names (web.search, web.fetch - the builtin tier
	// registers natural names, no namespace pairing), never in the
	// disable list.
	wt := agentapi.NewWebTools(cfg.WebTools)
	for name, tool := range wt.Tools() {
		if err := api.RegisterTool(name, tool); err != nil {
			return err
		}
	}
	return nil
}

// NewPlugin returns an HTTP channel Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }

// Dismount removes the channels Mount appended, by identity (the E1
// dismount proof). The shell never lands in the disable list -
// Dismount exists for the teardown proof, not for toggling.
func (p *Plugin) Dismount(ctx *core.Context) error {
	chans := ctx.Channels[:0]
	for _, ch := range ctx.Channels {
		dead := false
		for _, m := range p.mounted {
			if ch == m {
				dead = true
				break
			}
		}
		if !dead {
			chans = append(chans, ch)
		}
	}
	ctx.Channels = chans
	p.mounted = nil
	return nil
}
