package http_channel

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"io/fs"
	"sync"
	"strings"
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
	agent   *agent.Agent
	tools   map[string]agent.Tool
	store   agent.StoreProvider
	httpSrv *http.Server

	ctxMu     sync.Mutex
	ctxWindow map[string]int // model name -> context window (probed once)
}

// NewServer creates a Server. tools is the registry of executable tools
// the agent may call; a nil map uses the built-in registry. store is the
// optional session persistence; a nil store disables persistence and the
// session CRUD endpoints.
func NewServer(a *agent.Agent, tools map[string]agent.Tool, store agent.StoreProvider) *Server {
	if tools == nil {
		tools = map[string]agent.Tool{}
	}
	s := &Server{agent: a, tools: tools, store: store, ctxWindow: map[string]int{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/models", s.handleModels)
	mux.HandleFunc("/chat", s.handleChat)
	mux.HandleFunc("/sessions", s.handleSessions)
	mux.HandleFunc("/sessions/{id}", s.handleSessionByID)
	// SPA: the built webui is embedded in the binary and served at /.
	// Exact patterns above win over the catch-all, so the API routes keep
	// working; assets under /_nuxt/ come from the same FS. Sync the build
	// with webui/scripts/build.sh before `go build`.
	mux.Handle("/", http.FileServer(http.FS(webuiFS)))
	s.httpSrv = &http.Server{Handler: mux}
	return s
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
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
// session_id, when set, persists the conversation to that session.
// model, when set, serves this run on that model - the default stays
// untouched, so concurrent chats keep their own model.
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

	flusher, ok := setupSSE(w)
	if !ok {
		return
	}

	tools := s.selectTools(req.Tools)
	recs := s.streamAgentEvents(r.Context(), w, flusher, prov, messages, tools, req)

	s.persistAssistant(r.Context(), w, flusher, prov, req, recs)
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
// It returns the accumulated assistant records for session persistence.
func (s *Server) streamAgentEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, prov ai.Provider, messages []ai.Message, tools map[string]agent.Tool, req chatRequest) []assistantRecord {
	var recs []assistantRecord
	var rec assistantRecord
	for event := range s.agent.RunWithProvider(ctx, prov, messages, tools) {
		s.accumulateAssistant(&recs, &rec, event, req)
		eventType, data, err := sseEvent(event)
		if err != nil {
			writeSSE(w, "error", []byte(err.Error()))
			flusher.Flush()
			return append(recs, rec)
		}
		writeSSE(w, eventType, data)
		flusher.Flush()
	}
	return append(recs, rec)
}

// accumulateAssistant appends streamed response-chunk content to the
// segment accumulator when a session is active. A response chunk that
// arrives after tool results starts a new segment - the frontend splits
// the live view at the same boundary.
func (s *Server) accumulateAssistant(recs *[]assistantRecord, rec *assistantRecord, event agent.Event, req chatRequest) {
	if s.store == nil || req.SessionID == "" {
		return
	}
	// ToolResultEvent is a top-level agent event (not wrapped in
	// StreamEvent) - the TUI persists it the same way
	if tre, ok := event.(agent.ToolResultEvent); ok {
		rec.results = append(rec.results, tre.Message)
		return
	}
	se, ok := event.(agent.StreamEvent)
	if !ok {
		return
	}
	switch ev := se.Event.(type) {
	case ai.ThinkingChunk:
		rec.thinking += ev.Content
	case ai.ResponseChunk:
		if len(rec.results) > 0 {
			// tool phase ended the current segment - the frontend
			// renders a fresh bubble for what follows, so we persist
			// a fresh record for it
			*recs = append(*recs, *rec)
			*rec = assistantRecord{}
		}
		rec.content += ev.Content
	case ai.ToolCallEvent:
		rec.toolCalls = append(rec.toolCalls, ev.ToolCall)
	}
}

// persistAssistant saves the assistant turn to the session, one message
// per response segment: assistant(A + tool calls) -> tool results ->
// assistant(B). Providers require this order (final answer as its own
// message after the tool results); the webui reload renders one bubble
// per message, matching the live segment split.
func (s *Server) persistAssistant(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, prov ai.Provider, req chatRequest, recs []assistantRecord) {
	if s.store == nil || req.SessionID == "" || len(recs) == 0 {
		return
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
		if err := s.store.AppendMessage(ctx, req.SessionID, assistant); err != nil {
			writeSSE(w, "error", []byte("persist response: "+err.Error()))
			flusher.Flush()
			return
		}
		// tool results follow the assistant-with-calls message - providers
		// require a tool-role message after every tool call in history
		for _, res := range rec.results {
			if err := s.store.AppendMessage(ctx, req.SessionID, res); err != nil {
				writeSSE(w, "error", []byte("persist tool result: "+err.Error()))
				flusher.Flush()
				return
			}
		}
		if rec.content != "" {
			lastContent = rec.content
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
		// ponytail: two rapid first-messages can race the label=="" check -
		// last write wins, an untitled session is cosmetic. Single user.
		go s.maybeTitleSession(context.WithoutCancel(ctx), prov, req, lastContent)
	}
}

// maybeTitleSession generates a short session title with a cheap model call
// when the session has no label yet (first exchange) and writes it via
// UpdateSession. prov is the pinned per-request provider copy. Runs in a
// detached goroutine after the stream ends; failure is silent - an
// untitled session is cosmetic, not fatal.
func (s *Server) maybeTitleSession(ctx context.Context, prov ai.Provider, req chatRequest, lastAssistant string) {
	sess, err := s.store.GetSession(ctx, req.SessionID)
	if err != nil || sess.Label != "" {
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
	prompt := "Write a conversation title of at most 5 words for this exchange. Reply with ONLY the title, no quotes, no punctuation at the end.\n\nUser: " + firstUser + "\nAssistant: " + truncate(lastAssistant, 400)

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
	_ = s.store.UpdateSession(ctx, req.SessionID, title)
}

// truncate cuts s at n runes, appending an ellipsis marker when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
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
		writeJSON(w, http.StatusOK, sessions)
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
		messages, err := s.store.GetMessages(r.Context(), id)
		if err != nil {
			http.Error(w, "get messages: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// typed messages marshal without a role field (ai.User/ai.Assistant
		// only carry content) - inject ai.MessageRole so the UI can tell
		// user and assistant apart when restoring history.
		msgs := make([]map[string]any, len(messages))
		for i, m := range messages {
			data, _ := json.Marshal(m)
			var mm map[string]any
			json.Unmarshal(data, &mm)
			mm["role"] = ai.MessageRole(m)
			msgs[i] = mm
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"session":  sess,
			"messages": msgs,
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
