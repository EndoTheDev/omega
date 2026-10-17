package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// --- httptest helpers ---

// mockMCPServer returns an httptest.Server implementing a minimal Streamable
// HTTP MCP server: it handles initialize, notifications/initialized,
// tools/list, and tools/call. The returned server also sets the
// mcp-session-id header on initialize responses.
func mockMCPServer(t *testing.T, sessionID string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Only POST is supported.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		method, _ := req["method"].(string)

		switch method {
		case "initialize":
			w.Header().Set("mcp-session-id", sessionID)
			w.Header().Set("Content-Type", "application/json")
			resp := mcpResponse{
				JSONRPC: "2.0",
				ID:     int64(intVal(req["id"])),
				Result: json.RawMessage(`{"protocolVersion":"2025-11-25","serverInfo":{"name":"mock","version":"1.0"}}`),
			}
			json.NewEncoder(w).Encode(resp)

		case "notifications/initialized":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// notifications have no response body.

		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			tools := mcpToolsListResult{
				Tools: []mcpTool{
					{Name: "echo", Description: "Echoes input", InputSchema: map[string]any{"type": "object"}},
				},
			}
			out, _ := json.Marshal(tools)
			resp := mcpResponse{
				JSONRPC: "2.0",
				ID:     int64(intVal(req["id"])),
				Result: out,
			}
			json.NewEncoder(w).Encode(resp)

		case "tools/call":
			w.Header().Set("Content-Type", "application/json")
			callResult := mcpCallResult{
				Content: []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}{{Type: "text", Text: "echo-result"}},
			}
			out, _ := json.Marshal(callResult)
			resp := mcpResponse{
				JSONRPC: "2.0",
				ID:     int64(intVal(req["id"])),
				Result: out,
			}
			json.NewEncoder(w).Encode(resp)

		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := mcpResponse{
				JSONRPC: "2.0",
				ID:     int64(intVal(req["id"])),
				Error:  &mcpError{Code: -32601, Message: "method not found"},
			}
			json.NewEncoder(w).Encode(resp)
		}
	})
	return httptest.NewServer(mux)
}

