// Package mcp bridges MCP (Model Context Protocol) servers to omega.
// It connects to MCP servers via stdio or Streamable HTTP, discovers
// their tools, and exposes them as omega agent tools. Tool calls from
// the agent loop are forwarded to the appropriate MCP server.
//
// Config: MCP_SERVERS env var (JSON) or mcp.yaml/mcp.json in OMEGA_HOME.
// Uses the "servers" array format.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// --- MCP protocol types ---

type mcpRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpToolsListResult struct {
	Tools []mcpTool `json:"tools"`
}

type mcpCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// --- unified MCP connection interface ---

// MCPConn is the abstraction over stdio and HTTP MCP server connections.
type MCPConn interface {
	listTools() ([]mcpTool, error)
	callTool(name string, args map[string]any) (string, bool, error)
	close()
}

// --- stdio MCP server ---

type stdioServer struct {
	name      string
	cmd       *exec.Cmd
	stdin     *json.Encoder
	stdout    *bufio.Reader
	mu        sync.Mutex
	pending   map[int64]chan mcpResponse
	pendingMu sync.Mutex
	nextID    int64
}

func newStdioServer(name, command string, args []string, env map[string]string) (*stdioServer, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	s := &stdioServer{
		name:    name,
		cmd:     cmd,
		stdin:   json.NewEncoder(stdinPipe),
		stdout:  bufio.NewReader(stdoutPipe),
		pending: make(map[int64]chan mcpResponse),
	}
	go s.readLoop()
	if err := s.initialize(); err != nil {
		s.close()
		return nil, fmt.Errorf("initialize %s: %w", name, err)
	}
	return s, nil
}

func (s *stdioServer) readLoop() {
	for {
		line, err := s.stdout.ReadString('\n')
		if err != nil {
			return
		}
		var resp mcpResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		s.pendingMu.Lock()
		ch, ok := s.pending[resp.ID]
		if ok {
			delete(s.pending, resp.ID)
		}
		s.pendingMu.Unlock()
		if ok {
			ch <- resp
		}
	}
}

func (s *stdioServer) request(method string, params any) (json.RawMessage, error) {
	s.pendingMu.Lock()
	s.nextID++
	id := s.nextID
	ch := make(chan mcpResponse, 1)
	s.pending[id] = ch
	s.pendingMu.Unlock()

	req := mcpRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	s.mu.Lock()
	err := s.stdin.Encode(req)
	s.mu.Unlock()
	if err != nil {
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
		return nil, fmt.Errorf("send: %w", err)
	}
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-time.After(30 * time.Second):
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
		return nil, fmt.Errorf("timeout: %s", method)
	}
}

func (s *stdioServer) initialize() error {
	params := map[string]any{
		"protocolVersion": "2025-11-25",
		"clientInfo":      map[string]string{"name": "omega-mcp-bridge", "version": "0.1.0"},
		"capabilities":    map[string]any{},
	}
	_, err := s.request("initialize", params)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.stdin.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	s.mu.Unlock()
	return nil
}

func (s *stdioServer) listTools() ([]mcpTool, error) {
	result, err := s.request("tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var list mcpToolsListResult
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("parse tools: %w", err)
	}
	return list.Tools, nil
}

func (s *stdioServer) callTool(name string, args map[string]any) (string, bool, error) {
	result, err := s.request("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", false, err
	}
	var call mcpCallResult
	if err := json.Unmarshal(result, &call); err != nil {
		return "", false, fmt.Errorf("parse call result: %w", err)
	}
	var text strings.Builder
	for _, c := range call.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return text.String(), call.IsError, nil
}

func (s *stdioServer) close() {
	// ponytail: no graceful shutdown notification; kill directly.
	// MCP servers handle SIGTERM. Upgrade path: send proper shutdown.
	s.cmd.Process.Kill()
	s.cmd.Wait()
}

// --- HTTP MCP server (Streamable HTTP transport) ---

type httpServer struct {
	name      string
	url       string
	headers   map[string]string
	client    *http.Client
	sessionID string
	nextID    int64
	idMu      sync.Mutex
}

