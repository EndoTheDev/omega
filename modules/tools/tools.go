// Package tools provides the built-in shell and file tools as an
// in-process core.ToolProvider. shell.run, plus the consolidated
// files domain tool (read/write/edit under one action protocol, with
// an atomic batch operations array). The legacy files.read /
// files.write / files.edit names remain registered as alias shims.
package tools

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

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// ToolProvider implements core.ToolProvider. It holds per-path file
// locks so concurrent tool calls on the same file are serialized.
type ToolProvider struct {
	// fileLocks serializes read/write/edit on the same path.
	// ponytail: unbounded growth ceiling = distinct paths per session.
	// Upgrade path: LRU eviction of idle mutexes if path count grows
	// large, but in practice the set is bounded by working-tree size.
	fileLocks sync.Map // map[string]*sync.Mutex, keyed by abs path

	// dbGuard refuses any shell command or file path that resolves
	// into the instance database (ENDO'S LAW 2026-10-18). nil and
	// zero-value guards are pass-through.
	dbGuard *dbGuard
}

// filesActions is the structured sub-action metadata for the consolidated
// files tool: one source of truth for the schema enum, the prompt, and
// the /tools inventory.
var filesActions = []core.ToolAction{
	{Name: "read", Description: "Read a file, returning its contents with line numbers."},
	{Name: "write", Description: "Write content to a file, creating parent directories as needed."},
	{Name: "edit", Description: "Apply a targeted find-and-replace patch to a file."},
}

// filesAliasName maps a read/write/edit-style action to its legacy tool
// name (the alias shim set registered alongside the folded tool).
func filesLegacyName(action string) string {
	switch action {
	case "read":
		return "files.read"
	case "write":
		return "files.write"
	case "edit":
		return "files.edit"
	}
	return ""
}

// Tools returns the tool map keyed by tool name. The file tools are
// consolidated into one "files" domain tool: the domain description
// carries the contract (targets, atomic batch), Actions carries the
// verbs, and the legacy files.read/files.write/files.edit names stay
// registered as alias shims until the v0.12 mount migration retires
// them.
func (p *ToolProvider) Tools() map[string]core.Tool {
	tools := map[string]core.Tool{
		"shell.run": {
			Description: "Run a shell command and return its stdout and stderr.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "The command to run."},
				},
				"required": []string{"command"},
			},
			Run: p.RunShell,
		},
		"files": {
			Description: "Read, write, and edit files on disk. Writes create parent directories as needed. Batch: pass operations (write and edit ops across multiple files) for an atomic multi-file change - any failure rolls the whole batch back.",
			Actions:     filesActions,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"description": "What to do: read, write, or edit.",
					},
					"path":       map[string]any{"type": "string", "description": "Path of the file (all actions; batch ops carry their own)."},
					"offset":     map[string]any{"type": "integer", "description": "1-based first line to read (default 1)."},
					"limit":      map[string]any{"type": "integer", "description": "Max lines to read (default all)."},
					"content":    map[string]any{"type": "string", "description": "Full content to write (required for write)."},
					"old_string": map[string]any{"type": "string", "description": "Exact text to find; must occur exactly once (required for edit)."},
					"new_string": map[string]any{"type": "string", "description": "Replacement text (required for edit)."},
					"operations": map[string]any{
						"type":        "array",
						"description": "Atomic batch: array of {action, path, content?} write ops (or edit ops with old_string/new_string). Any failure rolls the whole batch back.",
						"items": map[string]any{
							"type":                 "object",
							"properties":           map[string]any{"action": map[string]any{"type": "string", "enum": fileActionNames()}, "path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}, "old_string": map[string]any{"type": "string"}, "new_string": map[string]any{"type": "string"}},
							"required":             []string{"action", "path"},
							"additionalProperties": false,
						},
					},
				},
				"required": []string{"action"},
			},
			Run: p.RunFiles,
		},
	}
	for name, alias := range p.fileAliasTools() {
		if _, exists := tools[name]; !exists {
			tools[name] = alias
		}
	}
	return tools
}

