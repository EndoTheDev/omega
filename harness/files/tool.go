// Package files owns the file domain, all of it (the shell-ring shrink
// wave 1, ADR Workshop/omega-shell-ring-shrink.md): the dotted file
// wire tools (files.read / files.write / files.edit, plus files.batch
// for the atomic multi-file change - the registry unification B
// ruling, t_d8fbad57, which closed the files-vs-files.* collision
// class by retiring the flat action-protocol tool and its alias shims;
// files.grep + files.glob ride the tools capsule) and the webui
// explorer's REST face (/files/browse, /files/read, /files/raw,
// /files/write, /files/mkdir - the root-resolution law rides the same
// capsule).
//
// The instance-database deny law (ENDO'S LAW 2026-10-18) is the db
// family's own (harness/datadir's Guard); the file tool's paths and
// the shell tool's commands consult it, the tool layer refuses the
// accidental db-touch class.
package files

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// FileProvider serves the dotted files tools as an additive
// tools-seam contributor. It holds per-path file locks so concurrent
// tool calls on the same file are serialized, and the db-door guard
// every file path must pass.
type FileProvider struct {
	// fileLocks serializes read/write/edit on the same path.
	// ponytail: unbounded growth ceiling = distinct paths per session.
	// Upgrade path: LRU eviction of idle mutexes if path count grows
	// large, but in practice the set is bounded by working-tree size.
	fileLocks sync.Map // map[string]*sync.Mutex, keyed by abs path

	// guard refuses any file path that resolves into the instance
	// database (ENDO'S LAW 2026-10-18). nil and zero-value guards are
	// pass-through.
	guard *datadir.Guard
}

// Tools returns the dotted file wire: one tool per verb plus
// files.batch for the atomic multi-file change (the registry
// unification B ruling, t_d8fbad57 - the flat action-protocol `files`
// tool and its alias shims are gone; the names files.read/write/edit
// survive as REAL tools, each schema carrying ONLY its own args, no
// action property anywhere). files.grep + files.glob already ride the
// tools capsule as dotted tools - this closes the files-vs-files.*
// collision class permanently.
func (p *FileProvider) Tools() map[string]core.Tool {
	return map[string]core.Tool{
		"files.read": {
			Description: "Read a file, returning its contents with line numbers.",
			Parameters: object(map[string]any{
				"path":   prop("string", "Path of the file."),
				"offset": prop("integer", "1-based first line to read (default 1)."),
				"limit":  prop("integer", "Max lines to read (default all)."),
			}, "path"),
			Run: p.RunReadFile,
		},
		"files.write": {
			Description: "Write content to a file, creating parent directories as needed.",
			Parameters: object(map[string]any{
				"path":    prop("string", "Path of the file."),
				"content": prop("string", "Full content to write."),
			}, "path", "content"),
			Run: p.RunWriteFile,
		},
		"files.edit": {
			Description: "Apply a targeted find-and-replace patch to a file.",
			Parameters: object(map[string]any{
				"path":       prop("string", "Path of the file."),
				"old_string": prop("string", "Exact text to find; must occur exactly once."),
				"new_string": prop("string", "Replacement text."),
			}, "path", "old_string", "new_string"),
			Run: p.RunEdit,
		},
		"files.batch": {
			Description: "Atomic multi-file change: array of {action, path, content?} write ops (or edit ops with old_string/new_string). Any failure rolls the whole batch back - no half-applied change.",
			Parameters: object(map[string]any{
				"operations": map[string]any{
					"type":        "array",
					"description": "Array of {action, path, content?} write ops (or edit ops with old_string/new_string). Any failure rolls the whole batch back.",
					"items": map[string]any{
						"type":                 "object",
						"properties":           map[string]any{"action": map[string]any{"type": "string", "enum": fileBatchVerbs}, "path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}, "old_string": map[string]any{"type": "string"}, "new_string": map[string]any{"type": "string"}},
						"required":             []string{"action", "path"},
						"additionalProperties": false,
					},
				},
			}, "operations"),
			Run: p.runBatchTool,
		},
	}
}

// fileBatchVerbs is the batch contract's per-op action enum: the
// operations array admits write and edit (a read op inside a batch
// is not a change - the flat tool refused it and so does the batch).
var fileBatchVerbs = []string{"write", "edit"}

// prop assembles one typed schema property.
func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

// object assembles the tool's JSON object schema. required names the
// args THIS verb cannot run without - its own list, never another
// verb's.
func object(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// runBatchTool is files.batch's Run: parse the operations array (its
// own per-op action contract), then hand it to the atomic batch
// engine.
func (p *FileProvider) runBatchTool(ctx context.Context, args map[string]any) (string, error) {
	ops, err := parseFileOperations(args["operations"])
	if err != nil {
		return "", err
	}
	return p.runFileBatch(ctx, ops)
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
func (p *FileProvider) runFileBatch(ctx context.Context, ops []fileOp) (string, error) {
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
			_ = os.Remove(abs) // rollback is best effort
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
func (p *FileProvider) fileMutex(path string) *sync.Mutex {
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

// argOptionalInt reads an optional integer argument (absent or null =
// not present). JSON numbers arrive as float64; a numeric string is
// also accepted - the legacy alias shims went schema-less for a while
// and models still emit stringified integers without a schema
// (card t_b9f74bea, live repro 2026-11-07: the wire carried
// "limit":"5"). A non-numeric token is an error, worded honestly.
func argOptionalInt(args map[string]any, key string) (int, bool, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false, nil
	}
	f, isNum := v.(float64)
	if !isNum {
		// Internal Go callers pass int/int64; the tool wire passes
		// float64; mistyped models pass strings. All of them parse.
		switch n := v.(type) {
		case int:
			return n, true, nil
		case int64:
			return int(n), true, nil
		default:
			s, isStr := v.(string)
			if !isStr {
				return 0, false, fmt.Errorf("argument %q must be an integer", key)
			}
			parsed, parseErr := strconv.Atoi(strings.TrimSpace(s))
			if parseErr != nil {
				return 0, false, fmt.Errorf("argument %q must be an integer, got %q", key, s)
			}
			f = float64(parsed)
		}
	}
	if f != float64(int(f)) {
		return 0, false, fmt.Errorf("argument %q must be an integer", key)
	}
	return int(f), true, nil
}

// RunReadFile reads a file with line numbers, honoring offset/limit.
func (p *FileProvider) RunReadFile(ctx context.Context, args map[string]any) (string, error) {
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
	if p.guard.DenyFile(path) {
		return "", datadir.DBDenied(path)
	}

	mu := p.fileMutex(path)
	mu.Lock()
	defer mu.Unlock()

	offset, hasOffset, err0 := argOptionalInt(args, "offset")
	if err0 != nil {
		return "", err0
	}
	if hasOffset && offset < 1 {
		return "", fmt.Errorf("offset must be >= 1")
	}
	if !hasOffset {
		offset = 1
	}
	limit, hasLimit, err1 := argOptionalInt(args, "limit")
	if err1 != nil {
		return "", err1
	}
	if hasLimit && limit < 0 {
		return "", fmt.Errorf("limit must be >= 0")
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
func (p *FileProvider) RunWriteFile(ctx context.Context, args map[string]any) (string, error) {
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
	if p.guard.DenyFile(path) {
		return "", datadir.DBDenied(path)
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
func (p *FileProvider) RunEdit(ctx context.Context, args map[string]any) (string, error) {
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
	if p.guard.DenyFile(path) {
		return "", datadir.DBDenied(path)
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
