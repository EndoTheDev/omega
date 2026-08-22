// core-delegate is an omega extension that provides the delegate.task
// and delegate.status tools. delegate.task spawns child omega processes
// (omega run) to work on subtasks in the background. When a child
// finishes, the result is sent to the host as a delegate_result
// notification, which re-enters the conversation as a new turn.
// delegate.status returns the current state of all tasks.
//
// Recursion guard: OMEGA_SUBAGENT=1 env var. Child processes see it
// and do not register the delegate tools.
//
// OMEGA_BIN env var (set by the host) points to the omega binary.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

var (
	encMu       sync.Mutex // serializes all writes to stdout
	enc         *json.Encoder
	mu          sync.Mutex
	tasks       = map[string]*delegateTask{}
	taskCounter int64
)

type delegateTask struct {
	id     string
	prompt string
	cmd    *exec.Cmd
	output strings.Builder
	mu     sync.Mutex
	done   bool
}

func main() {
	enc = json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			handleInitialize(req)

		case "tool_call":
			handleToolCall(req)

		default:
			if req.ID != nil {
				send(rpcResponse{
					JSONRPC: "2.0", ID: req.ID,
					Error: &rpcError{Code: -32601, Message: "unknown method: " + req.Method},
				})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "core-delegate: scanner: %v\n", err)
	}
}

// send writes a JSON-RPC message to stdout, serialized by encMu.
func send(v any) {
	encMu.Lock()
	defer encMu.Unlock()
	enc.Encode(v)
}

func handleInitialize(req rpcRequest) {
	isSubagent := os.Getenv("OMEGA_SUBAGENT") != ""

	var tools []toolDef
	if !isSubagent {
		tools = []toolDef{
			{
				Name:        "delegate.task",
				Description: "Spawn a subagent to work on a task in the background. The subagent runs as a separate omega process with a fresh context. Returns immediately with a task ID. The result is automatically injected when the subagent finishes. Do not end the conversation until all delegated tasks complete.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"prompt": map[string]any{
							"type":        "string",
							"description": "Self-contained task prompt for the subagent",
						},
						"timeout": map[string]any{
							"type":        "integer",
							"description": "Max seconds to wait (default 300)",
						},
					},
					"required": []string{"prompt"},
				},
			},
			{
				Name:        "delegate.status",
				Description: "Check the status of running and completed subagent tasks. Returns running count and task list with IDs, prompts, and status.",
				Parameters: map[string]any{
					"type": "object",
				},
			},
		}
	}

	result, _ := json.Marshal(map[string]any{
		"tools":      tools,
		"commands":   []any{},
		"seams":      []string{},
		"subscribed": []string{},
	})
	send(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func handleToolCall(req rpcRequest) {
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

	switch toolName {
	case "delegate.task":
		handleDelegateTask(req, params.Args)
	case "delegate.status":
		handleDelegateStatus(req)
	default:
		send(rpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32601, Message: "unknown tool: " + toolName},
		})
	}
}

func handleDelegateTask(req rpcRequest, args map[string]any) {
	prompt, _ := args["prompt"].(string)
	timeoutSec := 300
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}

	taskID := fmt.Sprintf("task-%d", atomic.AddInt64(&taskCounter, 1))
	omegaBin := findOmegaBinary()
	if omegaBin == "" {
		result, _ := json.Marshal(map[string]any{
			"content":  "error: could not find omega binary (OMEGA_BIN not set)",
			"is_error": true,
		})
		send(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	cmd := exec.CommandContext(ctx, omegaBin, "run", prompt)
	cmd.Env = append(os.Environ(), "OMEGA_SUBAGENT=1")

	task := &delegateTask{id: taskID, prompt: prompt, cmd: cmd}

	mu.Lock()
	tasks[taskID] = task
	mu.Unlock()

	// Notify host that a task started (for pending count tracking).
	startParams, _ := json.Marshal(map[string]any{
		"task_id": taskID,
	})
	send(rpcRequest{
		JSONRPC: "2.0",
		Method:  "delegate_start",
		Params:  startParams,
	})

	go func() {
		defer cancel()
		output, err := cmd.CombinedOutput()
		task.mu.Lock()
		if err != nil {
			task.output.WriteString(fmt.Sprintf("error: %v\n%s", err, string(output)))
		} else {
			task.output.Write(output)
		}
		task.done = true
		result := task.output.String()
		task.mu.Unlock()

		// Send delegate_result notification to host.
		notifyParams, _ := json.Marshal(map[string]any{
			"task_id": taskID,
			"output":  result,
		})
		send(rpcRequest{
			JSONRPC: "2.0",
			Method:  "delegate_result",
			Params:  notifyParams,
		})

		// Keep task in map with done=true for delegate.status.
	}()

	result, _ := json.Marshal(map[string]any{
		"content":  fmt.Sprintf("Subagent %s started. The result will appear automatically when it finishes. Use delegate.status to check progress.", taskID),
		"is_error": false,
	})
	send(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func handleDelegateStatus(req rpcRequest) {
	mu.Lock()
	defer mu.Unlock()

	type taskInfo struct {
		ID     string `json:"id"`
		Prompt string `json:"prompt"`
		Status string `json:"status"`
	}

	var infos []taskInfo
	running := 0
	for _, t := range tasks {
		t.mu.Lock()
		status := "running"
		if t.done {
			status = "done"
		} else {
			running++
		}
		infos = append(infos, taskInfo{
			ID:     t.id,
			Prompt: t.prompt,
			Status: status,
		})
		t.mu.Unlock()
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Running: %d\n", running)
	if len(infos) == 0 {
		sb.WriteString("No tasks.\n")
	} else {
		for _, t := range infos {
			fmt.Fprintf(&sb, "- %s [%s]: %s\n", t.ID, t.Status, t.Prompt)
		}
	}
	result, _ := json.Marshal(map[string]any{
		"content":  sb.String(),
		"is_error": false,
	})
	send(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func findOmegaBinary() string {
	if bin := os.Getenv("OMEGA_BIN"); bin != "" {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
	}
	// Fall back: relative to extension's directory (../omega or ../omega.exe)
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	for _, name := range []string{"omega", "omega.exe"} {
		p := filepath.Join(filepath.Dir(dir), name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
