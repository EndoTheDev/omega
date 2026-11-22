// Package mcp bridges MCP (Model Context Protocol) servers to omega.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// MCPConn is the abstraction over stdio and HTTP MCP server connections. Every call
// that a live run drives takes a ctx: the agent loop's tool call carries cancellation,
// and a cancelled call must abort instead of riding out the 30s request budget.
type MCPConn interface {
	request(ctx context.Context, method string, params any) (json.RawMessage, error)
	listTools(ctx context.Context) ([]mcpTool, error)
	callTool(ctx context.Context, name string, args map[string]any) (string, bool, error)
	close()
}

func mcpListTools(ctx context.Context, c MCPConn) ([]mcpTool, error) {
	result, err := c.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var list mcpToolsListResult
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("parse tools: %w", err)
	}
	return list.Tools, nil
}

func mcpCallTool(ctx context.Context, c MCPConn, name string, args map[string]any) (string, bool, error) {
	result, err := c.request(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
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

type stdioServer struct {
	name      string
	cmd       *exec.Cmd
	stdin     *json.Encoder
	stdout    *bufio.Reader
	mu        sync.Mutex
	pending   map[int64]chan mcpResponse
	pendingMu sync.Mutex
	nextID    int64
	// done closes on close so an in-flight request fails fast instead of riding out.
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

func (s *stdioServer) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
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
	case <-ctx.Done():
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
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
	_, err := s.request(context.Background(), "initialize", params)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.stdin.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	s.mu.Unlock()
	return nil
}

func (s *stdioServer) listTools(ctx context.Context) ([]mcpTool, error) {
	return mcpListTools(ctx, s)
}

func (s *stdioServer) callTool(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	return mcpCallTool(ctx, s, name, args)
}

func (s *stdioServer) close() {
	if s.done != nil {
		s.closeOnce.Do(func() {
			close(s.done)
		})
	}
	s.cmd.Process.Kill()
	s.cmd.Wait()
}

// HTTP MCP server (Streamable HTTP transport)

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

func (s *httpServer) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := s.nextRequestID()
	req := mcpRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	httpReq, err := s.newMCPRequest(ctx, s.url, "POST", body)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		return s.readSSE(ctx, resp.Body)
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

// SSE frame limits. The scanner's default 64 KiB token cap killed any larger frame with
// bufio.ErrTooLong; 10 MiB is the loud ceiling - a message beyond it is a hard error,
// never a silent truncation.
const (
	sseInitialBufferBytes = 64 * 1024
	sseMaxFrameBytes      = 10 * 1024 * 1024
)

// readSSE reads an SSE stream and returns the first JSON-RPC response. Parsing each
// data line on its own (the old shape) silently dropped every continuation line, so a
// long payload arrived cut in half.
func (s *httpServer) readSSE(ctx context.Context, body io.Reader) (json.RawMessage, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, sseInitialBufferBytes), sseMaxFrameBytes)
	var data strings.Builder
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("SSE read: %w", err)
		}
		line := scanner.Text()
		if line == "" {
			res, ok, err := parseSSEEvent(data.String())
			data.Reset()
			if err != nil {
				return nil, err
			}
			if ok {
				return res, nil
			}
			continue
		}
		value, isData := sseDataValue(line)
		if !isData {
			continue
		}
		if data.Len() > 0 {
			data.WriteByte('\n')
		}
		data.WriteString(value)
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("SSE frame exceeds the %d byte cap: %w", sseMaxFrameBytes, err)
		}
		return nil, fmt.Errorf("SSE read: %w", err)
	}
	if res, ok, err := parseSSEEvent(data.String()); err != nil {
		return nil, err
	} else if ok {
		return res, nil
	}
	return nil, fmt.Errorf("SSE stream ended without response")
}

func sseDataValue(line string) (string, bool) {
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "), true
}

