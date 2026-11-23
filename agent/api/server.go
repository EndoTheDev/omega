package api

// Server base for the channel's HTTP surface. Import law: this package may import core,
// agent and llm - never harness.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"slices"
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
	// runs is a per-PROCESS hub, not per-bind: every multi-bind mirror shares it
	// (HTTPChannel.Start swaps it in), or a run started through the action pipe on one
	// bind is invisible to the transcript/events reads on the others.
	runs *RunHub // detached /chat runs by session id (reattach surface)
	// sockets is the push channel's fan-out. eph is the no-persist (ephemeral) session
	// registry: `eph-` ids live here, never in the store - invisible to every
	// store-derived surface, gone at restart.
	eph     ephRegistry
	store   agent.StoreProvider
	sockets *SocketHub
	verdictMu  sync.Mutex
	verdictSrc verdictCardSource
	settings   core.Settings
	// modules is the optional module-toggle seam: the /modules endpoints serve
	// registry introspection + live toggles through it; a nil value answers 501.
	modules core.ModuleToggles
	// registries is the optional registry-browser toggle seam: the /disabled
	// endpoints serve and write the config.yaml `disabled:` fold's skills/tools/agents
	// namespaces through it; a nil value answers 501.
	registries core.RegistryToggles
	seams seamSet
	// canvas is the runtime behind the canvas transport. Nil means nothing
	// mounted - /ui/canvas serves [] and /ui/actions answers the unknown-module 404.
	canvas *agent.Canvas
	chat *ChatCapsule
	runTimeout time.Duration
	// agentRuns is the host's per-agent run factory. Nil = the daemon's agent serves every
	// session (tests, embedding).
	agentRuns AgentRunFactory

	wakes       *wakeHub
	// steers is the session-held live-turn input queue (steer.go): rows outlive the run
	// that queued them and drain at its next batch boundary.
	steers      *steerHub
	bootCtx     context.Context
	bootStop    context.CancelFunc
	yieldLedger agent.YieldLedger
	wakeMu       sync.Mutex
	wakeLaunches map[string]int
	// ephLedger is the `eph-` lane of the pending-calls table. RESTART EATS ITS PENDING
	// RESULTS - the eph mode's own contract (the session dies with the process; a pending
	// result cannot outlive its home), documented at sessionTypedLedger.
	ephLedger *agent.MemYieldLedger

	titleModel string

	defaultWorkspace string

	wsMu sync.Mutex

	ctxMu     sync.Mutex
	ctxWindow map[string]int // model name -> context window (probed once)

	mux *http.ServeMux
	// serve is the serving handler Handler returns. Built once in NewServer; registration
	// on mux keeps working - the rescue reads the live seamSet, never a stale route table.
	serve http.Handler
}

// seamSet is the optional-module surface behind the Server's routes.
type seamSet struct {
	mu sync.RWMutex
	// modules is the module-rest mux rebuilt from ctx.ModuleRoutes at every rewire. A nil
	// mux sends every request to the SPA fallback - no module routes, no module paths.
	modules *http.ServeMux
	routeRoots map[string]bool
	// moduleLiterals is the exact-match lane of the module REST set (the same. ServeMux
	// has no precedence hook across muxes, so the rescue layer reads.
	moduleLiterals map[string]http.HandlerFunc
	logger         agent.LoggerProvider
	// ctx is the live Context (the seam constitution): the capsule-facing routes
	// resolve their services per request from the universal registry - a live toggle
	// swaps the pointer under their feet. Nil-safe: a directly constructed Server
	// (tests) with no Context rewired answers the nil-seam degradation (501 / empty).
	ctx *core.Context
	commands       []core.ExtensionCommand
	commandHandler func(ctx context.Context, name, args string) (core.CommandResult, error)
}

// LiveServices returns the live universal seam registry. Nil-safe.
func (s *Server) LiveServices() *core.Services {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	if s.seams.ctx == nil {
		return nil
	}
	return s.seams.ctx.Services()
}

// Consumers' local faces.

// skillFaces is the skill inventory face the content/atmention consumers hold.
type skillFaces interface {
	SkillsEntries() (index map[string]string, contents map[string]string, err error)
	SkillRead(name string) (description, content string, err error)
}

// contextResolver is the workspace prompt-context face the session-send path holds.
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

// rewireSeams re-reads the module seams from the live Context.
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
	s.subscribeVerdictCards(ctx)
	if s.agent != nil {
		s.agent.RefreshToolProviders(ctx)
		s.agent.RefreshDisabledTools(ctx)
		s.agent.RefreshApprovals(ctx)
		s.agent.RefreshExtensionInfos(ctx)
	}
	if s.canvas != nil {
		return s.canvas.Resync(ctx)
	}
	return nil
}

// NewServer creates the base server.
func NewServer(a *agent.Agent, tools map[string]core.Tool, store agent.StoreProvider, settings core.Settings, modules core.ModuleToggles, registries core.RegistryToggles) *Server {
	if tools == nil {
		tools = map[string]core.Tool{}
	}
	s := &Server{agent: a, tools: tools, store: store, settings: settings, modules: modules, registries: registries, ctxWindow: map[string]int{}, runTimeout: 30 * time.Minute, runs: &RunHub{}, sockets: NewSocketHub(), mux: http.NewServeMux()}
	s.chat = NewChatCapsule()
	s.chat.srv.Store(s)
	// The yield harness: the pending-calls table is session-typed. A nil agent (some tests
	// construct a bare Server) skips the harness entirely.
	s.bootCtx, s.bootStop = context.WithCancel(context.Background())
	s.wakes = newWakeHub()
	// The steer queue is session-held and process-local: rows outlive the run that queued
	// them (a crashed turn eats no human words), never the process.
	s.steers = newSteerHub()
	// Session-typed pending-calls table: the `eph-` lane lives in memory beside the
	// ephRegistry - an ephemeral session's rows are process-local (restart eats its
	// pending results: the mode's own contract, see sessionTypedLedger), and nothing it
	// owns may land in the store.
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
		s.recoverPendingCalls()
	}
	return s
}

