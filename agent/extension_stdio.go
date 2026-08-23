package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/EndoTheDev/omega/ai"
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

	// delegateCh receives InjectedMessage values from extensions
	// (subagent results). Created lazily when a delegate extension
	// sends a delegate_result notification.
	delegateCh chan InjectedMessage
	// pendingDelegations tracks the number of running subagent tasks.
	pendingDelegations int32
}

// stdioExt is one loaded extension process.
type stdioExt struct {
	name    string
	path    string
	cmd     *exec.Cmd
	stdin   *json.Encoder
	stdout  *bufio.Reader
	mu      sync.Mutex    // serializes writes to the process stdin
	manager *StdioManager // back-reference for delegate notifications

	tools      map[string]toolDef
	commands   []ExtensionCommand
	subscribed map[string]bool            // event types this extension wants
	seams      []string                   // declared seam types
	pending    map[int64]chan rpcResponse // pending request responses
	pendingMu  sync.Mutex
	nextID     int64
	notifyCh   chan map[string]any // notification channel for streaming (set during Stream)
	alive      bool
}

// toolDef is a tool declared by an extension during initialize.
type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// initResult is the result of the initialize JSON-RPC method.
type initResult struct {
	Name          string             `json:"name"`
	Tools         []toolDef          `json:"tools"`
	Commands      []ExtensionCommand `json:"commands"`
	Subscriptions []string           `json:"subscriptions"`
	Seams         []string           `json:"seams"`
}

