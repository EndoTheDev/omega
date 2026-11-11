package api

// Server base for the channel's HTTP surface (restructure part 2,
// card t_905fa502): the AGENT's own endpoint set lives here - chat
// streaming, sessions, transcript/variants/branch, files, workspaces,
// canvas, commands, notifications, verdicts, agents, settings, models,
// the tool inventory and the module-route seam machinery. The serving
// shell (listener, host guard, SPA fallback, composition surfaces)
// lives in the rump package; this type is embedded there and only
// shell state stays behind. Import law: this package may import core,
// agent and llm - never harness.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/llm"
)

// Server exposes the agent over HTTP with SSE streaming.
type Server struct {
	agent *agent.Agent
	tools map[string]core.Tool
	// runs is a per-PROCESS hub, not per-bind (t_c6f13a82): every
	// multi-bind mirror shares it (HTTPChannel.Start swaps it in), or a
	// run started through the action pipe on one bind is invisible to
	// the transcript/events reads on the others - exactly the hub
	// invisibility journey 30 caught. NewServer wires a private hub so
	// directly constructed Servers (tests, embedding) stay self-contained.
	runs *RunHub // detached /chat runs by session id (reattach surface)
	// sockets is the push channel's fan-out (card t_7624bab1): ONE hub,
	// every typed topic - chat deltas, chip tails and fills, canvas
	// deltas, live-run truth, health. Per-server like the run hub's
	// private default, so a directly constructed Server (tests,
	// embedding) stays self-contained; the shell attaches connections.
	// eph is the no-persist (ephemeral) session registry: `eph-` ids
	// live here, never in the store - invisible to every store-derived
	// surface, gone at restart (card t_d96a5cd5).
	eph      ephRegistry
	store    agent.StoreProvider
	sockets  *SocketHub
	settings core.Settings
	// modules is the optional module-toggle seam (v0.12 E2): the
	// /modules endpoints serve registry introspection + live toggles
	// through it; a nil value answers 501.
	modules core.ModuleToggles
	// registries is the optional registry-browser toggle seam (v0.14.1):
	// the /disabled endpoints serve and write the config.yaml
	// `disabled:` fold's skills/tools/agents namespaces through it; a
	// nil value answers 501.
	registries core.RegistryToggles
	// seams is the optional-module surface of the serving routes
	// (v0.12 E2): read per request behind a lock, rewritten by
	// RewireSeams after every live module toggle. A mount-time pointer
	// copy would keep a disabled module's routes alive - the board is
	// optional, but its death is not optional once the UI can toggle it.
	seams seamSet
	// canvas is the runtime behind the canvas transport (arc 3b):
	// panel snapshot source and the one action pipe (server_canvas.go).
	// Nil means nothing mounted - /ui/canvas serves [] and /ui/actions
	// answers the unknown-module 404.
	canvas *agent.Canvas
	// chat is the conversation's canvas capsule (chat-last 1/3,
	// chat_panel.go): the 'chat' panel's snapshot source, the run-loop
	// push hooks and the action-pipe chat verbs. Default-wired on the
	// server; the plugin's Mount-instanced capsule replaces it at
	// channel start (the registered one and the served one meet there).
	chat *ChatCapsule
	// runTimeout caps each detached /chat run's wall-clock time. The
	// LLM call carries the HTTP timeout, but a hung tool call would
	// otherwise run forever and hold the session's run slot until
	// restart. Default set here; the mount overrides it from config.
	runTimeout time.Duration
	// agentRuns is the host's per-agent run factory (card t_a7fab745):
	// a session owned by an agent home runs on THAT home's agent. Nil =
	// the daemon's agent serves every session (tests, embedding).
	agentRuns AgentRunFactory

	// The yield harness (async agent loop phase 1, t_f3f953d0): the
	// wake hub - the per-home FIFO of settled yield results and the
	// runaway guard - plus the pending-calls table the loop dispatches
	// against. bootCtx is the base context wake launches detach from
	// (they carry no request).
	wakes       *wakeHub
	bootCtx     context.Context
	bootStop    context.CancelFunc
	yieldLedger agent.YieldLedger
	// wakeMu guards wakeLaunches: the per-home count of wake runs this
	// server's run slot admitted. It is a monotone RECORD of an event
	// that happened, not a sampled run state - a wake run's live slot
	// lasts a couple of milliseconds (the boot recovery run measures
	// 2-4ms under load), shorter than any test's poll interval, so
	// s.runs.active sampled from outside decides nothing about whether
	// a launch happened. Tests read this through wakeLaunchCount
	// (card t_32c3fce2: the boot-recovery wait's 30s deadline, not the
	// launch, was deciding the verdict).
	wakeMu       sync.Mutex
	wakeLaunches map[string]int
	// ephLedger is the `eph-` lane of the pending-calls table (card
	// t_61d3def9, session-type parity): the in-memory backing beside
	// the ephRegistry, wired into yieldLedger through
	// sessionTypedLedger when the store carries the durable surface.
	// RESTART EATS ITS PENDING RESULTS - the eph mode's own contract
	// (the session dies with the process; a pending result cannot
	// outlive its home), documented at sessionTypedLedger. It doubles
	// as the whole table's floor when no store carries the surface -
	// then every session is memory-backed, the documented store-less
	// degradation.
	ephLedger *agent.MemYieldLedger

	// titleModel is the title_model knob: session-title calls route
	// through this model when set, instead of the call's pinned
	// provider (the main model). Empty = no override.
	titleModel string

	// defaultWorkspace is the server.default_workspace knob (card
	// t_ccf74d88): the TOOLS' fallback working directory. A session
	// without a pinned workspace roots its tool runs here instead of
	// the process working directory - a defined default, not an
	// implicit cwd. Empty = unset, the cwd fallback stands.
	defaultWorkspace string

	// wsMu guards defaultWorkspace: the settings-pane hot apply writes
	// it while a detached run's prepareRun may be reading it.
	wsMu sync.Mutex

	ctxMu     sync.Mutex
	ctxWindow map[string]int // model name -> context window (probed once)

	// mux is the shared route table: registerRoutes mounts the base
	// endpoint set here; the shell registers its composition routes on
	// the same mux and wraps the listener in the host guard.
	mux *http.ServeMux
	// serve is the serving handler Handler() returns: the module
	// literal rescue wrapping mux (serveModuleRescue). Built once in
	// NewServer; registration on mux keeps working - the rescue reads
	// the live seamSet, never a stale route table.
	serve http.Handler
}

// seamSet is the optional-module surface behind the Server's routes
// (v0.12 E2). One write side - RewireSeams, called at channel mount and
// by the host after each live module toggle - and the per-request read
// accessors the handlers use. The lock matters because the toggle path
// rewrites every field while any of these handlers may be mid-request.
type seamSet struct {
	mu sync.RWMutex
	// modules is the module-rest mux rebuilt from ctx.ModuleRoutes at
	// every rewire (boot, mount, live toggle): the routes a mounted
	// module registered, nothing else. A nil mux sends every request
	// to the SPA fallback - no module routes, no module paths.
	modules *http.ServeMux
	// routeRoots are the first path segments module routes own, the
	// 404 lane: a URL rooted at a module route that matches no
	// registered pattern answers 404, not the SPA shell. Empty when
	// no routes: history-mode routes like /tasks-off-webui stay SPA.
	routeRoots map[string]bool
	// moduleLiterals is the exact-match lane of the module REST set
	// (the same ctx.ModuleRoutes at every rewire): "METHOD /path" and
	// "ANY /path" keys holding capsule handlers for literal patterns
	// (no wildcards). It exists because the base mux's fixed wildcard
	// patterns out-specify a capsule literal living behind the "/"
	// catch-all - without this exact-match lane, GET /sessions/search
	// died into the base /sessions/{id} detail handler (proven live
	// 2026-11-05: the 50-sessions-reload nightly red). ServeMux has no
	// precedence hook across muxes, so the rescue layer reads this.
	moduleLiterals map[string]http.HandlerFunc
	logger         agent.LoggerProvider
	// ctx is the live Context (part 3, the seam constitution): the
	// capsule-facing routes resolve their services per request from
	// the universal registry - a live toggle swaps the pointer under
	// their feet exactly the way the old pinned fields followed the
	// rewire. Nil-safe: a directly constructed Server (tests) with no
	// Context rewired answers the nil-seam degradation (501 / empty).
	ctx *core.Context
	// commands + commandHandler are the extension command registry
	// (ctx.Commands, ctx.CommandHandler) - the palette's dispatch
	// surface reads them per request (card t_07aed3f9).
	commands       []core.ExtensionCommand
	commandHandler func(ctx context.Context, name, args string) (core.CommandResult, error)
}

// LiveServices returns the live universal seam registry (part 3): the
// shell's capsule-facing inspector routes resolve by key. Nil-safe.
func (s *Server) LiveServices() *core.Services {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	if s.seams.ctx == nil {
		return nil
	}
	return s.seams.ctx.Services()
}

// Consumers' local faces (part 3): every capsule-facing route
// type-asserts the resolved service to what it calls - no shared
// named interface crosses the seam. (The finished-things and
// question-card faces rode their capsules out with the shrink wave 3 -
// card t_b75df168; the routes resolve their own handles there.)

// skillFaces is the skill inventory face the content/atmention
// consumers hold (std types only; the capsule owns its type).
type skillFaces interface {
	SkillsEntries() (index map[string]string, contents map[string]string, err error)
	SkillRead(name string) (description, content string, err error)
}

// contextResolver is the workspace prompt-context face the
// session-send path holds: resolve AGENTS.md context behind the
// mounted trust capsule.
type contextResolver interface {
	ResolveContext(cwd string, approve, noApprove, interactive bool) string
}

// skills resolves the skill inventory service.
func (s *Server) skills() skillFaces {
	svc, _ := s.LiveServices().Resolve("skills").(skillFaces)
	return svc
}

// trustResolver resolves the trust service for workspace sessions.
func (s *Server) trustResolver() contextResolver {
	svc, _ := s.LiveServices().Resolve("trust").(contextResolver)
	return svc
}

func (s *Server) Logger() agent.LoggerProvider {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	return s.seams.logger
}

// rewireSeams re-reads the module seams from the live Context: the
// route surface above, the canvas panel registry, and the agent's tool
// providers. The channel calls it at mount (initial wiring, the same
// fields the old boot path set) and the host calls it through
// core.LiveSeams after every successful toggle - so a disabled module's
// tools, panels, and routes die without a restart and a re-enabled
// module's surface returns (E2 journal: the toggle machinery changed
// the Context; this re-reads it).
// RewireSeams re-threads the module seam from the live Context. The
// module mux itself is the shell's vocabulary (moduleRouter builds it
// from ctx.ModuleRoutes in the rump), so the split package hands it
// in: same single-R/write seamSet law, same field set.
func (s *Server) RewireSeams(ctx *core.Context, modmux *http.ServeMux, roots map[string]bool) error {
	s.seams.mu.Lock()
	s.seams.modules = modmux
	s.seams.routeRoots = roots
	s.seams.moduleLiterals = moduleLiterals(ctx.ModuleRoutes)
	if lg := ctx.Services().Resolve("logger"); lg != nil {
		logger, ok := lg.(agent.LoggerProvider)
		if !ok {
			panic(fmt.Sprintf("seam %q: offered service (%T) does not satisfy the %q need (wrong shape)", "logger", lg, "logger"))
		}
		s.seams.logger = logger
	} else {
		s.seams.logger = nil
	}
	s.seams.ctx = ctx
	s.seams.commands = append([]core.ExtensionCommand(nil), ctx.Commands...)
	s.seams.commandHandler = ctx.CommandHandler
	s.seams.mu.Unlock()
	if s.agent != nil {
		s.agent.RefreshToolProviders(ctx)
		s.agent.RefreshDisabledTools(ctx)
		// The smart-approval seam rides the same Context-stays-
		// source-of-truth law (card t_c2b52cb5): a live toggle that
		// mounts or unmounts the gate re-arms or disarms the loop.
		s.agent.RefreshApprovals(ctx)
		// The extension inventory follows the same Context-stays-
		// source-of-truth law: the toggle journal keeps ctx.Infos
		// exact, so /extensions lists what is actually mounted.
		s.agent.RefreshExtensionInfos(ctx)
	}
	if s.canvas != nil {
		return s.canvas.Resync(ctx)
	}
	return nil
}

