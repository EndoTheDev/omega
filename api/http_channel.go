// Package http_channel provides the HTTP/SSE delivery transport as a
// Channel extension. The HTTP server implementation (server.go) lives
// in this package; the adapter wires it to the Channel seam.
//
// Seam: channel (additive — multiple channels can run simultaneously).
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

// runtimeTitleModel resolves the persisted title_model word at the wire
// boundary (card t_b403ad88). config.yaml keeps the choice VERBATIM -
// "default" is what the settings pane shows back - but the base server
// must never route a title call to a model literally NAMED "default":
// that word means "follow the model this chat runs", which is exactly
// the server's no-knob behaviour (the title call streams through the
// per-request pinned provider). So the word lands as "" here and the
// resolution stays at the call site, every call.
func runtimeTitleModel(configured string) string {
	if strings.TrimSpace(configured) == agent.DefaultModelSlot {
		return ""
	}
	return configured
}

// sharedCanvas builds the process's ONE canvas on first use and hands
// every bind's server the same instance (t_1c9793a8). A per-bind canvas
// left every mirror but the last-started one frozen at its boot-time
// seed: the capsule's push closure is last-one-wins, so only one
// bind's canvas ever tracked panel state, and the other mirrors' GET
// /ui/canvas served the seed forever - journey 30's variant switch
// back read that frozen snapshot and the flip never rendered. The emit
// closure rides the first starter's server; its fan-out is the shared
// run hub, so deltas reach every mirror's subscribers either way.
type sharedCanvas struct {
	once sync.Once
	c    *agent.Canvas
	err  error
}

// build returns the process canvas, building it on first call from
// the fully mounted Context (the same registration point every other
// seam reads). Later calls reuse the instance - one canvas, one
// SetPanelUpdate installation, one panel state per process.
func (sc *sharedCanvas) build(ctx *core.Context, emit func(core.Event)) (*agent.Canvas, error) {
	sc.once.Do(func() { sc.c, sc.err = agent.NewCanvas(ctx, emit) })
	return sc.c, sc.err
}

// HTTPChannel adapts the HTTP server to the core.Channel interface.
// It creates one agent from the Context and reuses it for all requests.
type HTTPChannel struct {
	addr       string
	runTimeout time.Duration // detached-run wall-clock cap from config
	// titleModel is the title_model knob from config, VERBATIM - the
	// persisted word (including "default") is what the pane reads back.
	// runtimeTitleModel resolves it at the wire boundary.
	titleModel string
	// defaultWorkspace is the server.default_workspace knob: the
	// tools' fallback workdir for workspace-less sessions (card
	// t_ccf74d88). Empty = unset - the cwd fallback stands.
	defaultWorkspace string
	// The serving agent's live knobs (card t_f78acfd6): the custom
	// prompt (system_prompt) and the per-run turn cap (max_turns).
	// The hot-apply writes BOTH sides - these fields cover a bind that
	// wires later (wire applies them at start), the write-through on
	// SetAgentPromptConfig covers the live bind.
	agentPrompt   string
	agentMaxTurns int
	// agentRuns is the host's per-agent run factory (card t_a7fab745):
	// a session owned by an agent home runs on that home's agent. Same
	// two-sided discipline - the field covers a bind that wires later,
	// the write-through covers a live server.
	agentRuns   agentapi.AgentRunFactory
	publicHosts []string // server.public_hosts: declared public identities
	srv         *Server
	// hub is the process-wide run registry (t_c6f13a82): one per
	// plugin, shared by every bind's server. A second bind must not get
	// a second hub - the action pipe's launchRun registers on the
	// capsule's pinned last-started server, and with per-bind hubs the
	// transcript/events reads on the other mirrors go blind to that run.
	hub *agentapi.RunHub
	// canvas is the per-process canvas builder every bind shares
	// (t_1c9793a8): one panel state behind every mirror's snapshot
	// GET, action pipe and delta fan-out - the canvas is per-process,
	// not per-bind (the same ruling the run hub already follows).
	canvas *sharedCanvas
	// chat is the canvas capsule the plugin registered (chat-last 1/3):
	// Start binds it to this channel's server before the canvas runtime
	// builds - the snapshot and the action pipe meet the served state.
	chat *agentapi.ChatCapsule
	// ext is the mount-time registration surface (Endo ruling
	// 2026-11-04): wire offers the chat-runs service through the
	// proper api.Offer path - the journal claims the key and the
	// census names it - never the raw core primitive. Set by the
	// plugin at Mount; a hand-built test channel carries nil and
	// skips the offer (the consumer's missing-offer floor).
	ext *agent.ExtensionAPI
}