// rpcRequest is a JSON-RPC 2.0 request or notification.
type rpcRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      *int64         `json:"id,omitempty"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
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

// writeTimeout is the max time to block writing to an extension's stdin.
// A stalled extension that can't accept input is logged and skipped.
const writeTimeout = 2 * time.Second

// skipExts are file extensions that are never extension executables.
// Source, config, and documentation files are skipped so a folder can
// hold both the extension binary and its source without false spawns.
var skipExts = map[string]bool{
	".md":   true,
	".txt":  true,
	".go":   true,
	".json": true,
	".yaml": true,
	".yml":  true,
	".toml": true,
}

// Load discovers and initializes extensions from dir, recursing into
// subdirectories. Files starting with "." are skipped. Files with an
// extension in skipExts are skipped. Everything else is treated as a
// candidate extension.
//
// On Windows, files without a known extension are checked for a shebang
// line to route through the right interpreter. Files with .sh extension
// are run via bash.
//
// Load may be called multiple times (e.g. once for the main dir, once
// for the project dir); each call appends to the existing set.
func (m *StdioManager) Load(dir string, apiKey string) error {
	if m.toolMap == nil {
		m.toolMap = make(map[string]Tool)
	}

	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // missing dir = zero extensions, not an error
			}
			return nil // skip unreadable entries, don't abort the walk
		}
		if d.IsDir() {
			return nil // walk into directories
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if skipExts[filepath.Ext(name)] {
			return nil
		}

		if err := m.LoadFile(path, apiKey); err != nil {
			// Non-fatal: log and skip. One bad extension does not kill the manager.
			fmt.Fprintf(os.Stderr, "omega: extension %s: %v\n", path, err)
		}
		return nil
	})
}

// LoadFile spawns and initializes a single extension at path, merging
// its tools into the manager. First registration wins on tool-name
// conflict. It is on the concrete type (not the ExtensionManager
// interface) so callers that load explicit --extension paths can use it
// before the manager is handed off as an interface.
func (m *StdioManager) LoadFile(path string, apiKey string) error {
	if m.toolMap == nil {
		m.toolMap = make(map[string]Tool)
	}

	ext, err := spawnExtension(path, apiKey, m)
	if err != nil {
		return err
	}

	m.exts = append(m.exts, ext)

	// Wrap extension tools as agent.Tool values.
	for _, t := range ext.tools {
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
	return nil
}

// spawnExtension spawns a single extension process and runs initialize.
// apiKey is passed to the extension via the OLLAMA_API_KEY env var.
func spawnExtension(path string, apiKey string, m *StdioManager) (*stdioExt, error) {
	// WalkDir returns paths relative to the working directory. On Windows,
	// exec.Command can't resolve relative paths with backslashes, so
	// convert to absolute first.
	path, _ = filepath.Abs(path)

	cmd, err := buildCommand(path)
	if err != nil {
		return nil, fmt.Errorf("build command: %w", err)
	}

	// Pass the API key to the extension via env.
	cmd.Env = append(os.Environ(), "OLLAMA_API_KEY="+apiKey)

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
		name:       filepath.Base(path),
		path:       path,
		cmd:        cmd,
		stdin:      json.NewEncoder(stdinPipe),
		stdout:     bufio.NewReader(stdoutPipe),
		manager:    m,
		tools:      make(map[string]toolDef),
		subscribed: make(map[string]bool),
		pending:    make(map[int64]chan rpcResponse),
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
		if err := validateToolSchema(t); err != nil {
			return nil, fmt.Errorf("extension %s: tool %s: %w", filepath.Base(path), t.Name, err)
		}
		ext.tools[t.Name] = t
	}

	for i, c := range init.Commands {
		if !strings.HasPrefix(c.Name, "/") {
			init.Commands[i].Name = "/" + c.Name
		}
	}
	ext.commands = append(ext.commands, init.Commands...)

	for _, sub := range init.Subscriptions {
		ext.subscribed[sub] = true
	}

	ext.seams = init.Seams
	ext.alive = true
	return ext, nil
}

// buildCommand creates an exec.Cmd for an extension file. On Windows,
// .sh files are routed through bash. Files with a shebang line use the
// declared interpreter. On Windows, if a file has no extension and a
// .exe variant exists, the .exe variant is used (exec.Command does not
// auto-append .exe for absolute paths).
func buildCommand(path string) (*exec.Cmd, error) {
	ext := filepath.Ext(path)

	// .sh files go through bash everywhere.
	if ext == ".sh" {
		return exec.Command("bash", path), nil
	}

	// On Windows, try appending .exe for extensionless binaries.
	if runtime.GOOS == "windows" && ext == "" {
		exePath := path + ".exe"
		if _, err := os.Stat(exePath); err == nil {
			path = exePath
		}
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

// readLoop reads JSON-RPC messages from the extension's stdout.
// Lines with an ID are responses routed to the waiting caller via
// the pending map. Lines without an ID are notifications routed to
// notifyCh (if set).
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

		// Parse with *int64 to distinguish notifications (no id)
		// from responses with id=0.
		var raw struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *int64          `json:"id,omitempty"`
			Method  string          `json:"method,omitempty"`
			Params  map[string]any  `json:"params,omitempty"`
			Result  json.RawMessage `json:"result,omitempty"`
			Error   *rpcError       `json:"error,omitempty"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			continue // skip malformed lines
		}

		if raw.ID == nil {
			// Notification — route to notifyCh if active, or
			// handle delegate notifications specially.
			switch raw.Method {
			case "delegate_result":
				e.manager.handleDelegateResult(raw.Params)
				continue
			case "delegate_start":
				e.manager.incrementPendingDelegations()
				continue
			}
			e.mu.Lock()
			ch := e.notifyCh
			e.mu.Unlock()
			if ch != nil {
				ch <- raw.Params
			}
			continue
		}

		// Response — route to waiting caller.
		resp := rpcResponse{
			JSONRPC: raw.JSONRPC,
			ID:      *raw.ID,
			Result:  raw.Result,
			Error:   raw.Error,
		}
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

// streamRequest sends a JSON-RPC request that triggers a stream of
// notifications back. It sets up notifyCh to receive notifications,
// sends the request, and returns the notification channel plus a
// result channel. The caller reads notifications from notifyCh until
// it's closed, then reads the final response from resultCh.
func (e *stdioExt) streamRequest(ctx context.Context, method string, params map[string]any) (<-chan map[string]any, <-chan streamResult, error) {
	e.mu.Lock()
	id := e.nextID
	e.nextID++
	e.mu.Unlock()

	respCh := make(chan rpcResponse, 1)
	e.pendingMu.Lock()
	e.pending[id] = respCh
	e.pendingMu.Unlock()

	// Set up notification channel (protected by e.mu to avoid
	// racing with readLoop's nil check).
	notifyCh := make(chan map[string]any, 64)
	e.mu.Lock()
	e.notifyCh = notifyCh
	e.mu.Unlock()

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
		e.mu.Lock()
		e.notifyCh = nil
		e.mu.Unlock()
		return nil, nil, fmt.Errorf("write: %w", err)
	}

	// Read final response in a goroutine; close notifyCh when done.
	resultCh := make(chan streamResult, 1)
	go func() {
		select {
		case resp := <-respCh:
			e.mu.Lock()
			e.notifyCh = nil
			e.mu.Unlock()
			close(notifyCh)
			if resp.Error != nil {
				resultCh <- streamResult{err: fmt.Errorf("%s", resp.Error.Message)}
			} else {
				resultCh <- streamResult{result: resp.Result}
			}
		case <-ctx.Done():
			e.pendingMu.Lock()
			delete(e.pending, id)
			e.pendingMu.Unlock()
			e.mu.Lock()
			e.notifyCh = nil
			e.mu.Unlock()
			close(notifyCh)
			resultCh <- streamResult{err: ctx.Err()}
		}
	}()

	return notifyCh, resultCh, nil
}

