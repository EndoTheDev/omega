// Package http_channel provides the HTTP/SSE delivery transport as a Channel extension.
package api

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	agentapi "github.com/EndoTheDev/omega/agent/api"
	"github.com/EndoTheDev/omega/core"
)

// runtimeTitleModel resolves the persisted title_model word at the wire boundary.
// config.yaml keeps the choice VERBATIM - "default" is what the settings pane shows back
// - but nothing may route a title call to a model literally NAMED "default": that word
// means "follow the model this chat runs".
func runtimeTitleModel(configured string) string {
	if strings.TrimSpace(configured) == agent.DefaultModelSlot {
		return ""
	}
	return configured
}

// sharedCanvas builds the process's ONE canvas on first use and hands it to every bind.
// The capsule's push closure is last-one-wins, so a per-bind canvas left every mirror but
// the last-started one frozen at its boot-time seed.
type sharedCanvas struct {
	once sync.Once
	c    *agent.Canvas
	err  error
}

// build returns the process canvas, constructing it on first call.
func (sc *sharedCanvas) build(ctx *core.Context, emit func(core.Event)) (*agent.Canvas, error) {
	sc.once.Do(func() { sc.c, sc.err = agent.NewCanvas(ctx, emit) })
	return sc.c, sc.err
}

// HTTPChannel adapts the HTTP server to the core.Channel interface.
type HTTPChannel struct {
	addr       string
	runTimeout time.Duration // detached-run wall-clock cap from config
	titleModel string
	defaultWorkspace string
	agentPrompt   string
	agentMaxTurns int
	agentRuns   agentapi.AgentRunFactory
	publicHosts []string // server.public_hosts: declared public identities
	srv         *Server
	// hub is the process-wide run registry: one per plugin, shared by every bind's server.
	// A second bind must not get a second hub - launchRun registers on the capsule's
	// pinned last-started server, and per-bind hubs leave the transcript/events reads on
	// the other mirrors blind to that run.
	hub *agentapi.RunHub
	// sockets is the process-wide push hub. Per-bind hubs served each bind's /ws from its
	// own subscriber set, so a frame published on one bind never reached a client
	// connected to another - the same multi-bind blindness as the run hub above.
	sockets *agentapi.SocketHub
	canvas *sharedCanvas
	chat *agentapi.ChatCapsule
	// ext is the mount-time registration surface: wire offers the chat-runs service
	// through api.Offer - the journal claims the key and the census names it - never the
	// raw core primitive. A hand-built test channel leaves it nil and skips the offer (the
	// consumer's missing-offer floor).
	ext *agent.ExtensionAPI
}

// Start creates an agent from deps.Ctx and serves HTTP/SSE until ctx is cancelled.
func (c *HTTPChannel) Start(ctx context.Context, deps core.ChannelDeps) error {
	if err := c.wire(deps); err != nil {
		return err
	}
	return c.srv.Serve(ctx, c.addr)
}

// SetAgentRunFactory wires the host's per-agent run factory onto this bind's server.
func (c *HTTPChannel) SetAgentRunFactory(f agentapi.AgentRunFactory) {
	c.agentRuns = f
	if c.srv != nil {
		c.srv.SetAgentRunFactory(f)
	}
}

// SetAgentPromptConfig is the hot-apply hook for the serving agent's live knobs.
func (c *HTTPChannel) SetAgentPromptConfig(prompt string, maxTurns int) {
	c.agentPrompt = prompt
	c.agentMaxTurns = maxTurns
	if c.srv != nil {
		c.srv.SetAgentPrompt(prompt, maxTurns)
	}
}

