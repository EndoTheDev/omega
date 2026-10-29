package http_channel

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/engine/ai"
)

//go:embed static/*
var staticFS embed.FS

// webuiFS is the embedded SPA at the FS root, so the FileServer at "/"
// finds index.html (embed places files under static/; fs.Sub strips it).
var webuiFS, _ = fs.Sub(staticFS, "static")

// Server exposes the agent over HTTP with SSE streaming.
type Server struct {
	agent *core.Agent
	tools map[string]core.Tool
	// runs is a per-PROCESS hub, not per-bind (t_c6f13a82): every
	// multi-bind mirror shares it (HTTPChannel.Start swaps it in), or a
	// run started through the action pipe on one bind is invisible to
	// the transcript/events reads on the others - exactly the hub
	// invisibility journey 30 caught. NewServer wires a private hub so
	// directly constructed Servers (tests, embedding) stay self-contained.
	runs *runHub // detached /chat runs by session id (reattach surface)
	// eph is the no-persist (ephemeral) session registry: `eph-` ids
	// live here, never in the store - invisible to every store-derived
	// surface, gone at restart (card t_d96a5cd5).
	eph      ephRegistry
	store    core.StoreProvider
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
	chat    *chatCapsule
	httpSrv *http.Server
	// runTimeout caps each detached /chat run's wall-clock time. The
	// LLM call carries the HTTP timeout, but a hung tool call would
	// otherwise run forever and hold the session's run slot until
	// restart. Default set here; the mount overrides it from config.
	runTimeout time.Duration

	// version is the host's build tag, surfaced through /health. Empty
	// omits the field.
	version string
	// titleModel is the title_model knob: session-title calls route
	// through this model when set, instead of the call's pinned
	// provider (the main model). Empty = no override.
	titleModel string
	// startedAt anchors the /health uptime (time since server construction).
	startedAt time.Time

	ctxMu     sync.Mutex
	ctxWindow map[string]int // model name -> context window (probed once)

	// themes serves GET /themes backed by the OMEGA_HOME/themes folder,
	// watched with the cron watcher's mtime-poll pattern. Nil-safe: no
	// home configured means an empty list and no watcher.
	themes *themeRegistry
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
	routeRoots    map[string]bool
	skills        core.SkillsProvider
	cron          core.CronProvider
	logger        core.LoggerProvider
	notifications core.NotificationsProvider
	verdicts      core.VerdictsProvider
	trust         core.TrustProvider
	// commands + commandHandler are the extension command registry
	// (ctx.Commands, ctx.CommandHandler) - the palette's dispatch
	// surface reads them per request (card t_07aed3f9).
	commands       []core.ExtensionCommand
	commandHandler func(ctx context.Context, name, args string) (core.CommandResult, error)
}

// Accessors, one per seam. All nil-safe: the nil seam is the contract
// the handlers degrade through (501 / empty list / context-free run).

func (s *Server) Skills() core.SkillsProvider {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	return s.seams.skills
}
func (s *Server) Cron() core.CronProvider {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	return s.seams.cron
}
func (s *Server) Logger() core.LoggerProvider {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	return s.seams.logger
}
func (s *Server) Notifications() core.NotificationsProvider {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	return s.seams.notifications
}
func (s *Server) Verdicts() core.VerdictsProvider {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	return s.seams.verdicts
}
func (s *Server) Trust() core.TrustProvider {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	return s.seams.trust
}