// streamResult carries the final response of a stream request.
type streamResult struct {
	result json.RawMessage
	err    error
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
// On timeout the extension is killed: the closed pipe unblocks Encode and
// the goroutine exits. This prevents a leaked goroutine holding the lock.
func (e *stdioExt) write(req rpcRequest) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- e.stdin.Encode(req)
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(writeTimeout):
		// Kill the process so the pipe closes and Encode unblocks.
		e.alive = false
		e.cmd.Process.Kill()
		<-done
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
		var toolList []ToolInfo
		for name, t := range ext.tools {
			toolList = append(toolList, ToolInfo{Name: name, Description: t.Description})
		}
		sort.Slice(toolList, func(i, j int) bool { return toolList[i].Name < toolList[j].Name })
		infos[i] = ExtensionInfo{
			Name:     ext.name,
			Tools:    len(ext.tools),
			Commands: len(ext.commands),
			Seams:    ext.seams,
			ToolList: toolList,
			Status:   status,
		}
	}
	return infos
}

// SeamProviders returns a map of seam type to extension name for
// extensions that declared the seam during initialize. First extension
// to declare a seam wins.
func (m *StdioManager) SeamProviders() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	providers := map[string]string{}
	for _, ext := range m.exts {
		for _, seam := range ext.seams {
			if _, exists := providers[seam]; !exists {
				providers[seam] = ext.name
			}
		}
	}
	return providers
}