// NewServer creates the base server. Same signature the channel shell
// has always passed: agent, tool registry, the optional store, settings
// / module / registry seams. The base builds its own mux and registers
// the base endpoint set on it (Mux/Handler); the shell (rump) adds its
// composition routes to the same mux and wraps the listener in the host
// guard - one mux, one guard law over fixed routes AND module capsule
// routes alike.
func NewServer(a *agent.Agent, tools map[string]core.Tool, store agent.StoreProvider, settings core.Settings, modules core.ModuleToggles, registries core.RegistryToggles) *Server {
	if tools == nil {
		tools = map[string]core.Tool{}
	}
	s := &Server{agent: a, tools: tools, store: store, settings: settings, modules: modules, registries: registries, ctxWindow: map[string]int{}, runTimeout: 30 * time.Minute, runs: &RunHub{}, sockets: NewSocketHub(), mux: http.NewServeMux()}
	// Chat panel capsule (chat-last 1/3): default-wired so a directly
	// constructed Server (tests, embedding) has the push hooks and the
	// panel even without the plugin's channel-start handoff.
	s.chat = NewChatCapsule()
	s.chat.srv.Store(s)
	// The yield harness (async agent loop phase 1, t_f3f953d0): the
	// pending-calls table is session-typed (card t_61d3def9) - the
	// store's surface for durable sessions when it carries it, the
	// in-memory eph lane for `eph-` homes always; the wake injects
	// through the same home input path a user send takes. A nil agent
	// (some tests construct a bare Server) skips the harness
	// entirely.
	s.bootCtx, s.bootStop = context.WithCancel(context.Background())
	s.wakes = newWakeHub()
	// Session-typed pending-calls table (card t_61d3def9): the `eph-`
	// lane lives in memory beside the ephRegistry - an ephemeral
	// session's rows are process-local (restart eats its pending
	// results: the mode's own contract, see sessionTypedLedger), and
	// nothing it owns may land in the store. Durable sessions keep
	// the store's table when the store carries the surface; a store
	// without it keeps the one in-memory floor for every session.
	s.ephLedger = agent.NewMemYieldLedger()
	if yl, ok := store.(agent.YieldLedger); ok {
		s.yieldLedger = sessionTypedLedger{durable: yl, ephemeral: s.ephLedger}
	} else {
		s.yieldLedger = s.ephLedger
	}
	if a != nil {
		a.SetYieldLedger(s.yieldLedger)
		a.SetYieldWake(s.wakeHome)
		a.SetYieldTimeout(s.runTimeout)
	}
	s.registerRoutes(s.mux)
	s.serve = http.HandlerFunc(s.serveModuleRescue)
	if a != nil {
		// Boot recovery (ruling 5): every entry the durable table
		// carried across a restart injects its synthetic timeout
		// result - the home wakes, the agent sees the interrupted
		// result and may retry.
		s.recoverPendingCalls()
	}
	return s
}

// Mux exposes the shared route table so the shell can register its
// composition routes onto the same mux.
func (s *Server) Mux() *http.ServeMux { return s.mux }

// Handler returns the HTTP handler, for tests and embedding. The
// module-literal rescue outer layer wraps the bare mux (the host guard
// is the shell's vocabulary - the rump wraps Handler() with the same
// guard the shell's own NewServer always did).
func (s *Server) Handler() http.Handler { return s.serve }

// serveModuleRescue is the outer serving layer: a capsule's literal
// route exact-matches here BEFORE the base mux dispatches, because the
// base mux has no view of module patterns and its own fixed wildcards
// (/sessions/{id}) out-specify a module literal behind the "/"
// catch-all in every dispatch. Wildcard module patterns are unaffected
// (no base pattern shares their shape beyond the collision family) and
// the module-rooted 404 lane in handleModuleOrSPA is untouched: the
// rescue only answers an exact registered literal, everything else
// rides the mux as always.
func (s *Server) serveModuleRescue(w http.ResponseWriter, r *http.Request) {
	if h, ok := s.moduleLiteralLookup(r.Method, r.URL.Path); ok {
		h(w, r)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// moduleLiteralLookup exact-matches the live module route set under one
// read lock: "METHOD /path" first (a method-scoped registration), then
// "ANY /path" (a methodless registration answers every method).
func (s *Server) moduleLiteralLookup(method, path string) (http.HandlerFunc, bool) {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	if h, ok := s.seams.moduleLiterals[method+" "+path]; ok {
		return h, true
	}
	h, ok := s.seams.moduleLiterals["ANY "+path]
	return h, ok
}

// moduleLiterals builds the exact-match lane from the live module
// routes: literal patterns only (a wildcard pattern rides the muxes).
// Method-scoped patterns key "METHOD /path"; methodless ones key
// "ANY /path". Same-path re-registration lands the same handler the
// capsules registered; a nil map is a legal empty lane.
func moduleLiterals(routes []core.ModuleRoute) map[string]http.HandlerFunc {
	out := map[string]http.HandlerFunc{}
	for _, rt := range routes {
		if strings.Contains(rt.Pattern, "{") {
			continue
		}
		p := strings.TrimSpace(rt.Pattern)
		if p == "" {
			continue
		}
		if i := strings.IndexByte(p, ' '); i > 0 {
			out[p[:i]+" "+p[i+1:]] = rt.Handler
			continue
		}
		out["ANY "+p] = rt.Handler
	}
	return out
}

// registerRoutes mounts the base endpoint set onto the shared mux. The
// patterns are exactly the ones the pre-split NewServer registered for
// these handlers; the shell's own routes (health, themes, module
// toggle, disabled fold, inspector reads, SPA catch-all) stay in the
// rump's NewServer against the same mux.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/models", s.handleModels)
	mux.HandleFunc("/chat", s.handleChat)
	mux.HandleFunc("/attach-image", s.handleAttachImage)
	// The non-image sibling of the image attach lane (card t_1ffd07de):
	// the composer's strip uploads any other file type here; the bytes
	// land in the session's upload dir and the message carries a typed
	// reference (server_attachments.go).
	mux.HandleFunc("/attach-file", s.handleAttachFile)
	// card t_08d822bb: (the raw-bytes image route was the file explorer's
	// - it rode /files/* and moved with the file capsule, the shell-ring
	// shrink wave 1. The workspaces routes also rode out with wave 3.
	// The shell-ring shrink wave 2 (ADR Workshop/omega-shell-ring-shrink.md,
	// the three-layer sessions law): the session MANAGEMENT surface rides
	// the store capsule - GET /sessions (list), /sessions/search,
	// /sessions/{id}/branch, /sessions/{id}/export and GET /insights are
	// capsule routes now (harness/store/routes.go); the /agents roster
	// reads ride the continuous capsule (harness/continuous/agentsrest.go).
	// The base keeps the chat floor: POST /sessions (session id
	// creation), /sessions/{id} (detail/context/delete - pattern-atomic,
	// the chat page calls its PATCH and ephemeral DELETE live), and the
	// live-run reads (events/stop/transcript/variants) + verdicts.
	mux.HandleFunc("POST /sessions", s.handleSessionCreate)
	mux.HandleFunc("/sessions/{id}/events", s.handleSessionEvents)
	mux.HandleFunc("/sessions/{id}/stop", s.handleSessionStop)
	mux.HandleFunc("/sessions/{id}/transcript", s.handleSessionTranscript)
	mux.HandleFunc("/sessions/{id}/variants", s.handleSessionVariants)
	mux.HandleFunc("/sessions/{id}", s.handleSessionByID)
	// The workspace PICKER + @ intent walk rode the files capsule out
	// with wave 3 (card t_b75df168): a filesystem listing face is the
	// files domain's work - /workspaces/browse and /workspaces/files
	// are module routes (harness/files/workspaces.go) now; the
	// session-born PIN (the row writes below) stays base, it is the
	// session row's own truth.
	mux.HandleFunc("/tools", s.handleTools)
	mux.HandleFunc("/commands", s.handleCommands)
	mux.HandleFunc("/commands/dispatch", s.handleCommandDispatch)
	mux.HandleFunc("/settings", s.handleSettings)
	// GET /insights rode the store capsule out with wave 2 (a view over
	// store data disclosed in the review comment).
	// The /notifications finished-things surface (card t_5d9032d3: the
	// list/read + the toast SSE, /notifications/events) rode the
	// notifications capsule out with wave 3 (card t_b75df168,
	// harness/notifications/routes.go) - the bell reads the capsule it
	// belongs to, the 501 degradation retired with the 404 module lane.
	// The skills content-view route (card t_36ceb99b) rode the skills
	// capsule out in the same sweep (harness/skills/routes.go).
	// The /verdicts answer surface rode the verdict capsule out with
	// wave 3 (card t_b75df168, harness/verdict/routes.go): the bell's
	// pending listing + the answering POST are module routes now - the
	// card renders live from the tool SSE frames it always has.
	// The /agents roster surface rode the continuous capsule out with
	// wave 2 (harness/continuous/agentsrest.go): every handler in the
	// old server_agents.go served the roster tab - the chat page never
	// fetches agent profiles (verified: the only /agents consumer is
	// AgentsPanel.vue), so the whole file moved.
	// card t_e143f41f (arc 3b): the canvas transport. Handlers live in
	// server_canvas.go.
	mux.HandleFunc("/ui/canvas", s.handleCanvas)
	mux.HandleFunc("/ui/actions", s.handleUIActions)
}

// Active reports whether the session has a live run right now - the
// RunHub read the serving channel offers under the "chat-runs"
// capsule-service key (the store capsule resolves it per request
// through its OWN consumer-owned ChatRunsReader face in
// harness/store - no shared type; the seam constitution, Endo
// ruling 2026-11-04). The run hub itself stays the base set's (the
// shell-ring shrink wave 2 disclosure).
func (s *Server) Active(id string) bool { return s.runs.active(id) }

// --- carve seams: the shell reaches the base state it must wire,
// through these exported methods. Every one mirrors what the
// single-package original did with a direct field write.

// Agent exposes the agent handle (the shell's extension-inventory and
// disabled-tools surfaces ride it).
func (s *Server) Agent() *agent.Agent { return s.agent }

// Store exposes the store seam (the shell's /health database fact pings
// it through the optional agent.StorePinger upgrade). Nil when no store
// module is mounted - the health body then carries no database fact.
func (s *Server) Store() agent.StoreProvider { return s.store }

// ModuleToggles exposes the module-toggle seam (GET/PUT /modules).
func (s *Server) ModuleToggles() core.ModuleToggles { return s.modules }

// Registries exposes the registry-toggle seam (GET/PUT /disabled/...).
func (s *Server) Registries() core.RegistryToggles { return s.registries }

// ModuleMux returns the capsule mux and its route roots read under one
// lock - the same single read the pre-split handleModuleOrSPA did.
func (s *Server) ModuleMux() (*http.ServeMux, map[string]bool) {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	return s.seams.modules, s.seams.routeRoots
}

// Canvas exposes the canvas runtime (the boot-wiring test reads it).
func (s *Server) Canvas() *agent.Canvas { return s.canvas }

// ActiveRun reports the run hub's active state for one session id.
func (s *Server) ActiveRun(id string) bool { return s.runs.active(id) }

// BindInspectSeams installs the optional provider surfaces the shell's
// inspector reads serve (cron jobs, skills, logs). The logger lands in
// the same seamSet RewireSeams rewrites; the shell's tests use it where
// the pre-split suite set the fields directly. The capsule services
// (cron, skills) ride the universal registry instead - the caller
// rewires the Context (RewireSeams) and the routes resolve by key.
func (s *Server) BindInspectSeams(logger agent.LoggerProvider) {
	s.seams.mu.Lock()
	if logger != nil {
		s.seams.logger = logger
	}
	s.seams.mu.Unlock()
}

// BindRunHub swaps the per-process run registry in (HTTPChannel.wire).
func (s *Server) BindRunHub(h *RunHub) { s.runs = h }

// Sockets exposes the push channel's hub: the serving shell attaches
// websocket connections here and pushes the health heartbeat (the
// transport lives in the shell, the fan-out lives with the loop).
func (s *Server) Sockets() *SocketHub { return s.sockets }

// BindChatCapsule binds the plugin's registered chat capsule.
func (s *Server) BindChatCapsule(c *ChatCapsule) { s.chat = c }

// BindCanvas binds the canvas runtime built at channel start.
func (s *Server) BindCanvas(c *agent.Canvas) { s.canvas = c }

// RegisterChatPanel appends the chat capsule to the context's panel
// providers (the Start-time registration, chat-last 2b ruling).
func (s *Server) RegisterChatPanel(ctx *core.Context) { s.chat.registerAt(ctx) }

// SetChannelKnobs applies the channel config knobs (run wall-clock cap,
// title model) that the wire overrides after construction.
func (s *Server) SetChannelKnobs(runTimeout time.Duration, titleModel string) {
	s.runTimeout = runTimeout
	s.titleModel = titleModel
}

// SetDefaultWorkspace applies the server.default_workspace knob (card
// t_ccf74d88): the tools' fallback workdir for sessions without a
// pinned workspace. Called by the wire (boot value) and the settings
// hot apply (a live knob save). An empty value is the intentional
// clear - the cwd fallback law stands. A stat failure refuses the
// knob (card t_9825147c part 3): the previous value survives, so a
// transient stat failure (network drive hiccup, AV lock) during an
// unrelated save never silently re-roots workspace-less sessions'
// tool runs at the process cwd - the refusal is logged with the
// reason and the kept value.
func (s *Server) SetDefaultWorkspace(dir string) {
	if dir != "" {
		info, err := os.Stat(dir)
		switch {
		case err != nil:
			s.refuseWorkspace(dir, "stat failed: "+err.Error())
			return
		case !info.IsDir():
			s.refuseWorkspace(dir, "not a directory")
			return
		}
	}
	s.wsMu.Lock()
	s.defaultWorkspace = dir
	s.wsMu.Unlock()
}

// refuseWorkspace keeps the previous default workspace and logs why
// the submitted one was refused (card t_9825147c part 3). The logger
// seam may be nil (no logging capsule) - the value is intact either
// way, the log line is the audit trail.
func (s *Server) refuseWorkspace(dir, why string) {
	s.wsMu.Lock()
	prev := s.defaultWorkspace
	s.wsMu.Unlock()
	if l := s.Logger(); l != nil {
		l.Errorf("api: server.default_workspace %q refused (%s) - keeping %q", dir, why, prev)
	}
}

// SetAgentPrompt is the settings hot-apply hook for the live runtime
// knobs (card t_f78acfd6): prompt.Custom (the settings system_prompt
// knob) and the per-run turn cap (max_turns) land on THIS bind's
// serving agent. The agent re-reads both per run (the probe verdict:
// the loop rebuilds prompt + cap per Run from its fields), so a save
// answers with the new values at the next run - no restart, no rewire.
// The shell api.Server walks its mounted binds to call this; a nil
// agent is a no-op.
func (s *Server) SetAgentPrompt(prompt string, maxTurns int) {
	if s.agent == nil {
		return
	}
	s.agent.SetPromptCustom(prompt)
	s.agent.SetMaxTurns(maxTurns)
}

// WriteJSON is the exported seam for the shell's JSON responses.
func WriteJSON(w http.ResponseWriter, status int, v any) { writeJSON(w, status, v) }

// getOnly is the GET-only guard shared by the base handlers (the
// inspector family in the shell reuses it through GetOnly).
func getOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// GetOnly is the exported seam for the shell's GET-only guard.
func GetOnly(w http.ResponseWriter, r *http.Request) bool { return getOnly(w, r) }

// WriteSettingsJSON is the exported seam for the no-store settings payloads.
func WriteSettingsJSON(w http.ResponseWriter, payload any) { writeSettingsJSON(w, payload) }

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries, err := s.agent.ListModelEntries()
	if err != nil {
		http.Error(w, "list models: "+err.Error(), http.StatusInternalServerError)
		return
	}
	models := make([]string, 0, len(entries))
	for _, e := range entries {
		models = append(models, e.Name)
	}
	caps := map[string][]string{}
	for _, e := range entries {
		if len(e.Capabilities) > 0 {
			caps[e.Name] = e.Capabilities
		}
	}
	name := s.agent.ModelName()
	s.ctxMu.Lock()
	window, cached := s.ctxWindow[name]
	s.ctxMu.Unlock()
	if !cached {
		// one provider probe per model per process - the webui calls /models
		// on every page load and the probe is a ~200ms network round trip.
		// The probe runs on a copy so the base provider is never mutated.
		info, _ := s.agent.Provider().WithModel(name).ModelInfo()
		window = info.ContextWindow
		s.ctxMu.Lock()
		s.ctxWindow[name] = window
		s.ctxMu.Unlock()
	}
	// ponytail: keypool/ollama-cloud providers cannot report a context window,
	// so fall back to the config default the agent itself budgets against
	// (cmd/omega config default 32768). Upgrade path: thread the resolved
	// CompactionConfig into the channel and report that instead.
	if window <= 0 {
		window = 32768
	}
	w.Header().Set("Content-Type", "application/json")
	out := map[string]any{
		"current":        name,
		"models":         models,
		"context_window": window,
	}
	// card t_70c28128: the typed model pickers filter on capability -
	// the flags ride the payload when the provider carries them
	// (ollama /api/tags); name-only providers omit the map and the
	// pickers fall back to their curated sets.
	if len(caps) > 0 {
		out["capabilities"] = caps
	}
	// card t_1914a8c9: the GROUPED catalog - the chat picker renders
	// one collapsible section per provider. The default instance
	// first, then every named instance (sorted, the config map is
	// unordered). Each section lists the endpoint's own /models
	// answer; an instance that fails to answer still carries the
	// section with the error named - the picker shows the failure
	// inline, never a silent drop. Sections ride ALONGSIDE the flat
	// fields above (the settings pickers read those).
	if sections := s.modelSections(); len(sections) > 0 {
		out["providers"] = sections
	}
	json.NewEncoder(w).Encode(out)
}

