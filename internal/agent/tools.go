package agent

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
)

// NewRegistry returns the built-in tools the model may call during a
// conversation, keyed by tool name.

// fileLocks serializes concurrent file mutations to the same path.
// ponytail: unbounded growth ceiling = distinct paths per session.
var fileLocks sync.Map // map[string]*sync.Mutex, keyed by abs path

// fileMutex returns a per-path mutex. Paths are normalized to absolute
// so that "foo.go" and "./foo.go" share the same lock.
func fileMutex(path string) *sync.Mutex {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	v, _ := fileLocks.LoadOrStore(abs, &sync.Mutex{})
	return v.(*sync.Mutex)
}
func NewRegistry() map[string]Tool {
	return map[string]Tool{
		"shell": {
			Description: "Run a shell command and return its stdout and stderr.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "The command to run."},
				},
				"required": []string{"command"},
			},
			Run: runShell,
		},
		"read_file": {
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
			Run: runReadFile,
		},
		"write_file": {
			Description: "Write content to a file, creating parent directories as needed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Path of the file to write."},
					"content": map[string]any{"type": "string", "description": "Full content to write."},
				},
				"required": []string{"path", "content"},
			},
			Run: runWriteFile,
		},
		"edit": {
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
			Run: runEdit,
		},
	}
}

// argString extracts a string argument, returning a clear error when it is
// missing or not a string.
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

// runLoadSkill looks up a skill by name and returns its content and
// directory path. The skills slice is captured by the closure in
// SetSkills.
func runLoadSkill(skills []Skill, args map[string]any) (string, error) {
	name, err := argString(args, "name")
	if err != nil {
		return "", err
	}
	for _, s := range skills {
		if s.Name == name {
			var sb strings.Builder
			fmt.Fprintf(&sb, "Skill: %s\n", s.Name)
			fmt.Fprintf(&sb, "Directory: %s\n\n", s.Dir)
			sb.WriteString(s.Content)
			return sb.String(), nil
		}
	}
	// Not found: list available skill names to help the agent.
	var names []string
	for _, s := range skills {
		names = append(names, s.Name)
	}
	return "", fmt.Errorf("skill %q not found. Available skills: %s", name, strings.Join(names, ", "))
}

// runShell executes a command through the platform shell and returns its
// combined stdout and stderr.
func runShell(ctx context.Context, args map[string]any) (string, error) {
	command, err := argString(args, "command")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command must not be empty")
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("command failed: %w", err)
	}
	return string(out), nil
}

// runReadFile reads a file and returns its contents with line numbers.
// offset is 1-based; limit caps the number of lines returned.
func runReadFile(_ context.Context, args map[string]any) (string, error) {
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path must not be empty")
	}

	mu := fileMutex(path)
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

// runWriteFile writes content to a file, creating parent directories.
func runWriteFile(_ context.Context, args map[string]any) (string, error) {
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

	mu := fileMutex(path)
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

// runEdit applies a targeted find-and-replace patch to a file. It reads the
// file, requires old_string to occur exactly once, replaces it with
// new_string, and writes the result back. It returns the patched path and a
// diff summary built from the standard library.
func runEdit(_ context.Context, args map[string]any) (string, error) {
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

	mu := fileMutex(path)
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

// diffSummary renders a minimal unified-style diff for a single replacement
// using only the standard library. It shows the removed and added lines.
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