// wire builds the channel's server and binds it to the process-wide state.
func (c *HTTPChannel) wire(deps core.ChannelDeps) error {
	if deps.Ctx == nil || deps.Ctx.Services().Resolve("provider") == nil {
		return fmt.Errorf("http_channel: no provider configured")
	}
	svc := deps.Ctx.Services().Resolve("provider")
	prov, ok := svc.(agent.Provider)
	if !ok {
		return fmt.Errorf("http_channel: offered service (%T) does not satisfy the provider need (wrong shape)", svc)
	}
	_ = prov
	var store agent.StoreProvider
	if deps.Store != nil {
		st, ok := deps.Store.(agent.StoreProvider)
		if !ok {
			return fmt.Errorf("http_channel: offered service (%T) does not satisfy the store need (wrong shape)", deps.Store)
		}
		store = st
	}
	ag := agent.NewFromContext(deps.Ctx, agent.AgentOptions{})
	c.srv = NewServer(ag, nil, store, deps.Settings, deps.Modules, deps.Registries)
	// Chat panel: the registration is idempotent, and the capsule follows the
	// last-started server - one frontend is live per process.
	if c.chat != nil {
		c.srv.BindChatCapsule(c.chat)
		c.srv.RegisterChatPanel(deps.Ctx)
	}
	if c.hub != nil {
		c.srv.BindRunHub(c.hub)
	}
	// Multi-bind push mirror: every bind's /ws attaches to the ONE process-wide hub.
	// Per-bind hubs left a client on one bind blind to every frame published on another.
	if c.sockets != nil {
		c.srv.BindSocketHub(c.sockets)
	}
	// The chat-runs offer: the first shell-offered capsule-service key. The store
	// capsule's session list reads live-run truth per request through this service's own
	// consumer-owned ChatRunsReader face (harness/store), never a shared type.
	if c.ext != nil {
		if err := c.ext.Offer("chat-runs", c.srv); err != nil {
			return fmt.Errorf("http_channel: chat-runs offer: %w", err)
		}
	}
	if c.canvas == nil {
		c.canvas = &sharedCanvas{}
	}
	canvas, err := c.canvas.build(deps.Ctx, c.srv.CanvasEmit)
	if err != nil {
		return fmt.Errorf("http_channel: canvas: %w", err)
	}
	c.srv.BindCanvas(canvas)
	// Optional-module seams ride the shared rewire.
	c.srv.rewireSeams(deps.Ctx)
	c.srv.SetChannelKnobs(c.runTimeout, runtimeTitleModel(c.titleModel))
	c.srv.SetDefaultWorkspace(c.defaultWorkspace)
	// The serving agent's live knobs: the channel builds its own agent with empty
	// AgentOptions, so the boot cfg never reaches the serving agent - this pair is the
	// only surface they ride.
	c.srv.SetAgentPrompt(c.agentPrompt, c.agentMaxTurns)
	c.srv.SetAgentRunFactory(c.agentRuns)
	c.srv.SetPublicHosts(c.publicHosts)
	c.srv.version = deps.Version
	// The capsule pin goes live LAST, after every server field above is written: the Store
	// publishes a fully-wired server, so a reader that Loads the pin never sees a
	// half-wired one.
	if c.chat != nil {
		c.chat.PinServer(c.srv.Server)
	}
	return nil
}

// RewireSeams implements core.LiveSeams. The shell stays mounted, but the modules it
// serves do not - routes, panels and tool inventory re-read the Context instead of boot
// copies.
func (c *HTTPChannel) RewireSeams(ctx *core.Context) error {
	return c.srv.rewireSeams(ctx)
}

// SetDefaultWorkspace carries the hot knob.
func (c *HTTPChannel) SetDefaultWorkspace(ws string) {
	c.defaultWorkspace = ws
	if c.srv != nil {
		c.srv.SetDefaultWorkspace(ws)
	}
}

// SetTitleModel applies the title_model knob LIVE: the persisted word is stored verbatim
// (the pane reads it back) and resolved at the wire boundary, exactly like the boot value.
func (c *HTTPChannel) SetTitleModel(configured string) {
	c.titleModel = configured
	if c.srv != nil {
		c.srv.SetChannelKnobs(c.runTimeout, runtimeTitleModel(configured))
	}
}

// Stop shuts the HTTP server down.
func (c *HTTPChannel) Stop() error {
	return nil
}

// Compile-time assertion that HTTPChannel implements core.Channel.
var _ core.Channel = (*HTTPChannel)(nil)