// Mux exposes the shared route table so the shell can register its composition routes.
func (s *Server) Mux() *http.ServeMux { return s.mux }

// Handler returns the HTTP handler, for tests and embedding.
func (s *Server) Handler() http.Handler { return s.serve }

// serveModuleRescue is the outer serving layer.
func (s *Server) serveModuleRescue(w http.ResponseWriter, r *http.Request) {
	if h, ok := s.moduleLiteralLookup(r.Method, r.URL.Path); ok {
		h(w, r)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// moduleLiteralLookup exact-matches the live module route set under one read lock.
func (s *Server) moduleLiteralLookup(method, path string) (http.HandlerFunc, bool) {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	if h, ok := s.seams.moduleLiterals[method+" "+path]; ok {
		return h, true
	}
	h, ok := s.seams.moduleLiterals["ANY "+path]
	return h, ok
}

// moduleLiterals builds the exact-match lane from the live module routes. Same-path
// re-registration lands the same handler the capsules registered; a nil map is a legal
// empty lane.
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

// registerRoutes mounts the base endpoint set onto the shared mux. The shell's own
// routes (health, themes, module toggle, disabled fold, inspector reads, SPA catch-all)
// stay in api's NewServer against the same mux.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/models", s.handleModels)
	mux.HandleFunc("/chat", s.handleChat)
	mux.HandleFunc("/attach-image", s.handleAttachImage)
	mux.HandleFunc("/attach-file", s.handleAttachFile)
	// The base keeps the chat floor: POST /sessions (session id creation),
	// /sessions/{id} (detail/context/delete - pattern-atomic, the chat page calls its
	// PATCH and ephemeral DELETE live), and the live-run reads
	// (events/stop/transcript/variants) + verdicts.
	mux.HandleFunc("POST /sessions", s.handleSessionCreate)
	mux.HandleFunc("/sessions/{id}/events", s.handleSessionEvents)
	mux.HandleFunc("/sessions/{id}/stop", s.handleSessionStop)
	mux.HandleFunc("/sessions/{id}/transcript", s.handleSessionTranscript)
	mux.HandleFunc("/sessions/{id}/variants", s.handleSessionVariants)
	mux.HandleFunc("/sessions/{id}/queue", s.handleSessionQueue)
	mux.HandleFunc("/sessions/{id}/queue/{row}", s.handleSessionQueueRow)
	mux.HandleFunc("/sessions/{id}/new", s.handleSessionNew)
	mux.HandleFunc("/sessions/{id}", s.handleSessionByID)
	mux.HandleFunc("/tools", s.handleTools)
	mux.HandleFunc("/commands", s.handleCommands)
	mux.HandleFunc("/commands/dispatch", s.handleCommandDispatch)
	mux.HandleFunc("/settings", s.handleSettings)
	mux.HandleFunc("/settings/raw", s.handleSettingsRaw)
	mux.HandleFunc("/settings/reset", s.handleSettingsReset)
	mux.HandleFunc("/ui/canvas", s.handleCanvas)
	mux.HandleFunc("/ui/actions", s.handleUIActions)
}

// Active reports whether the session has a live run right now.
func (s *Server) Active(id string) bool { return s.runs.active(id) }

// carve seams: the shell reaches the base state it must wire, through these.

// Agent exposes the agent handle.
func (s *Server) Agent() *agent.Agent { return s.agent }

// Store exposes the store seam. Nil when no store module is mounted - the health body
// then carries no database fact.
func (s *Server) Store() agent.StoreProvider { return s.store }

// ModuleToggles exposes the module-toggle seam (GET/PUT /modules).
func (s *Server) ModuleToggles() core.ModuleToggles { return s.modules }

// Registries exposes the registry-toggle seam (GET/PUT /disabled/.).
func (s *Server) Registries() core.RegistryToggles { return s.registries }

// ModuleMux returns the capsule mux and its route roots read under one lock.
func (s *Server) ModuleMux() (*http.ServeMux, map[string]bool) {
	s.seams.mu.RLock()
	defer s.seams.mu.RUnlock()
	return s.seams.modules, s.seams.routeRoots
}

// Canvas exposes the canvas runtime (the boot-wiring test reads it).
func (s *Server) Canvas() *agent.Canvas { return s.canvas }

// ActiveRun reports the run hub's active state for one session id.
func (s *Server) ActiveRun(id string) bool { return s.runs.active(id) }

// BindInspectSeams installs the optional provider surfaces the shell's inspector reads.
// The capsule services (cron, skills) ride the universal registry instead - the caller
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

// BindSocketHub swaps the per-process push hub in (HTTPChannel.wire).
func (s *Server) BindSocketHub(h *SocketHub) { s.sockets = h }

// Sockets exposes the push channel's hub.
func (s *Server) Sockets() *SocketHub { return s.sockets }

// BindChatCapsule binds the plugin's registered chat capsule.
func (s *Server) BindChatCapsule(c *ChatCapsule) { s.chat = c }

// BindCanvas binds the canvas runtime built at channel start.
func (s *Server) BindCanvas(c *agent.Canvas) { s.canvas = c }

// RegisterChatPanel appends the chat capsule to the context's panel providers.
func (s *Server) RegisterChatPanel(ctx *core.Context) { s.chat.registerAt(ctx) }

// SetChannelKnobs applies the channel config knobs (run wall-clock cap, title model).
func (s *Server) SetChannelKnobs(runTimeout time.Duration, titleModel string) {
	s.runTimeout = runTimeout
	s.titleModel = titleModel
}

// SetDefaultWorkspace applies the server.default_workspace knob. A stat failure refuses
// the knob: the previous value survives, so a transient stat failure (network drive
// hiccup, AV lock) during an unrelated save never silently re-roots workspace-less
// sessions' tool runs at the process cwd - the refusal is logged with the reason and
// the kept value.
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

// refuseWorkspace keeps the previous default workspace and logs why the submitted one.
// The logger seam may be nil (no logging capsule) - the value is intact either way, the
// log line is the audit trail.
func (s *Server) refuseWorkspace(dir, why string) {
	s.wsMu.Lock()
	prev := s.defaultWorkspace
	s.wsMu.Unlock()
	if l := s.Logger(); l != nil {
		l.Errorf("api: server.default_workspace %q refused (%s) - keeping %q", dir, why, prev)
	}
}

// SetAgentPrompt is the settings hot-apply hook for the live runtime knobs. The shell
// api.Server walks its mounted binds to call this; a nil agent is a no-op.
func (s *Server) SetAgentPrompt(prompt string, maxTurns int) {
	if s.agent == nil {
		return
	}
	s.agent.SetPromptCustom(prompt)
	s.agent.SetMaxTurns(maxTurns)
}

// WriteJSON is the exported seam for the shell's JSON responses.
func WriteJSON(w http.ResponseWriter, status int, v any) { writeJSON(w, status, v) }

// getOnly is the GET-only guard shared by the base handlers.
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
		// one provider probe per model per process. The probe runs on a copy so the base
		// provider is never mutated.
		info, _ := s.agent.Provider().WithModel(name).ModelInfo()
		window = info.ContextWindow
		s.ctxMu.Lock()
		s.ctxWindow[name] = window
		s.ctxMu.Unlock()
	}
	// ponytail: keypool/ollama-cloud providers cannot report a context window, so fall.
	if window <= 0 {
		window = 32768
	}
	w.Header().Set("Content-Type", "application/json")
	out := map[string]any{
		"current":        name,
		"models":         models,
		"context_window": window,
	}
	if len(caps) > 0 {
		out["capabilities"] = caps
	}
	// the GROUPED catalog - the chat picker renders one collapsible section per provider.
	// The default instance first, then every named instance (sorted, the config map is
	// unordered). Each section lists the endpoint's own /models answer; an instance that
	// fails to answer still carries the section with the error named - the picker shows
	// the failure inline, never a silent drop.
	if sections := s.modelSections(); len(sections) > 0 {
		out["providers"] = sections
	}
	json.NewEncoder(w).Encode(out)
}

