package terminal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// Plugin adapts the terminal capsule into omega's plugin system. It
// provides no seams of its own - no commands, no panels: the module's
// whole surface is its REST routes (the pane streams those) plus the
// lifecycle fence that owns the shells. (The model's shell verb family
// rides the same package through the separate builtin shell plugin -
// shelltool.go.) Mounts through the
// ExtensionAPI exclusively; the trust-model ruling (the package
// comment) is part of the design - the route surface is the whole
// feature and it is unconditionally available like the files routes.
//
// The routes (all under the module's own "/terminal" namespace):
//
//	GET    /terminal/sessions     - the store snapshot (claim is POST)
//	POST   /terminal/sessions     - claim-else-spawn one session
//	                               (?session=<id>: the focused chat row
//	                               roots the spawn's cwd - t_62d72028)
//	GET    /terminal/{id}/stream  - the output SSE (replays the tail)
//	POST   /terminal/{id}/input   - keystrokes/paste into the shell
//	POST   /terminal/{id}/resize  - the pane's rows/cols (SIGWINCH)
//	DELETE /terminal/{id}         - kill + forget the shell
//
// Removal proof: TestTerminalRemovalProof (cmd/omega/removal_test.go).
type Plugin struct {
	store *Store
	// storeFn resolves the workspace-pin face LIVE per claim (the seam
	// resolve per request - a toggle swaps the offer under the
	// handlers' feet, exactly the Files tab's law). nil = the chain
	// falls through to the knob and the cwd.
	storeFn func() sessionStores
	// defaultWorkspace is the `default_workspace` knob (card
	// t_62d72028): the spawn workspace chain's step 2. Written by
	// SetDefaultWorkspace (boot + the settings seam's hot-apply)
	// while claims read it concurrently, so it rides an atomic
	// value - never a bare field.
	defaultWorkspace atomic.Value
}

// NewPlugin builds the terminal plugin. workdir is the spawn fallback
// (omega's process working directory); defaultWorkspace is the
// server.default_workspace knob's boot value ("" = unset).
func NewPlugin(workdir, defaultWorkspace string) *Plugin {
	p := &Plugin{store: NewStore(spawnProcess, workdir, nil)}
	p.defaultWorkspace.Store(defaultWorkspace)
	return p
}

// SetDefaultWorkspace is the host's hot-apply hook (card t_62d72028):
// the settings pane (and the config watcher) push a changed
// default_workspace so the NEXT spawn follows it live, no restart -
// the files capsule's knob, same discipline.
func (p *Plugin) SetDefaultWorkspace(ws string) {
	p.defaultWorkspace.Store(ws)
}

// NewPluginWithStore is the test constructor (the store's spawn seam
// is the injection point; handlers stay the production ones).
func NewPluginWithStore(store *Store) *Plugin {
	return &Plugin{store: store}
}

// Store returns the plugin's store (the lifecycle fence drives it).
func (p *Plugin) Store() *Store { return p.store }

// Name returns the plugin name (the modules registry row, the
// capsule namespace owner).
func (p *Plugin) Name() string { return "terminal" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "Terminal: the webui's integrated shell (xterm.js over a pty stream)"
}

// Provides lists the seams this plugin mounts: none. The terminal is
// a pure route capsule - no tools (the shell.run/list/read/kill family
// rides the same package through the separate builtin shell plugin -
// shelltool.go), no commands, no panels.
func (p *Plugin) Provides() []string { return nil }

// Requires lists seams that must mount before this plugin.
func (p *Plugin) Requires() []string { return nil }

// ConfigKey names the config section this plugin reads from
// ctx.Configs. The terminal is restriction-free by ruling - no
// config section exists.
func (p *Plugin) ConfigKey() string { return "" }

// Toggleable marks the terminal as a module capsule: the config.yaml
// disabled: list may name it (the Modules tab's two-step toggle owns
// the write, the lifecycle stop kills the shells).
func (p *Plugin) Toggleable() bool { return true }

