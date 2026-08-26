// Package tools provides the built-in shell and file tools as an
// in-process agent.ToolProvider. It implements shell.run, files.read,
// files.write, and files.edit.
package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/agent"
)

// ToolProvider implements agent.ToolProvider. It holds per-path file
// locks so concurrent tool calls on the same file are serialized.
type ToolProvider struct {
	// fileLocks serializes read/write/edit on the same path.
	// ponytail: unbounded growth ceiling = distinct paths per session.
	// Upgrade path: LRU eviction of idle mutexes if path count grows
	// large, but in practice the set is bounded by working-tree size.
	fileLocks sync.Map // map[string]*sync.Mutex, keyed by abs path
}

// Tools returns the tool map keyed by tool name.
func (p *ToolProvider) Tools() map[string]agent.Tool {
	return map[string]agent.Tool{
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
		"files.read": {
			Description: "Read a file, returning its contents with line numbers.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "Path to the file to read."},
					"offset": map[string]any{"type": "integer", "description": "1-based first line to read (default 1)."},
					"limit":  map[string]any{"type": "integer", "description": "Max lines to read (default all)."},
				},
				"required": []string{"path"},
			},
			Run: p.RunReadFile,
		},
		"files.write": {
			Description: "Write content to a file, creating parent directories as needed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Path of the file to write."},
					"content": map[string]any{"type": "string", "description": "Full content to write."},
				},
				"required": []string{"path", "content"},
			},
			Run: p.RunWriteFile,
		},
		"files.edit": {
			Description: "Apply a targeted find-and-replace patch to a file.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":       map[string]any{"type": "string", "description": "Path of the file to edit."},
					"old_string": map[string]any{"type": "string", "description": "Exact text to find; must occur exactly once."},
					"new_string": map[string]any{"type": "string", "description": "Replacement text."},
				},
				"required": []string{"path", "old_string", "new_string"},
			},
			Run: p.RunEdit,
		},
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

// RunShell executes a shell command and returns combined output.
func (p *ToolProvider) RunShell(_ context.Context, args map[string]any) (string, error) {
	command, err := argString(args, "command")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command must not be empty")
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("command failed: %w", err)
	}
	return string(out), nil
}

// RunReadFile reads a file with line numbers, honoring offset/limit.
func (p *ToolProvider) RunReadFile(_ context.Context, args map[string]any) (string, error) {
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path must not be empty")
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
func (p *ToolProvider) RunWriteFile(_ context.Context, args map[string]any) (string, error) {
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

	mu := p.fileMutex(path)
	mu.Lock()
	defer mu.Unlock()

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

// RunEdit applies a find-and-replace patch to a file.
func (p *ToolProvider) RunEdit(_ context.Context, args map[string]any) (string, error) {
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
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
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