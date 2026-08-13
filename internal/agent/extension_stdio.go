package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// StdioManager is an ExtensionManager that runs each extension as a
// separate process, communicating via JSON-RPC over stdin/stdout.
//
// Extensions are discovered as executable files in a directory. Each
// extension is spawned, sent an initialize request, and kept alive for
// the session. Events are dispatched as JSON-RPC notifications (no
// response expected). Tool and command calls are JSON-RPC requests with
// a response expected.
//
// Error isolation: each extension is a separate process. A crash in one
// extension does not affect others or the host. A stalled extension
// cannot block the agent loop because DispatchEvent uses a write
// timeout and tool/command calls use a caller-supplied context.
type StdioManager struct {
	mu      sync.Mutex
	exts    []*stdioExt
	closed  bool
	toolMap map[string]Tool
}

// stdioExt is one loaded extension process.
type stdioExt struct {
	name   string
	path   string
	cmd    *exec.Cmd
	stdin  *json.Encoder
	stdout *bufio.Reader
	mu     sync.Mutex // serializes writes to the process stdin

	tools       map[string]toolDef
	commands    []ExtensionCommand
	subscriptns map[string]bool // event types this extension wants
	pending     map[int64]chan rpcResponse // pending request responses
	pendingMu   sync.Mutex
	nextID      int64
	alive       bool
}

// toolDef is a tool declared by an extension during initialize.
type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// commandDef is a slash command declared by an extension.
type commandDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// initResult is the result of the initialize JSON-RPC method.
type initResult struct {
	Name          string        `json:"name"`
	Tools         []toolDef     `json:"tools"`
	Commands      []commandDef  `json:"commands"`
	Subscriptions []string      `json:"subscriptions"`
}

// rpcRequest is a JSON-RPC 2.0 request or notification.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  map[string]any  `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// toolCallResult is the result of a tool_call JSON-RPC method.
type toolCallResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// commandResult is the result of a command JSON-RPC method.
type commandResult struct {
	Output string `json:"output"`
}

// eventParams is the params for the event JSON-RPC notification.
type eventParams struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// writeTimeout is the max time to block writing to an extension's stdin.
// A stalled extension that can't accept input is logged and skipped.
const writeTimeout = 2 * time.Second

// Load discovers and initializes extensions from dir. Files starting
// with "." are skipped. Files ending in ".md" or ".txt" are skipped
// (documentation). Everything else is treated as a candidate extension.
//
// On Windows, files without a known extension are checked for a shebang
// line to route through the right interpreter. Files with .sh extension
// are run via bash.
func (m *StdioManager) Load(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // missing dir = zero extensions, not an error
		}
		return fmt.Errorf("read extensions dir: %w", err)
	}

	m.toolMap = make(map[string]Tool)

	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".md") || strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		ext, err := spawnExtension(path)
		if err != nil {
			// Non-fatal: log and skip. One bad extension does not kill the manager.
			fmt.Fprintf(os.Stderr, "omega: extension %s: %v\n", entry.Name(), err)
			continue
		}

		m.exts = append(m.exts, ext)

		// Wrap extension tools as agent.Tool values.
		for _, t := range ext.tools {
			t := t // capture for closure
			if _, exists := m.toolMap[t.Name]; exists {
				continue // first registration wins
			}
			m.toolMap[t.Name] = Tool{
				Description: t.Description,
				Parameters:  t.Parameters,
				Run: func(ctx context.Context, args map[string]any) (string, error) {
					return ext.callTool(ctx, t.Name, args)
				},
			}
		}
	}

	return nil
}