// MountWithAPI implements agent.APIPlugin: the route surface + the
// lifecycle fence (the pruning lives in the fence's start; the stop
// kills every session - a toggle-off or a dismount reaps its shells).
// The claim is the module name: /terminal here, cross-tenant claims
// of the same route patterns are the registry's refusal.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	// the workspace-pin face resolves LIVE per claim (card t_62d72028):
	// nil-safe absence rides - the chain falls through to the knob
	p.storeFn = func() sessionStores { return seam[sessionStores](ctx, "store") }
	if err := api.RegisterRoute("GET /terminal/sessions", p.handleList); err != nil {
		return err
	}
	if err := api.RegisterRoute("POST /terminal/sessions", p.handleClaim); err != nil {
		return err
	}
	if err := api.RegisterRoute("GET /terminal/{id}/stream", p.handleStream); err != nil {
		return err
	}
	if err := api.RegisterRoute("POST /terminal/{id}/input", p.handleInput); err != nil {
		return err
	}
	if err := api.RegisterRoute("POST /terminal/{id}/resize", p.handleResize); err != nil {
		return err
	}
	if err := api.RegisterRoute("DELETE /terminal/{id}", p.handleKill); err != nil {
		return err
	}
	return api.RegisterLifecycle("sessions", p.start, p.stop)
}

func (p *Plugin) start(ctx context.Context, deps core.StartDeps) error {
	p.store.Start()
	return nil
}

func (p *Plugin) stop() error {
	return p.store.Stop()
}

// claimReply is POST /terminal/sessions's body.
type claimReply struct {
	ID      string `json:"id"`
	Existed bool   `json:"claimed"` // false = freshly spawned
}

// handleClaim claims-else-spawns one session (the pane's open). The
// reattach scan, the spawn and the claim mark ride one lock in
// Store.Claim; the stream's subscribe below clears the mark - the
// claim/attach gap cannot hand the shell to a second pane. The spawn
// lands in the RESOLVED workspace (card t_62d72028): the focused
// session's pinned root (?session=), else the default_workspace knob,
// else omega's process cwd - the same chain the Files tab and the
// shell tool resolve, so a freshly spawned shell starts at pwd ==
// the workspace the pane sits on.
func (p *Plugin) handleClaim(w http.ResponseWriter, r *http.Request) {
	s, claimed, err := p.store.Claim(p.resolveSpawnDir(r))
	if err != nil {
		var capErr *capError
		if errors.As(err, &capErr) {
			httpError(w, err, http.StatusConflict)
		} else {
			httpError(w, err, http.StatusServiceUnavailable)
		}
		return
	}
	writeJSON(w, http.StatusOK, claimReply{ID: s.id, Existed: claimed})
}

// resolveSpawnDir is the spawn workspace law (card t_62d72028), the
// Files tab's resolution chain wearing the terminal's face:
//  1. the focused session's pinned workspace (the store row), live:
//     every claim re-reads it, so a re-pin lands on the next spawn;
//  2. else the configured default workspace (the hot knob);
//  3. else "" - Store.Claim's fallback, the process cwd (the shape
//     the pre-law spawn always used).
//
// A session id the store does not know (a draft, an unknown id, a
// no-store lane) simply falls through the chain - resolution never
// fails on it. The pane sends the ID, never a root.
func (p *Plugin) resolveSpawnDir(r *http.Request) string {
	if id := strings.TrimSpace(r.URL.Query().Get("session")); id != "" && p.storeFn != nil {
		if sess := p.storeFn(); sess != nil {
			if s, err := sess.GetSession(r.Context(), id); err == nil &&
				strings.TrimSpace(s.Workspace) != "" {
				return s.Workspace
			}
		}
	}
	if ws, _ := p.defaultWorkspace.Load().(string); strings.TrimSpace(ws) != "" {
		return ws
	}
	return ""
}

// handleList answers the store snapshot, oldest first.
func (p *Plugin) handleList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": p.store.List()})
}

// handleStream is the output SSE: replay the tail first, then live
// events ("o" output chunks, "x" exit), pings keeping the stream warm.
// Unknown id = 404; a known id streams even after exit (the replay +
// exit frame is how a page reload shows the dead shell's tail).
func (p *Plugin) handleStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s := p.store.Get(id)
	if s == nil {
		httpError(w, fmt.Errorf("no such terminal"), http.StatusNotFound)
		return
	}
	// the adopt: a stream opening the claimed session clears the
	// claim mark inside Session.subscribe (one lock, no replay/live gap)
	sub, replay, alreadyExited := s.subscribe()
	defer s.unsubscribe(sub)

	flusher, ok := setupSSE(w)
	if !ok {
		return
	}
	if len(replay) > 0 {
		writeSSE(w, outputFrame(replay))
		flusher.Flush()
	}
	// a reattach to an already-dead shell: the exit frame carries the
	// recorded code and is the stream's last (no future events exist)
	if alreadyExited {
		s.mu.Lock()
		code := s.exitCode
		s.mu.Unlock()
		writeSSE(w, frameJSON(wireFrame{T: "x", C: &code}))
		flusher.Flush()
		return
	}
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()

	// the pane fell behind (publish dropped it) - one miss frame, the
	// pane reconnects and replays
	if sub.missed {
		writeSSE(w, frameJSON(wireFrame{T: "m"}))
		flusher.Flush()
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if sub.checkMissed() { // a silent drop still needs the wake
				writeSSE(w, frameJSON(wireFrame{T: "m"}))
				flusher.Flush()
				return
			}
			// the SSE comment ping keeps middleboxes from dropping
			// an idle stream (a quiet shell is a legal state)
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, ok := <-sub.ch:
			if !ok {
				// the session was removed under us (kill, prune,
				// cap law) - the close is the signal
				writeSSE(w, frameJSON(wireFrame{T: "m"}))
				flusher.Flush()
				return
			}
			if sub.checkMissed() {
				writeSSE(w, frameJSON(wireFrame{T: "m"}))
				flusher.Flush()
				return
			}
			switch ev.kind {
			case 'o':
				writeSSE(w, outputFrame(ev.data))
			case 'x':
				writeSSE(w, frameJSON(wireFrame{T: "x", C: &ev.code}))
				return // the exit frame is the stream's last
			}
			flusher.Flush()
		}
	}
}