// ModelSection is one picker section (card t_1914a8c9): the named
// endpoint and the models its own catalog answers, capability flags
// included when the wire carries them. A listing failure keeps the
// section alive with the error named - the picker's Refresh action
// re-fetches, never a silent drop of the whole provider.
type ModelSection struct {
	Name         string              `json:"name"`
	Models       []string            `json:"models"`
	Capabilities map[string][]string `json:"capabilities,omitempty"`
	Error        string              `json:"error,omitempty"`
}

// modelSectionsTimeout caps one endpoint's listing inside the grouped
// catalog (card t_9825147c part 1): the listings ride no context, so
// the catalog collects with a deadline instead of wg.Wait - one
// unreachable instance (VPN down, provider outage, typo'd host)
// answers as its section's Error field in seconds while the live
// sections list their models, instead of stalling every GET /models
// behind it for up to the http_timeout default (300s). A var: tests
// shorten it.
var modelSectionsTimeout = 5 * time.Second

// listOneSection fetches one endpoint's catalog into its section - it
// never touches shared state, so a section fetched late or never
// (missed deadline) is safe to serve without it.
func listOneSection(name string, inst agent.Provider) ModelSection {
	sec := ModelSection{Name: name, Models: []string{}}
	// the same degradation agent.ListModelEntries applies: the
	// capability upgrade answers entries when mounted, a name-only
	// provider's ListModels is the floor
	var entries []core.ModelEntry
	var err error
	if lp, ok := inst.(agent.ModelCapabilitiesProvider); ok {
		entries, err = lp.ListModelEntries()
	} else {
		var names []string
		names, err = inst.ListModels()
		if err == nil {
			entries = make([]core.ModelEntry, len(names))
			for k, m := range names {
				entries[k] = core.ModelEntry{Name: m}
			}
		}
	}
	if err != nil {
		sec.Error = err.Error()
		return sec
	}
	for _, e := range entries {
		sec.Models = append(sec.Models, e.Name)
		if len(e.Capabilities) > 0 {
			if sec.Capabilities == nil {
				sec.Capabilities = map[string][]string{}
			}
			sec.Capabilities[e.Name] = e.Capabilities
		}
	}
	return sec
}

// modelSections builds the grouped catalog: the default instance,
// then every named instance (sorted - a config map iterates
// unordered). Each listing rides its own provider copy, so no shared
// state mutates; a listing error lands IN the section (the picker
// shows it), never a 500 of the whole payload. Sections collect under
// a deadline (modelSectionsTimeout): a section whose endpoint misses
// it answers as an Error section, and the picker's Refresh re-runs
// the whole GET as a fresh per-provider fetch.
func (s *Server) modelSections() []ModelSection {
	prov := s.agent.Provider()
	if prov == nil {
		return nil
	}
	type job struct {
		name string
		inst agent.Provider
	}
	jobs := []job{{name: "default", inst: prov}}
	if ir, ok := prov.(agent.ProviderInstances); ok {
		// the named set rides the listed names (an optional upgrade
		// like the instances one); each name resolves through the
		// same resolver a run's pin uses
		if listed, ok := prov.(agent.ProviderInstanceNames); ok {
			names := listed.InstanceNames()
			sort.Strings(names)
			for _, n := range names {
				if inst, ok2 := ir.Instance(n); ok2 {
					jobs = append(jobs, job{name: n, inst: inst})
				}
			}
		}
	}
	// every fetch goroutine sends exactly once on a buffered channel,
	// so a section that reaches the deadline never blocks the late
	// sender - the goroutine is simply left behind it.
	type listed struct {
		idx int
		sec ModelSection
	}
	results := make(chan listed, len(jobs))
	for i, j := range jobs {
		go func(idx int, j job) {
			results <- listed{idx: idx, sec: listOneSection(j.name, j.inst)}
		}(i, j)
	}
	out := make([]ModelSection, len(jobs))
	received := make([]bool, len(jobs))
	timer := time.NewTimer(modelSectionsTimeout)
	defer timer.Stop()
	for done := 0; done < len(jobs); done++ {
		select {
		case r := <-results:
			out[r.idx] = r.sec
			received[r.idx] = true
		case <-timer.C:
			timeout := modelSectionsTimeout
			for i := range jobs {
				if !received[i] {
					out[i] = ModelSection{Name: jobs[i].name, Models: []string{},
						Error: fmt.Sprintf("listing did not answer within %s", timeout)}
				}
			}
			return out
		}
	}
	return out
}