// Start creates an agent from deps.Ctx and serves HTTP/SSE until the
// context is cancelled. Blocks.
func (c *HTTPChannel) Start(ctx context.Context, deps core.ChannelDeps) error {
	if err := c.wire(deps); err != nil {
		return err
	}
	return c.srv.Serve(ctx, c.addr)
}

// SetAgentRunFactory wires the host's per-agent run factory (card
// t_a7fab745) onto this bind's server. Same two-sided discipline as
// SetAgentPromptConfig: the field covers a bind that wires later, the
// write-through covers a live server.
func (c *HTTPChannel) SetAgentRunFactory(f agentapi.AgentRunFactory) {
	c.agentRuns = f
	if c.srv != nil {
		c.srv.SetAgentRunFactory(f)
	}
}

// SetAgentPromptConfig is the hot-apply hook for the serving agent's
// live knobs (card t_f78acfd6): a settings save of system_prompt or
// max_turns lands on the store-then-apply double - the fields cover a
// bind that wires later, the write-through covers the running server.
// Mirrors SetDefaultWorkspace's discipline exactly.
func (c *HTTPChannel) SetAgentPromptConfig(prompt string, maxTurns int) {
	c.agentPrompt = prompt
	c.agentMaxTurns = maxTurns
	if c.srv != nil {
		c.srv.SetAgentPrompt(prompt, maxTurns)
	}
}

