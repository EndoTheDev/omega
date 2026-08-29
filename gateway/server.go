package gateway

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

//go:embed static/*
var staticFS embed.FS

// Server exposes the agent over HTTP with SSE streaming.
type Server struct {
	agent   *agent.Agent
	tools   map[string]agent.Tool
	store   agent.StoreProvider
	httpSrv *http.Server
}

// NewServer creates a Server. tools is the registry of executable tools
// the agent may call; a nil map uses the built-in registry. store is the
// optional session persistence; a nil store disables persistence and the
// session CRUD endpoints.
func NewServer(a *agent.Agent, tools map[string]agent.Tool, store agent.StoreProvider) *Server {
	if tools == nil {
		tools = map[string]agent.Tool{}
	}
	s := &Server{agent: a, tools: tools, store: store}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/models", s.handleModels)
	mux.HandleFunc("/chat", s.handleChat)
	mux.HandleFunc("/sessions", s.handleSessions)
	mux.HandleFunc("/sessions/{id}", s.handleSessionByID)
	mux.Handle("/static/", http.FileServer(http.FS(staticFS)))
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"current": s.agent.ModelName(),
		"models":  models,
	})
}

// chatRequest is the /chat body. tools is an optional list of tool names
// to enable for this run; empty enables the full server registry.
// session_id, when set, persists the conversation to that session.
type chatRequest struct {
	Messages  []json.RawMessage `json:"messages"`
	Tools     []string          `json:"tools,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
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

	flusher, ok := setupSSE(w)
	if !ok {
		return
	}

	tools := s.selectTools(req.Tools)
	lastAssistant := s.streamAgentEvents(r.Context(), w, flusher, messages, tools, req)

	s.persistAssistant(r.Context(), w, flusher, req, lastAssistant)
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
// It returns the accumulated assistant text for session persistence.
func (s *Server) streamAgentEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, messages []ai.Message, tools map[string]agent.Tool, req chatRequest) string {
	var lastAssistant string
	for event := range s.agent.Run(ctx, messages, tools) {
		lastAssistant = s.accumulateAssistant(lastAssistant, event, req)
		eventType, data, err := sseEvent(event)
		if err != nil {
			writeSSE(w, "error", []byte(err.Error()))
			flusher.Flush()
			return lastAssistant
		}
		writeSSE(w, eventType, data)
		flusher.Flush()
	}
	return lastAssistant
}

// accumulateAssistant appends streamed response-chunk content to the
// accumulator when a session is active.
func (s *Server) accumulateAssistant(acc string, event agent.Event, req chatRequest) string {
	if s.store == nil || req.SessionID == "" {
		return acc
	}
	se, ok := event.(agent.StreamEvent)
	if !ok {
		return acc
	}
	chunk, ok := se.Event.(ai.ResponseChunk)
	if !ok {
		return acc
	}
	return acc + chunk.Content
}

// persistAssistant saves the final assistant response to the session.
// ponytail: intermediate tool-loop messages (assistant-with-toolcalls
// + tool results) are not flushed mid-run; only the user message and
// the final assistant response are persisted. Matches the spec. If a
// session needs full multi-turn fidelity, surface agent history via
// the event stream and persist it here.
func (s *Server) persistAssistant(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, req chatRequest, lastAssistant string) {
	if s.store == nil || req.SessionID == "" || lastAssistant == "" {
		return
	}
	if err := s.store.AppendMessage(ctx, req.SessionID, ai.NewAssistant(lastAssistant)); err != nil {
		writeSSE(w, "error", []byte("persist response: "+err.Error()))
		flusher.Flush()
	}
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
			req.ID = newSessionID()
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
		writeJSON(w, http.StatusOK, map[string]any{
			"session":  sess,
			"messages": messages,
		})
	case http.MethodDelete:
		if err := s.store.DeleteSession(r.Context(), id); err != nil {
			http.Error(w, "delete session: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// newSessionID returns a random hex session id.
func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// ponytail: crypto/rand never fails on supported platforms; a
		// fallback timestamp keeps the server alive if it ever does.
		return fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
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