// chatRequest is the /chat body. tools is an optional list of tool names
// to enable for this run; empty enables the full server registry.
// session_id, when set, persists the conversation to that session; when
// empty on a server WITH a store, the request auto-creates a session and
// persists normally (Endo 2026-10-18: /chat is always durable - the new
// id rides the first `session` SSE frame). Without a store the run stays
// ephemeral. model, when set, serves this run on that model - the
// default stays untouched, so concurrent chats keep their own model.
// thinking_level indexes the shared llm.ThinkingLevels ladder (0 = none
// = no thinking parameter sent = provider default); unset means
// provider default.
type chatRequest struct {
	Messages  []json.RawMessage `json:"messages"`
	SessionID string            `json:"session_id,omitempty"`
	Model     string            `json:"model,omitempty"`
	// Provider selects the provider INSTANCE the run serves through
	// (config.yaml providers:, card t_7dcd4280). Empty = the default
	// provider: section. A name the config no longer carries (the
	// instance was removed) fails the run loudly (400, "not
	// configured") - never a silent fallback. A successful send pins
	// the name on the session (the wire wins and sticks, the same
	// rule the model pin carries), so the session keeps serving the
	// instance until it is removed - and then fails loudly.
	Provider      string `json:"provider,omitempty"`
	ThinkingLevel int    `json:"thinking_level,omitempty"`
	// Retry re-runs the session's LAST user turn as a SIBLING VARIANT
	// (t_17365247, superseding the append semantics of t_357cbe9c): the
	// fresh response's head parents to the same USER message the prior
	// response answered, so the store stays an append list with no
	// duplicate user bubble while the tree gains a sibling. No messages
	// are accepted on the wire: the persisted history itself is the
	// replayed turn.
	Retry bool `json:"retry,omitempty"`
	// Fork re-plays at an arbitrary branch point (the per-message fork
	// button): the value is a message id. A user-turn target behaves
	// as a retry pinned there (no messages); an assistant/tool target
	// continues FROM it - it takes the fork's user message (whose
	// bubble chains onto the forked message) and runs the turn.
	// Requires a session id. Mutually exclusive with retry.
	Fork string `json:"fork,omitempty"`
	// Workspace roots the session created by a BARE send (card
	// t_ea7e6a58): the webui's draft workspace intent rides the first
	// POST /chat and the auto-created row is born pinned. Honored ONLY
	// on that path - an explicit session_id, a fork/replay turn, or an
	// ephemeral mint plus a workspace is refused loudly (pin at
	// creation or not at all).
	Workspace string `json:"workspace,omitempty"`
	// branchParent pins the run head's parent when the channel computed
	// it (retry/fork-of-user) - empty = derive. Unexported: never part
	// of the request wire.
	branchParent string
	// wake marks a WAKE LAUNCH (async agent loop, agent/api/wakes.go):
	// the detached executor of a yield-class call settled and the
	// settled core.YieldResult re-enters as this request's input. A
	// wake launch takes the same prepareRun pipe but never fires the
	// title kick (it is agent-speech, not a conversation start) and
	// never resets the runaway guard. Unexported: machine-internal
	// only - no /chat wire field reaches it (a forged wake turn is
	// just another run).
	wake bool
	// branchFloor is the session's max store row id observed at run
	// start, set alongside branchParent: while a branch-pinned run
	// streams, the panel renders the active path cut at the branch row
	// and keeps only rows with id > floor (rows the run itself
	// persisted mid-stream) - the old sibling's rows leave the panel
	// so the fresh response streams into their place (t_19c100dd,
	// Endo ruling: retry renders in place, never stacked). Unexported:
	// never part of the request wire.
	branchFloor int64
	// persistInput is the send's incoming messages - the run's turn
	// copy the LOOP persists through its sink before the first turn
	// (fork parents already applied). Unexported: never part of the
	// request wire.
	// workspace is the session's working folder for THIS run (from the
	// session's Workspace column, prepared in prepareRun). Unexported:
	// never part of the request wire.
	workspace string
	// workspacePromptContext is the trust-gated AGENTS.md context of
	// the workspace folder, resolved at prepare time. Unexported:
	// never part of the request wire.
	workspacePromptContext string
	persistInput           []core.Message
	// Ephemeral (card t_d96a5cd5): the run is a NO-PERSIST chat - no
	// store row, nothing on disk; the `eph-` id (carried on the leading
	// `session` frame) lives in the server's in-memory registry until
	// its DELETE (close = gone) or restart. Retry/fork are refused
	// (400): both replay the persisted tree, which ephemeral sessions
	// never have. The memory tool never runs in an ephemeral session.
	Ephemeral bool `json:"ephemeral,omitempty"`
	// runAgent is the agent that serves THIS run when the session
	// belongs to an agent home (card t_a7fab745, prepared in
	// prepareRun): that home's provider, model, system prompt, skills
	// and max_turns instead of the daemon's. Nil = the daemon's agent.
	// Unexported: never part of the request wire.
	runAgent *agent.Agent
}

// assistantRecord accumulates one response segment of an assistant turn
// for session persistence: thinking, tool calls, tool results, and the
// response content. A tool phase splits the turn into segments (frontend
// mirrors this): segment A carries the preamble + tool calls, segment B
// the final answer. records() returns them in order.
type assistantRecord struct {
	thinking  string
	toolCalls []core.ToolCall
	results   []core.ToolResult
	content   string
	// server-side timing so reloaded sessions render real durations
	// (client meters die with the SSE stream). Durations accumulate raw
	// (not per-chunk ms floors): individual sub-millisecond chunk
	// deltas would each floor to 0, but summed they survive. lastXxx
	// zero = span closed; the next chunk opens a fresh one. openAt is
	// the opening boundary of the current span window - the turn start
	// or last tool result. A chunk opening a span bills boundary-to-
	// chunk, so provider latency before the first token (prompt
	// processing, the model's silent build-up) counts as part of the
	// phase it produces: without it, a single-chunk segment reports 0.
	thinkDur, respDur   time.Duration
	lastThink, lastResp time.Time
	openAt              time.Time
	// call dispatch times by tool call id; the matching ToolResult
	// reads and clears its entry when it arrives.
	callStarts map[string]time.Time
}

// streamUsage totals the real provider-reported usage across every
// StreamEnd of one /chat run (a multi-turn tool loop streams once per
// turn; each StreamEnd carries that turn's usage). Zero means the
// provider reported nothing.
type streamUsage struct {
	prompt     int
	completion int
}

// newRunMeter starts the clock for one run's last-run stats: the clock
// opens at meter creation (the send boundary, mirroring the webui's
// live meters) and closes at the last chunk, so prompt-processing
// latency before the first token is part of the run just like it is
// for the client measuring wall time.
func newRunMeter() *runMeter {
	return &runMeter{started: time.Now()}
}

// replayGuards validates the replay-turn request shapes (retry/fork).
// They belong BEFORE auto-create (both re-play an EXISTING session's
// persisted tree - a sessionless replay has nothing to replay, and
// messages alongside a replay-turn are a client bug), and BOTH entry
// paths share them (chat-last 1/3): handleChat and the chat panel's
// retry action through the pipe (chat_panel.go).
func replayGuards(req *chatRequest, hasStore bool) error {
	if req.Retry && (!hasStore || req.SessionID == "") {
		return &runError{http.StatusBadRequest, "retry requires a session"}
	}
	if req.Retry && len(req.Messages) > 0 {
		return &runError{http.StatusBadRequest, "retry takes no messages"}
	}
	if req.Fork != "" && (!hasStore || req.SessionID == "") {
		return &runError{http.StatusBadRequest, "fork requires a session"}
	}
	if req.Fork != "" && req.Retry {
		return &runError{http.StatusBadRequest, "fork and retry are mutually exclusive"}
	}
	// Replay turns need the persisted tree; an ephemeral session never
	// has one. The webui hides both affordances on an ephemeral tab -
	// this is the wire-level backstop.
	if req.Ephemeral && (req.Retry || req.Fork != "") {
		return &runError{http.StatusBadRequest, "retry/fork are not available in ephemeral sessions"}
	}
	return nil
}

// runError is one chat run's start failure with its HTTP status: the
// /chat handler writes it verbatim (status + message), the action pipe
// surfaces only the message on its own error contract.
type runError struct {
	code int
	msg  string
}

func (e *runError) Error() string { return e.msg }

// chatPrepare is one prepared detached run: the acquired slot, the
// pinned provider and tool set, and the resolved context messages.
type chatPrepare struct {
	run      *activeRun
	prov     agent.Provider
	messages []core.Message
	tools    map[string]core.Tool
}

// prepareRun performs the shared pre-launch steps of a session-backed
// chat run (chat-last 1/3): the replay guards, the run-slot
// acquisition, the active-path history load (retry/fork branch
// resolution included), the per-run provider pin and the tool
// selection - everything handleChat did between its decode and its
// subscribe-before-launch, and everything the chat panel's retry
// action runs with no response body. On any failure the run's slot is
// released and the error carries the same status + message the handler
// wrote before the refactor. The provisional title kick stays here
// too: it fires before any streaming for both entry paths.
func (s *Server) prepareRun(ctx context.Context, req *chatRequest, incoming []core.Message) (*chatPrepare, error) {
	// Replay-turn guards: store presence is enforced here too - the
	// handler's early replayGuards ran before auto-create, this one
	// holds the same wall for the action pipe's entry.
	if err := replayGuards(req, s.store != nil); err != nil {
		return nil, err
	}
	// The pipe's entry needs nothing else here - the guards above
	// carried the store wall for both entry paths now.

	// Run-slot acquisition comes BEFORE any persistence: a second /chat
	// for a session with a live run is a 409 that must not leave the
	// incoming user message appended - the next successful send would
	// double it.
	run, live := s.runs.start(req.SessionID)
	if !live {
		return nil, &runError{http.StatusConflict, "session already running"}
	}
	// Live-run truth on the push channel (card t_7624bab1): the slot was
	// just taken - human sends and wake launches (launchWake calls
	// prepareRun too) both land here, so the runs topic's "active" edge
	// has exactly one source. The release edge is launchRun's finish hook.
	s.publishRunState(req.SessionID, true)

	// One session row up front (card t_9de5b009): the model pin, the
	// workspace scope and the title kick all resolve from the same
	// read. Ephemeral ids have no row; an unknown id leaves everything
	// at the legacy defaults. Read BEFORE sessionHistory so @-mention
	// expansion below roots relative paths in the session's workspace.
	var sess core.Session
	sessionKnown := false
	if s.store != nil && req.SessionID != "" && !req.Ephemeral {
		if v, err := s.store.GetSession(ctx, req.SessionID); err == nil {
			sess, sessionKnown = v, true
		}
	}

	// Per-agent run config (card t_a7fab745): a session owned by an agent
	// home runs on THAT home's agent - provider, model, system prompt,
	// skills and max_turns all resolve from its config layer. The host
	// builds (and caches) it; a failure here is a loud 500, never a
	// silent run as the wrong agent.
	if ag, aerr := s.runAgentFor(ctx, req.SessionID); aerr != nil {
		s.runs.finish(run)
		s.publishRunState(req.SessionID, false)
		return nil, &runError{http.StatusInternalServerError, aerr.Error()}
	} else {
		req.runAgent = ag
	}

	// Per-agent run config OWNS provider and model (card t_81109e1c): the
	// home's config layer names both, and every send from the webui
	// carries the composer's model - a chat tab with no pinned model
	// inherits the DAEMON's default, so an agent-owned session would
	// answer on a model its home never configured (or fail on a name its
	// endpoint does not serve). Neither the wire's pick nor one
	// remembered from an earlier send applies here; the picks below are
	// the daemon's picker concerns, and this session is not the daemon's.
	agentOwned := req.runAgent != nil
	if agentOwned {
		req.Model, req.Provider = "", ""
	}

	// Workspace scope: a session rooted in a folder roots THIS run's
	// relative file operations there (the loop and the tools read the
	// scope off ctx) and serves the folder's trust-gated AGENTS.md
	// context instead of the process working directory's. Non-tty
	// resolution skips an untrusted folder's context with a stderr
	// note - the same gate the host applies at boot. A session without
	// a pin falls back to the default_workspace knob (card t_ccf74d88):
	// the tools run against a DEFINED directory, never an implicit cwd.
	s.wsMu.Lock()
	req.workspace = sess.Workspace
	if req.workspace == "" {
		req.workspace = s.defaultWorkspace
	}
	s.wsMu.Unlock()
	if req.workspace != "" {
		if tr := s.trustResolver(); tr != nil {
			req.workspacePromptContext = tr.ResolveContext(req.workspace, false, false, false)
		}
	}

	// @-mention expansion (card t_07aed3f9): the new input is expanded
	// BEFORE history assembly, so the loop's sink persists the expanded
	// prompt - the same truth every submit path holds. The session's
	// workspace roots relative paths.
	incoming = s.expandAtMentions(incoming, req.workspace)
	messages, err := s.sessionHistory(ctx, req, incoming)
	if err != nil {
		s.runs.finish(run)
		s.publishRunState(req.SessionID, false)
		return nil, err
	}

	// Instance pin (card t_7dcd4280): a wire provider name WINS and
	// sticks on the session (picker concerns ride the same shape the
	// model pin carries). No wire name serves the session's pinned
	// one - which is exactly how a removed instance strands ITS
	// sessions: the next send resolves the dead name and the run
	// fails loudly (pinProvider's 400), never a silent fallback to
	// the default instance.
	wireProvider := req.Provider
	if sessionKnown && !agentOwned {
		if req.Provider != "" {
			if req.Provider != sess.ProviderName {
				if scx, ok := s.store.(agent.SessionContextStore); ok {
					_ = scx.SetSessionProvider(ctx, req.SessionID, req.Provider)
				}
			}
		} else if sess.ProviderName != "" {
			req.Provider = sess.ProviderName
		}
	}

	// Model pin: a wire model WINS and sticks (the picker's choice is
	// what the session remembers from here on, card t_9de5b009); no
	// wire model serves the session's remembered one. The write is
	// best-effort - a failed pin leaves the send on the request model.
	// Agent-owned sessions keep their home's model (card t_81109e1c).
	if sessionKnown && !agentOwned {
		if req.Model != "" {
			if req.Model != sess.Model {
				if scx, ok := s.store.(agent.SessionContextStore); ok {
					_ = scx.SetSessionModel(ctx, req.SessionID, req.Model)
				}
			}
		} else if sess.Model != "" {
			req.Model = sess.Model
		}
	}

	// Card t_1914a8c9: the model picker addresses an instance through
	// the slot grammar - resolveSlotModel below. A slot that names a
	// non-default instance can only succeed or 400 at pinProvider,
	// never a silent fallback - nothing more to say here. The slot's
	// resolved pin sticks like the wire rule of t_9de5b009 (best-
	// effort): the session remembers the instance the picker chose.
	s.resolveSlotModel(req, wireProvider)
	if sessionKnown && !agentOwned {
		if scx, ok := s.store.(agent.SessionContextStore); ok {
			switch {
			case req.Provider != "" && req.Provider != sess.ProviderName:
				// the slot's instance choice sticks like the wire rule
				// of t_9de5b009 (best-effort)
				_ = scx.SetSessionProvider(ctx, req.SessionID, req.Provider)
			case req.Provider == "" && sess.ProviderName != "":
				// the "default:" pick unpins (best-effort): the picker
				// explicitly left the instance
				_ = scx.SetSessionProvider(ctx, req.SessionID, "")
			}
		}
	}

	// Per-run provider pin: model override + thinking level (shared
	// with the legacy inline path).
	prov, pinErr := s.pinProvider(req)
	if pinErr != nil {
		s.runs.finish(run)
		s.publishRunState(req.SessionID, false)
		return nil, pinErr
	}

	// Provisional title: the cheap state-machine check rides the same
	// row (no second GetSession), and the title call fires detached
	// before any streaming, so the sidebar leaves the raw session id
	// while the model is still thinking. maybeTitleSession re-reads
	// the session and enforces the state machine before writing. A WAKE
	// launch never fires it (agent-speech, not a conversation start).
	if sessionKnown && sess.TitleState == 0 && !req.wake {
		go s.maybeTitleSession(context.WithoutCancel(ctx), prov, *req, "", false)
	}

	tools := s.tools
	if req.Ephemeral {
		// No-persist contract: the memory tools never run in an
		// ephemeral session (nothing they write outlives the session).
		// The run context carries the deny too, so extension-merged
		// providers cannot reintroduce them (the loop filters post-merge).
		tools = withoutMemoryTools(tools)
	}
	if req.wake {
		// Launch record (card t_32c3fce2): the slot is taken, the run is
		// prepared, and launchWake's launchRun is the only statement
		// left - this wake IS launching. Recorded as a monotone count
		// because the run's live slot outlives no poll interval; a test
		// that samples s.runs.active for it reads a transient that has
		// already passed (see wakeLaunchCount).
		s.wakeMu.Lock()
		if s.wakeLaunches == nil {
			s.wakeLaunches = map[string]int{}
		}
		s.wakeLaunches[req.SessionID]++
		s.wakeMu.Unlock()
	}
	return &chatPrepare{run: run, prov: prov, messages: messages, tools: tools}, nil
}

