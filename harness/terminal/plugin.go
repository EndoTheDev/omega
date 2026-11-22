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

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
	"github.com/EndoTheDev/omega/harness/store"
)

// Plugin adapts the terminal capsule into omega's plugin system: ONE registration
// carries both faces of the shell domain, the agent's tool family (shell.run /
// shell.list / shell.read / shell.kill over the process: section's registry) and the
// webui's integrated terminal pane (the /terminal pty routes + the lifecycle fence
// that owns the shells).
//
// The module is toggleable, so a `disabled: [terminal]` takes the tools, the routes
// and every live shell with it.
//
// IDENTITY: the module name IS the package/capsule name. The webui's composition
// manifests derive each dockable id from the capsule's ui/ dir (`harness/<name>/ui` ->
// `{ module: "<name>" }`) and the dock's runtime gate joins those ids against the live
// module list, so plugin Name, the package dir and the ui dir agree by construction.
// The capsule answered "shell" while its dir and dockable id said "terminal", and the
// Terminal chip therefore survived a module toggle-off. The model-facing tool names
// stay `shell.*` - that is API vocabulary, not the module identity (see
// RegisterBuiltinTool).
//
// The route surface is the whole feature, so it is unconditionally available like the
// files routes.
//
// Removal proof: TestTerminalRemovalProof (cmd/omega/removal_test.go).
type Plugin struct {
	store *Store
	provider *ShellProvider
	// storeFn resolves the workspace-pin face LIVE per claim (the seam resolve per request
	// - a toggle swaps the offer under the handlers' feet, exactly the Files tab's law).
	// nil = the chain falls through to the knob and the cwd.
	storeFn func() sessionStores
	// defaultWorkspace is the `default_workspace` knob. Written by SetDefaultWorkspace
	// (boot + the settings seam's hot-apply) while claims read it concurrently, so it
	// rides an atomic value - never a bare field.
	defaultWorkspace atomic.Value
}

func NewPlugin(workdir, defaultWorkspace string) *Plugin {
	p := &Plugin{store: NewStore(spawnProcess, workdir, nil)}
	p.defaultWorkspace.Store(defaultWorkspace)
	return p
}

func (p *Plugin) SetDefaultWorkspace(ws string) {
	p.defaultWorkspace.Store(ws)
}

func NewPluginWithStore(store *Store) *Plugin {
	return &Plugin{store: store}
}

func (p *Plugin) Store() *Store { return p.store }

func (p *Plugin) Name() string { return "terminal" }

func (p *Plugin) Description() string {
	return "shell: the agent's shell tool family (shell.run/list/read/kill) + the webui's integrated terminal (xterm.js over a pty stream)"
}

func (p *Plugin) Provides() []string { return []string{"tools"} }

func (p *Plugin) Requires() []string { return nil }

func (p *Plugin) ConfigKey() string { return "process" }

func (p *Plugin) Toggleable() bool { return true }

func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	var guard *datadir.Guard
	if c, ok := ctx.Configs["store"].(store.Config); ok {
		guard = datadir.NewGuard(c.DBPath)
	} else {
		guard = datadir.NewGuard("")
	}
	rcfg := RegistryConfig{}
	if c, ok := ctx.Configs["process"].(RegistryConfig); ok {
		rcfg = c
	}
	p.provider = NewShellProvider(guard, NewRegistry(rcfg))
	// the shell.* verb family keeps its model-facing names
	// (the prompt, the approval gate and every transcript address them): the
	// module-namespace claim would refuse `shell.run` from a capsule named `terminal`, so
	// the names ride the built-in VOCABULARY rail exactly like the store's sessions.* and
	// the tools capsule's files.grep/glob (RegisterBuiltinTool).
	for name, t := range p.provider.Tools() {
		if err := api.RegisterBuiltinTool(name, t); err != nil {
			return err
		}
	}

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

type claimReply struct {
	ID      string `json:"id"`
	Existed bool   `json:"claimed"` // false = freshly spawned
}

// handleClaim rides ONE lock for the reattach scan, the spawn and the claim mark
// (Store.Claim); the stream's subscribe below clears the mark - the claim/attach gap
// cannot hand the shell to a second pane.
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

// resolveSpawnDir is the spawn workspace law, the
// Files tab's resolution chain wearing the terminal's face:
//  1. the focused session's pinned workspace (the store row), live:
//     every claim re-reads it, so a re-pin lands on the next spawn;
//  2. else the configured default workspace (the hot knob);
//  3. else "" - Store.Claim's fallback, the process cwd.
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

func (p *Plugin) handleList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": p.store.List()})
}

func (p *Plugin) handleStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s := p.store.Get(id)
	if s == nil {
		httpError(w, fmt.Errorf("no such terminal"), http.StatusNotFound)
		return
	}
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
			// the SSE comment ping keeps middleboxes from dropping an idle stream.
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, ok := <-sub.ch:
			if !ok {
				// the session was removed under us (kill, prune, cap law) - the close is the signal
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

// handleKill answers 204 either way: an unknown id is not an error.
func (p *Plugin) handleKill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	gone := p.store.Drop(id)
	w.WriteHeader(http.StatusNoContent)
	_ = gone
}

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

type wireFrame struct {
	T string `json:"t"`
	D string `json:"d,omitempty"`
	C *int   `json:"c,omitempty"`
}

// writeSSE writes one data frame. The newline risk is zero: shell bytes never ride the
// wire raw - the frames are JSON, the payload is base64.
func writeSSE(w io.Writer, frame string) {
	fmt.Fprintf(w, "data: %s\n\n", frame)
}

func outputFrame(d []byte) string {
	return frameJSON(wireFrame{T: "o", D: base64.StdEncoding.EncodeToString(d)})
}

func frameJSON(f wireFrame) string {
	b, err := json.Marshal(f)
	if err != nil {
		return frameJSON(wireFrame{T: "m"})
	}
	return string(b)
}
