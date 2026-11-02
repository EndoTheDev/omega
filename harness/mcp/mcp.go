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
	request(method string, params any) (json.RawMessage, error)
	listTools() ([]mcpTool, error)
	callTool(name string, args map[string]any) (string, bool, error)
	close()
}

// mcpListTools calls tools/list on any MCP connection.
func mcpListTools(c MCPConn) ([]mcpTool, error) {
	result, err := c.request("tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var list mcpToolsListResult
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("parse tools: %w", err)
	}
	return list.Tools, nil
}

// mcpCallTool calls tools/call on any MCP connection and extracts text content.
func mcpCallTool(c MCPConn, name string, args map[string]any) (string, bool, error) {
	result, err := c.request("tools/call", map[string]any{"name": name, "arguments": args})
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
	// done closes on close() so an in-flight request fails fast
	// instead of riding out its timeout against a killed process
	// (the reload removal path, card t_d6e94e79).
	done      chan struct{}
	closeOnce sync.Once
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
		done:    make(chan struct{}),
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
	case <-s.done:
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
		return nil, fmt.Errorf("server stopped: %s", method)
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
	return mcpListTools(s)
}

func (s *stdioServer) callTool(name string, args map[string]any) (string, bool, error) {
	return mcpCallTool(s, name, args)
}