// wakeLaunchCount reports how many wake runs this server has launched for
// the home - an event record, never a sampled state. A wake run's live slot
// on the boot-recovery path measures 2-4ms (100 boots, loaded machine),
// shorter than the 10ms poll interval the tests used to use and far shorter
// than the 30s bound the wait had: polling s.runs.active for the launch
// therefore missed the run entirely whenever the test goroutine was
// descheduled after NewServer returned, and the deadline - not the launch -
// decided the verdict (card t_32c3fce2).
func (s *Server) wakeLaunchCount(sessionID string) int {
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	return s.wakeLaunches[sessionID]
}

// runMeter tallies the chunks and streaming wall clock one run
// produced. Chunk counts are not persisted anywhere else - the meter
// is the live-run state the last-known rates are computed from at
// persist time (newRunMeter -> tick -> stats in the persist call).
type runMeter struct {
	started   time.Time
	lastChunk time.Time
	chunks    int
}

// tick counts one streamed chunk (thinking or response - the webui's
// live chunks/s meter counts both).
func (m *runMeter) tick(event core.Event) {
	se, ok := event.(core.StreamEvent)
	if !ok {
		return
	}
	switch se.Event.(type) {
	case core.ThinkingChunk, core.ResponseChunk:
		m.chunks++
		m.lastChunk = time.Now()
	}
}