// ModelSection is one picker section. A listing failure keeps the section alive with
// the error named - the picker's Refresh action re-fetches, never a silent drop of the
// whole provider.
type ModelSection struct {
	Name         string              `json:"name"`
	Models       []string            `json:"models"`
	Capabilities map[string][]string `json:"capabilities,omitempty"`
	Error        string              `json:"error,omitempty"`
}

// modelSectionsTimeout caps one endpoint's listing inside the grouped catalog.
var modelSectionsTimeout = 5 * time.Second

// listOneSection fetches one endpoint's catalog into its section - it
// never touches shared state, so a section fetched late or never
// (missed deadline) is safe to serve without it.
func listOneSection(name string, inst agent.Provider) ModelSection {
	sec := ModelSection{Name: name, Models: []string{}}
	// the same degradation agent.ListModelEntries applies.
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

// modelSections builds the grouped catalog: every provider INSTANCE, primary first.
// Each listing rides its own provider copy, so no shared state mutates; a listing error
// lands IN the section (the picker shows it), never a 500 of the whole payload.
func (s *Server) modelSections() []ModelSection {
	prov := s.agent.Provider()
	if prov == nil {
		return nil
	}
	type job struct {
		name string
		inst agent.Provider
	}
	var jobs []job
	if ir, ok := prov.(agent.ProviderInstances); ok {
		if listed, ok := prov.(agent.ProviderInstanceNames); ok {
			for _, n := range listed.InstanceNames() {
				if inst, ok2 := ir.Instance(n); ok2 {
					jobs = append(jobs, job{name: n, inst: inst})
				}
			}
		}
	}
	if len(jobs) == 0 || jobs[0].name != "default" {
		jobs = append([]job{{name: "default", inst: prov}}, jobs...)
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

// chatRequest is the /chat body. model, when set, serves this run on that model - the
// default stays untouched, so concurrent chats keep their own model.
type chatRequest struct {
	Messages  []json.RawMessage `json:"messages"`
	SessionID string            `json:"session_id,omitempty"`
	Model     string            `json:"model,omitempty"`
	// Provider selects the provider INSTANCE the run serves through. A name the config no
	// longer carries (the instance was removed) fails the run loudly (400, "not
	// configured") - never a silent fallback.
	Provider      string `json:"provider,omitempty"`
	ThinkingLevel int    `json:"thinking_level,omitempty"`
	// Retry re-runs the session's LAST user turn as a SIBLING VARIANT: the fresh
	// response's head parents to the same USER message the prior response answered, so the
	// store stays an append list with no duplicate user bubble while the tree gains a
	// sibling.
	Retry bool `json:"retry,omitempty"`
	Fork string `json:"fork,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	// branchParent pins the run head's parent when the channel computed it. Unexported:
	// never part of the request wire.
	branchParent string
	// wake marks a WAKE LAUNCH (async agent loop, agent/api/wakes.go). A wake launch takes
	// the same prepareRun pipe but never fires the title kick (it is agent-speech, not a
	// conversation start) and never resets the runaway guard.
	wake bool
	// branchFloor is the session's max store row id observed at run start, set alongside
	// branch.
	// Unexported: never part of the request wire.
	branchFloor int64
	// persistInput is the send's incoming messages. Unexported: never part of the request
	// wire.
	workspace string
	// workspacePromptContext is the trust-gated AGENTS.md context of the workspace.
	// Unexported: never part of the request wire.
	workspacePromptContext string
	persistInput           []core.Message
	// Ephemeral: the run is a NO-PERSIST chat. Retry/fork are refused (400): both replay
	// the persisted tree, which ephemeral sessions never have. The memory tool never runs
	// in an ephemeral session.
	Ephemeral bool `json:"ephemeral,omitempty"`
	// runAgent is the agent that serves THIS run when the session belongs to an agent. Nil
	// = the daemon's agent. Unexported: never part of the request wire.
	runAgent *agent.Agent
}

// assistantRecord accumulates one response segment of an assistant turn for session.
type assistantRecord struct {
	thinking  string
	toolCalls []core.ToolCall
	results   []core.ToolResult
	content   string
	thinkDur, respDur   time.Duration
	lastThink, lastResp time.Time
	openAt              time.Time
	// segDone marks the record's CURRENT segment as finished: a tool call followed text
	// this record already accumulated, so that text will never grow again (the next chunk
	// splits into a fresh record) and its phase window is closed. The record stays open -
	// it still takes the call's result and the split - but its thinking/answer rows are
	// settled history, not streaming ones, and must not ship a running bit while the tool
	// runs.
	segDone bool
	callStarts map[string]time.Time
}

// streamUsage totals the real provider-reported usage across every StreamEnd of one.
type streamUsage struct {
	prompt     int
	completion int
}

// newRunMeter starts the clock for one run's last-run stats.
func newRunMeter() *runMeter {
	return &runMeter{started: time.Now()}
}

// replayGuards validates the replay-turn request shapes (retry/fork).
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
	// Replay turns need the persisted tree; an ephemeral session never has one.
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

// chatPrepare is one prepared detached run.
type chatPrepare struct {
	run      *activeRun
	prov     agent.Provider
	messages []core.Message
	tools    map[string]core.Tool
	// queued holds the rows this launch folded into messages; the launcher ACKS them once
	// the run is launched (a prepare that never launches hands them back instead).
	queued []queuedInput
}

// prepareRun performs the shared pre-launch steps of a session-backed chat run.
func (s *Server) prepareRun(ctx context.Context, req *chatRequest, incoming []core.Message) (*chatPrepare, error) {
	if err := replayGuards(req, s.store != nil); err != nil {
		return nil, err
	}

	// Run-slot acquisition comes BEFORE any persistence: a second /chat for a session with
	// a live run is a 409 that must not leave the incoming user message appended - the
	// next successful send would double it.
	run, live := s.runs.start(req.SessionID)
	if !live {
		return nil, &runError{http.StatusConflict, "session already running"}
	}
	// A human send re-arms the session's stop latch. A WAKE launch never re-arms: a
	// cancelled home stays cancelled until a human speaks, so a late settlement cannot
	// restart the conversation the stop ended.
	if !req.wake {
		s.runs.rearm(req.SessionID)
	}
	// Live-run truth on the push channel: the slot was just taken - human sends and wake
	// launches (launchWake calls prepareRun too) both land here, so the runs topic's
	// "active" edge has exactly one source.
	s.publishRunState(req.SessionID, true)

	// Queued rows deliver FIRST on this turn: they were typed before the send that starts
	// it, and a crashed turn must not reorder the conversation.
	var queuedLead []queuedInput
	if !req.wake && !req.Retry && req.Fork == "" {
		if lead, batch := s.takeQueuedInto(req.SessionID); len(lead) > 0 {
			incoming = append(lead, incoming...)
			queuedLead = batch
		}
	}

	var sess core.Session
	sessionKnown := false
	if s.store != nil && req.SessionID != "" && !req.Ephemeral {
		if v, err := s.store.GetSession(ctx, req.SessionID); err == nil {
			sess, sessionKnown = v, true
		}
	}

	// Per-agent run config: a session owned by an agent home runs on THAT home's agent.
	// The host builds (and caches) it; a failure here is a loud 500, never a silent run as
	// the wrong agent.
	if ag, aerr := s.runAgentFor(ctx, req.SessionID); aerr != nil {
		s.prepareFailed(run, req.SessionID)
		return nil, &runError{http.StatusInternalServerError, aerr.Error()}
	} else {
		req.runAgent = ag
	}

	// Per-agent run config OWNS provider and model: the home's config layer names both,
	// and every send from the webui carries the composer's model - a chat tab with no
	// pinned model inherits the DAEMON's default, so an agent-owned session would answer
	// on a model its home never configured (or fail on a name its endpoint does not
	// serve).
	agentOwned := req.runAgent != nil
	if agentOwned {
		req.Model, req.Provider = "", ""
	}

	// Workspace scope: a session rooted in a folder roots THIS run's relative file. A
	// session without a pin falls back to the default_workspace knob: the tools run
	// against a DEFINED directory, never an implicit cwd.
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

	// @-mention expansion: the new input is expanded BEFORE history assembly,.
	incoming = s.expandAtMentions(incoming, req.workspace)
	messages, err := s.sessionHistory(ctx, req, incoming)
	if err != nil {
		s.prepareFailed(run, req.SessionID)
		return nil, err
	}
	// An ephemeral session has no store row: the registry IS the conversation, so the taken
	// rows are committed the moment history adopts them - handing them back now would deliver
	// them twice.
	if req.Ephemeral {
		s.ackQueued(req.SessionID, queuedLead)
	}

	// Instance pin: a wire provider name WINS and sticks on the session.
	// No wire name serves the session's pinned one - which is exactly how a removed
	// instance strands ITS sessions: the next send resolves the dead name and the run
	// fails loudly (pinProvider's 400), never a silent fallback to the default instance.
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

	// Model pin: a wire model WINS and sticks.
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

	// the model picker addresses an instance through the slot grammar. A slot that names a
	// non-default instance can only succeed or 400 at pinProvider, never a silent fallback
	// - nothing more to say here.
	s.resolveSlotModel(req, wireProvider)
	if sessionKnown && !agentOwned {
		if scx, ok := s.store.(agent.SessionContextStore); ok {
			switch {
			case req.Provider != "" && req.Provider != sess.ProviderName:
				_ = scx.SetSessionProvider(ctx, req.SessionID, req.Provider)
			case req.Provider == "" && sess.ProviderName != "":
				_ = scx.SetSessionProvider(ctx, req.SessionID, "")
			}
		}
	}

	prov, pinErr := s.pinProvider(req)
	if pinErr != nil {
		s.prepareFailed(run, req.SessionID)
		return nil, pinErr
	}

	// Provisional title: the cheap state-machine check rides the same row (no second. A
	// WAKE launch never fires it (agent-speech, not a conversation start).
	if sessionKnown && sess.TitleState == 0 && !req.wake {
		go s.maybeTitleSession(context.WithoutCancel(ctx), prov, *req, "", false)
	}

	tools := s.tools
	if req.Ephemeral {
		// No-persist contract: the memory tools never run in an ephemeral session.
		// The run context carries the deny too, so extension-merged providers cannot
		// reintroduce them (the loop filters post-merge).
		tools = withoutMemoryTools(tools)
	}
	if req.wake {
		// Launch record: the slot is taken, the run is prepared, and launchWake's launchRun.
		s.wakeMu.Lock()
		if s.wakeLaunches == nil {
			s.wakeLaunches = map[string]int{}
		}
		s.wakeLaunches[req.SessionID]++
		s.wakeMu.Unlock()
	}
	return &chatPrepare{run: run, prov: prov, messages: messages, tools: tools, queued: queuedLead}, nil
}

// wakeLaunchCount reports how many wake runs this server has launched for the home - an
// event record, never a sampled state. A wake run's live slot on the boot-recovery path
// measures 2-4ms (100 boots, loaded machine), shorter than the 10ms poll interval the
// tests used to use and far shorter than the 30s bound the wait had: polling
// s.runs.active for the launch therefore missed the run entirely whenever the test
// goroutine was descheduled after NewServer returned, and the deadline - not the launch
// - decided the verdict.
func (s *Server) wakeLaunchCount(sessionID string) int {
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	return s.wakeLaunches[sessionID]
}

// runMeter tallies the chunks and streaming wall clock one run produced.
type runMeter struct {
	started   time.Time
	lastChunk time.Time
	chunks    int
}

// tick counts one streamed chunk.
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
	// Non-image attachment references: validated BEFORE any session row is written, so a
	// bad reference never leaves an empty auto-created session behind.
	if err := resolveAttachments(messages, ""); err != nil {
		http.Error(w, "attachment: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Replay guards run BEFORE auto-create.
	if err := replayGuards(&req, s.store != nil); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The draft workspace intent pins ONLY the bare create.
	if req.Workspace != "" &&
		(req.SessionID != "" || req.Ephemeral || req.Fork != "" || req.Retry || s.store == nil) {
		http.Error(w, "workspace rides session creation only", http.StatusBadRequest)
		return
	}
	autoCreated := false
	if req.Ephemeral && req.SessionID == "" {
		id := mintEphemeralID()
		s.eph.create(id)
		req.SessionID = id
		autoCreated = true
	} else if req.Ephemeral && req.SessionID != "" {
		// Continue-on-id: the client reattaches to server memory - an unknown
		// (expired/restarted) ephemeral id is a 404, never a silent resurrect.
		if s.eph.get(req.SessionID) == nil {
			http.Error(w, "ephemeral session not found: expired or restarted", http.StatusNotFound)
			return
		}
	} else if s.store != nil && req.SessionID == "" {
		id, err := agent.NewSessionID()
		if err != nil {
			// same fallback POST /sessions uses - crypto/rand never fails on supported.
			id = fmt.Sprintf("sess-%d", time.Now().UnixNano())
		}
		// Born-pin: the draft intent resolves BEFORE the row write.
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

	// The non-image attachment relocation runs HERE.
	if err := resolveAttachments(messages, req.SessionID); err != nil {
		http.Error(w, "attachment: "+err.Error(), http.StatusBadRequest)
		return
	}

	// A live run takes this send as a STEER instead of the 409: the row is parked in the
	// session-held queue and delivered at the run's next batch boundary, so the agent
	// adjusts in the SAME turn. Replay turns (retry/fork) address the persisted tree and
	// keep their own gates.
	if s.steerNow(&req) {
		s.steerSend(w, r, &req, messages)
		return
	}

	// The legacy no-store path runs inline AFTER the SSE headers.
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
		// Subscribe BEFORE anything client-visible (the session frame below) - the same law
		// handleSessionEvents serves on the reattach side: the moment the origin response is
		// visible on the wire, the subscriber's channel must be in run.subs, so a broadcast
		// landing in the visible-to-subscribed gap (a concurrent canvas delta rides
		// broadcastAll) cannot be dropped.
		ch := s.runs.subscribe(cr.run)
		flusher, ok := setupSSE(w)
		if !ok {
			s.prepareFailed(cr.run, req.SessionID)
			return
		}
		if autoCreated {
			writeSSE(w, "session", []byte(fmt.Sprintf(`{"id":%q}`, req.SessionID)))
			flusher.Flush()
		}
		// The rows are spent BEFORE the run goroutine exists: its finish hook returns any
		// still-unacknowledged rows, so a fast run would otherwise re-queue rows it already
		// persisted. Every fallible step above this line handed them back instead.
		s.ackQueued(req.SessionID, cr.queued)
		// Session-backed chats run DETACHED (the cron runner's detached-turn shape).
		s.launchRun(context.WithoutCancel(r.Context()), cr.run, cr.prov, cr.messages, cr.tools, req)
		s.serveRunFrames(w, flusher, cr.run, ch, r.Context())
		return
	}

	if s.store == nil && !req.Ephemeral {
		if isEphemeralID(req.SessionID) {
			http.Error(w, "ephemeral session id requires the ephemeral flag", http.StatusBadRequest)
			return
		}
		// the default_workspace fallback applies here too.
		s.wsMu.Lock()
		if req.workspace == "" {
			req.workspace = s.defaultWorkspace
		}
		s.wsMu.Unlock()
		messageIn := s.expandAtMentions(messages, req.workspace)
		// the picker's slot grammar applies on the legacy path too.
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

// handleSessionStop serves POST /sessions/{id}/stop: cancels the live run's context.
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
	ok, cancellable := s.stopRun(id)
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
	_, _ = w.Write([]byte(`{"status":"cancelling"}`)) // constant body
}

// stopRun is the user hard stop, in one place for every surface that issues it.
func (s *Server) stopRun(id string) (ok, cancellable bool) {
	ok, cancellable = s.runs.stop(id)
	if ok && cancellable {
		s.abandonPending(id)
	}
	return ok, cancellable
}

// sessionHistory loads the session's persisted history and returns it with the incoming
// messages appended. When no session is configured (nil store or empty ID), messages
// are returned unchanged. Failures come back as runErrors - the caller owns its error
// surface (one history load for the /chat entry AND the chat panel's retry action).
func (s *Server) sessionHistory(ctx context.Context, req *chatRequest, incoming []core.Message) ([]core.Message, error) {
	if isEphemeralID(req.SessionID) {
		// Ephemeral context: the registry's in-memory history IS the conversation - the store
		// is never read.
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
	sessionRow, err := s.store.GetSession(ctx, req.SessionID)
	if err != nil {
		return nil, &runError{http.StatusNotFound, "session not found: " + err.Error()}
	}
	view, err := s.store.GetActiveView(ctx, req.SessionID)
	if err != nil {
		return nil, &runError{http.StatusInternalServerError, "load history: " + err.Error()}
	}
	blocks, err := s.contextBlocks(ctx, req.SessionID)
	if err != nil {
		return nil, &runError{http.StatusInternalServerError, "load context blocks: " + err.Error()}
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
		return agent.DeriveWire(view.Path[:end+1], blocks, sessionRow), nil
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
			return agent.DeriveWire(view.Path[:at+1], blocks, sessionRow), nil
		default:
			if len(incoming) == 0 {
				return nil, &runError{http.StatusBadRequest, "fork requires a message to run from the forked point"}
			}
			for i := range incoming {
				incoming[i] = core.WithParentID(incoming[i], req.Fork)
			}
			req.persistInput = incoming
			return append(agent.DeriveWire(view.Path[:at+1], blocks, sessionRow), incoming...), nil
		}
	}
	req.persistInput = incoming
	return append(agent.DeriveWire(view.Path, blocks, sessionRow), incoming...), nil
}

// contextBlocks reads the session's compact structure, tolerant of a store that cannot hold
// one (it then serves the legacy boundary shape).
func (s *Server) contextBlocks(ctx context.Context, sessionID string) ([]core.ContextBlock, error) {
	blocks, ok := s.store.(agent.SessionBlockStore)
	if !ok {
		return nil, nil
	}
	return blocks.ContextBlocks(ctx, sessionID)
}

// branchFloorOf reports the active path's max store row id.
func branchFloorOf(path []core.StoredMessage) int64 {
	var floor int64
	for _, sm := range path {
		if sm.ID > floor {
			floor = sm.ID
		}
	}
	return floor
}

// setupSSE sets SSE response headers and returns the flusher.
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

// resolveSlotModel applies the model picker's slot grammar. Anything else stays a bare
// model on the request provider (ParseModelSlot is grammar-only, so ollama-style
// "family:tag" names that do not collide with a configured instance id pass through
// untouched).
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
		// Instance resolution: the run serves through the NAMED provider instance. Unknown
		// name = the instance was removed (or never existed) - the run fails loudly and the
		// default never picks the work up silently.
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

// turnSinkFor builds the run's TurnSink. A legacy no-store non-ephemeral run gets nil -
// it persists nothing, exactly.
func (s *Server) turnSinkFor(req chatRequest) agent.TurnSink {
	var sink agent.TurnSink
	if req.Ephemeral && req.SessionID != "" {
		sink = &ephTurnSink{r: &s.eph, id: req.SessionID}
	} else {
		if s.store == nil || req.SessionID == "" {
			return nil
		}
		sink = agent.NewStoreTurnSink(s.store, req.SessionID, req.branchParent)
	}
	if req.wake {
		// A wake run must never end with zero persisted output: the wrapper stands a visible
		// note in for the all-empty completion the sink would otherwise drop silently.
		sink = &wakeNoteSink{s: s, id: req.SessionID, inner: sink}
	}
	return sink
}

// streamAgentEvents runs the agent event loop, writing SSE events to w.
func (s *Server) streamAgentEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, prov agent.Provider, messages []core.Message, tools map[string]core.Tool, req chatRequest) ([]assistantRecord, streamUsage, *core.LastRunStats) {
	// tools run in THIS session's context.
	ctx = agent.WithSessionID(ctx, req.SessionID)
	ctx = core.WithRunWorkspace(ctx, req.workspace, req.workspacePromptContext)
	ctx = core.WithRunOrigin(ctx, "interactive")
	// Turn copy persistence belongs to the loop. Channels render from the store, never
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

// accumulateAssistant appends streamed response-chunk content to the segment.
func (s *Server) accumulateAssistant(recs *[]assistantRecord, rec *assistantRecord, event core.Event, req chatRequest) {
	if s.store == nil || req.SessionID == "" {
		return
	}
	now := time.Now()
	// ToolResultEvent is a top-level agent event (not wrapped in StreamEvent).
	if tre, ok := event.(core.ToolResultEvent); ok {
		if start, ok := rec.callStarts[tre.Message.ToolCallID]; ok && !start.IsZero() {
			tre.Message.Timing = &core.Timing{ElapsedMs: now.Sub(start).Milliseconds()}
			delete(rec.callStarts, tre.Message.ToolCallID)
		}
		rec.results = append(rec.results, tre.Message)
		rec.openAt = now
		return
	}
	se, ok := event.(core.StreamEvent)
	if !ok {
		// TurnStart opens the next provider call's window.
		if _, ok := event.(core.TurnStart); ok {
			rec.openAt = now
		}
		return
	}
	switch ev := se.Event.(type) {
	case core.ThinkingChunk:
		if len(rec.results) > 0 {
			*recs = append(*recs, *rec)
			*rec = assistantRecord{openAt: rec.openAt}
		}
		rec.segDone = false
		rec.thinking += ev.Content
		rec.thinkDur += phaseDelta(rec.lastThink, rec.openAt, now)
		rec.lastThink = now
		rec.openAt = now
	case core.ResponseChunk:
		if len(rec.results) > 0 {
			*recs = append(*recs, *rec)
			*rec = assistantRecord{openAt: rec.openAt}
		}
		rec.segDone = false
		rec.content += ev.Content
		rec.respDur += phaseDelta(rec.lastResp, rec.openAt, now)
		rec.lastResp = now
		rec.openAt = now
	case core.ToolCallEvent:
		// the call ends the segment it follows: text already in this record will never grow
		// again (the next chunk splits into a fresh record), so its rows settle - a remount
		// mid-call must render the measured span, never a still-streaming row
		if rec.thinking != "" || rec.content != "" {
			rec.segDone = true
		}
		rec.toolCalls = append(rec.toolCalls, ev.ToolCall)
		// a tool call closes both spans - the gap while the tool runs belongs to nobody
		rec.lastThink, rec.lastResp, rec.openAt = time.Time{}, time.Time{}, time.Time{}
		if rec.callStarts == nil {
			rec.callStarts = map[string]time.Time{}
		}
		if ev.ToolCall.ID != "" {
			rec.callStarts[ev.ToolCall.ID] = now
		}
	}
}

// phaseDelta returns the wall clock the chunk bills into its span.
func phaseDelta(last, open, now time.Time) time.Duration {
	if !last.IsZero() {
		return now.Sub(last)
	}
	if !open.IsZero() {
		return now.Sub(open)
	}
	return 0
}

// phaseElapsed is a phase span's elapsed-so-far.
func phaseElapsed(acc time.Duration, last, open, now time.Time) int64 {
	if open.IsZero() {
		return acc.Milliseconds()
	}
	return (acc + phaseDelta(last, open, now)).Milliseconds()
}

// persistAssistant closes the run's persistence epilogue. The store branch adds the
// run's real token totals to the session row and writes the run's computed last-known
// rates when non-nil (never cleared here - a run without reported usage leaves the
// previous block standing). The first persistence error is RETURNED, not written to a
// wire - the caller owns the wire (the legacy request writes one error frame; the
// detached run broadcasts one).
func (s *Server) persistAssistant(ctx context.Context, prov agent.Provider, req chatRequest, recs []assistantRecord, usage streamUsage, last *core.LastRunStats) error {
	if req.Ephemeral && req.SessionID != "" {
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
	// the run's last-known rates ride the same persist; a nil block.
	if last != nil {
		if err := s.store.SetSessionLastRun(ctx, req.SessionID, last); err != nil {
			return fmt.Errorf("persist last run: %w", err)
		}
	}
	if lastContent != "" {
		// Detached: the title call is a second agent run, and running it in the request.
		// ponytail: two rapid first-exchanges can interleave provisional and final writes -
		// both go through SetSessionTitle guards and last write still wins on the label, an
		// acceptable cosmetic race at single-user load.
		if sess, err := s.store.GetSession(ctx, req.SessionID); err == nil && sess.TitleState <= 1 {
			go s.maybeTitleSession(context.WithoutCancel(ctx), prov, req, lastContent, true)
		}
	}
	return nil
}

// maybeTitleSession generates a short session title with a cheap model call and never
// fails the turn (every error path returns without one). The state machine gate:
// provisional only for an untitled session (state 0), final only while the title is
// missing or provisional (state 0 or 1) - and a manually set label
// (PUT /sessions/{id}, /label) is never overwritten by a generated one.
func (s *Server) maybeTitleSession(ctx context.Context, prov agent.Provider, req chatRequest, lastAssistant string, final bool) {
	// title_model knob: when set, BOTH title calls route through it.
	if s.titleModel != "" {
		prov = prov.WithModel(s.titleModel)
	}
	sess, err := s.store.GetSession(ctx, req.SessionID)
	if err != nil {
		return
	}
	// Manual-label guard: a label sitting at title state 0 was set by a user (PUT
	// /sessions/{id} or /label), not by this flow - never overwritten.
	if sess.Label != "" && sess.TitleState == 0 {
		return
	}
	if final && sess.TitleState > 1 {
		return
	}
	if !final && sess.TitleState != 0 {
		return
	}
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
		prompt += "\nAssistant: " + truncate(lastAssistant, 400)
	}

	// The title turn is metadata, not a conversation - it must never call a tool. Before
	// the toolless run this was exactly the live defect shipped with: the user content
	// invited tool use and the title model SPAWNED REAL subagents (delegate:task-3/6), one
	// parentless - the title ctx carries no WithSessionID.
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

// handleSessionCreate serves POST /sessions.
func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "session store not configured", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodPost:
		var req struct {
			ID string `json:"id"`
			Workspace string `json:"workspace,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.ID == "" {
			id, err := agent.NewSessionID()
			if err != nil {
				// ponytail: crypto/rand never fails on supported platforms.
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
		// The born-pin: resolve + store-capability check BEFORE CreateSession - never.
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

// manualCompactor is the on-demand compact a human asks for (/compact): the mounted
// service, with the growth gate out of the way and the summary answered back instead of a
// message list.
func (s *Server) manualCompactor() agent.ContextCompactor {
	svc, _ := s.LiveServices().Resolve("compactor").(agent.ContextCompactor)
	return svc
}

// errNoCompactFace: the store cannot hold a context boundary - the agent package's
// sentinel, because the compact itself lives there (agent.CompactSession) and both hosts
// must see the same verdict.
var errNoCompactFace = agent.ErrNoSessionContext

// handleSessionNew serves POST /sessions/{id}/new. A conversation the HUMAN owns
// answers "fork" - the shell opens a fresh tab, exactly what /new always did. A
// conversation the MACHINERY owns (an agent's canonical chat) NEVER forks: the
// relationship is the point, so /new compacts it instead.
func (s *Server) handleSessionNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "store not mounted", http.StatusNotImplemented)
		return
	}
	id := r.PathValue("id")
	if isEphemeralID(id) {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "fork", "session": id})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	row, err := s.store.GetSession(ctx, id)
	if err != nil {
		http.Error(w, "session not found: "+err.Error(), http.StatusNotFound)
		return
	}
	if !row.MachineOwned {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "fork", "session": id})
		return
	}
	// The compact happens on the ACTIVE PATH and keeps the last row as a foothold: a fresh
	// context with a foothold, never an empty one. A compact with no summary to carry
	// writes nothing - dropping the middle with nothing in its place costs more than it
	// saves.
	out, err := agent.CompactSessionKeep(ctx, s.store, s.manualCompactor(), id, 1)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errNoCompactFace) {
			status = http.StatusNotImplemented
		}
		http.Error(w, "compact conversation: "+err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":      "compacted",
		"session":   id,
		"compacted": out.Rows,
		"summary":   out.Compacted,
	})
}