func (s *stdioServer) close() {
	// ponytail: no graceful shutdown notification; kill directly.
	// MCP servers handle SIGTERM. Upgrade path: send proper shutdown.
	if s.done != nil {
		s.closeOnce.Do(func() {
			close(s.done)
		})
	}
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

// newMCPRequest builds an HTTP request to the MCP endpoint with the common
// headers (Content-Type, Accept, and any configured custom headers) applied.
func (s *httpServer) newMCPRequest(url, method string, body []byte) (*http.Request, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	return req, nil
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
	httpReq, err := s.newMCPRequest(s.url, "POST", reqBody)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
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
	httpReq2, err := s.newMCPRequest(s.url, "POST", notifBody)
	if err != nil {
		return nil
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
	return mcpListTools(s)
}

func (s *httpServer) callTool(name string, args map[string]any) (string, bool, error) {
	return mcpCallTool(s, name, args)
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
	// Enabled is the per-server toggle (card t_83f37525). The ruling:
	// mcp.yaml owns the servers, so the flag lives on the per-server
	// entry there (`enabled: false`) - config.yaml's `disabled:` list
	// is whole-module scope (the mcp module as a capsule), not
	// per-server. A pointer so an absent key reads as on: configs
	// written before the flag keep working unchanged.
	//
	// The JSON mirror exists for the MCP_SERVERS env shape only; the
	// toggle POST never writes there (the env var is not a file, the
	// wire route refuses it).
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

// isEnabled reports the toggle state (nil-safe: an absent or nil flag
// is an enabled server - the pre-flag default).
func (sc *serverConfig) isEnabled() bool { return sc.Enabled == nil || *sc.Enabled }

type mcpConfig struct {
	Servers []serverConfig `json:"servers" yaml:"servers"`
}

func loadConfig() (*mcpConfig, string, error) {
	// Try env var first (JSON only — env vars with YAML are awkward).
	if raw := os.Getenv("MCP_SERVERS"); raw != "" {
		cfg, err := parseConfig([]byte(raw))
		return cfg, "env", err
	}
	// Try config file: mcp.yaml first, then mcp.json. Each name is
	// looked up at the home root first (legacy installs keep working
	// unchanged), then in the config/ subdirectory (Endo 2026-10-18
	// layout where the three infrastructure yamls live). The lookup
	// order lives in configFile, so the toggle write (status.go) can
	// resolve the exact same file the loader would read.
	path, name, ok := configFile()
	if !ok {
		return &mcpConfig{}, "none", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, name, fmt.Errorf("read %s: %w", name, err)
	}
	cfg, err := parseConfig(data)
	return cfg, name, err
}

// configFile resolves the config file the loader reads, walking the
// IDENTICAL lookup order (mcp.yaml/mcp.json per dir, home root first,
// then config/). Returns ok=false when no config file exists - the
// env/none sources, which the toggle route refuses. Only mcp.yaml is
// ever written: the flag lives on the per-server entry (card
// t_83f37525) and mcp.json entries stay hand-edited.
func configFile() (path string, name string, ok bool) {
	home := os.Getenv("OMEGA_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".omega")
	}
	cfgDir := filepath.Join(home, "config")
	for _, name := range []string{"mcp.yaml", "mcp.json"} {
		for _, dir := range []string{home, cfgDir} {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err != nil {
				continue
			}
			return path, name, true
		}
	}
	return "", "", false
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
// It implements core.ToolProvider via the Plugin's Mount method.
type Bridge struct {
	servers  map[string]MCPConn
	toolMap  map[string]toolMapping
	toolDefs map[string]mcpTool
	// cfg + source back the read-only inventory (Status, card
	// t_c7136c50): the configured servers as the bridge sees them.
	// errs carries the per-server connect/listTools failures - a
	// skipped server stays visible to the listing instead of
	// vanishing into a stderr line.
	cfg    *mcpConfig
	source string
	errs   map[string]string
	// stopped closes on shutdown (the reload removal path, card
	// t_d6e94e79): a tool run that reaches a removed bridge answers
	// an IsError tool result instead of routing into torn-down
	// connections. The maps are written once in connect (bridge-build
	// time), so a live bridge's reads need no lock.
	stopped   chan struct{}
	closeOnce sync.Once
}

// NewBridge creates a Bridge from an explicit config, connecting to
// all configured servers and discovering their tools.
func NewBridge(cfg *mcpConfig) (*Bridge, error) {
	b := &Bridge{
		servers:  make(map[string]MCPConn),
		toolMap:  make(map[string]toolMapping),
		toolDefs: make(map[string]mcpTool),
		errs:     make(map[string]string),
		stopped:  make(chan struct{}),
	}
	if cfg != nil {
		b.cfg = cfg
		if err := b.connect(cfg); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// isStopped reports whether shutdown has run (nil-safe: a zero Bridge
// from tests never counts as stopped).
func (b *Bridge) isStopped() bool {
	if b == nil || b.stopped == nil {
		return false
	}
	select {
	case <-b.stopped:
		return true
	default:
		return false
	}
}

// shutdown disconnects every server this bridge holds - the reload's
// removal step (card t_d6e94e79): a server that left the config
// disconnects on reload, stdio processes are killed. In-flight calls
// to removed servers surface their failure as an IsError tool result
// (the run checks isStopped; stdio requests also see the closed
// connection), never a panic and never a lost response. Idempotent.
func (b *Bridge) shutdown() {
	if b == nil {
		return
	}
	if b.stopped != nil {
		b.closeOnce.Do(func() {
			close(b.stopped)
		})
	}
	for _, c := range b.servers {
		c.close()
	}
}

// NewBridgeFromEnv loads config from MCP_SERVERS env var or mcp.yaml/mcp.json
// in OMEGA_HOME and creates a Bridge. Returns an empty Bridge (no servers)
// if no config is found — the extension is a no-op in that case.
func NewBridgeFromEnv() (*Bridge, error) {
	cfg, source, err := loadConfig()
	if err != nil {
		return nil, err
	}
	b, err := NewBridge(cfg)
	if err != nil {
		return nil, err
	}
	b.source = source
	return b, nil
}

func (b *Bridge) connect(cfg *mcpConfig) error {
	for _, sc := range cfg.Servers {
		// Per-server toggle (card t_83f37525): a disabled server is
		// intentionally not connected and discovers no tools - its
		// tools never enter toolDefs, so Tools() never serves them
		// and the namespace guard has nothing to front. The entry
		// stays listed in Status (enabled:false, not connected) like
		// the skills list keeps its disabled rows visible.
		if !sc.isEnabled() {
			continue
		}
		var conn MCPConn
		var err error
		if sc.URL != "" {
			conn, err = newHTTPServer(sc.Name, sc.URL, sc.Headers)
		} else if sc.Command != "" {
			conn, err = newStdioServer(sc.Name, sc.Command, sc.Args, sc.Env)
		} else {
			fmt.Fprintf(os.Stderr, "mcp: skipping %s: no command or url\n", sc.Name)
			b.errs[sc.Name] = "no command or url configured"
			continue
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp: failed to connect %s: %v\n", sc.Name, err)
			b.errs[sc.Name] = err.Error()
			continue
		}
		b.servers[sc.Name] = conn
		tools, err := conn.listTools()
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp: failed to list tools from %s: %v\n", sc.Name, err)
			b.errs[sc.Name] = "tools/list failed: " + err.Error()
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

// Tools builds the core.Tool map from discovered MCP tools. Each tool
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
					return "", fmt.Errorf("unknown tool: %s", tName)
				}
				if b.isStopped() {
					// the reload removed this bridge's server - the
					// failure is a structured IsError tool result
					// (the tool-error law), never a panic
					return "", fmt.Errorf("mcp server %s was removed on reload", mapping.server)
				}
				srv := b.servers[mapping.server]
				if srv == nil {
					return "", fmt.Errorf("MCP server %s is not running", mapping.server)
				}
				content, isErr, err := srv.callTool(mapping.mcp, args)
				if err != nil {
					return "", fmt.Errorf("MCP call failed: %v", err)
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
// adapter converts these to core.Tool values via the ToolProvider
// interface. Kept as a struct to avoid importing agent in mcp.go.
type agentTool struct {
	description string
	parameters  map[string]any
	run         func(ctx context.Context, args map[string]any) (string, error)
}
