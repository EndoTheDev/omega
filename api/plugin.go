package api

import (
	"fmt"
	"time"

	"github.com/EndoTheDev/omega/agent"
	agentapi "github.com/EndoTheDev/omega/agent/api"
	"github.com/EndoTheDev/omega/core"
)

// Plugin implements agent.Plugin for the HTTP channel.
type Plugin struct {
	// hub is the process-wide run registry: one hub, shared by every bind's channel start.
	// Multi-bind mirrors serve one logical frontend - a run started on any mirror must be
	// visible to the reads and reattach on all of them.
	hub *agentapi.RunHub
	// sockets is the process-wide push hub: one fan-out for every bind's /ws, for the same
	// reason as hub - a multi-bind process serves one logical frontend, and a browser
	// attached to one bind must see the frames another bind publishes.
	sockets *agentapi.SocketHub
	chat *agentapi.ChatCapsule
	// canvas is the per-process canvas builder: one canvas behind every bind's server -
	// the panel state the snapshot GET serves, the action pipe routes and the deltas carry
	// must be the same on all mirrors.
	canvas *sharedCanvas
	api *agent.ExtensionAPI
	mounted []core.Channel
}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string { return "http_channel" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "HTTP channel: the web UI API server - chat streaming, sessions, inspector endpoints"
}

// ConfigKey names the config section this plugin reads from ctx.Configs (see.
func (p *Plugin) ConfigKey() string { return "http_channel" }

func (p *Plugin) Provides() []string { return []string{"channel"} }
func (p *Plugin) Requires() []string { return nil }

// MountWithAPI creates an HTTPChannel from config and registers it on ctx.Channels.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["http_channel"].(Config); ok {
		cfg = c
	}
	port := cfg.Port
	if port <= 0 {
		port = 8099
	}
	// Chat panel: the conversation's canvas surface. One chat panel per live frontend: a
	// serve-less process never reaches Start, so its canvas stays free of the dead "chat
	// channel not started" duplicate.
	p.chat = agentapi.NewChatCapsule()

	// Bind loopback only by default: the API is unauthenticated and /chat can execute
	// shell commands via the tool registry — it must not be reachable from the LAN (a
	// Content-Type-blind JSON decoder also makes cross-origin no-cors POSTs exploitable
	// from any LAN browser).
	binds := cfg.Bind
	if len(binds) == 0 {
		binds = []string{"127.0.0.1"}
	}
	// One run registry per process.
	p.hub = &agentapi.RunHub{}
	// ONE push hub per process, the run registry's law one seam over.
	p.sockets = agentapi.NewSocketHub()
	p.canvas = &sharedCanvas{}
	p.api = api
	for _, b := range binds {
		ch := &HTTPChannel{
			addr:             fmt.Sprintf("%s:%d", b, port),
			runTimeout:       time.Duration(cfg.RunTimeoutSeconds()) * time.Second,
			titleModel:       cfg.TitleModel,
			defaultWorkspace: cfg.DefaultWorkspace,
			publicHosts:      cfg.PublicHosts,
			chat:             p.chat,
			hub:              p.hub,
			sockets:          p.sockets,
			canvas:           p.canvas,
			ext:              p.api,
		}
		p.mounted = append(p.mounted, ch)
		if err := api.RegisterChannel(ch); err != nil {
			return err
		}
	}
	// Web tools: the former modules/web capsule dissolved into the channel. The pair is
	// shell vocabulary: always mounted with the channel at its natural names (web.search,
	// web.fetch - the builtin tier registers natural names, no namespace pairing), never
	// in the disable list.
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

// SetDefaultWorkspace is the settings hot-apply hook.
func (p *Plugin) SetDefaultWorkspace(ws string) {
	for _, ch := range p.mounted {
		if h, ok := ch.(*HTTPChannel); ok {
			h.SetDefaultWorkspace(ws)
		}
	}
}

// SetAgentPromptConfig is the settings hot-apply hook for the serving agent's live
// knobs.
func (p *Plugin) SetAgentPromptConfig(prompt string, maxTurns int) {
	for _, ch := range p.mounted {
		if h, ok := ch.(*HTTPChannel); ok {
			h.SetAgentPromptConfig(prompt, maxTurns)
		}
	}
}

// SetAgentRunFactory hands the host's per-agent run factory to every mounted bind.
func (p *Plugin) SetAgentRunFactory(f agentapi.AgentRunFactory) {
	for _, ch := range p.mounted {
		if h, ok := ch.(*HTTPChannel); ok {
			h.SetAgentRunFactory(f)
		}
	}
}

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