// fileAliasTools builds the legacy-name compat shims: each maps its old
// tool name onto the folded files tool with the action pre-set. They
// stay callable by the model until the v0.12 mount migration retires
// them and are hidden from the prompt and /tools inventory by
// the AliasOf marker.
func (p *ToolProvider) fileAliasTools() map[string]core.Tool {
	out := make(map[string]core.Tool, len(filesActions))
	for _, a := range filesActions {
		legacy := filesLegacyName(a.Name)
		if legacy == "" {
			continue
		}
		action := a.Name
		out[legacy] = core.Tool{
			Description: fmt.Sprintf("%s (alias for files with action %q)", a.Description, action),
			AliasOf:     "files",
			Run: func(ctx context.Context, args map[string]any) (string, error) {
				args["action"] = action
				return p.RunFiles(ctx, args)
			},
		}
	}
	return out
}

// fileActionNames returns the action names from the metadata.
func fileActionNames() []string {
	names := make([]string, len(filesActions))
	for i, a := range filesActions {
		names[i] = a.Name
	}
	return names
}

// resolveToolPath roots a tool path argument at the run's session
// workspace (card t_9de5b009). A session-rooted run resolves RELATIVE
// paths against that folder; absolute paths pass through untouched. No
// workspace (or an absolute path) is exactly today's behavior - the
// path is returned as-is and the process working directory applies.
func resolveToolPath(ctx context.Context, path string) string {
	dir := core.RunWorkspace(ctx)
	if dir == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// RunFiles dispatches the consolidated files tool: a single operation
// (action read/write/edit) or an atomic batch via operations. The
// action enum itself is derived server-side from filesActions (see
// Tool.SchemaParameters); here we only route.
func (p *ToolProvider) RunFiles(ctx context.Context, args map[string]any) (string, error) {
	if opsRaw, ok := args["operations"]; ok && opsRaw != nil {
		ops, err := parseFileOperations(opsRaw)
		if err != nil {
			return "", err
		}
		return p.runFileBatch(ctx, ops)
	}
	if v, ok := args["path"].(string); ok {
		args["path"] = resolveToolPath(ctx, v)
	}
	action, _ := args["action"].(string)
	switch action {
	case "read":
		return p.RunReadFile(ctx, args)
	case "write":
		return p.RunWriteFile(ctx, args)
	case "edit":
		return p.RunEdit(ctx, args)
	case "":
		return "", fmt.Errorf("action is required: one of read, write, edit, or pass operations for an atomic batch")
	default:
		return "", fmt.Errorf("unknown action %q; must be one of read, write, edit", action)
	}
}

// fileOp is one operation in a files batch.
type fileOp struct {
	Action    string `json:"action"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// parseFileOperations converts the raw interface{} from tool args into
// a slice of fileOp.
func parseFileOperations(raw any) ([]fileOp, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid operations: %v", err)
	}
	var ops []fileOp
	if err := json.Unmarshal(data, &ops); err != nil {
		return nil, fmt.Errorf("invalid operations: %v", err)
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("operations array is empty")
	}
	for i, op := range ops {
		if op.Path == "" {
			return nil, fmt.Errorf("operations[%d]: path is required", i)
		}
		if op.Action != "write" && op.Action != "edit" {
			return nil, fmt.Errorf("operations[%d]: batch actions are write and edit, got %q", i, op.Action)
		}
	}
	return ops, nil
}

// runFileBatch applies a multi-file operation list atomically. Every
// target path is snapshotted (current bytes, or absent) before any
// write; if any operation fails, every snapshot is restored - a
// half-applied multi-file change is impossible. Each file surface is a
// plain file, not a DB, so snapshot/restore is the rollback.
func (p *ToolProvider) runFileBatch(ctx context.Context, ops []fileOp) (string, error) {
	// Root every op at the run's session workspace FIRST (card
	// t_9de5b009): the snapshot, the dispatch and the error text all
	// see the same rooted path, and absolute op paths pass through.
	for i := range ops {
		ops[i].Path = resolveToolPath(ctx, ops[i].Path)
	}
	snap := make(map[string][]byte, len(ops)) // abs path -> bytes; absent from map = file did not exist
	existed := make(map[string]bool, len(ops))
	modes := make(map[string]os.FileMode, len(ops)) // abs path -> original mode, for rollback
	for _, op := range ops {
		abs, err := filepath.Abs(op.Path)
		if err != nil {
			abs = op.Path
		}
		if _, seen := existed[abs]; seen {
			continue
		}
		data, err := os.ReadFile(abs)
		existed[abs] = err == nil
		snap[abs] = data
		if fi, statErr := os.Stat(abs); statErr == nil {
			modes[abs] = fi.Mode().Perm()
		} else {
			modes[abs] = 0o644
		}
	}

	var out strings.Builder
	for i, op := range ops {
		var summary string
		var err error
		switch op.Action {
		case "write":
			summary, err = p.RunWriteFile(ctx, opArgs(op))
		case "edit":
			summary, err = p.RunEdit(ctx, opArgs(op))
		}
		if err == nil {
			fmt.Fprintf(&out, "%d: %s\n", i+1, summary)
			continue
		}
		// Roll back everything touched.
		restoreFiles(snap, existed, modes)
		return "", fmt.Errorf("batch failed at operations[%d] (%s %s): %v (rolled back)", i, op.Action, op.Path, err)
	}
	out.WriteString(fmt.Sprintf("%d operations applied atomically", len(ops)))
	return out.String(), nil
}

// opArgs converts a fileOp back into the tool-args map its handlers read.
func opArgs(op fileOp) map[string]any {
	args := map[string]any{"path": op.Path}
	switch op.Action {
	case "write":
		args["content"] = op.Content
	case "edit":
		args["old_string"] = op.OldString
		args["new_string"] = op.NewString
	}
	return args
}

// restoreFiles writes every snapshot back and removes files that did
// not exist before the batch. Writes are atomic (temp + rename) with
// each file's ORIGINAL mode - a rollback must not truncate a
// half-written file in place or chmod a 0600 file world-readable.
func restoreFiles(snap map[string][]byte, existed map[string]bool, modes map[string]os.FileMode) {
	for abs, data := range snap {
		if !existed[abs] {
			os.Remove(abs) //nolint:errcheck // rollback is best effort
			continue
		}
		mode, ok := modes[abs]
		if !ok {
			mode = 0o644
		}
		if err := agent.WriteFileAtomic(abs, data, mode); err != nil {
			// Keep going: restoring the remaining files matters more
			// than failing loudly on one.
			continue
		}
	}
}

// fileMutex returns a per-path mutex, creating one if needed.
func (p *ToolProvider) fileMutex(path string) *sync.Mutex {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	v, _ := p.fileLocks.LoadOrStore(abs, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// argString extracts a required string argument.
func argString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", key, v)
	}
	return s, nil
}

// shellOutputCap bounds how much of a command's combined output
// buffers in memory: CombinedOutput buffered EVERYTHING before the
// agent loop's MaxToolOutput truncation ever ran, so 'cat 500MB' or a
// forever-streaming command ballooned omega's RSS into an OOM that
// killed the server holding the store.
const shellOutputCap = 1 << 20 // 1 MB

// cappedBuffer accepts all writes but keeps at most shellOutputCap
// bytes, counting the overflow for an honest truncation note.
type cappedBuffer struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	over int64
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := shellOutputCap - b.buf.Len(); room > 0 {
		if len(p) <= room {
			b.buf.Write(p) //nolint:errcheck // bytes.Buffer never errors
		} else {
			b.buf.Write(p[:room]) //nolint:errcheck // bytes.Buffer never errors
		}
	}
	b.over += int64(len(p))
	return len(p), nil
}

// RunShell executes a shell command and returns combined output. The
// command runs under the call ctx: a cancelled ctx (the detached run's
// wall-clock cap) kills the process - without this, a command that
// never returns hangs the tool loop past any run timeout. Output is
// capped at shellOutputCap bytes (cappedBuffer), not buffered whole.
func (p *ToolProvider) RunShell(ctx context.Context, args map[string]any) (string, error) {
	command, err := argString(args, "command")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command must not be empty")
	}
	if p.dbGuard.denyShell(command) {
		return "", dbDenied(command)
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
		shellKillSetup(cmd)
	}
	// A session-rooted run executes INSIDE its workspace (card
	// t_9de5b009): relative commands (ls, grep, go test...) operate on
	// the session's folder. No workspace = the process working
	// directory, exactly as before.
	cmd.Dir = core.RunWorkspace(ctx)
	outBuf := &cappedBuffer{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("command failed: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return shellOutput(outBuf), fmt.Errorf("command failed: %w", err)
	}
	return shellOutput(outBuf), nil
}

// shellOutput renders a capped buffer: the kept bytes plus an honest
// total-count note when output was dropped.
func shellOutput(b *cappedBuffer) string {
	out := b.buf.String()
	if b.over > 0 {
		out += fmt.Sprintf("\n... [output truncated, %d bytes total]", b.over+int64(b.buf.Len()))
	}
	return out
}

// RunReadFile reads a file with line numbers, honoring offset/limit.
func (p *ToolProvider) RunReadFile(ctx context.Context, args map[string]any) (string, error) {
	// Session workspace rooting (card t_9de5b009): the alias shims and
	// the fold dispatch root the path before this handler reads it -
	// resolve again for the direct-call safety (idempotent when
	// absolute).
	if v, ok := args["path"].(string); ok {
		args["path"] = resolveToolPath(ctx, v)
	}
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	if p.dbGuard.denyFile(path) {
		return "", dbDenied(path)
	}

	mu := p.fileMutex(path)
	mu.Lock()
	defer mu.Unlock()

	offset := 1
	if v, ok := args["offset"]; ok {
		f, ok := v.(float64)
		if !ok {
			return "", fmt.Errorf("argument %q must be an integer", "offset")
		}
		offset = int(f)
		if offset < 1 {
			return "", fmt.Errorf("offset must be >= 1")
		}
	}
	limit := 0
	if v, ok := args["limit"]; ok {
		f, ok := v.(float64)
		if !ok {
			return "", fmt.Errorf("argument %q must be an integer", "limit")
		}
		limit = int(f)
		if limit < 0 {
			return "", fmt.Errorf("limit must be >= 0")
		}
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	var out strings.Builder
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		if line < offset {
			continue
		}
		if limit > 0 && line >= offset+limit {
			break
		}
		fmt.Fprintf(&out, "%d|%s\n", line, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return out.String(), nil
}

// RunWriteFile writes content to a file, creating parent dirs.
func (p *ToolProvider) RunWriteFile(ctx context.Context, args map[string]any) (string, error) {
	if v, ok := args["path"].(string); ok {
		args["path"] = resolveToolPath(ctx, v)
	}
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	content, err := argString(args, "content")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	if p.dbGuard.denyFile(path) {
		return "", dbDenied(path)
	}

	mu := p.fileMutex(path)
	mu.Lock()
	defer mu.Unlock()

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	if err := agent.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

// RunEdit applies a find-and-replace patch to a file.
func (p *ToolProvider) RunEdit(ctx context.Context, args map[string]any) (string, error) {
	if v, ok := args["path"].(string); ok {
		args["path"] = resolveToolPath(ctx, v)
	}
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	oldString, err := argString(args, "old_string")
	if err != nil {
		return "", err
	}
	newString, err := argString(args, "new_string")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	if p.dbGuard.denyFile(path) {
		return "", dbDenied(path)
	}
	if oldString == "" {
		return "", fmt.Errorf("old_string must not be empty")
	}

	mu := p.fileMutex(path)
	mu.Lock()
	defer mu.Unlock()

	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	text := string(content)

	count := strings.Count(text, oldString)
	if count == 0 {
		return "", fmt.Errorf("old_string not found in %s", path)
	}
	if count > 1 {
		return "", fmt.Errorf("old_string occurs %d times in %s; it must be unique", count, path)
	}

	patched := strings.Replace(text, oldString, newString, 1)
	if err := agent.WriteFileAtomic(path, []byte(patched), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}

	summary := diffSummary(oldString, newString)
	return fmt.Sprintf("patched %s\n%s", path, summary), nil
}

// diffSummary produces a simple +/- diff of old vs new string.
func diffSummary(oldString, newString string) string {
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(oldString, "\n"), "\n") {
		fmt.Fprintf(&out, "-%s\n", line)
	}
	for _, line := range strings.Split(strings.TrimSuffix(newString, "\n"), "\n") {
		fmt.Fprintf(&out, "+%s\n", line)
	}
	return strings.TrimSuffix(out.String(), "\n")
}
