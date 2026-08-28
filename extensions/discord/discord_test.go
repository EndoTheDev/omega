package discord

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestChatParsesSSEResponse verifies the SSE parser accumulates response_chunk
// content and ignores other event types.
func TestChatParsesSSEResponse(t *testing.T) {
	sse := "" +
		"event: agent_start\ndata: {}\n\n" +
		"event: response_chunk\ndata: {\"type\":\"response_chunk\",\"content\":\"Hello \"}\n\n" +
		"event: response_chunk\ndata: {\"type\":\"response_chunk\",\"content\":\"world\"}\n\n" +
		"event: stream_end\ndata: {}\n\n"
	out, err := readSSEResponse(strings.NewReader(sse))
	if err != nil {
		t.Fatalf("readSSEResponse: %v", err)
	}
	if out != "Hello world" {
		t.Fatalf("output = %q, want %q", out, "Hello world")
	}
}

// TestEnsureSessionAndChat verifies session creation + chat against a fake
// gateway: the client POSTs to /sessions (creating a session id) then POSTs
// the message to /chat and parses the SSE reply.
func TestEnsureSessionAndChat(t *testing.T) {
	var gotSession string
	var gotMessage string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sessions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": "sess-1"})
		case "/chat":
			var body struct {
				SessionID string `json:"session_id"`
				Messages  []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			gotSession = body.SessionID
			if len(body.Messages) > 0 {
				gotMessage = body.Messages[0].Content
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			flusher, _ := w.(http.Flusher)
			// Stream a response_chunk.
			w.Write([]byte("event: response_chunk\ndata: {\"type\":\"response_chunk\",\"content\":\"hi there\"}\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := New("test-token", server.URL)
	// Session should be created and remembered for the channel.
	sess1, err := c.ensureSession("chan-1")
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	if sess1 != "sess-1" {
		t.Fatalf("session = %q, want sess-1", sess1)
	}
	// Same channel returns the cached session (no second /sessions call).
	sess2, _ := c.ensureSession("chan-1")
	if sess2 != sess1 {
		t.Fatalf("cached session mismatch: %q vs %q", sess2, sess1)
	}
	// Chat forwards the session and message, parses the reply.
	reply, err := c.chat(sess1, "hello")
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if gotSession != "sess-1" {
		t.Fatalf("gateway received session %q, want sess-1", gotSession)
	}
	if gotMessage != "hello" {
		t.Fatalf("gateway received message %q, want hello", gotMessage)
	}
	if reply != "hi there" {
		t.Fatalf("reply = %q, want 'hi there'", reply)
	}
}

// TestNewCommandStartsFreshSession verifies /new creates a new session and
// remaps the channel to it.
func TestNewCommandStartsFreshSession(t *testing.T) {
	var sessionCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sessions" && r.Method == http.MethodPost {
			sessionCalls++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": fmt.Sprintf("sess-%d", sessionCalls)})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := New("test-token", server.URL)
	// First ensureSession creates sess-1.
	sess1, _ := c.ensureSession("chan-1")
	if sess1 != "sess-1" {
		t.Fatalf("initial session = %q, want sess-1", sess1)
	}
	// /new should create a new session and remap the channel.
	c.mu.Lock()
	var created struct {
		ID string `json:"id"`
	}
	if err := c.gatewayPost("/sessions", map[string]any{}, &created); err != nil {
		t.Fatalf("gatewayPost: %v", err)
	}
	c.byChannel["chan-1"] = created.ID
	c.mu.Unlock()
	if created.ID != "sess-2" {
		t.Fatalf("new session = %q, want sess-2", created.ID)
	}
	// Channel now maps to the new session.
	sess2, _ := c.ensureSession("chan-1")
	if sess2 != "sess-2" {
		t.Fatalf("channel remapped to %q, want sess-2", sess2)
	}
	if sessionCalls != 2 {
		t.Fatalf("session create calls = %d, want 2", sessionCalls)
	}
}