// intVal extracts an int from an interface{} coming out of a generic JSON
// decode (numbers arrive as float64).
func intVal(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

// mockSSEServer returns an httptest.Server that responds to requests with a
// text/event-stream body containing the given data lines. Each data line is
// written as `data: <line>\n\n`.
func mockSSEServer(t *testing.T, lines ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("mcp-session-id", "sse-session")
		flusher, _ := w.(http.Flusher)
		for _, l := range lines {
			fmt.Fprintf(w, "data: %s\n\n", l)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
}

// --- tests ---

// TestHTTPRequestJSONResponse covers httpServer.request with a plain JSON
// (application/json) response.
func TestHTTPRequestJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := mcpResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{"ok":true}`)}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := &httpServer{
		name:   "t",
		url:    srv.URL,
		client: srv.Client(),
	}
	res, err := s.request("tools/list", map[string]any{})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if strings.TrimSpace(string(res)) != `{"ok":true}` {
		t.Fatalf("result: got %q", res)
	}
}

// TestHTTPRequestError covers httpServer.request when the JSON-RPC response
// carries an error object.
func TestHTTPRequestError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := mcpResponse{JSONRPC: "2.0", ID: 1, Error: &mcpError{Code: -32000, Message: "boom"}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := &httpServer{name: "t", url: srv.URL, client: srv.Client()}
	_, err := s.request("anything", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error should mention 'boom', got %v", err)
	}
}

// TestHTTPRequestHTTPStatus covers httpServer.request when the server returns
// a non-200 status with a JSON body (error path: status check precedes decode).
func TestHTTPRequestHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("oops"))
	}))
	defer srv.Close()

	s := &httpServer{name: "t", url: srv.URL, client: srv.Client()}
	_, err := s.request("anything", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should mention 500, got %v", err)
	}
}

// TestHTTPRequestSSEResponse covers httpServer.request when the server
// responds with text/event-stream — the request method should delegate to
// readSSE and return the parsed result.
func TestHTTPRequestSSEResponse(t *testing.T) {
	srv := mockSSEServer(t, `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`, "[DONE]")
	defer srv.Close()

	s := &httpServer{name: "t", url: srv.URL, client: srv.Client()}
	res, err := s.request("tools/list", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if strings.TrimSpace(string(res)) != `{"ok":true}` {
		t.Fatalf("result: got %q", res)
	}
}

// TestReadSSEHappyPath covers readSSE parsing a data line with a JSON-RPC
// response and returning its result.
func TestReadSSEHappyPath(t *testing.T) {
	body := "event: message\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[]}}\n\n"
	s := &httpServer{}
	res, err := s.readSSE(strings.NewReader(body))
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if strings.TrimSpace(string(res)) != `{"tools":[]}` {
		t.Fatalf("result: got %q", res)
	}
}

// TestReadSSEDoneMarker covers readSSE skipping the [DONE] marker and then
// reading a real response afterwards.
func TestReadSSEDoneMarker(t *testing.T) {
	body := "data: [DONE]\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"x\":1}}\n\n"
	s := &httpServer{}
	res, err := s.readSSE(strings.NewReader(body))
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if strings.TrimSpace(string(res)) != `{"x":1}` {
		t.Fatalf("result: got %q", res)
	}
}

// TestReadSSEError covers readSSE returning an error when the SSE data
// carries a JSON-RPC error object.
func TestReadSSEError(t *testing.T) {
	body := "data: {\"jsonrpc\":\"2.0\",\"id\":3,\"error\":{\"code\":-1,\"message\":\"oops\"}}\n\n"
	s := &httpServer{}
	_, err := s.readSSE(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "oops") {
		t.Fatalf("error should mention oops, got %v", err)
	}
}

// TestReadSSENoResponse covers readSSE when the stream ends without a
// JSON-RPC response (only a [DONE] marker or nothing useful).
func TestReadSSENoResponse(t *testing.T) {
	body := "data: [DONE]\n\n"
	s := &httpServer{}
	_, err := s.readSSE(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "without response") {
		t.Fatalf("error should mention 'without response', got %v", err)
	}
}

// TestReadSSEEmpty covers readSSE on a completely empty stream.
func TestReadSSEEmpty(t *testing.T) {
	s := &httpServer{}
	_, err := s.readSSE(strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "without response") {
		t.Fatalf("error should mention 'without response', got %v", err)
	}
}

// TestHTTPInitialize covers httpServer.initialize against a mock MCP
// server: it should capture the session ID and send the
// notifications/initialized POST.
func TestHTTPInitialize(t *testing.T) {
	gotNotif := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		switch method {
		case "initialize":
			w.Header().Set("mcp-session-id", "sess-123")
			w.Header().Set("Content-Type", "application/json")
			resp := mcpResponse{
				JSONRPC: "2.0",
				ID:     int64(intVal(req["id"])),
				Result: json.RawMessage(`{"protocolVersion":"2025-11-25"}`),
			}
			json.NewEncoder(w).Encode(resp)
		case "notifications/initialized":
			// Capture the session id header used on the notification.
			gotNotif <- r.Header.Get("mcp-session-id")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	s := &httpServer{name: "t", url: srv.URL, client: srv.Client()}
	if err := s.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if s.sessionID != "sess-123" {
		t.Fatalf("sessionID: got %q, want %q", s.sessionID, "sess-123")
	}
	select {
	case sid := <-gotNotif:
		if sid != "sess-123" {
			t.Fatalf("notification session id: got %q, want %q", sid, "sess-123")
		}
	default:
		t.Fatal("notifications/initialized was not sent")
	}
}

// TestHTTPInitializeSSE covers httpServer.initialize when the server replies
// to initialize via text/event-stream instead of application/json.
func TestHTTPInitializeSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		switch method {
		case "initialize":
			w.Header().Set("mcp-session-id", "sse-sess")
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			fmt.Fprintf(w, "data: %s\n\n", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25"}}`)
			if flusher != nil {
				flusher.Flush()
			}
		case "notifications/initialized":
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	s := &httpServer{name: "t", url: srv.URL, client: srv.Client()}
	if err := s.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if s.sessionID != "sse-sess" {
		t.Fatalf("sessionID: got %q, want %q", s.sessionID, "sse-sess")
	}
}

