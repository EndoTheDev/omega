// Package http_channel provides the HTTP/SSE delivery transport as a
// Channel extension. The HTTP server implementation (server.go) lives
// in this package; the adapter wires it to the Channel seam.
//
// Seam: channel (additive — multiple channels can run simultaneously).
package http_channel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

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
	titleModel string        // title_model knob from config
	srv        *Server
	// hub is the process-wide run registry (t_c6f13a82): one per
	// plugin, shared by every bind's server. A second bind must not get
	// a second hub - the action pipe's launchRun registers on the
	// capsule's pinned last-started server, and with per-bind hubs the
	// transcript/events reads on the other mirrors go blind to that run.
	hub *runHub
	// canvas is the per-process canvas builder every bind shares
	// (t_1c9793a8): one panel state behind every mirror's snapshot
	// GET, action pipe and delta fan-out - the canvas is per-process,
	// not per-bind (the same ruling the run hub already follows).
	canvas *sharedCanvas
	// chat is the canvas capsule the plugin registered (chat-last 1/3):
	// Start binds it to this channel's server before the canvas runtime
	// builds - the snapshot and the action pipe meet the served state.
	chat *chatCapsule
}

// Start creates an agent from deps.Ctx and serves HTTP/SSE until the
// context is cancelled. Blocks.
func (c *HTTPChannel) Start(ctx context.Context, deps core.ChannelDeps) error {
	if err := c.wire(deps); err != nil {
		return err
	}
	return c.srv.Serve(ctx, c.addr)
}

// wire builds the channel's server and binds it to the process-wide
// state (hub, chat capsule, canvas). Split from Start so a host or
// test can wire a mirror without serving it.
func (c *HTTPChannel) wire(deps core.ChannelDeps) error {
	if deps.Ctx == nil || deps.Ctx.Provider == nil {
		return fmt.Errorf("http_channel: no provider configured")
	}
	ag := core.NewFromContext(deps.Ctx, core.AgentOptions{})
	c.srv = NewServer(ag, nil, deps.Store, deps.Settings)
	// Chat panel (chat-last 1/3): bind the plugin's registered capsule
	// to THIS server before the canvas runtime builds over the
	// providers, and register it with the canvas seam here - gated on
	// the channel actually starting (chat-last 2b ruling: one chat
	// panel per live frontend; a TUI-only process never reaches Start,
	// so its canvas carries no dead http chat panel). Multi-bind binds:
	// the registration is idempotent, the capsule follows the
	// last-started server - one frontend is live per process.
	if c.chat != nil {
		c.srv.chat = c.chat
		c.srv.chat.srv = c.srv
		c.srv.chat.registerAt(deps.Ctx)
	}
	// Multi-bind mirror (t_c6f13a82): every bind's server joins the one
	// process-wide run registry, so a run launched through the action
	// pipe on any mirror is visible to the transcript/events reads and
	// the SSE attach on every other - the runHub is the reattach
	// surface's source of truth and it is per-process, not per-bind.
	if c.hub != nil {
		c.srv.runs = c.hub
	}
	// Canvas transport (arc 3b): the runtime is built from the fully
	// mounted Context after MountAll - the same registration point
	// every other seam reads (card t_058edeb3). Deltas fan out through
	// the server's run hub (server_canvas.go). ONE canvas per process
	// (t_1c9793a8): every bind's server serves the same instance, so
	// every mirror's snapshot GET tracks the pushed panel state.
	if c.canvas == nil {
		c.canvas = &sharedCanvas{}
	}
	canvas, err := c.canvas.build(deps.Ctx, c.srv.canvasEmit)
	if err != nil {
		return fmt.Errorf("http_channel: canvas: %w", err)
	}
	c.srv.canvas = canvas
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
	return nil
}

// Stop shuts the HTTP server down. The host calls this after the
// context is cancelled; Serve returns on cancellation, so Stop is a
// no-op safety net.
func (c *HTTPChannel) Stop() error {
	return nil
}

// Compile-time assertion that HTTPChannel implements core.Channel.
var _ core.Channel = (*HTTPChannel)(nil)