func newHTTPServer(name, url string, headers map[string]string) (*httpServer, error) {
	s := &httpServer{
		name:    name,
		url:     url,
		headers: headers,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	if err := s.initialize(); err != nil {
		return nil, fmt.Errorf("initialize %s: %w", name, err)
	}
	return s, nil
}

func (s *httpServer) nextRequestID() int64 {
	s.idMu.Lock()
	s.nextID++
	id := s.nextID
	s.idMu.Unlock()
	return id
}

func (s *httpServer) request(method string, params any) (json.RawMessage, error) {
	id := s.nextRequestID()
	req := mcpRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	httpReq, err := http.NewRequest("POST", s.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range s.headers {
		httpReq.Header.Set(k, v)
	}
	if s.sessionID != "" {
		httpReq.Header.Set("mcp-session-id", s.sessionID)
	}
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		return s.readSSE(resp.Body)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var mcpResp mcpResponse
	if err := json.NewDecoder(resp.Body).Decode(&mcpResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if mcpResp.Error != nil {
		return nil, fmt.Errorf("mcp error %d: %s", mcpResp.Error.Code, mcpResp.Error.Message)
	}
	return mcpResp.Result, nil
}

// readSSE reads an SSE stream and returns the first JSON-RPC response.
func (s *httpServer) readSSE(body io.Reader) (json.RawMessage, error) {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				continue
			}
			var mcpResp mcpResponse
			if err := json.Unmarshal([]byte(data), &mcpResp); err == nil {
				if mcpResp.Error != nil {
					return nil, fmt.Errorf("mcp error %d: %s", mcpResp.Error.Code, mcpResp.Error.Message)
				}
				return mcpResp.Result, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("SSE read: %w", err)
	}
	return nil, fmt.Errorf("SSE stream ended without response")
}

func (s *httpServer) initialize() error {
	params := map[string]any{
		"protocolVersion": "2025-11-25",
		"clientInfo":      map[string]string{"name": "omega-mcp-bridge", "version": "0.1.0"},
		"capabilities":    map[string]any{},
	}
	// Do the initialize request manually to capture the session ID header.
	id := s.nextRequestID()
	reqBody, _ := json.Marshal(mcpRequest{JSONRPC: "2.0", ID: id, Method: "initialize", Params: params})
	httpReq, err := http.NewRequest("POST", s.url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range s.headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	// Capture the session ID for subsequent requests.
	s.sessionID = resp.Header.Get("mcp-session-id")
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		_, err = s.readSSE(resp.Body)
		if err != nil {
			return fmt.Errorf("read SSE: %w", err)
		}
	} else {
		var mcpResp mcpResponse
		if err := json.NewDecoder(resp.Body).Decode(&mcpResp); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		if mcpResp.Error != nil {
			return fmt.Errorf("mcp error %d: %s", mcpResp.Error.Code, mcpResp.Error.Message)
		}
	}
	// Send initialized notification. For HTTP, this is a POST with no ID.
	notifBody, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	httpReq2, err := http.NewRequest("POST", s.url, bytes.NewReader(notifBody))
	if err != nil {
		return nil
	}
	httpReq2.Header.Set("Content-Type", "application/json")
	httpReq2.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range s.headers {
		httpReq2.Header.Set(k, v)
	}
	if s.sessionID != "" {
		httpReq2.Header.Set("mcp-session-id", s.sessionID)
	}
	resp2, err := s.client.Do(httpReq2)
	if err != nil {
		return nil
	}
	if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusAccepted {
		resp2.Body.Close()
		return fmt.Errorf("notifications/initialized: http %d", resp2.StatusCode)
	}
	resp2.Body.Close()
	return nil
}

func (s *httpServer) listTools() ([]mcpTool, error) {
	result, err := s.request("tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var list mcpToolsListResult
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("parse tools: %w", err)
	}
	return list.Tools, nil
}

func (s *httpServer) callTool(name string, args map[string]any) (string, bool, error) {
	result, err := s.request("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", false, err
	}
	var call mcpCallResult
	if err := json.Unmarshal(result, &call); err != nil {
		return "", false, fmt.Errorf("parse call result: %w", err)
	}
	var text strings.Builder
	for _, c := range call.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return text.String(), call.IsError, nil
}

func (s *httpServer) close() {
	// HTTP servers have no process to kill.
}

// --- config ---

type serverConfig struct {
	Name    string            `json:"name" yaml:"name"`
	Command string            `json:"command" yaml:"command"`
	Args    []string          `json:"args" yaml:"args"`
	Env     map[string]string `json:"env" yaml:"env"`
	URL     string            `json:"url" yaml:"url"`
	Headers map[string]string `json:"headers" yaml:"headers"`
}

type mcpConfig struct {
	Servers []serverConfig `json:"servers" yaml:"servers"`
}

func loadConfig() (*mcpConfig, error) {
	// Try env var first (JSON only — env vars with YAML are awkward).
	if raw := os.Getenv("MCP_SERVERS"); raw != "" {
		return parseConfig([]byte(raw))
	}
	// Try config file: mcp.yaml first, then mcp.json.
	home := os.Getenv("OMEGA_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".omega")
	}
	for _, name := range []string{"mcp.yaml", "mcp.json"} {
		path := filepath.Join(home, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		return parseConfig(data)
	}
	return &mcpConfig{}, nil
}

func parseConfig(data []byte) (*mcpConfig, error) {
	// YAML is a superset of JSON, so yaml.Unmarshal handles both.
	var cfg mcpConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// --- bridge ---

type toolMapping struct {
	server string
	mcp    string
}

// Bridge manages MCP server connections and routes tool calls.
// It implements agent.ToolProvider via the Plugin's Mount method.
type Bridge struct {
	servers  map[string]MCPConn
	toolMap  map[string]toolMapping
	toolDefs map[string]mcpTool
}

// NewBridge creates a Bridge from an explicit config, connecting to
// all configured servers and discovering their tools.
func NewBridge(cfg *mcpConfig) (*Bridge, error) {
	b := &Bridge{
		servers:  make(map[string]MCPConn),
		toolMap:  make(map[string]toolMapping),
		toolDefs: make(map[string]mcpTool),
	}
	if cfg != nil {
		if err := b.connect(cfg); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// NewBridgeFromEnv loads config from MCP_SERVERS env var or mcp.yaml/mcp.json
// in OMEGA_HOME and creates a Bridge. Returns an empty Bridge (no servers)
// if no config is found — the extension is a no-op in that case.
func NewBridgeFromEnv() (*Bridge, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return NewBridge(cfg)
}

func (b *Bridge) connect(cfg *mcpConfig) error {
	for _, sc := range cfg.Servers {
		var conn MCPConn
		var err error
		if sc.URL != "" {
			conn, err = newHTTPServer(sc.Name, sc.URL, sc.Headers)
		} else if sc.Command != "" {
			conn, err = newStdioServer(sc.Name, sc.Command, sc.Args, sc.Env)
		} else {
			fmt.Fprintf(os.Stderr, "mcp: skipping %s: no command or url\n", sc.Name)
			continue
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp: failed to connect %s: %v\n", sc.Name, err)
			continue
		}
		b.servers[sc.Name] = conn
		tools, err := conn.listTools()
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp: failed to list tools from %s: %v\n", sc.Name, err)
			continue
		}
		for _, t := range tools {
			toolName := sc.Name + "." + t.Name
			b.toolMap[toolName] = toolMapping{server: sc.Name, mcp: t.Name}
			b.toolDefs[toolName] = t
		}
	}
	return nil
}

// Close releases all server connections.
func (b *Bridge) Close() {
	for _, s := range b.servers {
		s.close()
	}
}

// Tools builds the agent.Tool map from discovered MCP tools. Each tool
// wraps a call to the appropriate MCP server.
func (b *Bridge) Tools() map[string]agentTool {
	tools := make(map[string]agentTool, len(b.toolDefs))
	for name, mt := range b.toolDefs {
		tName := name
		tDef := mt
		tools[tName] = agentTool{
			description: tDef.Description,
			parameters:  tDef.InputSchema,
			run: func(ctx context.Context, args map[string]any) (string, error) {
				mapping, ok := b.toolMap[tName]
				if !ok {
					return fmt.Sprintf("unknown tool: %s", tName), nil
				}
				srv := b.servers[mapping.server]
				if srv == nil {
					return fmt.Sprintf("MCP server %s is not running", mapping.server), nil
				}
				content, isErr, err := srv.callTool(mapping.mcp, args)
				if err != nil {
					return fmt.Sprintf("MCP call failed: %v", err), nil
				}
				if isErr {
					return content, fmt.Errorf("%s", content)
				}
				return content, nil
			},
		}
	}
	return tools
}

// agentTool is the local representation of an agent tool. The plugin
// adapter converts these to agent.Tool values via the ToolProvider
// interface. Kept as a struct to avoid importing agent in mcp.go.
type agentTool struct {
	description string
	parameters  map[string]any
	run         func(ctx context.Context, args map[string]any) (string, error)
}