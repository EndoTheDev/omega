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
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

//go:embed static/*
var staticFS embed.FS

// webuiFS is the embedded SPA at the FS root, so the FileServer at "/"
// finds index.html (embed places files under static/; fs.Sub strips it).
var webuiFS, _ = fs.Sub(staticFS, "static")

// Server exposes the agent over HTTP with SSE streaming.
type Server struct {
	agent    *agent.Agent
	tools    map[string]agent.Tool
	runs     runHub // detached /chat runs by session id (reattach surface)
	store    agent.StoreProvider
	settings agent.Settings
	httpSrv  *http.Server
	// runTimeout caps each detached /chat run's wall-clock time. The
	// LLM call carries the HTTP timeout, but a hung tool call would
	// otherwise run forever and hold the session's run slot until
	// restart. Default set here; the mount overrides it from config.
	runTimeout time.Duration

	// version is the host's build tag, surfaced through /health. Empty
	// omits the field.
	version string
	// startedAt anchors the /health uptime (time since server construction).
	startedAt time.Time

	ctxMu     sync.Mutex
	ctxWindow map[string]int // model name -> context window (probed once)
}

// NewServer creates a Server. tools is the registry of executable tools
// the agent may call; a nil map uses the built-in registry. store is the
// optional session persistence; a nil store disables persistence and the
// session CRUD endpoints. settings is the optional config-knobs seam
// (/settings endpoints); a nil value answers 501 on those endpoints.
func NewServer(a *agent.Agent, tools map[string]agent.Tool, store agent.StoreProvider, settings agent.Settings) *Server {
	if tools == nil {
		tools = map[string]agent.Tool{}
	}
	s := &Server{agent: a, tools: tools, store: store, settings: settings, ctxWindow: map[string]int{}, runTimeout: 30 * time.Minute, startedAt: time.Now()}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/models", s.handleModels)
	mux.HandleFunc("/chat", s.handleChat)
	mux.HandleFunc("/sessions", s.handleSessions)
	mux.HandleFunc("/sessions/search", s.handleSessionSearch)
	mux.HandleFunc("/sessions/{id}/events", s.handleSessionEvents)
	mux.HandleFunc("/sessions/{id}", s.handleSessionByID)
	mux.HandleFunc("/files/browse", s.handleFilesBrowse)
	mux.HandleFunc("/files/read", s.handleFilesRead)
	mux.HandleFunc("/settings", s.handleSettings)
	// SPA: the built webui is embedded in the binary and served at /.
	// Exact patterns above win over the catch-all, so the API routes keep
	// working; the catch-all falls back to index.html for non-file paths
	// (history-mode routes like /chat/<id>) and 404s for asset misses.
	// Sync the build with modules/ui/scripts/build.sh before `go build`.
	mux.Handle("/", spaHandler(webuiFS))
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
type chatRequest struct {
	Messages  []json.RawMessage `json:"messages"`
	Tools     []string          `json:"tools,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
	Model     string            `json:"model,omitempty"`
}

// assistantRecord accumulates one response segment of an assistant turn
// for session persistence: thinking, tool calls, tool results, and the
// response content. A tool phase splits the turn into segments (frontend
// mirrors this): segment A carries the preamble + tool calls, segment B
// the final answer. records() returns them in order.
type assistantRecord struct {
	thinking  string
	toolCalls []ai.ToolCall
	results   []ai.ToolResult
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
func (m *runMeter) tick(event agent.Event) {
	se, ok := event.(agent.StreamEvent)
	if !ok {
		return
	}
	switch se.Event.(type) {
	case ai.ThinkingChunk, ai.ResponseChunk:
		m.chunks++
		m.lastChunk = time.Now()
	}
}

// stats computes the run's average rates over its streaming wall
// clock. A provider that reported no completion tokens, or a run too
// short to average honestly (same 0.2s floor the live meter applies),
// yields nil - the caller keeps any previous block instead.
func (m *runMeter) stats(usage streamUsage) *agent.LastRunStats {
	if usage.completion <= 0 || m.chunks == 0 || m.lastChunk.IsZero() {
		return nil
	}
	elapsed := m.lastChunk.Sub(m.started).Seconds()
	if elapsed <= 0.2 {
		return nil
	}
	return &agent.LastRunStats{
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

	// Auto-create: a /chat without session_id IS a new session now
	// (Endo 2026-10-18: /chat is always durable, the webui's
	// create-then-send dance is gone). Generate the id here and ride
	// the normal session-backed path - persistence, provisional title,
	// live reattach all behave exactly as for a POST /sessions id.
	// This runs BEFORE loadSessionMessages: the incoming user messages
	// belong to the new session and persist through the same path as
	// for a pre-created id.
	autoCreated := false
	if s.store != nil && req.SessionID == "" {
		id, err := agent.NewSessionID()
		if err != nil {
			// same fallback POST /sessions uses - crypto/rand never
			// fails on supported platforms; this only keeps the
			// server alive if it ever does
			id = fmt.Sprintf("sess-%d", time.Now().UnixNano())
		}
		if err := s.store.CreateSession(r.Context(), id, "", ""); err != nil {
			http.Error(w, "create session: "+err.Error(), http.StatusInternalServerError)
			return
		}
		req.SessionID = id
		autoCreated = true
	}

	messages, ok := s.loadSessionMessages(r.Context(), w, req, messages)
	if !ok {
		return
	}

	// Per-run provider: base copy with a model override when the request
	// carries one. The copy rides LoopOptions - no shared state mutated.
	prov := s.agent.Provider()
	if req.Model != "" {
		prov = prov.WithModel(req.Model)
	}

	// Provisional title: one cheap GetSession at request start, and the
	// title call fires detached before any streaming, so the sidebar
	// leaves the raw session id while the model is still thinking.
	// maybeTitleSession re-reads the session and enforces the state
	// machine before anything is written.
	if s.store != nil && req.SessionID != "" {
		if sess, err := s.store.GetSession(r.Context(), req.SessionID); err == nil && sess.TitleState == 0 {
			go s.maybeTitleSession(context.WithoutCancel(r.Context()), prov, req, "", false)
		}
	}

	flusher, ok := setupSSE(w)
	if !ok {
		return
	}

	// Tell the client its session id before any run frames: the webui
	// re-keys its stream state from this frame (it no longer POSTs
	// /sessions first). Only the auto-created origin stream carries it -
	// reattach subscribers already know the id from their URL.
	if autoCreated {
		writeSSE(w, "session", []byte(fmt.Sprintf(`{"id":%q}`, req.SessionID)))
		flusher.Flush()
	}

	tools := s.selectTools(req.Tools)

	// No store: the legacy ephemeral path. The stream still rides this
	// request, client presence still gates the run.
	if s.store == nil {
		recs, usage, last := s.streamAgentEvents(r.Context(), w, flusher, prov, messages, tools, req)
		if err := s.persistAssistant(r.Context(), prov, req, recs, usage, last); err != nil {
			writeSSE(w, "error", []byte(err.Error()))
			flusher.Flush()
		}
		return
	}

	// Session-backed chats run DETACHED (the cron runner's detached-turn
	// shape): the run consumes the agent event channel itself on a
	// context that outlives this request, so a client disconnect - a page
	// refresh mid-generation - no longer cancels the run. This response
	// becomes just another subscriber of the run's live frames; reattach
	// is GET /sessions/{id}/events (handleSessionEvents).
	// Subscribe BEFORE launch: the origin client must see the run's first
	// frame, so the consumer channel exists before the run broadcasts.
	run, live := s.runs.start(req.SessionID)
	if !live {
		http.Error(w, "session already running", http.StatusConflict)
		return
	}
	ch := s.runs.subscribe(run)
	s.launchRun(context.WithoutCancel(r.Context()), run, prov, messages, tools, req)
	s.serveRunFrames(w, flusher, run, ch, r.Context())
}

// loadSessionMessages loads persisted history for a session, appends the
// incoming messages, and persists each appended message. When no session
// is configured (nil store or empty ID), messages are returned unchanged.
// On failure an HTTP error is written to w and ok is false.
func (s *Server) loadSessionMessages(ctx context.Context, w http.ResponseWriter, req chatRequest, messages []ai.Message) ([]ai.Message, bool) {
	if s.store == nil || req.SessionID == "" {
		return messages, true
	}
	if _, err := s.store.GetSession(ctx, req.SessionID); err != nil {
		http.Error(w, "session not found: "+err.Error(), http.StatusNotFound)
		return nil, false
	}
	history, err := s.store.GetMessages(ctx, req.SessionID)
	if err != nil {
		http.Error(w, "load history: "+err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	messages = append(history, messages...)
	for _, m := range messages[len(history):] {
		if err := s.store.AppendMessage(ctx, req.SessionID, m); err != nil {
			http.Error(w, "persist message: "+err.Error(), http.StatusInternalServerError)
			return nil, false
		}
	}
	return messages, true
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

// streamAgentEvents runs the agent event loop, writing SSE events to w.
// prov is the per-request provider (a copy carrying the request's model).
// It returns the accumulated assistant records for session persistence
// and the real provider-reported usage for the token totals.
func (s *Server) streamAgentEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, prov ai.Provider, messages []ai.Message, tools map[string]agent.Tool, req chatRequest) ([]assistantRecord, streamUsage, *agent.LastRunStats) {
	var recs []assistantRecord
	var rec assistantRecord
	var usage streamUsage
	meter := newRunMeter()
	for event := range s.agent.RunWithProvider(ctx, prov, messages, tools) {
		s.accumulateAssistant(&recs, &rec, event, req)
		meter.tick(event)
		// Usage arrives on each stream_end; sum every turn of the run.
		if se, ok := event.(agent.StreamEvent); ok {
			if end, ok := se.Event.(ai.StreamEnd); ok {
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
func (s *Server) accumulateAssistant(recs *[]assistantRecord, rec *assistantRecord, event agent.Event, req chatRequest) {
	if s.store == nil || req.SessionID == "" {
		return
	}
	now := time.Now()
	// ToolResultEvent is a top-level agent event (not wrapped in
	// StreamEvent) - the TUI persists it the same way
	if tre, ok := event.(agent.ToolResultEvent); ok {
		if start, ok := rec.callStarts[tre.Message.ToolCallID]; ok && !start.IsZero() {
			tre.Message.Timing = &ai.Timing{ElapsedMs: now.Sub(start).Milliseconds()}
			delete(rec.callStarts, tre.Message.ToolCallID)
		}
		rec.results = append(rec.results, tre.Message)
		// The result opens the next segment's clock: the wait from the
		// last result to the model's next token is generation latency,
		// not a tool gap.
		rec.openAt = now
		return
	}
	se, ok := event.(agent.StreamEvent)
	if !ok {
		// TurnStart opens the next provider call's window: from here
		// until the stream's first chunk, the loop and prompt
		// processing bill to the phase that first chunk lands in.
		if _, ok := event.(agent.TurnStart); ok {
			rec.openAt = now
		}
		return
	}
	switch ev := se.Event.(type) {
	case ai.ThinkingChunk:
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
	case ai.ResponseChunk:
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
	case ai.ToolCallEvent:
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

// persistAssistant saves the assistant turn to the session, one message
// per response segment: assistant(A + tool calls) -> tool results ->
// assistant(B). Providers require this order (final answer as its own
// message after the tool results); the webui reload renders one bubble
// per message, matching the live segment split. usage adds the run's
// real token totals to the session row; last writes the run's computed
// last-known rates when non-nil (never cleared here - a run without
// reported usage leaves the previous block standing). The first
// persistence error is RETURNED, not written to a wire - the caller
// owns the wire (the legacy request writes one error frame; the
// detached run broadcasts one).
func (s *Server) persistAssistant(ctx context.Context, prov ai.Provider, req chatRequest, recs []assistantRecord, usage streamUsage, last *agent.LastRunStats) error {
	if s.store == nil || req.SessionID == "" || len(recs) == 0 {
		return nil
	}
	lastContent := ""
	for _, rec := range recs {
		if rec.content == "" && rec.thinking == "" && len(rec.toolCalls) == 0 {
			continue
		}
		assistant := ai.NewAssistant(rec.content)
		if rec.thinking != "" {
			text := rec.thinking
			assistant.Thinking = &text
		}
		assistant.ToolCalls = rec.toolCalls
		if rec.thinkDur > 0 || rec.respDur > 0 {
			assistant.Timing = &ai.Timing{ThinkingMs: rec.thinkDur.Milliseconds(), ResponseMs: rec.respDur.Milliseconds()}
		}
		if err := s.store.AppendMessage(ctx, req.SessionID, assistant); err != nil {
			return fmt.Errorf("persist response: %w", err)
		}
		// tool results follow the assistant-with-calls message - providers
		// require a tool-role message after every tool call in history
		for _, res := range rec.results {
			if err := s.store.AppendMessage(ctx, req.SessionID, res); err != nil {
				return fmt.Errorf("persist tool result: %w", err)
			}
		}
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

	events := s.agent.RunWithProvider(ctx, prov, []ai.Message{ai.NewUser(prompt)}, nil)
	title := ""
	for ev := range events {
		se, ok := ev.(agent.StreamEvent)
		if !ok {
			continue
		}
		chunk, ok := se.Event.(ai.ResponseChunk)
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
// in-memory busy map no longer survives the reload).
type sessionPayload struct {
	agent.Session
	MessageCount int    `json:"message_count"`
	ToolCount    int    `json:"tool_count"`
	Model        string `json:"model,omitempty"`
	Unread       bool   `json:"unread"`
	Active       bool   `json:"active,omitempty"`
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
			payload := sessionPayload{Session: sess}
			if c, ok := counts[sess.ID]; ok {
				payload.MessageCount = c.MessageCount
				payload.ToolCount = c.ToolCount
			}
			payload.Model = models[sess.ID]
			// computed at read time from the row itself - no counters,
			// no schema writes (same pattern as the counts)
			payload.Unread = agent.SessionUnread(sess)
			payload.Active = s.runs.active(sess.ID)
			out = append(out, payload)
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var req struct {
			ID string `json:"id"`
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
		if err := s.store.CreateSession(r.Context(), req.ID, "", ""); err != nil {
			http.Error(w, "create session: "+err.Error(), http.StatusConflict)
			return
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

// sessionSearchHit is one /sessions/search row: the matching session id
// plus a snippet stub of the hit (a later card renders the context).
type sessionSearchHit struct {
	ID      string `json:"id"`
	Snippet string `json:"snippet,omitempty"`
}

// handleSessionSearch serves GET /sessions/search?q=<term>: the store's
// FTS5 content search over session messages, deduped to one hit per
// session (the first, best-rank message wins). Sessions match CONTENT
// here, not their titles. It returns 501 when the server has no store,
// 200 [] on a missing/empty q (nothing to search), and 400 when FTS5
// rejects the query syntax.
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
	results, err := s.store.SearchMessages(r.Context(), q)
	if err != nil {
		// The only input-dependent failure mode of this read-only
		// query is the query itself; the sqlite driver surfaces FTS5
		// syntax errors as plain "SQL logic error" strings, so the
		// bad call gets 400 (a real store fault trips on the /sessions
		// list too, not just here).
		http.Error(w, "search: "+err.Error(), http.StatusBadRequest)
		return
	}
	seen := make(map[string]bool, len(results))
	out := make([]sessionSearchHit, 0, len(results))
	for _, res := range results {
		if seen[res.SessionID] {
			continue
		}
		seen[res.SessionID] = true
		out = append(out, sessionSearchHit{ID: res.SessionID, Snippet: res.Snippet})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSessionByID serves GET /sessions/{id} (session + messages) and
// DELETE /sessions/{id}. It returns 501 when the server has no store.
func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
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
		// Typed messages marshal without a role field (ai.User/ai.Assistant
		// only carry content) - inject ai.MessageRole so the UI can tell
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
			mm["role"] = ai.MessageRole(sm.Message)
			mm["created_at"] = sm.CreatedAt
			msgs[i] = mm
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"session":  sess,
			"messages": msgs,
			"unread":   agent.SessionUnread(sess),
			// live-run flag: true while a detached /chat run is streaming
			// this session - the webui open-session flow auto-subscribes
			// to /sessions/{id}/events when it sees this
			"active": s.runs.active(id),
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
		// Row flags: {"pinned": true|false} (pin toggle, existing) and
		// {"read": true} (mark read - stamps last_read_at without
		// touching updated_at, same shape as the pinned PATCH). Both
		// are list concerns: pinning and reading are not activity, and
		// the webui time buckets read updated_at.
		var body struct {
			Pinned *bool `json:"pinned"`
			Read   *bool `json:"read"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Pinned == nil && body.Read == nil {
			http.Error(w, "pinned or read must not be missing", http.StatusBadRequest)
			return
		}
		if body.Read != nil && !*body.Read {
			http.Error(w, "read must be true", http.StatusBadRequest)
			return
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

// writeJSON writes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// selectTools filters the server registry to the requested tool names.
// An empty list returns the full registry.
func (s *Server) selectTools(names []string) map[string]agent.Tool {
	if len(names) == 0 {
		return s.tools
	}
	selected := make(map[string]agent.Tool, len(names))
	for _, name := range names {
		if tool, ok := s.tools[name]; ok {
			selected[name] = tool
		}
	}
	return selected
}

// decodeMessages decodes polymorphic ai.Message values from JSON using a
// role discriminator: system, user, assistant, or tool.
func decodeMessages(raw []json.RawMessage) ([]ai.Message, error) {
	messages := make([]ai.Message, 0, len(raw))
	for _, item := range raw {
		var header struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(item, &header); err != nil {
			return nil, err
		}
		switch header.Role {
		case "system":
			var m ai.System
			if err := json.Unmarshal(item, &m); err != nil {
				return nil, err
			}
			messages = append(messages, m)
		case "user":
			var m ai.User
			if err := json.Unmarshal(item, &m); err != nil {
				return nil, err
			}
			messages = append(messages, m)
		case "assistant":
			var m ai.Assistant
			if err := json.Unmarshal(item, &m); err != nil {
				return nil, err
			}
			messages = append(messages, m)
		case "tool":
			var m ai.ToolResult
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
func sseEvent(event agent.Event) (string, []byte, error) {
	if stream, ok := event.(agent.StreamEvent); ok {
		return sseStreamEvent(stream.Event)
	}
	data, err := json.Marshal(event)
	if err != nil {
		return "", nil, err
	}
	return eventTypeOf(event), data, nil
}

func sseStreamEvent(event ai.StreamEvent) (string, []byte, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return "", nil, err
	}
	switch event.(type) {
	case ai.ThinkingChunk:
		return "thinking_chunk", data, nil
	case ai.ResponseChunk:
		return "response_chunk", data, nil
	case ai.ToolCallEvent:
		return "tool_call", data, nil
	case ai.StreamEnd:
		return "stream_end", data, nil
	}
	return "stream", data, nil
}

func eventTypeOf(event agent.Event) string {
	switch event.(type) {
	case agent.AgentStart:
		return "agent_start"
	case agent.TurnStart:
		return "turn_start"
	case agent.TurnEnd:
		return "turn_end"
	case agent.AgentEnd:
		return "agent_end"
	case agent.AssistantMessageEvent:
		return "assistant_message"
	case agent.ToolResultEvent:
		return "tool_result"
	}
	return "event"
}

// writeSSE writes one SSE frame in the format
// "event: <type>\ndata: <json>\n\n".
func writeSSE(w io.Writer, eventType string, data []byte) {
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", data)
}