// TestHTTPInitializeError covers httpServer.initialize failing when the
// server returns a JSON-RPC error on initialize.
func TestHTTPInitializeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := mcpResponse{JSONRPC: "2.0", ID: 1, Error: &mcpError{Code: -1, Message: "init failed"}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := &httpServer{name: "t", url: srv.URL, client: srv.Client()}
	err := s.initialize()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "init failed") {
		t.Fatalf("error should mention 'init failed', got %v", err)
	}
}

// TestHTTPInitializeBadNotifStatus covers initialize failing when the
// notifications/initialized POST returns an unexpected status.
func TestHTTPInitializeBadNotifStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		switch method {
		case "initialize":
			w.Header().Set("mcp-session-id", "sess")
			w.Header().Set("Content-Type", "application/json")
			resp := mcpResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)}
			json.NewEncoder(w).Encode(resp)
		case "notifications/initialized":
			http.Error(w, "bad", http.StatusForbidden)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	s := &httpServer{name: "t", url: srv.URL, client: srv.Client()}
	err := s.initialize()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "notifications/initialized") {
		t.Fatalf("error should mention notifications/initialized, got %v", err)
	}
}

// TestNewHTTPServerEndToEnd exercises newHTTPServer (which calls initialize)
// against a full mock MCP server, then drives listTools and callTool through
// the resulting connected httpServer.
func TestNewHTTPServerEndToEnd(t *testing.T) {
	srv := mockMCPServer(t, "session-abc")
	defer srv.Close()

	s, err := newHTTPServer("mock", srv.URL, map[string]string{"X-Custom": "yes"})
	if err != nil {
		t.Fatalf("newHTTPServer: %v", err)
	}
	defer s.close()
	if s.sessionID != "session-abc" {
		t.Fatalf("sessionID: got %q, want %q", s.sessionID, "session-abc")
	}

	// listTools via the shared helper.
	tools, err := s.listTools()
	if err != nil {
		t.Fatalf("listTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools: got %+v", tools)
	}

	// callTool via the shared helper.
	text, isErr, err := s.callTool("echo", map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}
	if isErr {
		t.Fatal("unexpected isError=true")
	}
	if text != "echo-result" {
		t.Fatalf("callTool text: got %q, want %q", text, "echo-result")
	}
}

// TestHTTPListToolsError covers listTools propagating a request error
// (server unreachable / bad status).
func TestHTTPListToolsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	s := &httpServer{name: "t", url: srv.URL, client: srv.Client()}
	_, err := s.listTools()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestHTTPCallToolError covers callTool propagating a JSON-RPC error.
func TestHTTPCallToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := mcpResponse{JSONRPC: "2.0", ID: 1, Error: &mcpError{Code: -32602, Message: "bad args"}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := &httpServer{name: "t", url: srv.URL, client: srv.Client()}
	_, _, err := s.callTool("echo", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bad args") {
		t.Fatalf("error should mention 'bad args', got %v", err)
	}
}

// TestHTTPClose verifies close is a no-op and does not panic.
func TestHTTPClose(t *testing.T) {
	s := &httpServer{name: "t"}
	// Should not panic and should return nothing.
	s.close()
	// Calling twice should also be safe.
	s.close()
}