// DispatchEvent sends an event to all extensions that subscribed to it.
// Non-blocking and best-effort: a write timeout or a dead extension is
// silently skipped.
func (m *StdioManager) DispatchEvent(event Event) {
	typeName, data := eventPayload(event)
	if typeName == "" {
		return
	}

	m.mu.Lock()
	exts := make([]*stdioExt, len(m.exts))
	copy(exts, m.exts)
	m.mu.Unlock()

	for _, ext := range exts {
		if !ext.alive || !ext.subscribed[typeName] {
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

// PromptGuidelines collects guideline lines from all extensions
// that implement the prompt/guidelines method.
func (m *StdioManager) PromptGuidelines() []string {
	m.mu.Lock()
	exts := make([]*stdioExt, len(m.exts))
	copy(exts, m.exts)
	m.mu.Unlock()

	var guidelines []string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, ext := range exts {
		if !ext.alive {
			continue
		}
		result, err := ext.request(ctx, "prompt/guidelines", nil)
		if err != nil {
			continue
		}
		var gr struct {
			Guidelines []string `json:"guidelines"`
		}
		if err := json.Unmarshal(result, &gr); err != nil {
			continue
		}
		guidelines = append(guidelines, gr.Guidelines...)
	}
	return guidelines
}

// CustomizeBranchSummary asks extensions for a custom branch summary.
// The first extension that returns a non-empty summary wins.
func (m *StdioManager) CustomizeBranchSummary(ctx context.Context, messages []ai.Message) (string, bool) {
	m.mu.Lock()
	exts := make([]*stdioExt, len(m.exts))
	copy(exts, m.exts)
	m.mu.Unlock()

	for _, ext := range exts {
		if !ext.alive {
			continue
		}
		result, err := ext.request(ctx, "branch/summary", map[string]any{
			"messages": messages,
		})
		if err != nil {
			continue
		}
		var cr struct {
			Summary string `json:"summary"`
			OK      bool   `json:"ok"`
		}
		if err := json.Unmarshal(result, &cr); err != nil {
			continue
		}
		if cr.OK && cr.Summary != "" {
			return cr.Summary, true
		}
	}
	return "", false
}

// BuildPrompt asks extensions to build the complete system prompt.
// The first extension that returns ok=true wins.
func (m *StdioManager) BuildPrompt(ctx context.Context, opts PromptBuildOptions) (string, bool) {
	m.mu.Lock()
	exts := make([]*stdioExt, len(m.exts))
	copy(exts, m.exts)
	m.mu.Unlock()

	for _, ext := range exts {
		if !ext.alive {
			continue
		}
		result, err := ext.request(ctx, "prompt/build", map[string]any{
			"cwd":             opts.CWD,
			"messages":        opts.Messages,
			"extensions":      opts.Extensions,
			"project_context": opts.ProjectContext,
			"custom":          opts.Custom,
			"append":          opts.Append,
		})
		if err != nil {
			continue
		}
		var cr struct {
			Prompt string `json:"prompt"`
			OK     bool   `json:"ok"`
		}
		if err := json.Unmarshal(result, &cr); err != nil {
			continue
		}
		if cr.OK {
			return cr.Prompt, true
		}
	}
	return "", false
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

// providerExt returns the extension that declared the "provider" seam,
// or nil if none exists.
func (m *StdioManager) providerExt() *stdioExt {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ext := range m.exts {
		for _, seam := range ext.seams {
			if seam == "provider" {
				return ext
			}
		}
	}
	return nil
}

// storeExt returns the extension that declared the "store" seam,
// or nil if none exists.
func (m *StdioManager) storeExt() *stdioExt {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ext := range m.exts {
		for _, seam := range ext.seams {
			if seam == "store" {
				return ext
			}
		}
	}
	return nil
}

// StoreRequest dispatches a store-seam JSON-RPC call to the store
// extension. Returns the raw JSON result.
func (m *StdioManager) StoreRequest(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	ext := m.storeExt()
	if ext == nil {
		return nil, fmt.Errorf("no store extension loaded")
	}
	return ext.request(ctx, method, params)
}

// StoreProvider returns a ProxyStore that forwards to the store-seam
// extension, or nil if no store extension is loaded.
func (m *StdioManager) StoreProvider() StoreProvider {
	ext := m.storeExt()
	if ext == nil {
		return nil
	}
	return &ProxyStore{Dispatcher: m}
}

// skillsExt returns the extension that declared the "skills" seam,
// or nil if none exists.
func (m *StdioManager) skillsExt() *stdioExt {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ext := range m.exts {
		for _, seam := range ext.seams {
			if seam == "skills" {
				return ext
			}
		}
	}
	return nil
}

// SkillsRequest dispatches a skills-seam JSON-RPC call to the skills
// extension. Returns the raw JSON result.
func (m *StdioManager) SkillsRequest(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	ext := m.skillsExt()
	if ext == nil {
		return nil, fmt.Errorf("no skills extension loaded")
	}
	return ext.request(ctx, method, params)
}

// SkillsProvider returns a ProxySkills that forwards to the skills-seam
// extension, or nil if no skills extension is loaded.
func (m *StdioManager) SkillsProvider() SkillsProvider {
	ext := m.skillsExt()
	if ext == nil {
		return nil
	}
	return &ProxySkills{Dispatcher: m}
}

// compactorExt returns the extension that declared the "compactor" seam,
// or nil if none exists.
func (m *StdioManager) compactorExt() *stdioExt {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ext := range m.exts {
		for _, seam := range ext.seams {
			if seam == "compactor" {
				return ext
			}
		}
	}
	return nil
}

// CompactorRequest dispatches a compactor-seam JSON-RPC call to the
// compactor extension. Returns the raw JSON result.
func (m *StdioManager) CompactorRequest(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	ext := m.compactorExt()
	if ext == nil {
		return nil, fmt.Errorf("no compactor extension loaded")
	}
	return ext.request(ctx, method, params)
}

// CompactorProvider returns a ProxyCompactor that forwards to the
// compactor-seam extension, or nil if no compactor extension is loaded.
func (m *StdioManager) CompactorProvider(cfg CompactionConfig) Compactor {
	ext := m.compactorExt()
	if ext == nil {
		return nil
	}
	return &ProxyCompactor{Dispatcher: m, Config: cfg}
}

// ProviderStream dispatches to the provider-seam extension to stream
// a completion. Returns nil if no provider extension is loaded.
func (m *StdioManager) ProviderStream(ctx context.Context, messages []ai.Message, tools []ai.ToolSchema) <-chan ai.StreamEvent {
	ext := m.providerExt()
	if ext == nil {
		return nil
	}

	// Serialize messages with role field (ai.Message types don't
	// include role in their JSON tags — the old providers added it
	// in messagesToAPI. The extension needs role to build API requests).
	msgJSON := messagesToJSON(messages)
	toolJSON := make([]map[string]any, len(tools))
	for i, t := range tools {
		toolJSON[i] = map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  t.Parameters,
		}
	}

	notifyCh, resultCh, err := ext.streamRequest(ctx, "provider/stream", map[string]any{
		"messages": msgJSON,
		"tools":    toolJSON,
	})
	if err != nil {
		ch := make(chan ai.StreamEvent, 1)
		ch <- ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
		close(ch)
		return ch
	}

	events := make(chan ai.StreamEvent, 64)
	go func() {
		defer close(events)
		// Drain notifications into events.
		for params := range notifyCh {
			events <- notificationToEvent(params)
		}
		// Read final response.
		if res, ok := <-resultCh; ok {
			if res.err != nil {
				events <- ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: res.err.Error()}
			} else {
				events <- resultToStreamEnd(res.result)
			}
		}
	}()

	return events
}

// messagesToJSON serializes ai.Message values to maps with a role
// field added based on the concrete message type. The timestamp field
// is dropped (not needed by the provider API).
func messagesToJSON(messages []ai.Message) []map[string]any {
	msgJSON := make([]map[string]any, len(messages))
	for i, msg := range messages {
		data, _ := json.Marshal(msg)
		var m map[string]any
		json.Unmarshal(data, &m)
		switch msg.(type) {
		case ai.System:
			m["role"] = "system"
		case ai.User:
			m["role"] = "user"
		case ai.Assistant:
			m["role"] = "assistant"
		case ai.ToolResult:
			m["role"] = "tool"
		}
		delete(m, "timestamp")
		msgJSON[i] = m
	}
	return msgJSON
}

// validateToolSchema checks that a tool's Parameters field is a valid
// JSON Schema with a type field. If required is present, each entry
// must match a property key.
func validateToolSchema(t toolDef) error {
	if t.Name == "" {
		return fmt.Errorf("tool name is empty")
	}
	if t.Parameters == nil {
		return fmt.Errorf("parameters schema is nil")
	}
	typeVal, ok := t.Parameters["type"].(string)
	if !ok || typeVal == "" {
		return fmt.Errorf("parameters schema missing 'type' field")
	}
	if req, ok := t.Parameters["required"].([]any); ok {
		props, _ := t.Parameters["properties"].(map[string]any)
		for _, r := range req {
			if name, ok := r.(string); ok {
				if props == nil {
					return fmt.Errorf("required field %q but no properties", name)
				}
				if _, exists := props[name]; !exists {
					return fmt.Errorf("required field %q not in properties", name)
				}
			}
		}
	}
	return nil
}

// notificationToEvent converts a stream_event notification params map
// to a StreamEvent.
func notificationToEvent(params map[string]any) ai.StreamEvent {
	if params == nil {
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "nil notification params"}
	}
	typeStr, _ := params["type"].(string)
	switch typeStr {
	case "response_chunk":
		content, _ := params["content"].(string)
		return ai.ResponseChunk{Type: "response_chunk", Content: content}
	case "thinking_chunk":
		content, _ := params["content"].(string)
		return ai.ThinkingChunk{Type: "thinking_chunk", Content: content}
	case "tool_call":
		tc, _ := params["tool_call"].(map[string]any)
		if tc == nil {
			return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "nil tool_call in notification"}
		}
		id, _ := tc["id"].(string)
		name, _ := tc["name"].(string)
		args, _ := tc["arguments"].(map[string]any)
		return ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: id, Name: name, Arguments: args}}
	default:
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "unknown notification type: " + typeStr}
	}
}

