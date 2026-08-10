package gateway

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/EndoTheDev/omega-agent/internal/agent"
	"github.com/EndoTheDev/omega-agent/internal/ai"
)

//go:embed static/*
var staticFS embed.FS

// Server exposes the agent over HTTP with SSE streaming.
type Server struct {
	agent   *agent.Agent
	tools   map[string]agent.Tool
	httpSrv *http.Server
}

// NewServer creates a Server. tools is the registry of executable tools
// the agent may call; a nil map uses the built-in registry.
func NewServer(a *agent.Agent, tools map[string]agent.Tool) *Server {
	if tools == nil {
		tools = agent.NewRegistry()
	}
	s := &Server{agent: a, tools: tools}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/models", s.handleModels)
	mux.HandleFunc("/chat", s.handleChat)
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"model": s.agent.ModelName()})
}

// chatRequest is the /chat body. tools is an optional list of tool names
// to enable for this run; empty enables the full server registry.
type chatRequest struct {
	Messages []json.RawMessage `json:"messages"`
	Tools    []string          `json:"tools,omitempty"`
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
	tools := s.selectTools(req.Tools)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	for event := range s.agent.Run(ctx, messages, tools) {
		eventType, data, err := sseEvent(event)
		if err != nil {
			writeSSE(w, "error", []byte(err.Error()))
			flusher.Flush()
			return
		}
		writeSSE(w, eventType, data)
		flusher.Flush()
	}
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
	}
	return "event"
}

// writeSSE writes one SSE frame in the format
// "event: <type>\ndata: <json>\n\n".
func writeSSE(w io.Writer, eventType string, data []byte) {
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", data)
}