// wire builds the channel's server and binds it to the process-wide
// state (hub, chat capsule, canvas). Split from Start so a host or
// test can wire a mirror without serving it.
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
	// Chat panel (chat-last 1/3): bind the plugin's registered capsule
	// to THIS server before the canvas runtime builds over the
	// providers, and register it with the canvas seam here - gated on
	// the channel actually starting (chat-last 2b ruling: one chat
	// panel per live frontend; a serve-less process never reaches Start,
	// so its canvas carries no dead http chat panel). Multi-bind binds:
	// the registration is idempotent, the capsule follows the
	// last-started server - one frontend is live per process.
	if c.chat != nil {
		c.srv.BindChatCapsule(c.chat)
		c.srv.RegisterChatPanel(deps.Ctx)
	}
	// Multi-bind mirror (t_c6f13a82): every bind's server joins the one
	// process-wide run registry, so a run launched through the action
	// pipe on any mirror is visible to the transcript/events reads and
	// the SSE attach on every other - the agentapi.RunHub is the reattach
	// surface's source of truth and it is per-process, not per-bind.
	if c.hub != nil {
		c.srv.BindRunHub(c.hub)
	}
	// The chat-runs offer (the shell-ring shrink wave 2, Endo ruling
	// 2026-11-04): the first shell-offered capsule-service key. The
	// store capsule's session list reads the live-run truth per
	// request through this service - its OWN consumer-owned
	// ChatRunsReader face (harness/store), never a shared type. The
	// offer rides the proper api.Offer surface: the journal claims
	// the key, the one-tenant ledger holds it, the dismount releases
	// it - the raw deps.Ctx.ApplyOffer primitive is the mount
	// machinery's own, not a bypass (the enforcement test now pins
	// ApplyOffer call sites too, seams_test). The hub binds per this
	// channel's server, so the offered reader is the bound one - the
	// multi-bind mirror law rides through untouched; every bind
	// re-offers, a same-tenant replace (Q7).
	if c.ext != nil {
		if err := c.ext.Offer("chat-runs", c.srv); err != nil {
			return fmt.Errorf("http_channel: chat-runs offer: %w", err)
		}
	}
	// Canvas transport (arc 3b): the runtime is built from the fully
	// mounted Context after MountAllAPI - the same registration point
	// every other seam reads (card t_058edeb3). Deltas fan out through
	// the server's run hub (server_canvas.go). ONE canvas per process
	// (t_1c9793a8): every bind's server serves the same instance, so
	// every mirror's snapshot GET tracks the pushed panel state.
	if c.canvas == nil {
		c.canvas = &sharedCanvas{}
	}
	canvas, err := c.canvas.build(deps.Ctx, c.srv.CanvasEmit)
	if err != nil {
		return fmt.Errorf("http_channel: canvas: %w", err)
	}
	c.srv.BindCanvas(canvas)
	// Optional-module seams ride the shared rewire (v0.12 E2): the same
	// field set the old mount-time copies set, now read live per
	// request and re-readable after every toggle.
	c.srv.rewireSeams(deps.Ctx)
	// Inspector seams (card t_db430fea): read-only views for the
	// /cron/jobs, /skills and /logs endpoints. All optional - the
	// handlers degrade to empty lists / 404 when a module is absent.
	c.srv.SetChannelKnobs(c.runTimeout, runtimeTitleModel(c.titleModel))
	// The default workspace knob rides its own setter (the hot apply
	// calls it live): the wire applies the boot value here.
	c.srv.SetDefaultWorkspace(c.defaultWorkspace)
	// The serving agent's live knobs (card t_f78acfd6): the boot
	// values for the custom prompt + turn cap (the hot-apply fields
	// below). The channel builds its own agent with empty
	// AgentOptions - the boot cfg never reached the serving agent
	// through NewFromContext, so this pair is every surface they ride.
	c.srv.SetAgentPrompt(c.agentPrompt, c.agentMaxTurns)
	// The per-agent run factory (card t_a7fab745): the host wires it
	// once at boot; every bind's server serves an agent-owned session on
	// that home's agent.
	c.srv.SetAgentRunFactory(c.agentRuns)
	c.srv.SetPublicHosts(c.publicHosts)
	c.srv.version = deps.Version
	// The capsule pin goes live LAST, after every server field above
	// is written: the Store publishes (release) a fully-wired server,
	// so a reader that Loads the pin (acquire) never sees a
	// half-wired one - a mid-wire pin let another bind's canvas build
	// read this server's fields while wire still wrote them
	// (t_1c9793a8). Last wire finished wins, same one-frontend law.
	if c.chat != nil {
		c.chat.PinServer(c.srv.Server)
	}
	return nil
}

// RewireSeams implements core.LiveSeams (v0.12 E2): the host calls it
// after every successful live module toggle. The shell stays
// mounted, but the modules it serves do not - the routes, panels, and
// tool inventory re-read the Context instead of the boot copies.
func (c *HTTPChannel) RewireSeams(ctx *core.Context) error {
	return c.srv.rewireSeams(ctx)
}

// SetDefaultWorkspace carries the hot knob (card t_ccf74d88): the
// field update covers a not-yet-wired bind, a wired server gets the
// live value at once.
func (c *HTTPChannel) SetDefaultWorkspace(ws string) {
	c.defaultWorkspace = ws
	if c.srv != nil {
		c.srv.SetDefaultWorkspace(ws)
	}
}

// Stop shuts the HTTP server down. The host calls this after the
// context is cancelled; Serve returns on cancellation, so Stop is a
// no-op safety net.
func (c *HTTPChannel) Stop() error {
	return nil
}

// Compile-time assertion that HTTPChannel implements core.Channel.
var _ core.Channel = (*HTTPChannel)(nil)