// TestNextRequestID verifies the IDs are sequential and unique.
func TestNextRequestID(t *testing.T) {
	s := &httpServer{}
	var ids []int64
	for i := 0; i < 5; i++ {
		ids = append(ids, s.nextRequestID())
	}
	for i, id := range ids {
		if id != int64(i+1) {
			t.Fatalf("ids[%d]: got %d, want %d", i, id, i+1)
		}
	}
}

// TestNextRequestIDConcurrent verifies nextRequestID is safe under concurrent
// access: every ID produced concurrently must be unique.
func TestNextRequestIDConcurrent(t *testing.T) {
	s := &httpServer{}
	const n = 200
	var wg sync.WaitGroup
	ids := make([]int64, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			ids[i] = s.nextRequestID()
		}(i)
	}
	wg.Wait()
	seen := make(map[int64]bool, n)
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id: %d", id)
		}
		seen[id] = true
	}
}

// TestHTTPRequestCustomHeaders verifies that configured custom headers are
// sent on every request.
func TestHTTPRequestCustomHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Test"); got != "hello" {
			t.Errorf("missing custom header X-Test: got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		resp := mcpResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := &httpServer{name: "t", url: srv.URL, client: srv.Client(), headers: map[string]string{"X-Test": "hello"}}
	if _, err := s.request("ping", nil); err != nil {
		t.Fatalf("request: %v", err)
	}
}

// TestHTTPRequestSessionHeader verifies the mcp-session-id header is sent on
// requests after a session ID is set on the httpServer.
func TestHTTPRequestSessionHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("mcp-session-id"); got != "sess-xyz" {
			t.Errorf("mcp-session-id header: got %q, want %q", got, "sess-xyz")
		}
		w.Header().Set("Content-Type", "application/json")
		resp := mcpResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := &httpServer{name: "t", url: srv.URL, client: srv.Client(), sessionID: "sess-xyz"}
	if _, err := s.request("ping", nil); err != nil {
		t.Fatalf("request: %v", err)
	}
}

// TestNewPluginFromEnv covers NewPluginFromEnv with MCP_SERVERS set to a JSON
// config pointing at a mock HTTP MCP server. Verifies the plugin loads and
// exposes the discovered tool.
func TestNewPluginFromEnv(t *testing.T) {
	srv := mockMCPServer(t, "plugin-session")
	defer srv.Close()

	cfg := mcpConfig{Servers: []serverConfig{{Name: "mock", URL: srv.URL}}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	t.Setenv("MCP_SERVERS", string(raw))
	// Make sure OMEGA_HOME doesn't shadow the env var.
	t.Setenv("OMEGA_HOME", "")

	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatalf("NewPluginFromEnv: %v", err)
	}
	if p == nil {
		t.Fatal("plugin is nil")
	}
	tools := p.bridge.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if _, ok := tools["mock.echo"]; !ok {
		t.Fatalf("expected tool mock.echo, got %v", tools)
	}
}

// TestNewPluginFromEnvEmpty covers NewPluginFromEnv when no config is
// present: it should return a no-op plugin (nil bridge) without error.
func TestNewPluginFromEnvEmpty(t *testing.T) {
	t.Setenv("MCP_SERVERS", "")
	t.Setenv("OMEGA_HOME", "")

	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatalf("NewPluginFromEnv: %v", err)
	}
	if p == nil {
		t.Fatal("plugin is nil")
	}
	// With no servers configured, the bridge is non-nil but empty (no tools).
	if p.bridge != nil && len(p.bridge.Tools()) != 0 {
		t.Fatalf("expected 0 tools for empty config, got %d", len(p.bridge.Tools()))
	}
}

// TestNewPluginFromEnvBadConfig covers NewPluginFromEnv returning an error
// when MCP_SERVERS contains invalid JSON.
func TestNewPluginFromEnvBadConfig(t *testing.T) {
	t.Setenv("MCP_SERVERS", "{not valid json")
	t.Setenv("OMEGA_HOME", "")

	_, err := NewPluginFromEnv()
	if err == nil {
		t.Fatal("expected error for bad config, got nil")
	}
}