// resultToStreamEnd converts the final provider/stream response to a
// StreamEnd event.
func resultToStreamEnd(result json.RawMessage) ai.StreamEnd {
	var end struct {
		FinishReason    string `json:"finish_reason"`
		PromptEvalCount *int   `json:"prompt_eval_count,omitempty"`
		EvalCount       *int   `json:"eval_count,omitempty"`
		Error           string `json:"error,omitempty"`
	}
	json.Unmarshal(result, &end)
	return ai.StreamEnd{
		Type:            "stream_end",
		FinishReason:    end.FinishReason,
		PromptEvalCount: end.PromptEvalCount,
		EvalCount:       end.EvalCount,
		Error:           end.Error,
	}
}

// ProviderModelName returns the model name from the provider-seam
// extension. Returns "" if no provider extension is loaded.
func (m *StdioManager) ProviderModelName() string {
	ext := m.providerExt()
	if ext == nil {
		return ""
	}
	result, err := ext.request(context.Background(), "provider/model_name", nil)
	if err != nil {
		return ""
	}
	var name struct {
		Model string `json:"model"`
	}
	json.Unmarshal(result, &name)
	return name.Model
}

// ProviderListModels lists available models from the provider-seam
// extension. Returns an error if no provider extension is loaded.
func (m *StdioManager) ProviderListModels() ([]string, error) {
	ext := m.providerExt()
	if ext == nil {
		return nil, fmt.Errorf("no provider extension loaded")
	}
	result, err := ext.request(context.Background(), "provider/list_models", nil)
	if err != nil {
		return nil, err
	}
	var res struct {
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return nil, err
	}
	return res.Models, nil
}