// rewireSeams re-reads the module seams from the live Context: the
// route surface above, the canvas panel registry, and the agent's tool
// providers. The channel calls it at mount (initial wiring, the same
// fields the old boot path set) and the host calls it through
// core.LiveSeams after every successful toggle - so a disabled module's
// tools, panels, and routes die without a restart and a re-enabled
// module's surface returns (E2 journal: the toggle machinery changed
// the Context; this re-reads it).
func (s *Server) rewireSeams(ctx *core.Context) error {
	modmux, roots := moduleRouter(ctx.ModuleRoutes)
	s.seams.mu.Lock()
	s.seams.modules = modmux
	s.seams.routeRoots = roots
	s.seams.skills = ctx.Skills
	s.seams.cron = ctx.Cron
	s.seams.logger = ctx.Logger
	s.seams.notifications = ctx.Notifications
	s.seams.verdicts = ctx.Verdicts
	s.seams.trust = ctx.Trust
	s.seams.commands = append([]core.ExtensionCommand(nil), ctx.Commands...)
	s.seams.commandHandler = ctx.CommandHandler
	s.seams.mu.Unlock()
	if s.agent != nil {
		s.agent.RefreshToolProviders(ctx)
		s.agent.RefreshDisabledTools(ctx)
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

// NewServer creates a Server. tools is the registry of executable tools
// the agent may call; a nil map uses the built-in registry. store is the
// optional session persistence; a nil store disables persistence and the
// session CRUD endpoints. settings is the optional config-knobs seam
// (/settings endpoints); a nil value answers 501 on those endpoints.
func NewServer(a *core.Agent, tools map[string]core.Tool, store core.StoreProvider, settings core.Settings, modules core.ModuleToggles, registries core.RegistryToggles) *Server {
	if tools == nil {
		tools = map[string]core.Tool{}
	}
	s := &Server{agent: a, tools: tools, store: store, settings: settings, modules: modules, registries: registries, ctxWindow: map[string]int{}, runTimeout: 30 * time.Minute, startedAt: time.Now(), themes: newThemeRegistry(resolveThemesDir()), runs: &runHub{}}
	// Chat panel capsule (chat-last 1/3): default-wired so a directly
	// constructed Server (tests, embedding) has the push hooks and the
	// panel even without the plugin's channel-start handoff.
	s.chat = newChatCapsule()
	s.chat.srv.Store(s)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/models", s.handleModels)
	mux.HandleFunc("/chat", s.handleChat)
	mux.HandleFunc("/attach-image", s.handleAttachImage)
	mux.HandleFunc("/sessions", s.handleSessions)
	mux.HandleFunc("/sessions/search", s.handleSessionSearch)
	mux.HandleFunc("/sessions/{id}/events", s.handleSessionEvents)
	mux.HandleFunc("/sessions/{id}/stop", s.handleSessionStop)
	mux.HandleFunc("/sessions/{id}/transcript", s.handleSessionTranscript)
	mux.HandleFunc("/sessions/{id}/variants", s.handleSessionVariants)
	mux.HandleFunc("/sessions/{id}/branch", s.handleSessionBranch)
	mux.HandleFunc("/sessions/{id}/export", s.handleSessionExport)
	mux.HandleFunc("/sessions/{id}", s.handleSessionByID)
	mux.HandleFunc("/files/browse", s.handleFilesBrowse)
	mux.HandleFunc("/files/read", s.handleFilesRead)
	// card t_a4504a25: the Files tab toolbar's write half - add file /
	// add folder. Same root jail as the GET half, POST-only bodies.
	mux.HandleFunc("POST /files/write", s.handleFilesWrite)
	mux.HandleFunc("POST /files/mkdir", s.handleFilesMkdir)
	mux.HandleFunc("/workspaces/browse", s.handleWorkspacesBrowse)
	mux.HandleFunc("/workspaces/files", s.handleWorkspaceFiles)
	mux.HandleFunc("/tools", s.handleTools)
	mux.HandleFunc("/commands", s.handleCommands)
	mux.HandleFunc("/commands/dispatch", s.handleCommandDispatch)
	mux.HandleFunc("/extensions", s.handleExtensions)
	// v0.12 E2: the module toggle surface - registry introspection and
	// the live toggle (Q7), rejections visible through the refusal
	// ledger (Q12). Handler family lives in server_modules.go.
	mux.HandleFunc("GET /modules", s.handleModules)
	mux.HandleFunc("PUT /modules/{name}", s.handleModuleToggle)
	// v0.14.1: the registry-browser toggle surface - the skills/tools/
	// agents namespaces of the config.yaml disabled: fold. Handler
	// family lives in server_registry.go.
	mux.HandleFunc("GET /disabled", s.handleDisabled)
	mux.HandleFunc("PUT /disabled/skills", s.handleDisabledSkills)
	mux.HandleFunc("PUT /disabled/tools", s.handleDisabledTools)
	mux.HandleFunc("PUT /disabled/agents", s.handleDisabledAgents)
	mux.HandleFunc("/cron/jobs", s.handleCronJobs)
	mux.HandleFunc("/skills", s.handleSkills)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/settings", s.handleSettings)
	mux.HandleFunc("/themes", s.handleThemes)
	mux.HandleFunc("/insights", s.handleInsights)
	// card t_5d9032d3: the finished-things surface. Handlers live in
	// server_notify.go; /notifications/events is the live toast stream.
	mux.HandleFunc("/notifications", s.handleNotifications)
	mux.HandleFunc("/notifications/read", s.handleNotificationsRead)
	mux.HandleFunc("/notifications/events", s.handleNotificationEvents)
	// card t_2ecf9c51: the verdict-cards answer surface. /verdicts is
	// the cross-session listing (the bell count); /sessions/{id}/verdicts
	// lists one session and accepts the answer POST. The card itself
	// renders live from the tool_call / tool_result SSE frames.
	mux.HandleFunc("/verdicts", s.handleVerdicts)
	mux.HandleFunc("/sessions/{id}/verdicts", s.handleVerdicts)
	// card t_7e2b7002 (deferred mount, review pass): the multi-agent
	// REST surface. Handlers live in server_agents.go.
	mux.HandleFunc("/agents", s.handleAgents)
	mux.HandleFunc("/agents/{name}", s.handleAgent)
	mux.HandleFunc("/agents/{name}/sessions", s.handleAgentSessions)
	// card t_e143f41f (arc 3b): the canvas transport. GET /ui/canvas is
	// the load-time snapshot catch-up; POST /ui/actions is the web half
	// of the one action pipe. Handlers live in server_canvas.go; the
	// ViewModelDelta frames ride the existing run streams (no routes
	// here for them - they are event types, not endpoints).
	mux.HandleFunc("/ui/canvas", s.handleCanvas)
	mux.HandleFunc("/ui/actions", s.handleUIActions)
	// SPA: the built webui is embedded in the binary and served at /.
	// Exact patterns above win over the catch-all, so the API routes keep
	// working; the catch-all falls back to index.html for non-file paths
	// (history-mode routes like /chat/<id>) and 404s for asset misses.
	// Sync the build with engine/webui/scripts/build.sh before `go build`.
	// Module REST (phase F): the registered module routes ride the
	// live Context through the seamSet; requests that reach here fell
	// through every fixed engine pattern, so the dispatch is: a
	// module-route root consults the capsule mux (missing pattern
	// answers 404), anything else serves the SPA shell.
	mux.HandleFunc("/", s.handleModuleOrSPA)
	s.httpSrv = &http.Server{Handler: mux}
	return s
}

// spaHandler serves the embedded SPA with a history-mode fallback. A
// request path that maps to a real file is served as-is; a miss under an
// asset path (or any dotted path) 404s; anything else is a client route
// (e.g. /chat/<id>) and serves index.html so a reload with a deep URL
// boots the SPA shell.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.Trim(r.URL.Path, "/")
		if name != "" {
			if _, err := fs.Stat(fsys, name); err != nil {
				// asset-like miss: /_nuxt/** explicitly, plus any dotted
				// path (favicon.ico, robots.txt, *.js) - the shell is for
				// route paths, never for missing files
				assetLike := strings.HasPrefix(name, "_nuxt/") ||
					strings.Contains(path.Base(name), ".")
				if assetLike {
					fileServer.ServeHTTP(w, r)
					return
				}
				b, err := fs.ReadFile(fsys, "index.html")
				if err != nil {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write(b) //nolint:errcheck // best-effort static response
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

// Handler returns the HTTP handler, for tests and embedding.
func (s *Server) Handler() http.Handler {
	return s.httpSrv.Handler
}

// Serve runs the HTTP server on addr until ctx is cancelled, then shuts
// down gracefully. It returns the ListenAndServe error, or nil on a
// clean shutdown. Signal wiring (SIGINT/SIGTERM) lives in cmd/omega,
// which passes a signal-derived context here.
func (s *Server) Serve(ctx context.Context, addr string) error {
	s.httpSrv.Addr = addr
	// hot-reload: the themes folder watcher dies with the server ctx.
	go s.themes.watch(ctx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpSrv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.httpSrv.Shutdown(shutdownCtx)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	out := map[string]any{
		"status":         "ok",
		"uptime_seconds": int(time.Since(s.startedAt).Seconds()),
	}
	// version rides along when the host supplied one
	if s.version != "" {
		out["version"] = s.version
	}
	json.NewEncoder(w).Encode(out)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	models, err := s.agent.ListModels()
	if err != nil {
		http.Error(w, "list models: "+err.Error(), http.StatusInternalServerError)
		return
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
	json.NewEncoder(w).Encode(map[string]any{
		"current":        name,
		"models":         models,
		"context_window": window,
	})
}

// chatRequest is the /chat body. tools is an optional list of tool names
// to enable for this run; empty enables the full server registry.
// session_id, when set, persists the conversation to that session; when
// empty on a server WITH a store, the request auto-creates a session and
// persists normally (Endo 2026-10-18: /chat is always durable - the new
// id rides the first `session` SSE frame). Without a store the run stays
// ephemeral. model, when set, serves this run on that model - the
// default stays untouched, so concurrent chats keep their own model.
// thinking_level indexes the shared ai.ThinkingLevels ladder (0 = none
// = no thinking parameter sent = provider default); unset means
// provider default.
type chatRequest struct {
	Messages      []json.RawMessage `json:"messages"`
	SessionID     string            `json:"session_id,omitempty"`
	Model         string            `json:"model,omitempty"`
	ThinkingLevel int               `json:"thinking_level,omitempty"`
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
	// it (retry/fork-of-user) - empty = derive (first assistant after a
	// user is the head; continuations / tool results chain off the
	// leaf). Unexported: never part of the request wire.
	branchParent string
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
	prov     ai.Provider
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

	// Workspace scope: a session rooted in a folder roots THIS run's
	// relative file operations there (the loop and the tools read the
	// scope off ctx) and serves the folder's trust-gated AGENTS.md
	// context instead of the process working directory's. Non-tty
	// resolution skips an untrusted folder's context with a stderr
	// note - the same gate the host applies at boot.
	req.workspace = sess.Workspace
	if sess.Workspace != "" && s.Trust() != nil {
		req.workspacePromptContext = s.Trust().ResolveContext(sess.Workspace, false, false, false)
	}

	// @-mention expansion (card t_07aed3f9): the new input is expanded
	// BEFORE history assembly, so the loop's sink persists the expanded
	// prompt - the same truth the TUI's submit path held. The session's
	// workspace roots relative paths.
	incoming = s.expandAtMentions(incoming, req.workspace)
	messages, err := s.sessionHistory(ctx, req, incoming)
	if err != nil {
		s.runs.finish(run)
		return nil, err
	}

	// Model pin: a wire model WINS and sticks (the picker's choice is
	// what the session remembers from here on, card t_9de5b009); no
	// wire model serves the session's remembered one. The write is
	// best-effort - a failed pin leaves the send on the request model.
	if sessionKnown {
		if req.Model != "" {
			if req.Model != sess.Model {
				if scx, ok := s.store.(core.SessionContextStore); ok {
					_ = scx.SetSessionModel(ctx, req.SessionID, req.Model)
				}
			}
		} else if sess.Model != "" {
			req.Model = sess.Model
		}
	}

	// Per-run provider pin: model override + thinking level (shared
	// with the legacy inline path).
	prov, pinErr := s.pinProvider(req)
	if pinErr != nil {
		s.runs.finish(run)
		return nil, pinErr
	}

	// Provisional title: the cheap state-machine check rides the same
	// row (no second GetSession), and the title call fires detached
	// before any streaming, so the sidebar leaves the raw session id
	// while the model is still thinking. maybeTitleSession re-reads
	// the session and enforces the state machine before writing.
	if sessionKnown && sess.TitleState == 0 {
		go s.maybeTitleSession(context.WithoutCancel(ctx), prov, *req, "", false)
	}

	tools := s.tools
	if req.Ephemeral {
		// No-persist contract: the memory tool never runs in an
		// ephemeral session (nothing it writes outlives the session).
		// The run context carries the deny too, so extension-merged
		// providers cannot reintroduce it (the loop filters post-merge).
		tools = withoutMemory(tools)
	}
	return &chatPrepare{run: run, prov: prov, messages: messages, tools: tools}, nil
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
			dir, err := resolveWorkspaceDir(req.Workspace)
			if err != nil {
				http.Error(w, "workspace: "+err.Error(), http.StatusBadRequest)
				return
			}
			if _, ok := s.store.(core.SessionContextStore); !ok {
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
			scx := s.store.(core.SessionContextStore)
			if err := scx.SetSessionWorkspace(r.Context(), id, wsDir); err != nil {
				http.Error(w, "set session workspace: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		req.SessionID = id
		autoCreated = true
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
	// prepareRun path enforces.
	if s.store == nil && !req.Ephemeral {
		if isEphemeralID(req.SessionID) {
			http.Error(w, "ephemeral session id requires the ephemeral flag", http.StatusBadRequest)
			return
		}
		messageIn := s.expandAtMentions(messages, "")
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

// pinProvider pins the per-run provider copy: base with a model
// override when the request carries one, plus a thinking level from
// the shared ladder. thinking_level 0 ("none") is skipped: no
// parameter sent, the provider keeps its default. The copy rides
// LoopOptions - no shared state mutated. Shared by the prepareRun
// detach path and the legacy inline path (chat-last 1/3).
func (s *Server) pinProvider(req *chatRequest) (ai.Provider, *runError) {
	prov := s.agent.Provider()
	if req.Model != "" {
		prov = prov.WithModel(req.Model)
	}
	if req.ThinkingLevel != 0 {
		if req.ThinkingLevel < 0 || req.ThinkingLevel >= len(ai.ThinkingLevels) {
			return nil, &runError{http.StatusBadRequest, fmt.Sprintf("invalid thinking_level: %d (0..%d)",
				req.ThinkingLevel, len(ai.ThinkingLevels)-1)}
		}
		prov = prov.WithThinkingLevel(ai.ThinkingLevels[req.ThinkingLevel])
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
func (s *Server) streamAgentEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, prov ai.Provider, messages []core.Message, tools map[string]core.Tool, req chatRequest) ([]assistantRecord, streamUsage, *core.LastRunStats) {
	// tools run in THIS session's context: board tools read the session
	// id from ctx to auto-link tasks created mid-chat.
	ctx = agent.WithSessionID(ctx, req.SessionID)
	// Session workspace (card t_9de5b009): relative file operations
	// root at the session's folder and the prompt carries the folder's
	// trust-gated AGENTS.md context (resolved in prepareRun). Empty =
	// no workspace - today's behavior, the ctx override is absent.
	ctx = core.WithRunWorkspace(ctx, req.workspace, req.workspacePromptContext)
	// Verdict cards v1: the no-store legacy path is still an interactive
	// turn - verdict.request may issue from here.
	ctx = core.WithRunOrigin(ctx, core.RunOriginInteractive)
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
	for event := range s.agent.RunWithProvider(ctx, prov, messages, tools) {
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
	// StreamEvent) - the TUI persists it the same way
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
func (s *Server) persistAssistant(ctx context.Context, prov ai.Provider, req chatRequest, recs []assistantRecord, usage streamUsage, last *core.LastRunStats) error {
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
func (s *Server) maybeTitleSession(ctx context.Context, prov ai.Provider, req chatRequest, lastAssistant string, final bool) {
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

	// the title turn reuses the FULL tool table (nil Tools falls back
	// to the registered set) and the incoming user content can invite
	// tool use. A title turn must never hard-pause on the card (no
	// session, nobody answering): deny the verdict tool by name - the
	// registered identity is modules/verdict's namespace pair
	// (verdict.request, Q10, v0.12 phase C2); the loop filters it
	// after every provider merge, the same mechanism the ephemeral
	// path denies memory.entry with.
	ctx = agent.WithDeniedTools(ctx, "verdict.request")

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

// sessionPayload is one /sessions list row: the session row (which
// carries the pinned flag, the persisted token totals, and the
// last-read stamp) plus the read-time counts, the last recorded model,
// the read-time unread state, and whether the session has a live run
// right now (the sidebar's streaming pulse after a page refresh - the
// in-memory busy map no longer survives the reload). The running flag
// is the backend's single truth for "is this session streaming" (it
// derives from the runHub, never from a client's memory of its own
// stream), so it is ALWAYS present on the row - true and false alike -
// and every tab reconciles its pulse against it.
type sessionPayload struct {
	core.Session
	MessageCount int    `json:"message_count"`
	ToolCount    int    `json:"tool_count"`
	Model        string `json:"model,omitempty"`
	Unread       bool   `json:"unread"`
	Running      bool   `json:"running"`
}

// handleSessions serves GET /sessions (list) and POST /sessions (create).
// It returns 501 when the server has no store.
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "session store not configured", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodGet:
		sessions, err := s.store.ListSessions(r.Context())
		if err != nil {
			http.Error(w, "list sessions: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Archived filter: default excludes archived sessions from the
		// list; archived=include returns them (with the rest). The flag
		// is a user-list concern, so the filter lives at the read.
		includeArchived := r.URL.Query().Get("archived") == "include"
		// ponytail: counts are computed at read time from SQL COUNT, so
		// the list stays accurate with no maintained counters.
		counts, err := s.store.SessionCounts(r.Context())
		if err != nil {
			http.Error(w, "session counts: "+err.Error(), http.StatusInternalServerError)
			return
		}
		models, err := s.store.SessionModels(r.Context())
		if err != nil {
			http.Error(w, "session models: "+err.Error(), http.StatusInternalServerError)
			return
		}
		out := make([]sessionPayload, 0, len(sessions))
		for _, sess := range sessions {
			if sess.Archived && !includeArchived {
				continue
			}
			payload := sessionPayload{Session: sess}
			if c, ok := counts[sess.ID]; ok {
				payload.MessageCount = c.MessageCount
				payload.ToolCount = c.ToolCount
			}
			// The pinned model wins; the TUI's model_change trail is the
			// fallback for sessions that never carried a pin (card t_9de5b009).
			payload.Model = models[sess.ID]
			if sess.Model != "" {
				payload.Model = sess.Model
			}
			// computed at read time from the row itself - no counters,
			// no schema writes (same pattern as the counts)
			payload.Unread = agent.SessionUnread(sess)
			payload.Running = s.runs.active(sess.ID)
			out = append(out, payload)
		}
		writeJSON(w, http.StatusOK, out)
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
			dir, err := resolveWorkspaceDir(req.Workspace)
			if err != nil {
				http.Error(w, "workspace: "+err.Error(), http.StatusBadRequest)
				return
			}
			if _, ok := s.store.(core.SessionContextStore); !ok {
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
			scx := s.store.(core.SessionContextStore)
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

// sessionSearchHit is one /sessions/search row: the matching session
// id plus the hit (a title hit carries the label with titleMatch set;
// a content hit carries a snippet stub - a later card renders the
// context).
type sessionSearchHit struct {
	ID         string `json:"id"`
	Snippet    string `json:"snippet,omitempty"`
	TitleMatch bool   `json:"titleMatch,omitempty"`
}

// handleSessionSearch serves GET /sessions/search?q=<term>: the store's
// unified FTS5 search (SearchSessions) - one endpoint, one result
// shape, session labels AND message content in the same index pass,
// deduped to one hit per session with title hits ranked first. It
// returns 501 when the server has no store, 200 [] on a missing/empty
// q (nothing to search), and 400 when FTS5 rejects the query syntax.
func (s *Server) handleSessionSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "session store not configured", http.StatusNotImplemented)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []sessionSearchHit{})
		return
	}
	results, err := s.store.SearchSessions(r.Context(), q)
	if err != nil {
		// The only input-dependent failure mode of this read-only
		// query is the query itself; the sqlite driver surfaces FTS5
		// syntax errors as plain "SQL logic error" strings, so the
		// bad call gets 400 (a real store fault trips on the /sessions
		// list too, not just here).
		http.Error(w, "search: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Archived filter: default excludes hits whose session is archived
	// (the sidebar's toggle drives the same archived=include param the
	// list takes); archived=include keeps them. One read of the session
	// list builds the exclusion set - the search stays a pure FTS
	// query, the flag is a user-list concern applied at read time.
	if r.URL.Query().Get("archived") != "include" {
		sessions, err := s.store.ListSessions(r.Context())
		if err != nil {
			http.Error(w, "list sessions: "+err.Error(), http.StatusInternalServerError)
			return
		}
		archived := make(map[string]bool, len(sessions))
		for _, sess := range sessions {
			archived[sess.ID] = sess.Archived
		}
		results = slices.DeleteFunc(results, func(res core.SearchResult) bool {
			return archived[res.SessionID]
		})
	}
	out := make([]sessionSearchHit, 0, len(results))
	for _, res := range results {
		out = append(out, sessionSearchHit{ID: res.SessionID, Snippet: res.Snippet, TitleMatch: res.Title})
	}
	writeJSON(w, http.StatusOK, out)
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
				dir, err := resolveWorkspaceDir(*body.Workspace)
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
			scx, ok := s.store.(core.SessionContextStore)
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
// (modules/tools/ui/ToolsList.vue) renders this. Descriptions only -
// parameter maps would only bloat the listing the UI shows.
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	inventory := s.agent.Tools()
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
		row := toolListing{Name: name, Description: t.Description}
		for _, a := range t.Actions {
			row.Actions = append(row.Actions, toolActionListing{Name: a.Name, Description: a.Description})
		}
		if t.AliasOf != "" {
			continue // compat shim, not inventory
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