// spawnExtension spawns a single extension process and runs initialize.
func spawnExtension(path string) (*stdioExt, error) {
	cmd, err := buildCommand(path)
	if err != nil {
		return nil, fmt.Errorf("build command: %w", err)
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		stdinPipe.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr // extension stderr goes to host stderr

	if err := cmd.Start(); err != nil {
		stdinPipe.Close()
		return nil, fmt.Errorf("start: %w", err)
	}

	ext := &stdioExt{
		name:        filepath.Base(path),
		path:        path,
		cmd:         cmd,
		stdin:       json.NewEncoder(stdinPipe),
		stdout:      bufio.NewReader(stdoutPipe),
		tools:       make(map[string]toolDef),
		subscriptns: make(map[string]bool),
		pending:     make(map[int64]chan rpcResponse),
	}

	// Start the response reader goroutine.
	go ext.readLoop()

	// Send initialize and wait for the response.
	result, err := ext.request(context.Background(), "initialize", map[string]any{
		"protocol": ExtensionProtocolVersion,
	})
	if err != nil {
		ext.kill()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	var init initResult
	if err := json.Unmarshal(result, &init); err != nil {
		ext.kill()
		return nil, fmt.Errorf("parse initialize result: %w", err)
	}

	ext.name = init.Name
	if ext.name == "" {
		ext.name = filepath.Base(path)
	}

	for _, t := range init.Tools {
		ext.tools[t.Name] = t
	}

	for _, c := range init.Commands {
		name := c.Name
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		ext.commands = append(ext.commands, ExtensionCommand{
			Name:        name,
			Description: c.Description,
		})
	}

	for _, sub := range init.Subscriptions {
		ext.subscriptns[sub] = true
	}

	ext.alive = true
	return ext, nil
}

// buildCommand creates an exec.Cmd for an extension file. On Windows,
// .sh files are routed through bash. Files with a shebang line use the
// declared interpreter.
func buildCommand(path string) (*exec.Cmd, error) {
	ext := filepath.Ext(path)

	// .sh files go through bash everywhere.
	if ext == ".sh" {
		if runtime.GOOS == "windows" {
			return exec.Command("bash", path), nil
		}
		return exec.Command("bash", path), nil
	}

	// Check for shebang line.
	f, err := os.Open(path)
	if err != nil {
		// Can't open — try direct execution.
		return exec.Command(path), nil
	}
	br := bufio.NewReader(f)
	firstLine, _ := br.ReadString('\n')
	f.Close()

	if strings.HasPrefix(firstLine, "#!") {
		interpreter := strings.TrimSpace(firstLine[2:])
		parts := strings.Fields(interpreter)
		if len(parts) > 0 {
			return exec.Command(parts[0], append(parts[1:], path)...), nil
		}
	}

	// No shebang — try direct execution.
	return exec.Command(path), nil
}

// readLoop reads JSON-RPC responses from the extension's stdout and
// routes them to the waiting caller via the pending map.
func (e *stdioExt) readLoop() {
	for {
		line, err := e.stdout.ReadBytes('\n')
		if err != nil {
			// Process exited or pipe closed.
			e.failPending(err)
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // skip malformed lines
		}

		// Route to waiting caller.
		e.pendingMu.Lock()
		ch, ok := e.pending[resp.ID]
		if ok {
			delete(e.pending, resp.ID)
		}
		e.pendingMu.Unlock()
		if ok {
			ch <- resp
		}
	}
}

// failPending unblocks all waiting callers with the given error.
func (e *stdioExt) failPending(err error) {
	e.pendingMu.Lock()
	for id, ch := range e.pending {
		delete(e.pending, id)
		ch <- rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error: &rpcError{
				Code:    -32000,
				Message: fmt.Sprintf("extension process error: %v", err),
			},
		}
	}
	e.pendingMu.Unlock()
}

// request sends a JSON-RPC request and waits for the response.
func (e *stdioExt) request(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	e.mu.Lock()
	id := e.nextID
	e.nextID++
	e.mu.Unlock()

	respCh := make(chan rpcResponse, 1)
	e.pendingMu.Lock()
	e.pending[id] = respCh
	e.pendingMu.Unlock()

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
		Params:  params,
	}

	if err := e.write(req); err != nil {
		e.pendingMu.Lock()
		delete(e.pending, id)
		e.pendingMu.Unlock()
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, fmt.Errorf("%s", resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		e.pendingMu.Lock()
		delete(e.pending, id)
		e.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// notify sends a JSON-RPC notification (no ID, no response expected).
func (e *stdioExt) notify(method string, params map[string]any) error {
	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return e.write(req)
}

// write sends a JSON-RPC message to the extension's stdin with a timeout.
func (e *stdioExt) write(req rpcRequest) error {
	done := make(chan error, 1)
	go func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		done <- e.stdin.Encode(req)
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(writeTimeout):
		return fmt.Errorf("write timeout after %s", writeTimeout)
	}
}

// callTool invokes a tool on this extension.
func (e *stdioExt) callTool(ctx context.Context, toolName string, args map[string]any) (string, error) {
	result, err := e.request(ctx, "tool_call", map[string]any{
		"tool": toolName,
		"args": args,
	})
	if err != nil {
		return "", err
	}

	var tcr toolCallResult
	if err := json.Unmarshal(result, &tcr); err != nil {
		return "", fmt.Errorf("parse tool_call result: %w", err)
	}

	if tcr.IsError {
		return tcr.Content, fmt.Errorf("%s", tcr.Content)
	}
	return tcr.Content, nil
}

// Tools returns the merged tool map from all extensions.
func (m *StdioManager) Tools() map[string]Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.toolMap
}

// Commands returns all extension-provided slash commands.
func (m *StdioManager) Commands() []ExtensionCommand {
	m.mu.Lock()
	defer m.mu.Unlock()
	var cmds []ExtensionCommand
	for _, ext := range m.exts {
		cmds = append(cmds, ext.commands...)
	}
	return cmds
}

// Infos returns metadata about each loaded extension.
func (m *StdioManager) Infos() []ExtensionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	infos := make([]ExtensionInfo, len(m.exts))
	for i, ext := range m.exts {
		status := "running"
		if !ext.alive {
			status = "stopped"
		}
		infos[i] = ExtensionInfo{
			Name:     ext.name,
			Tools:    len(ext.tools),
			Commands: len(ext.commands),
			Status:   status,
		}
	}
	return infos
}

