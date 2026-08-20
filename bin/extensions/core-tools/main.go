// core-tools is an omega extension that provides the built-in file,
// shell, and skill tools. It replaces the hardcoded tool registry in
// the agent core, making all tools pluggable.
//
// Tools: shell.run, files.read, files.write, files.edit
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// --- omega extension protocol types ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

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

type extTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// --- tool definitions ---

var toolDefs = []extTool{
	{
		Name:        "shell.run",
		Description: "Run a shell command and return its stdout and stderr.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "The command to run."},
			},
			"required": []string{"command"},
		},
	},
	{
		Name:        "files.read",
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
	},
	{
		Name:        "files.write",
		Description: "Write content to a file, creating parent directories as needed.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "Path of the file to write."},
				"content": map[string]any{"type": "string", "description": "Full content to write."},
			},
			"required": []string{"path", "content"},
		},
	},
	{
		Name:        "files.edit",
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
	},
}

// --- per-path file locking ---

// ponytail: unbounded growth ceiling = distinct paths per session.
var fileLocks sync.Map // map[string]*sync.Mutex, keyed by abs path

func fileMutex(path string) *sync.Mutex {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	v, _ := fileLocks.LoadOrStore(abs, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// --- helpers ---

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

// --- tool handlers ---

func runShell(args map[string]any) (string, error) {
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

func runReadFile(args map[string]any) (string, error) {
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

func runWriteFile(args map[string]any) (string, error) {
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

func runEdit(args map[string]any) (string, error) {
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



func readRemaining(scanner *bufio.Scanner) string {
	var sb strings.Builder
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// --- JSON-RPC dispatch ---

func main() {
	stdin := bufio.NewReader(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for {
		line, err := stdin.ReadString('\n')
		if err != nil {
			return
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			result, _ := json.Marshal(map[string]any{
				"name":          "core-tools",
				"tools":         toolDefs,
				"subscriptions": []string{},
			})
			if req.ID != nil {
				encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: result})
			}

		case "tool_call":
			var params struct {
				Name string         `json:"name"`
				Tool string         `json:"tool"`
				Args map[string]any `json:"args"`
			}
			json.Unmarshal(req.Params, &params)
			toolName := params.Name
			if toolName == "" {
				toolName = params.Tool
			}

			var output string
			var callErr error
			switch toolName {
			case "shell.run":
				output, callErr = runShell(params.Args)
			case "files.read":
				output, callErr = runReadFile(params.Args)
			case "files.write":
				output, callErr = runWriteFile(params.Args)
			case "files.edit":
				output, callErr = runEdit(params.Args)
			default:
				callErr = fmt.Errorf("unknown tool %q", toolName)
			}

			var content string
			isError := false
			if callErr != nil {
				content = callErr.Error()
				isError = true
			} else {
				content = output
			}
			result, _ := json.Marshal(map[string]any{
				"content":  content,
				"is_error": isError,
			})
			if req.ID != nil {
				encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: result})
			}

		case "shutdown":
			return

		default:
			// Respond with empty result for unknown request methods
			// so omega doesn't hang waiting for a response.
			if req.ID != nil {
				encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: json.RawMessage("{}")})
			}
		}
	}
}