// stats computes the run's average rates over its streaming wall
// clock. A provider that reported no completion tokens, or a run too
// short to average honestly (same 0.2s floor the live meter applies),
// yields nil - the caller keeps any previous block instead.
func (m *runMeter) stats(usage streamUsage) *core.LastRunStats {
	if usage.completion <= 0 || m.chunks == 0 || m.lastChunk.IsZero() {
		return nil
	}
	elapsed := m.lastChunk.Sub(m.started).Seconds()
	if elapsed <= 0.2 {
		return nil
	}
	return &core.LastRunStats{
		TokensPerSecond: int(math.Round(float64(usage.completion) / elapsed)),
		ChunksPerSecond: int(math.Round(float64(m.chunks) / elapsed)),
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	messages, err := decodeMessages(req.Messages)
	if err != nil {
		http.Error(w, "invalid messages: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := checkImageSizes(messages); err != nil {
		http.Error(w, "image too large: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Non-image attachment references (card t_1ffd07de): validated BEFORE
	// any session row is written, so a bad reference never leaves an empty
	// auto-created session behind. A staged draft upload (no session id at
	// attach time) is relocated by the second pass below, once the id
	// exists.
	if err := resolveAttachments(messages, ""); err != nil {
		http.Error(w, "attachment: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Replay guards run BEFORE auto-create: both re-play an EXISTING
	// session's persisted tree - a sessionless replay has nothing to
	// replay (auto-creating an empty session to 400 into would be
	// noise), and messages alongside a replay-turn are a client bug.
	// The same wall holds for the action pipe's entry (prepareRun).
	if err := replayGuards(&req, s.store != nil); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The draft workspace intent (t_ea7e6a58) pins ONLY the bare
	// create. Everything else - an explicit session id, an ephemeral
	// session (registry memory, no row to pin), a fork/replay turn, or
	// the legacy no-store lane (nothing to pin on) - plus a workspace
	// is a client bug, refused before anything runs.
	if req.Workspace != "" &&
		(req.SessionID != "" || req.Ephemeral || req.Fork != "" || req.Retry || s.store == nil) {
		http.Error(w, "workspace rides session creation only", http.StatusBadRequest)
		return
	}
	autoCreated := false
	if req.Ephemeral && req.SessionID == "" {
		// Ephemeral mint: an `eph-` id backed by the in-memory
		// registry, NOT the store (card t_d96a5cd5). The id still rides
		// the `session` frame so the webui binds the tab to it.
		id := mintEphemeralID()
		s.eph.create(id)
		req.SessionID = id
		autoCreated = true
	} else if req.Ephemeral && req.SessionID != "" {
		// Continue-on-id: the client reattaches to server memory - an
		// unknown (expired/restarted) ephemeral id is a 404, never a
		// silent resurrect.
		if s.eph.get(req.SessionID) == nil {
			http.Error(w, "ephemeral session not found: expired or restarted", http.StatusNotFound)
			return
		}
	} else if s.store != nil && req.SessionID == "" {
		id, err := agent.NewSessionID()
		if err != nil {
			// same fallback POST /sessions uses - crypto/rand never
			// fails on supported platforms; this only keeps the
			// server alive if it ever does
			id = fmt.Sprintf("sess-%d", time.Now().UnixNano())
		}
		// Born-pin (t_ea7e6a58): the draft intent resolves BEFORE the
		// row write - a bad path answers 400, a store
		// without the context seam answers 501, both create nothing.
		var wsDir string
		if req.Workspace != "" {
			dir, err := core.ResolveWorkspaceDir(req.Workspace)
			if err != nil {
				http.Error(w, "workspace: "+err.Error(), http.StatusBadRequest)
				return
			}
			if _, ok := s.store.(agent.SessionContextStore); !ok {
				http.Error(w, "session store has no session-context surface", http.StatusNotImplemented)
				return
			}
			wsDir = dir
		}
		if err := s.store.CreateSession(r.Context(), id, "", ""); err != nil {
			http.Error(w, "create session: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if wsDir != "" {
			scx := s.store.(agent.SessionContextStore)
			if err := scx.SetSessionWorkspace(r.Context(), id, wsDir); err != nil {
				http.Error(w, "set session workspace: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		req.SessionID = id
		autoCreated = true
	}

	// The non-image attachment relocation (card t_1ffd07de) runs HERE:
	// the session id is final now (explicit, minted, or auto-created), so
	// a file staged by a draft upload moves into the session's own upload
	// dir and the reference path is rewritten before prepareRun loads and
	// persists the turn - the stored message names the session's dir.
	if err := resolveAttachments(messages, req.SessionID); err != nil {
		http.Error(w, "attachment: "+err.Error(), http.StatusBadRequest)
		return
	}

	// The legacy no-store path runs inline AFTER the SSE headers (the
	// client's presence gates the run); session-backed runs - durable
	// AND ephemeral - flow through prepareRun: slot, history, provider
	// and title kick are one path for all detached entries (the
	// handler and the chat panel's retry action).
	if s.store != nil || req.Ephemeral {
		cr, prepErr := s.prepareRun(r.Context(), &req, messages)
		if prepErr != nil {
			code := http.StatusInternalServerError
			if re, ok := prepErr.(*runError); ok {
				code = re.code
			}
			http.Error(w, prepErr.Error(), code)
			return
		}
		// Subscribe BEFORE anything client-visible (the session frame
		// below) - the same law handleSessionEvents serves on the
		// reattach side (t_eba53ebe): the moment the origin response is
		// visible on the wire, the subscriber's channel must be in
		// run.subs, so a broadcast landing in the visible-to-subscribed
		// gap (a concurrent canvas delta rides broadcastAll) cannot be
		// dropped. setupSSE failure walks it back through the same
		// finish() the pre-SSE failure used.
		ch := s.runs.subscribe(cr.run)
		flusher, ok := setupSSE(w)
		if !ok {
			s.runs.finish(cr.run)
			s.publishRunState(cr.run.id, false)
			return
		}
		// Tell the client its session id before any run frames: the
		// webui re-keys its stream state from this frame (it no longer
		// POSTs /sessions first). Only the auto-created origin stream
		// carries it - reattach subscribers already know the id from
		// their URL.
		if autoCreated {
			writeSSE(w, "session", []byte(fmt.Sprintf(`{"id":%q}`, req.SessionID)))
			flusher.Flush()
		}
		// Session-backed chats run DETACHED (the cron runner's
		// detached-turn shape): the run consumes the agent event channel
		// itself on a context that outlives this request, so a client
		// disconnect - a page refresh mid-generation - no longer cancels
		// the run. This response becomes just another subscriber of the
		// run's live frames; reattach is GET /sessions/{id}/events
		// (handleSessionEvents). ch is the subscriber's channel,
		// already registered (above, before the session frame): the
		// run broadcasts into it from launch.
		s.launchRun(context.WithoutCancel(r.Context()), cr.run, cr.prov, cr.messages, cr.tools, req)
		s.serveRunFrames(w, flusher, cr.run, ch, r.Context())
		return
	}

	// No store: the legacy path. The run IS the request - inline, the
	// client's presence gates it, no run slot, no chat panel (no
	// session to cover). Same provider pin and eph-id wall the
	// prepareRun path enforces. The default_workspace fallback applies
	// here too (card t_ccf74d88): a no-store run's tools are rooted at
	// the defined knob, @-mention expansion along with them.
	if s.store == nil && !req.Ephemeral {
		if isEphemeralID(req.SessionID) {
			http.Error(w, "ephemeral session id requires the ephemeral flag", http.StatusBadRequest)
			return
		}
		// the default_workspace fallback applies here too (card
		// t_ccf74d88): a no-store run's tools are rooted at the knob,
		// @-mention expansion along with them.
		s.wsMu.Lock()
		if req.workspace == "" {
			req.workspace = s.defaultWorkspace
		}
		s.wsMu.Unlock()
		messageIn := s.expandAtMentions(messages, req.workspace)
		// Card t_1914a8c9: the picker's slot grammar applies on the
		// legacy path too (no store, so no sticky pin to clear).
		s.resolveSlotModel(&req, req.Provider)
		prov, pinErr := s.pinProvider(&req)
		if pinErr != nil {
			http.Error(w, pinErr.Error(), pinErr.code)
			return
		}
		flusher, ok := setupSSE(w)
		if !ok {
			return
		}
		recs, usage, last := s.streamAgentEvents(r.Context(), w, flusher, prov, messageIn, s.tools, req)
		if err := s.persistAssistant(r.Context(), prov, req, recs, usage, last); err != nil {
			writeSSE(w, "error", []byte(err.Error()))
			flusher.Flush()
		}
		return
	}
}

// handleSessionStop serves POST /sessions/{id}/stop: cancels the live
// run's context. The run does NOT die on a new unwind path - the same
// WithTimeout ctx the wall-clock cap rides gets cancelled, so the loop
// unwinds through its proven normal path: partial output persists, the
// loop's terminal agent_end (finish_reason "cancelled") broadcasts, and
// deregistration happens when the run really ends. 404 when the session
// has no live run; 409 when the run exists but has not registered its
// cancel yet (the start->launchRun window). A repeat stop on a
// winding-down run is a clean 200 - cancelling an already-cancelled ctx
// is a no-op.
func (s *Server) handleSessionStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	ok, cancellable := s.runs.stop(id)
	if !ok {
		http.Error(w, "session not active", http.StatusNotFound)
		return
	}
	if !cancellable {
		http.Error(w, "run not started", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"cancelling"}`)) //nolint:errcheck // constant body
}

// sessionHistory loads the session's persisted history and returns it
// with the incoming messages appended. The incoming messages ride
// req.persistInput - the loop persists them through its sink before the
// first turn; nothing here writes the store. When no session is
// configured (nil store or empty ID), messages are returned unchanged.
// Failures come back as runErrors - the caller owns its error surface
// (chat-last 1/3: one history load for the /chat entry AND the chat
// panel's retry action; the status codes are the old handler's).
//
// History is the ACTIVE PATH (t_17365247), not the linear list: sibling
// variants stay out of the provider's context, and the persisted tree is
// what a reload renders. Retry takes the other branch (t_357cbe9c,
// replay turn verbatim): NOTHING is appended, context truncates at the
// active path's last user message, and req.branchParent pins the fresh
// run head's parent to that user - the tree's sibling-variant hop. The
// prior partial/complete response stays in the store and becomes a
// selectable variant the transcript still addresses. A fork re-plays at
// its target: user-turn targets pin the head like a retry (no messages);
// assistant/tool targets continue FROM them - context truncates at the
// target inclusive and the fork's message chains onto it.
func (s *Server) sessionHistory(ctx context.Context, req *chatRequest, incoming []core.Message) ([]core.Message, error) {
	if isEphemeralID(req.SessionID) {
		// Ephemeral context (card t_d96a5cd5): the registry's in-memory
		// history IS the conversation - the store is never read. The
		// send's wire items land here (rendering); the provider-facing
		// history copy persists through the loop's eph sink at run
		// start. An eph id without the flag is a client bug (the
		// session frame the client reattaches with always accompanies
		// the flag).
		if !req.Ephemeral {
			return nil, &runError{http.StatusBadRequest, "ephemeral session id requires the ephemeral flag"}
		}
		if s.eph.get(req.SessionID) == nil {
			return nil, &runError{http.StatusNotFound, "ephemeral session not found: expired or restarted"}
		}
		s.eph.appendUser(req.SessionID, incoming, time.Now())
		req.persistInput = incoming
		return append(s.eph.history(req.SessionID), incoming...), nil
	}
	if s.store == nil || req.SessionID == "" {
		return incoming, nil
	}
	if _, err := s.store.GetSession(ctx, req.SessionID); err != nil {
		return nil, &runError{http.StatusNotFound, "session not found: " + err.Error()}
	}
	view, err := s.store.GetActiveView(ctx, req.SessionID)
	if err != nil {
		return nil, &runError{http.StatusInternalServerError, "load history: " + err.Error()}
	}
	if req.Retry {
		end := -1
		for i := len(view.Path) - 1; i >= 0; i-- {
			if _, ok := view.Path[i].Message.(core.User); ok {
				end = i
				break
			}
		}
		if end == -1 {
			return nil, &runError{http.StatusBadRequest, "nothing to retry: session has no user message"}
		}
		req.branchParent = core.MsgID(view.Path[end].ID)
		req.branchFloor = branchFloorOf(view.Path)
		return storedMessages(view.Path[:end+1]), nil
	}
	if req.Fork != "" {
		forkID, idErr := core.ParseMsgID(req.Fork)
		if idErr != nil {
			return nil, &runError{http.StatusBadRequest, idErr.Error()}
		}
		at := -1
		for i, sm := range view.Path {
			if sm.ID == forkID {
				at = i
				break
			}
		}
		if at == -1 {
			return nil, &runError{http.StatusBadRequest, "fork target is not on the active path (select its variant via /sessions/{id}/variants first, then fork)"}
		}
		switch view.Path[at].Message.(type) {
		case core.User:
			if len(incoming) > 0 {
				return nil, &runError{http.StatusBadRequest, "fork of a user turn takes no messages (retry semantics)"}
			}
			req.branchParent = req.Fork
			req.branchFloor = branchFloorOf(view.Path)
			return storedMessages(view.Path[:at+1]), nil
		default:
			if len(incoming) == 0 {
				return nil, &runError{http.StatusBadRequest, "fork requires a message to run from the forked point"}
			}
			for i := range incoming {
				incoming[i] = core.WithParentID(incoming[i], req.Fork)
			}
			// The fork's messages persist through the loop's sink;
			// their explicit parent is what the store materializes.
			req.persistInput = incoming
			return append(storedMessages(view.Path[:at+1]), incoming...), nil
		}
	}
	req.persistInput = incoming
	return append(storedMessages(view.Path), incoming...), nil
}

// storedMessages strips the tree view down to plain provider context.
func storedMessages(path []core.StoredMessage) []core.Message {
	out := make([]core.Message, 0, len(path))
	for _, sm := range path {
		out = append(out, sm.Message)
	}
	return out
}

// branchFloorOf reports the active path's max store row id - the floor a
// branch-pinned run's own rows all exceed (row ids rise monotonically
// with append order, and the loop's sink is the session's only writer
// during the run). Pure.
func branchFloorOf(path []core.StoredMessage) int64 {
	var floor int64
	for _, sm := range path {
		if sm.ID > floor {
			floor = sm.ID
		}
	}
	return floor
}

// setupSSE sets SSE response headers and returns the flusher. On failure
// an HTTP error is written to w and ok is false.
func setupSSE(w http.ResponseWriter) (http.Flusher, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return nil, false
	}
	return flusher, true
}

// resolveSlotModel applies the model picker's slot grammar (card
// t_1914a8c9). A model value shaped "instance:model" whose prefix
// names a CONFIGURED instance wins OVER the session's (or wire's)
// provider pin - the explicit picker choice is what the run serves
// now. "default:model" names the base provider: when the wire carried
// no provider of its own, it cancels the sticky pin for this run (the
// picker explicitly left the instance). Anything else stays a bare
// model on the request provider (ParseModelSlot is grammar-only, so
// ollama-style "family:tag" names that do not collide with a
// configured instance id pass through untouched). Call it after the
// sticky pin + model pin fills, before pinProvider, on both the
// prepareRun path and the legacy inline path; wireProvider is the
// wire's own provider field.
func (s *Server) resolveSlotModel(req *chatRequest, wireProvider string) bool {
	inst, model := agent.ParseModelSlot(req.Model)
	if inst == "" {
		return false
	}
	if inst != "default" {
		if prov := s.baseProvider(req); prov != nil {
			if ir, ok := prov.(agent.ProviderInstances); ok {
				if _, found := ir.Instance(inst); found {
					req.Provider = inst
					req.Model = model
					return false
				}
			}
		}
		return false
	}
	// "default:model": only a picker-written prefix cancels the pin.
	if wireProvider == "" {
		req.Provider = ""
		req.Model = model
		return true
	}
	return false
}

func (s *Server) pinProvider(req *chatRequest) (agent.Provider, *runError) {
	prov := s.baseProvider(req)
	if prov == nil {
		return nil, &runError{http.StatusInternalServerError, "no provider configured"}
	}
	if req.Provider != "" {
		// Instance resolution (card t_7dcd4280): the run serves
		// through the NAMED provider instance. Unknown name = the
		// instance was removed (or never existed) - the run fails
		// loudly and the default never picks the work up silently.
		ir, ok := prov.(agent.ProviderInstances)
		if !ok {
			return nil, &runError{http.StatusBadRequest,
				fmt.Sprintf("provider instance %q cannot serve: this provider carries no named instances", req.Provider)}
		}
		inst, ok := ir.Instance(req.Provider)
		if !ok {
			return nil, &runError{http.StatusBadRequest,
				fmt.Sprintf("provider instance %q is not configured (it was removed, or the name is misspelled); session: re-select a provider to unstick", req.Provider)}
		}
		prov = inst
	}
	if req.Model != "" {
		prov = prov.WithModel(req.Model)
	}
	if req.ThinkingLevel != 0 {
		if req.ThinkingLevel < 0 || req.ThinkingLevel >= len(llm.ThinkingLevels) {
			return nil, &runError{http.StatusBadRequest, fmt.Sprintf("invalid thinking_level: %d (0..%d)",
				req.ThinkingLevel, len(llm.ThinkingLevels)-1)}
		}
		prov = prov.WithThinkingLevel(llm.ThinkingLevels[req.ThinkingLevel])
	}
	return prov, nil
}

// turnSinkFor builds the run's TurnSink (card t_471f6d4a) - the loop
// is the only writer of turn copies, so every path that starts a run
// wraps one. Store-backed sessions persist through the standard seam
// (head parent = the branch point the channel computed); ephemeral
// sessions persist into the registry's in-memory history (the eph
// conversation's store equivalent). A legacy no-store non-ephemeral
// run gets nil - it persists nothing, exactly as before.
func (s *Server) turnSinkFor(req chatRequest) agent.TurnSink {
	if req.Ephemeral && req.SessionID != "" {
		return &ephTurnSink{r: &s.eph, id: req.SessionID}
	}
	if s.store == nil || req.SessionID == "" {
		return nil
	}
	return agent.NewStoreTurnSink(s.store, req.SessionID, req.branchParent)
}

// streamAgentEvents runs the agent event loop, writing SSE events to w.
// prov is the per-request provider (a copy carrying the request's model).
// It returns the accumulated assistant records for session persistence
// and the real provider-reported usage for the token totals.
func (s *Server) streamAgentEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, prov agent.Provider, messages []core.Message, tools map[string]core.Tool, req chatRequest) ([]assistantRecord, streamUsage, *core.LastRunStats) {
	// tools run in THIS session's context: board tools read the session
	// id from ctx to auto-link tasks created mid-chat.
	ctx = agent.WithSessionID(ctx, req.SessionID)
	// Session workspace (card t_9de5b009): relative file operations
	// root at the session's folder and the prompt carries the folder's
	// trust-gated AGENTS.md context (resolved in prepareRun). Empty =
	// no workspace - today's behavior, the ctx override is absent.
	ctx = core.WithRunWorkspace(ctx, req.workspace, req.workspacePromptContext)
	// Verdict cards v1: the no-store legacy path is still an interactive
	// turn - the verdict tool may issue from here.
	ctx = core.WithRunOrigin(ctx, "interactive")
	// Turn copy persistence belongs to the loop (card t_471f6d4a): the
	// sink carries the session and the head-parent pin, the run input
	// rides req.persistInput. Channels render from the store, never
	// append turn content.
	ctx = agent.WithTurnSink(ctx, s.turnSinkFor(req))
	ctx = agent.WithPersistInput(ctx, req.persistInput)
	var recs []assistantRecord
	var rec assistantRecord
	var usage streamUsage
	meter := newRunMeter()
	runner := s.runAgent(&req)
	for event := range runner.RunWithProvider(ctx, prov, messages, tools) {
		s.accumulateAssistant(&recs, &rec, event, req)
		meter.tick(event)
		// Usage arrives on each stream_end; sum every turn of the run.
		if se, ok := event.(core.StreamEvent); ok {
			if end, ok := se.Event.(core.StreamEnd); ok {
				if end.PromptEvalCount != nil {
					usage.prompt += *end.PromptEvalCount
				}
				if end.EvalCount != nil {
					usage.completion += *end.EvalCount
				}
			}
		}
		eventType, data, err := sseEvent(event)
		if err != nil {
			writeSSE(w, "error", []byte(err.Error()))
			flusher.Flush()
			return append(recs, rec), usage, meter.stats(usage)
		}
		writeSSE(w, eventType, data)
		flusher.Flush()
	}
	return append(recs, rec), usage, meter.stats(usage)
}

// accumulateAssistant appends streamed response-chunk content to the
// segment accumulator when a session is active. A response chunk that
// arrives after tool results starts a new segment - the frontend splits
// the live view at the same boundary.
func (s *Server) accumulateAssistant(recs *[]assistantRecord, rec *assistantRecord, event core.Event, req chatRequest) {
	if s.store == nil || req.SessionID == "" {
		return
	}
	now := time.Now()
	// ToolResultEvent is a top-level agent event (not wrapped in
	// StreamEvent) - the serving surface persists it the same way
	if tre, ok := event.(core.ToolResultEvent); ok {
		if start, ok := rec.callStarts[tre.Message.ToolCallID]; ok && !start.IsZero() {
			tre.Message.Timing = &core.Timing{ElapsedMs: now.Sub(start).Milliseconds()}
			delete(rec.callStarts, tre.Message.ToolCallID)
		}
		rec.results = append(rec.results, tre.Message)
		// The result opens the next segment's clock: the wait from the
		// last result to the model's next token is generation latency,
		// not a tool gap.
		rec.openAt = now
		return
	}
	se, ok := event.(core.StreamEvent)
	if !ok {
		// TurnStart opens the next provider call's window: from here
		// until the stream's first chunk, the loop and prompt
		// processing bill to the phase that first chunk lands in.
		if _, ok := event.(core.TurnStart); ok {
			rec.openAt = now
		}
		return
	}
	switch ev := se.Event.(type) {
	case core.ThinkingChunk:
		if len(rec.results) > 0 {
			// thinking after tool results is turn 2's - persist it
			// with the next message (the live view splits there
			// too), not concatenated onto turn 1's record. The
			// opening boundary rides into the fresh record so the
			// post-result latency still bills the new segment.
			*recs = append(*recs, *rec)
			*rec = assistantRecord{openAt: rec.openAt}
		}
		rec.thinking += ev.Content
		rec.thinkDur += phaseDelta(rec.lastThink, rec.openAt, now)
		rec.lastThink = now
		rec.openAt = now
	case core.ResponseChunk:
		if len(rec.results) > 0 {
			// tool phase ended the current segment - the frontend
			// renders a fresh bubble for what follows, so we persist
			// a fresh record for it
			*recs = append(*recs, *rec)
			*rec = assistantRecord{openAt: rec.openAt}
		}
		rec.content += ev.Content
		rec.respDur += phaseDelta(rec.lastResp, rec.openAt, now)
		rec.lastResp = now
		rec.openAt = now
	case core.ToolCallEvent:
		rec.toolCalls = append(rec.toolCalls, ev.ToolCall)
		// a tool call closes both spans - the gap while the tool runs
		// belongs to nobody
		rec.lastThink, rec.lastResp, rec.openAt = time.Time{}, time.Time{}, time.Time{}
		if rec.callStarts == nil {
			rec.callStarts = map[string]time.Time{}
		}
		if ev.ToolCall.ID != "" {
			rec.callStarts[ev.ToolCall.ID] = now
		}
	}
}

// phaseDelta returns the wall clock the chunk bills into its span:
// chunk-to-chunk while the same phase is running (last set), and
// boundary-to-chunk when the chunk opens a span (turn start / last
// tool result), so a single-chunk phase still carries its latency. An
// unset boundary contributes nothing.
func phaseDelta(last, open, now time.Time) time.Duration {
	if !last.IsZero() {
		return now.Sub(last)
	}
	if !open.IsZero() {
		return now.Sub(open)
	}
	return 0
}

// persistAssistant closes the run's persistence epilogue (the turn
// copies themselves - assistants and tool results - the loop's sink
// wrote while the turns completed, card t_471f6d4a). The ephemeral
// branch commits the run's records into the registry (wire items +
// telemetry, no disk). The store branch adds the run's real token
// totals to the session row and writes the run's computed last-known
// rates when non-nil (never cleared here - a run without reported
// usage leaves the previous block standing). The first persistence
// error is RETURNED, not written to a wire - the caller owns the wire
// (the legacy request writes one error frame; the detached run
// broadcasts one).
func (s *Server) persistAssistant(ctx context.Context, prov agent.Provider, req chatRequest, recs []assistantRecord, usage streamUsage, last *core.LastRunStats) error {
	if req.Ephemeral && req.SessionID != "" {
		// In-memory commit (card t_d96a5cd5): the run's records land in
		// the registry (transcript items + telemetry) - the messages
		// themselves ride the loop's eph sink; no store write, no
		// title run, gone at restart.
		s.eph.commit(req.SessionID, recs, usage, last, time.Now())
		return nil
	}
	if s.store == nil || req.SessionID == "" || len(recs) == 0 {
		return nil
	}
	lastContent := ""
	for _, rec := range recs {
		if rec.content != "" {
			lastContent = rec.content
		}
	}
	if err := s.store.AddSessionTokens(ctx, req.SessionID, usage.prompt, usage.completion); err != nil {
		return fmt.Errorf("persist usage: %w", err)
	}
	// the run's last-known rates ride the same persist; a nil block
	// (no usage, no chunks, too-short run) leaves the previous one
	if last != nil {
		if err := s.store.SetSessionLastRun(ctx, req.SessionID, last); err != nil {
			return fmt.Errorf("persist last run: %w", err)
		}
	}
	if lastContent != "" {
		// Detached: the title call is a second agent run, and running it
		// in the request path starved a back-to-back /chat for up to 240s
		// (observed live with the stream probe). WithoutCancel keeps
		// request values (logger) after the HTTP request returns. The
		// title run pins this request's provider copy - the session is
		// titled on the model that served it, regardless of what the
		// next /chat sends.
		// Final title: firstUser + reply context, once the exchange is
		// persisted. The title_state <= 1 check skips sessions already at
		// final state; maybeTitleSession re-reads and enforces the gate.
		// ponytail: two rapid first-exchanges can interleave provisional
		// and final writes - both go through SetSessionTitle guards and
		// last write still wins on the label, an acceptable cosmetic race
		// at single-user load.
		if sess, err := s.store.GetSession(ctx, req.SessionID); err == nil && sess.TitleState <= 1 {
			go s.maybeTitleSession(context.WithoutCancel(ctx), prov, req, lastContent, true)
		}
	}
	return nil
}

// maybeTitleSession generates a short session title with a cheap model
// call and writes it via SetSessionTitle in one state-transitioning
// update. final=true is the post-exchange call (firstUser + reply
// context); final=false is the provisional request-start call
// (user message only). The state machine gate: provisional only for an
// untitled session (state 0), final only while the title is missing or
// provisional (state 0 or 1) - and a manually set label (PUT
// /sessions/{id}, /label) is never overwritten by a generated one.
// prov is the pinned per-request provider copy. Runs detached; failure
// is silent - an untitled session is cosmetic, not fatal.
func (s *Server) maybeTitleSession(ctx context.Context, prov agent.Provider, req chatRequest, lastAssistant string, final bool) {
	// title_model knob: when set, BOTH title calls route through it -
	// the request's pinned provider (the model that served the chat)
	// is overridden. Without the knob everything is unchanged.
	if s.titleModel != "" {
		prov = prov.WithModel(s.titleModel)
	}
	sess, err := s.store.GetSession(ctx, req.SessionID)
	if err != nil {
		return
	}
	// Manual-label guard: a label sitting at title state 0 was set by
	// a user (PUT /sessions/{id} or /label), not by this flow - never
	// overwritten. Generated provisional labels carry state 1, so the
	// final call can still upgrade them, and state 2 blocks everything.
	if sess.Label != "" && sess.TitleState == 0 {
		return
	}
	if final && sess.TitleState > 1 {
		return
	}
	if !final && sess.TitleState != 0 {
		return
	}
	// ponytail: first user message from the request body, truncated hard —
	// the title call only needs a gist, not the full exchange.
	firstUser := ""
	for _, raw := range req.Messages {
		var m struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal(raw, &m) == nil && m.Role == "user" && m.Content != "" {
			firstUser = m.Content
			break
		}
	}
	if firstUser == "" {
		return
	}
	if len(firstUser) > 400 {
		firstUser = firstUser[:400]
	}
	prompt := "Write a conversation title of at most 5 words for this exchange. Reply with ONLY the title, no quotes, no punctuation at the end.\n\nUser: " + firstUser
	if final {
		// Final call: the reply's gist is part of the gist.
		prompt += "\nAssistant: " + truncate(lastAssistant, 400)
	}

	// The title turn is metadata, not a conversation - it must never
	// call a tool. Before the toolless run this was exactly the live
	// defect t_77e27ef8 shipped with: the user content invited tool use
	// and the title model SPAWNED REAL subagents (delegate:task-3/6),
	// one parentless - the title ctx carries no WithSessionID. A run
	// marked toolless (agent.WithToolless) drops the whole merged
	// registry - delegate.task, shell, verdict, all of
	// it, present or future: no schema reaches the model, no dispatch
	// entry survives the merge, and a stray call is an error result.
	// The run-scoped mechanism mirrors WithDeniedTools (t_e06b38bc,
	// t_77e27ef8's verdict denial - subsumed here).
	ctx = agent.WithToolless(ctx)

	events := s.agent.RunWithProvider(ctx, prov, []core.Message{core.NewUser(prompt)}, nil)
	title := ""
	for ev := range events {
		se, ok := ev.(core.StreamEvent)
		if !ok {
			continue
		}
		chunk, ok := se.Event.(core.ResponseChunk)
		if !ok {
			continue
		}
		title += chunk.Content
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	if len(title) > 60 {
		title = title[:60]
	}
	_ = s.store.SetSessionTitle(ctx, req.SessionID, title, final)
}

// truncate cuts s at n runes, appending an ellipsis marker when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// handleSessionCreate serves POST /sessions - the chat floor's session
// id creation + born-pin. The GET /sessions list rode the store capsule
// out with the shell-ring shrink wave 2 (the three-layer sessions law,
// ADR Workshop/omega-shell-ring-shrink.md; harness/store/routes.go).
// It returns 501 when the server has no store.
func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "session store not configured", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodPost:
		var req struct {
			ID string `json:"id"`
			// Workspace roots the session at creation (card
			// t_ea7e6a58): the webui pins the draft's folder intent at
			// the create call. Validated BEFORE the row write - a bad
			// path answers 400 and creates nothing. Empty = no root.
			Workspace string `json:"workspace,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.ID == "" {
			id, err := agent.NewSessionID()
			if err != nil {
				// ponytail: crypto/rand never fails on supported platforms; a
				// fallback timestamp keeps the server alive if it ever does.
				id = fmt.Sprintf("sess-%d", time.Now().UnixNano())
			}
			req.ID = id
		}
		// The `eph-` namespace is minted by the /chat ephemeral lane and
		// lives in memory only; a client-chosen `eph-` id would create a
		// durable row the store-read endpoints can never serve.
		if isEphemeralID(req.ID) {
			http.Error(w, "invalid request: reserved session id prefix "+ephemeralPrefix, http.StatusBadRequest)
			return
		}
		// The born-pin (t_ea7e6a58): resolve + store-capability check
		// BEFORE CreateSession - never a half-pinned row. The seam check
		// mirrors the PATCH surface's; a store without the context seam
		// answers 501 and creates nothing.
		var wsDir string
		if req.Workspace != "" {
			dir, err := core.ResolveWorkspaceDir(req.Workspace)
			if err != nil {
				http.Error(w, "workspace: "+err.Error(), http.StatusBadRequest)
				return
			}
			if _, ok := s.store.(agent.SessionContextStore); !ok {
				http.Error(w, "session store has no session-context surface", http.StatusNotImplemented)
				return
			}
			wsDir = dir
		}
		if err := s.store.CreateSession(r.Context(), req.ID, "", ""); err != nil {
			http.Error(w, "create session: "+err.Error(), http.StatusConflict)
			return
		}
		if wsDir != "" {
			scx := s.store.(agent.SessionContextStore)
			if err := scx.SetSessionWorkspace(r.Context(), req.ID, wsDir); err != nil {
				http.Error(w, "set session workspace: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		sess, err := s.store.GetSession(r.Context(), req.ID)
		if err != nil {
			http.Error(w, "get session: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, sess)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSessionByID serves GET /sessions/{id} (session + messages) and
// DELETE /sessions/{id}. It returns 501 when the server has no store.
func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		// Rename is store-owned; an eph id has no row to name.
		if id := r.PathValue("id"); isEphemeralID(id) {
			http.Error(w, "ephemeral sessions cannot be renamed", http.StatusBadRequest)
			return
		}
	} else {
		// Ephemeral ids never reach the store: detail, close and the
		// no-op read-stamp answer from the registry (card t_d96a5cd5).
		if id := r.PathValue("id"); isEphemeralID(id) {
			if s.serveEphemeralDetail(w, r, id) {
				return
			}
		}
	}
	if s.store == nil {
		http.Error(w, "session store not configured", http.StatusNotImplemented)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		sess, err := s.store.GetSession(r.Context(), id)
		if err != nil {
			http.Error(w, "session not found: "+err.Error(), http.StatusNotFound)
			return
		}
		// Typed messages marshal without a role field (core.User/core.Assistant
		// only carry content) - inject core.MessageRole so the UI can tell
		// user and assistant apart when restoring history. GetMessagesWithTimes
		// additionally pairs each message with its store-level created_at,
		// injected the same way so the UI can show message times.
		bare, err := s.store.GetMessagesWithTimes(r.Context(), id)
		if err != nil {
			http.Error(w, "get messages: "+err.Error(), http.StatusInternalServerError)
			return
		}
		msgs := make([]map[string]any, len(bare))
		for i, sm := range bare {
			data, _ := json.Marshal(sm.Message)
			var mm map[string]any
			json.Unmarshal(data, &mm)
			mm["role"] = core.MessageRole(sm.Message)
			mm["created_at"] = sm.CreatedAt
			msgs[i] = mm
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"session":  sess,
			"messages": msgs,
			"unread":   agent.SessionUnread(sess),
			// live-run flag (always present, running:true/false): true while
			// a detached /chat run is streaming this session - the webui
			// open-session flow auto-subscribes to /sessions/{id}/events
			// when it sees this
			"running": s.runs.active(id),
		})
	case http.MethodDelete:
		if err := s.store.DeleteSession(r.Context(), id); err != nil {
			http.Error(w, "delete session: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPut:
		// Rename a session (the webui sets an LLM-generated title as label).
		var body struct {
			Label string `json:"label"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Label == "" {
			http.Error(w, "label must not be empty", http.StatusBadRequest)
			return
		}
		if err := s.store.UpdateSession(r.Context(), id, body.Label); err != nil {
			http.Error(w, "update session: "+err.Error(), http.StatusInternalServerError)
			return
		}
		sess, err := s.store.GetSession(r.Context(), id)
		if err != nil {
			http.Error(w, "session not found: "+err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, sess)
	case http.MethodPatch:
		// Row flags: {"pinned": true|false} (pin toggle, existing),
		// {"read": true} (mark read - stamps last_read_at without
		// touching updated_at, same shape as the pinned PATCH) and
		// {"archived": true|false} (archive toggle, same no-touch
		// contract). All are list concerns: pinning, reading and
		// archiving are not activity, and the webui time buckets read
		// updated_at. Session context (card t_9de5b009) joins the
		// same no-touch contract: {"model": "..."} pins the model
		// the session remembers, {"workspace": "/abs/dir"} roots the
		// session's file operations ("" clears either).
		var body struct {
			Pinned    *bool   `json:"pinned"`
			Read      *bool   `json:"read"`
			Archived  *bool   `json:"archived"`
			Model     *string `json:"model"`
			Workspace *string `json:"workspace"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Pinned == nil && body.Read == nil && body.Archived == nil &&
			body.Model == nil && body.Workspace == nil {
			http.Error(w, "pinned, read, archived, model or workspace must not be missing", http.StatusBadRequest)
			return
		}
		if body.Read != nil && !*body.Read {
			http.Error(w, "read must be true", http.StatusBadRequest)
			return
		}
		var resolvedDir string
		// model/workspace: "": clears the pin, non-empty: sets it
		// (trimmed). Workspace additionally must be an existing
		// directory - rejected before any write lands.
		if body.Workspace != nil {
			resolvedDir = ""
			if *body.Workspace != "" {
				dir, err := core.ResolveWorkspaceDir(*body.Workspace)
				if err != nil {
					http.Error(w, "workspace: "+err.Error(), http.StatusBadRequest)
					return
				}
				resolvedDir = dir
			}
		}
		if body.Pinned != nil {
			if err := s.store.SetSessionPinned(r.Context(), id, *body.Pinned); err != nil {
				http.Error(w, "pin session: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if body.Read != nil {
			if err := s.store.SetSessionRead(r.Context(), id); err != nil {
				http.Error(w, "read session: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if body.Archived != nil {
			if err := s.store.SetSessionArchived(r.Context(), id, *body.Archived); err != nil {
				http.Error(w, "archive session: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		// Session context rides the optional SessionContextStore seam
		// - a store without it (fakes, embedders) answers 501 on the
		// model/workspace fields instead of failing the whole patch.
		if body.Model != nil || body.Workspace != nil {
			scx, ok := s.store.(agent.SessionContextStore)
			if !ok {
				http.Error(w, "session store has no session-context surface", http.StatusNotImplemented)
				return
			}
			if body.Model != nil {
				if err := scx.SetSessionModel(r.Context(), id, strings.TrimSpace(*body.Model)); err != nil {
					http.Error(w, "set session model: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}
			if body.Workspace != nil {
				// dir was resolved + validated above; "" (clear) or
				// the cleaned absolute path lands on the row.
				if err := scx.SetSessionWorkspace(r.Context(), id, resolvedDir); err != nil {
					http.Error(w, "set session workspace: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}
		sess, err := s.store.GetSession(r.Context(), id)
		if err != nil {
			http.Error(w, "session not found: "+err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, sess)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// toolActionListing is one sub-action of a domain tool: name and its
// own one-liner (the webui's dotted sub-rows).
type toolActionListing struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// toolListing is one /tools row: the registry entry's name, short
// description, and the sub-action metadata of a domain tool (the
// inventory, not the schemas - parameter maps would only bloat the
// listing the UI shows).
type toolListing struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Actions     []toolActionListing `json:"actions,omitempty"`
	// Origin (card t_c2da3379): where the tool registered from ("mcp"
	// = MCP bridge, "" = native, omitted on the wire). The tools tab
	// filters on this field - never on dotted-name shape.
	Origin string `json:"origin,omitempty"`
	// Disabled (card t_5c9bb50a): true only in the fold-aware read
	// (GET /tools?disabled=1) - the tool the config fold switched off,
	// kept in the inventory so the settings tab can re-enable it.
	// omitempty keeps the bare /tools wire byte-identical (a disabled
	// entry never appears there, and on-entries never carry the key).
	Disabled bool `json:"disabled,omitempty"`
}

// handleTools serves GET /tools: the agent's executable tool registry
// as a name+description array, sorted by name. Extension-provided
// alias shims (Tool.AliasOf) are hidden: they stay callable by the
// model until the v0.12 mount migration retires them, but the
// inventory shows the folded tool with
// its sub-actions. The inventory is the union the chat loop actually
// runs with - the agent's ToolProviders plus the server registry
// (agent.Tools() copies defensively; the server's own set overlays on
// top, matching the loop's merge precedent where existing entries
// win). The webui's module-owned tools panel
// (harness/tools/ui/ToolsList.vue) renders this. Descriptions only -
// parameter maps would only bloat the listing the UI shows.
//
// GET /tools?disabled=1 (card t_5c9bb50a) is the fold-aware
// MANAGEMENT read of the same inventory: the union BEFORE the
// disabled fold filters it (agent.ToolsWithDisabled), every entry
// stamped with the fold's verdict. The settings tab rides this so a
// disabled tool keeps its row and its re-enable toggle - the bare
// read stays the executable registry the chat loop consumes, and its
// response shape never changes.
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var inventory map[string]core.Tool
	fold := map[string]bool{}
	if r.URL.Query().Get("disabled") == "1" {
		inventory, fold = s.agent.ToolsWithDisabled()
	} else {
		inventory = s.agent.Tools()
	}
	if inventory == nil {
		inventory = map[string]core.Tool{}
	}
	for name, t := range s.tools {
		if _, exists := inventory[name]; !exists {
			inventory[name] = t
		}
	}
	names := make([]string, 0, len(inventory))
	for name := range inventory {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]toolListing, 0, len(names))
	for _, name := range names {
		t := inventory[name]
		if t.AliasOf != "" {
			continue // compat shim, not inventory
		}
		row := toolListing{Name: name, Description: t.Description, Origin: t.Origin, Disabled: fold[name]}
		for _, a := range t.Actions {
			row.Actions = append(row.Actions, toolActionListing{Name: a.Name, Description: a.Description})
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// writeJSON writes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// fileError writes the structured JSON error envelope for the request
// surfaces whose failures are data, not panic material (the workspace
// chip's routes and the skills content route).
func fileError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeMessages decodes polymorphic core.Message values from JSON using a
// role discriminator: system, user, assistant, or tool.
func decodeMessages(raw []json.RawMessage) ([]core.Message, error) {
	messages := make([]core.Message, 0, len(raw))
	for _, item := range raw {
		var header struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(item, &header); err != nil {
			return nil, err
		}
		switch header.Role {
		case "system":
			var m core.System
			if err := json.Unmarshal(item, &m); err != nil {
				return nil, err
			}
			messages = append(messages, m)
		case "user":
			var m core.User
			if err := json.Unmarshal(item, &m); err != nil {
				return nil, err
			}
			messages = append(messages, m)
		case "assistant":
			var m core.Assistant
			if err := json.Unmarshal(item, &m); err != nil {
				return nil, err
			}
			messages = append(messages, m)
		case "tool":
			var m core.ToolResult
			if err := json.Unmarshal(item, &m); err != nil {
				return nil, err
			}
			messages = append(messages, m)
		default:
			return nil, fmt.Errorf("unknown role %q", header.Role)
		}
	}
	return messages, nil
}

// sseEvent converts an agent event to an SSE (event type, data) pair.
// StreamEvent wraps an ai event with json:"-", so it is unwrapped and
// emitted under the inner event's own type.
func sseEvent(event core.Event) (string, []byte, error) {
	if stream, ok := event.(core.StreamEvent); ok {
		return sseStreamEvent(stream.Event)
	}
	data, err := json.Marshal(event)
	if err != nil {
		return "", nil, err
	}
	return eventTypeOf(event), data, nil
}

func sseStreamEvent(event core.ProviderStreamEvent) (string, []byte, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return "", nil, err
	}
	switch event.(type) {
	case core.ThinkingChunk:
		return "thinking_chunk", data, nil
	case core.ResponseChunk:
		return "response_chunk", data, nil
	case core.ToolCallEvent:
		return "tool_call", data, nil
	case core.StreamEnd:
		return "stream_end", data, nil
	}
	return "stream", data, nil
}

func eventTypeOf(event core.Event) string {
	switch event.(type) {
	case core.AgentStart:
		return "agent_start"
	case core.TurnStart:
		return "turn_start"
	case core.TurnEnd:
		return "turn_end"
	case core.AgentEnd:
		return "agent_end"
	case core.AssistantMessageEvent:
		return "assistant_message"
	case core.ToolResultEvent:
		return "tool_result"
	case core.ViewModelDelta:
		return "view_model_delta"
	}
	return "event"
}

// writeSSE writes one SSE frame in the format
// "event: <type>\ndata: <json>\n\n".
func writeSSE(w io.Writer, eventType string, data []byte) {
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", data)
}