// ProviderModelInfo queries the provider-seam extension for metadata
// about the current model (e.g. context window). Returns ModelInfo{}
// with zero values if the provider does not expose the info.
func (m *StdioManager) ProviderModelInfo() (ai.ModelInfo, error) {
	ext := m.providerExt()
	if ext == nil {
		return ai.ModelInfo{}, nil
	}
	result, err := ext.request(context.Background(), "provider/model_info", nil)
	if err != nil {
		return ai.ModelInfo{}, err
	}
	var res struct {
		ContextWindow int `json:"context_window"`
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return ai.ModelInfo{}, err
	}
	return ai.ModelInfo{ContextWindow: res.ContextWindow}, nil
}

// ProviderSetThinking sets the thinking level on the provider-seam
// extension. No-op if no provider extension is loaded.
func (m *StdioManager) ProviderSetThinking(level string) {
	ext := m.providerExt()
	if ext == nil {
		return
	}
	_, _ = ext.request(context.Background(), "provider/set_thinking", map[string]any{
		"level": level,
	})
}

// ProviderSetModel changes the model name on the provider-seam
// extension at runtime.
func (m *StdioManager) ProviderSetModel(model string) {
	ext := m.providerExt()
	if ext == nil {
		return
	}
	_, _ = ext.request(context.Background(), "provider/set_model", map[string]any{
		"model": model,
	})
}

// eventPayload returns the event type name and data payload for JSON
// serialization. Returns ("", nil) for events with no useful payload.
func eventPayload(e Event) (string, any) {
	switch v := e.(type) {
	case AgentStart:
		return "agent_start", map[string]any{"model_name": v.ModelName}
	case TurnStart:
		return "turn_start", map[string]any{"turn": v.Turn}
	case TurnEnd:
		return "turn_end", map[string]any{"turn": v.Turn, "tool_calls": v.ToolCalls}
	case ToolResultEvent:
		return "tool_result", map[string]any{"message": v.Message}
	case AssistantMessageEvent:
		return "assistant_message", map[string]any{"message": v.Message}
	case AgentEnd:
		return "agent_end", map[string]any{"turns": v.Turns, "finish_reason": v.FinishReason}
	case SessionEvent:
		return v.Type, map[string]any{"id": v.ID, "label": v.Label}
	default:
		return "", nil
	}
}

// handleDelegateResult processes a delegate_result notification from
// an extension. It decrements the pending count and pushes the result
// to the delegate channel.
func (m *StdioManager) handleDelegateResult(params map[string]any) {
	taskID, _ := params["task_id"].(string)
	output, _ := params["output"].(string)

	m.mu.Lock()
	if m.delegateCh == nil {
		m.delegateCh = make(chan InjectedMessage, 64)
	}
	ch := m.delegateCh
	m.mu.Unlock()

	ch <- InjectedMessage{
		Text:   output,
		Source: "delegate:" + taskID,
	}
	// Clamp at 0 to prevent desync from lost delegate_start notifications.
	for {
		old := atomic.LoadInt32(&m.pendingDelegations)
		if old <= 0 {
			break
		}
		if atomic.CompareAndSwapInt32(&m.pendingDelegations, old, old-1) {
			break
		}
	}
}

// InjectedMessages returns the channel of injected messages from
// delegate extensions. Nil if no delegate extension has been loaded.
func (m *StdioManager) InjectedMessages() <-chan InjectedMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.delegateCh
}

// PendingDelegations returns the number of running subagent tasks.
func (m *StdioManager) PendingDelegations() int {
	return int(atomic.LoadInt32(&m.pendingDelegations))
}

// incrementPendingDelegations is called by the delegate tool handler
// when a new subagent task is started.
func (m *StdioManager) incrementPendingDelegations() {
	atomic.AddInt32(&m.pendingDelegations, 1)
}