// handleSessionByID serves GET /sessions/{id} (session + messages) and DELETE.
func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		if id := r.PathValue("id"); isEphemeralID(id) {
			http.Error(w, "ephemeral sessions cannot be renamed", http.StatusBadRequest)
			return
		}
	} else {
		// Ephemeral ids never reach the store.
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
		// Typed messages marshal without a role field.
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
		// Row flags: {"pinned": true|false} (pin toggle, existing), {"read": true} (mark read
		// - stamps last_read_at without touching updated_at, same shape as the pinned PATCH)
		// and {"archived": true|false} (archive toggle, same no-touch contract). Session
		// context joins the same no-touch contract: {"model": "."} pins the model the session
		// remembers, {"workspace": "/abs/dir"} roots the session's file operations ("" clears
		// either).
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
		// model/workspace: "": clears the pin, non-empty: sets it (trimmed). Workspace
		// additionally must be an existing directory - rejected before any write lands.
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
		// Session context rides the optional SessionContextStore seam.
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

// toolActionListing is one sub-action of a domain tool.
type toolActionListing struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// toolListing is one /tools row.
type toolListing struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Actions     []toolActionListing `json:"actions,omitempty"`
	// Origin: where the tool registered from.
	// The tools tab filters on this field - never on dotted-name shape.
	Origin string `json:"origin,omitempty"`
	// Disabled: true only in the fold-aware read (GET /tools?disabled=1). omitempty keeps
	// the bare /tools wire byte-identical (a disabled entry never appears there, and
	// on-entries never carry the key).
	Disabled bool `json:"disabled,omitempty"`
}

// handleTools serves GET /tools: the agent's executable tool registry as a
// name+description array, sorted by name. The settings tab rides this so a disabled
// tool keeps its row and its re-enable toggle - the bare read stays the executable
// registry the chat loop consumes, and its response shape never changes.
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
	case core.SteerDeliveredEvent:
		return "steer_delivered"
	case core.ViewModelDelta:
		return "view_model_delta"
	}
	return "event"
}

// writeSSE writes one SSE frame in the format "event: <type>\ndata: <json>\n\n".
func writeSSE(w io.Writer, eventType string, data []byte) {
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", data)
}