// handleInput writes one keystroke/paste frame into the shell. The
// body is the raw bytes (octet-stream: control chars are data, JSON
// would quote them into ambiguity); a 64 KiB ceiling stops a file
// dump arriving as "a paste".
func (p *Plugin) handleInput(w http.ResponseWriter, r *http.Request) {
	s := p.store.Get(r.PathValue("id"))
	if s == nil {
		httpError(w, fmt.Errorf("no such terminal"), http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxInput))
	if err != nil {
		httpError(w, fmt.Errorf("input over the ceiling: %w", err), http.StatusRequestEntityTooLarge)
		return
	}
	if len(body) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"written": 0})
		return
	}
	if _, err := s.write(body); err != nil {
		httpError(w, err, http.StatusGone) // an exited shell accepts no input
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"written": len(body)})
}

// handleResize sets the pty's window: {rows, cols}. A resize wakes
// curses programs via SIGWINCH; the numbers are ints on the wire.
func (p *Plugin) handleResize(w http.ResponseWriter, r *http.Request) {
	s := p.store.Get(r.PathValue("id"))
	if s == nil {
		httpError(w, fmt.Errorf("no such terminal"), http.StatusNotFound)
		return
	}
	var req struct {
		Rows int `json:"rows"`
		Cols int `json:"cols"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512)).Decode(&req); err != nil {
		httpError(w, fmt.Errorf("resize: %w", err), http.StatusBadRequest)
		return
	}
	if req.Rows < 1 || req.Rows > 10000 || req.Cols < 1 || req.Cols > 10000 {
		httpError(w, fmt.Errorf("resize: rows/cols out of 1..10000"), http.StatusBadRequest)
		return
	}
	if err := s.resize(uint16(req.Rows), uint16(req.Cols)); err != nil {
		httpError(w, err, http.StatusGone)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": req.Rows, "cols": req.Cols})
}

// handleKill kills + forgets one session (the pane's kill affordance
// and the cap/leave path). Missing = idempotent 204.
func (p *Plugin) handleKill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	gone := p.store.Drop(id)
	w.WriteHeader(http.StatusNoContent)
	_ = gone
}

// -- small shared writers ------------------------------------------------

func setupSSE(w http.ResponseWriter) (http.Flusher, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return nil, false
	}
	return flusher, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return // the client is gone; nothing to report to
	}
}

func httpError(w http.ResponseWriter, err error, status int) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

// -- the SSE wire frames ------------------------------------------------

// wireFrame is the SSE "data:" payload. t: "o" output (d = the
// base64 chunk), "x" exit (c = the child's exit code), "m" missed
// (the pane fell behind or the session vanished - reconnect+replay).
type wireFrame struct {
	T string `json:"t"`
	D string `json:"d,omitempty"`
	C *int   `json:"c,omitempty"`
}

// writeSSE writes one data frame. The newline risk is zero: shell
// bytes never ride the wire raw - the frames are JSON, the payload is
// base64.
func writeSSE(w io.Writer, frame string) {
	fmt.Fprintf(w, "data: %s\n\n", frame)
}

func outputFrame(d []byte) string {
	return frameJSON(wireFrame{T: "o", D: base64.StdEncoding.EncodeToString(d)})
}

func frameJSON(f wireFrame) string {
	b, err := json.Marshal(f)
	if err != nil {
		// unreachable shape (bytes -> base64 -> string); a dropped
		// frame beats a broken stream - send the reconnect marker
		return frameJSON(wireFrame{T: "m"})
	}
	return string(b)
}
