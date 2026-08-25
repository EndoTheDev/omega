// Package delegate provides the delegate.task and delegate.status tools.
// delegate.task spawns child omega processes (omega run) to work on
// subtasks in the background. When a child finishes, the result is
// injected into the host conversation as a new turn via InjectedMessages.
// delegate.status returns the current state of all tasks.
//
// Recursion guard: OMEGA_SUBAGENT=1 env var. Child processes see it
// and do not register the delegate tools (handled by the host wiring).
//
// OMEGA_BIN env var points to the omega binary.
package delegate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// Delegate manages background subagent tasks. It implements ToolProvider
// and supplies InjectedMessages + PendingDelegations to the agent Context.
type Delegate struct {
	mu          sync.Mutex
	tasks       map[string]*delegateTask
	taskCounter int64
	injected    chan injectedMsg
}

// injectedMsg carries a subagent result to be injected as a new turn.
// It is converted to agent.InjectedMessage by the plugin adapter.
type injectedMsg struct {
	text   string
	source string
}

type delegateTask struct {
	id     string
	prompt string
	cmd    *exec.Cmd
	output strings.Builder
	mu     sync.Mutex
	done   bool
}

// NewDelegate creates a Delegate ready to mount. The injected channel
// is created internally; the host reads it via InjectedMessages.
func NewDelegate() *Delegate {
	return &Delegate{
		tasks:    make(map[string]*delegateTask),
		injected: make(chan injectedMsg, 16),
	}
}

// Tools returns the delegate.task and delegate.status tools.
func (d *Delegate) Tools() map[string]agent.Tool {
	return map[string]agent.Tool{
		"delegate.task": {
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
			Run: d.runDelegateTask,
		},
		"delegate.status": {
			Description: "Check the status of running and completed subagent tasks. Returns running count and task list with IDs, prompts, and status.",
			Parameters: map[string]any{
				"type": "object",
			},
			Run: d.runDelegateStatus,
		},
	}
}

// PendingCount returns the number of still-running subagent tasks.
func (d *Delegate) PendingCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	count := 0
	for _, t := range d.tasks {
		t.mu.Lock()
		if !t.done {
			count++
		}
		t.mu.Unlock()
	}
	return count
}

// InjectedChannel returns the read-only channel of subagent results.
// The plugin adapter exposes this as agent.Context.InjectedMessages.
func (d *Delegate) InjectedChannel() <-chan injectedMsg {
	return d.injected
}

func (d *Delegate) runDelegateTask(ctx context.Context, args map[string]any) (string, error) {
	prompt, _ := args["prompt"].(string)
	timeoutSec := 300
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}

	taskID := fmt.Sprintf("task-%d", atomic.AddInt64(&d.taskCounter, 1))
	omegaBin := d.findOmegaBinary()
	if omegaBin == "" {
		return "error: could not find omega binary (OMEGA_BIN not set)", fmt.Errorf("omega binary not found")
	}

	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	cmd := exec.CommandContext(taskCtx, omegaBin, "run", prompt)
	cmd.Env = append(os.Environ(), "OMEGA_SUBAGENT=1")

	task := &delegateTask{id: taskID, prompt: prompt, cmd: cmd}

	d.mu.Lock()
	d.tasks[taskID] = task
	d.mu.Unlock()

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

		// Inject result into host conversation. Non-blocking send;
		// if buffer full, the result is logged to stderr and dropped.
		select {
		case d.injected <- injectedMsg{text: result, source: "delegate:" + taskID}:
		default:
			fmt.Fprintf(os.Stderr, "delegate: injected channel full, dropping result for %s\n", taskID)
		}
	}()

	return fmt.Sprintf("Subagent %s started. The result will appear automatically when it finishes. Use delegate.status to check progress.", taskID), nil
}

func (d *Delegate) runDelegateStatus(ctx context.Context, args map[string]any) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	type taskInfo struct {
		id     string
		prompt string
		status string
	}

	var infos []taskInfo
	running := 0
	for _, t := range d.tasks {
		t.mu.Lock()
		status := "running"
		if t.done {
			status = "done"
		} else {
			running++
		}
		infos = append(infos, taskInfo{id: t.id, prompt: t.prompt, status: status})
		t.mu.Unlock()
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Running: %d\n", running)
	if len(infos) == 0 {
		sb.WriteString("No tasks.\n")
	} else {
		for _, t := range infos {
			fmt.Fprintf(&sb, "- %s [%s]: %s\n", t.id, t.status, t.prompt)
		}
	}
	return sb.String(), nil
}

func (d *Delegate) findOmegaBinary() string {
	if bin := os.Getenv("OMEGA_BIN"); bin != "" {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
	}
	// Fall back: relative to the current executable (../omega or ../omega.exe).
	// ponytail: os.Executable() path may be wrong in test/dev builds;
	// upgrade path: pass bin path explicitly via config in C11 wiring.
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