func parseSSEEvent(payload string) (res json.RawMessage, ok bool, err error) {
	if payload == "" || payload == "[DONE]" {
		return nil, false, nil
	}
	var mcpResp mcpResponse
	if err := json.Unmarshal([]byte(payload), &mcpResp); err != nil {
		return nil, false, nil
	}
	if mcpResp.Error != nil {
		return nil, false, fmt.Errorf("mcp error %d: %s", mcpResp.Error.Code, mcpResp.Error.Message)
	}
	return mcpResp.Result, true, nil
}

func (s *httpServer) newMCPRequest(ctx context.Context, url, method string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	if s.sessionID != "" {
		req.Header.Set("mcp-session-id", s.sessionID)
	}
	return req, nil
}

func (s *httpServer) initialize() error {
	params := map[string]any{
		"protocolVersion": "2025-11-25",
		"clientInfo":      map[string]string{"name": "omega-mcp-bridge", "version": "0.1.0"},
		"capabilities":    map[string]any{},
	}
	id := s.nextRequestID()
	reqBody, _ := json.Marshal(mcpRequest{JSONRPC: "2.0", ID: id, Method: "initialize", Params: params})
	ctx := context.Background()
	httpReq, err := s.newMCPRequest(ctx, s.url, "POST", reqBody)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	s.sessionID = resp.Header.Get("mcp-session-id")
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		_, err = s.readSSE(ctx, resp.Body)
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
	notifBody, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	httpReq2, err := s.newMCPRequest(ctx, s.url, "POST", notifBody)
	if err != nil {
		return nil
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

func (s *httpServer) listTools(ctx context.Context) ([]mcpTool, error) {
	return mcpListTools(ctx, s)
}

func (s *httpServer) callTool(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	return mcpCallTool(ctx, s, name, args)
}

func (s *httpServer) close() {
	// HTTP servers have no process to kill.
}

type serverConfig struct {
	Name    string            `json:"name" yaml:"name"`
	Command string            `json:"command" yaml:"command"`
	Args    []string          `json:"args" yaml:"args"`
	Env     map[string]string `json:"env" yaml:"env"`
	URL     string            `json:"url" yaml:"url"`
	Headers map[string]string `json:"headers" yaml:"headers"`
	// Enabled is the per-server toggle. The JSON mirror exists for the MCP_SERVERS env
	// shape only; the toggle POST never writes there (the env var is not a file, the wire
	// route refuses it).
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

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

func mcpHome() string {
	home := os.Getenv("OMEGA_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".omega")
	}
	return home
}

func configFile() (path string, name string, ok bool) {
	home := mcpHome()
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
	// YAML is a superset of JSON, so yaml.Unmarshal handles.
	var cfg mcpConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

type toolMapping struct {
	server string
	mcp    string
}

type Bridge struct {
	servers  map[string]MCPConn
	toolMap  map[string]toolMapping
	toolDefs map[string]mcpTool
	cfg    *mcpConfig
	source string
	errs   map[string]string
	stopped   chan struct{}
	closeOnce sync.Once
}

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

// shutdown disconnects every server this bridge holds - the reload's removal step.
// In-flight calls to removed servers surface their failure as an IsError tool result
// (the run checks isStopped; stdio requests also see the closed connection), never a
// panic and never a lost response. Idempotent.
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
		// Per-server toggle: a disabled server is intentionally not connected and discovers
		// no tools - its tools never enter toolDefs, so Tools never serves them and the
		// namespace guard has nothing to front.
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
		tools, err := conn.listTools(context.Background())
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
					// the reload removed this bridge's server - the failure is a structured IsError
					// tool result (the tool-error law), never a panic
					return "", fmt.Errorf("mcp server %s was removed on reload", mapping.server)
				}
				srv := b.servers[mapping.server]
				if srv == nil {
					return "", fmt.Errorf("MCP server %s is not running", mapping.server)
				}
				content, isErr, err := srv.callTool(ctx, mapping.mcp, args)
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

type agentTool struct {
	description string
	parameters  map[string]any
	run         func(ctx context.Context, args map[string]any) (string, error)
}