// DispatchEvent sends an event to all extensions that subscribed to it.
// Non-blocking and best-effort: a write timeout or a dead extension is
// silently skipped.
func (m *StdioManager) DispatchEvent(event Event) {
	typeName := eventType(event)
	if typeName == "" {
		return
	}

	data := eventData(event)

	m.mu.Lock()
	exts := make([]*stdioExt, len(m.exts))
	copy(exts, m.exts)
	m.mu.Unlock()

	for _, ext := range exts {
		if !ext.alive || !ext.subscriptns[typeName] {
			continue
		}
		// Best-effort: a failed notification is logged and skipped.
		if err := ext.notify("event", map[string]any{
			"type": typeName,
			"data": data,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "omega: extension %s: event %s: %v\n", ext.name, typeName, err)
		}
	}
}

// CallCommand invokes an extension-provided slash command.
func (m *StdioManager) CallCommand(ctx context.Context, name string, args string) (string, error) {
	m.mu.Lock()
	exts := make([]*stdioExt, len(m.exts))
	copy(exts, m.exts)
	m.mu.Unlock()

	for _, ext := range exts {
		for _, cmd := range ext.commands {
			if cmd.Name == name {
				result, err := ext.request(ctx, "command", map[string]any{
					"name": name,
					"args": args,
				})
				if err != nil {
					return "", err
				}
				var cr commandResult
				if err := json.Unmarshal(result, &cr); err != nil {
					return "", fmt.Errorf("parse command result: %w", err)
				}
				return cr.Output, nil
			}
		}
	}
	return "", fmt.Errorf("extension command %q not found", name)
}

// Close shuts down all extension processes.
func (m *StdioManager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	exts := m.exts
	m.mu.Unlock()

	for _, ext := range exts {
		ext.kill()
	}
	return nil
}

// kill sends a shutdown notification and kills the process.
func (e *stdioExt) kill() {
	if !e.alive {
		return
	}
	e.alive = false
	// Best-effort shutdown notification.
	_ = e.notify("shutdown", nil)
	e.cmd.Process.Kill()
	e.cmd.Wait()
}

// eventType returns the event type string for an Event.
func eventType(e Event) string {
	switch e.(type) {
	case AgentStart:
		return "agent_start"
	case TurnStart:
		return "turn_start"
	case TurnEnd:
		return "turn_end"
	case ToolResultEvent:
		return "tool_result"
	case AgentEnd:
		return "agent_end"
	default:
		return ""
	}
}

// eventData returns event data for JSON serialization. Returns nil for
// events with no useful payload beyond the type.
func eventData(e Event) any {
	switch v := e.(type) {
	case AgentStart:
		return map[string]any{"model_name": v.ModelName}
	case TurnStart:
		return map[string]any{"turn": v.Turn}
	case TurnEnd:
		return map[string]any{"turn": v.Turn, "tool_calls": v.ToolCalls}
	case ToolResultEvent:
		return map[string]any{"message": v.Message}
	case AgentEnd:
		return map[string]any{"turns": v.Turns, "finish_reason": v.FinishReason}
	default:
		return nil
	}
}