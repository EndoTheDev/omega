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

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// Delegate manages background subagent tasks. It implements ToolProvider
// and supplies InjectedMessages + PendingDelegations to the agent Context.
type Delegate struct {
	mu          sync.Mutex
	tasks       map[string]*delegateTask
	taskCounter int64
	injected    chan injectedMsg
	logger      core.LoggerProvider
	// notifier is the finished-things seam (card t_5d9032d3), wired by
	// the plugin at mount. Nil-safe: no seam, no notification.
	notifier core.NotificationsProvider
}

// injectedMsg carries a subagent result to be injected as a new turn.
// It is converted to core.InjectedMessage by the plugin adapter.
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

// Description constants for the delegate tools (the machine-facing
// identities, Q10 namespace pairs since the v0.12 mount migration).
const (
	delegateTaskDesc   = "Spawn a subagent to work on a task in the background."
	delegateStatusDesc = "Check the status of running and completed subagent tasks."
)

func (d *Delegate) Tools() map[string]core.Tool {
	// taskParams are the spawn parameters; status takes none.
	taskParams := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Self-contained task prompt for the subagent (required).",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Max seconds to wait (default 300).",
			},
			"agent": map[string]any{
				"type":        "string",
				"description": "Registered agent profile to run the subtask as (action task): its config, skills, memory and logs. Default: none - runs as the plain home config.",
			},
		},
		"required": []string{"prompt"},
	}
	return map[string]core.Tool{
		// The machine-facing identities are the module.verb pairs
		// (Q10, the v0.12 mount migration): the two verbs register as
		// delegate.task and delegate.status, each with its own
		// parameters, both routed through the shared dispatcher the
		// consolidated "delegate" primary used.
		"delegate.task": {
			Description: delegateTaskDesc,
			Parameters:  taskParams,
			Run: func(ctx context.Context, args map[string]any) (string, error) {
				args["action"] = "task"
				return d.runDelegate(ctx, args)
			},
		},
		"delegate.status": {
			Description: delegateStatusDesc,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
			Run: func(ctx context.Context, args map[string]any) (string, error) {
				args["action"] = "status"
				return d.runDelegate(ctx, args)
			},
		},
	}
}

// runDelegate dispatches the consolidated delegate tool.
func (d *Delegate) runDelegate(ctx context.Context, args map[string]any) (string, error) {
	action, _ := args["action"].(string)
	switch action {
	case "task":
		return d.runDelegateTask(ctx, args)
	case "status":
		return d.runDelegateStatus(ctx, args)
	default:
		return "", fmt.Errorf("action is required: one of task, status")
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
	agentName, _ := args["agent"].(string)
	timeoutSec := 300
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}

	// Agent target (card t_7e2b7002): the subtask runs as one of the
	// home's registered agents - its config layers over the home's,
	// its skills/memory/logs are its own. Validated here so a typo
	// fails immediately instead of quietly running as not-configured
	// defaults a minute later (the child's LoadConfig would only
	// reject a truly malformed layer, not a ghost name).
	if agentName != "" {
		home := os.Getenv("OMEGA_HOME")
		if _, err := agent.LoadProfile(home, agentName); err != nil {
			return "", fmt.Errorf("delegate: unknown agent %q - register it first (%s)", agentName, agent.AgentDir(home, agentName))
		}
	}

	taskID := fmt.Sprintf("task-%d", atomic.AddInt64(&d.taskCounter, 1))
	omegaBin := d.findOmegaBinary()
	if omegaBin == "" {
		return "error: could not find omega binary (OMEGA_BIN not set)", fmt.Errorf("omega binary not found")
	}

	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	cmd := exec.CommandContext(taskCtx, omegaBin, "run", prompt)
	cmd.Env = append(os.Environ(), "OMEGA_SUBAGENT=1")
	if agentName != "" {
		cmd.Env = append(cmd.Env, "OMEGA_AGENT="+agentName)
	}
	// Process-group kill, same as the shell tool: without it the
	// timeout kills only the omega child while shells it spawned
	// survive as orphans holding locks (the bug class shellKillSetup
	// was created to fix).
	agent.SetupProcessGroupKill(cmd)

	task := &delegateTask{id: taskID, prompt: prompt, cmd: cmd}

	d.mu.Lock()
	d.tasks[taskID] = task
	d.mu.Unlock()

	go func() {
		defer cancel()
		output, err := cmd.CombinedOutput()
		d.finish(taskID, output, err)
	}()

	return fmt.Sprintf("Subagent %s started. The result will appear automatically when it finishes. Use delegate.status to check progress.", taskID), nil
}

// finish records a dispatched run's completion (card t_5d9032d3): the
// captured output lands on the task, the result injects into the host
// conversation, and one finished-things record emits - verdict ok on a
// clean exit, failed on error. The notification title is metadata only
// (the dispatch id); prompt and output never enter it.
func (d *Delegate) finish(taskID string, output []byte, runErr error) {
	d.mu.Lock()
	task := d.tasks[taskID]
	if task != nil {
		task.mu.Lock()
		if runErr != nil {
			task.output.WriteString(fmt.Sprintf("error: %v\n%s", runErr, string(output)))
		} else {
			task.output.Write(output)
		}
		task.done = true
		task.mu.Unlock()
	}
	d.mu.Unlock()

	if task != nil {
		result := ""
		task.mu.Lock()
		result = task.output.String()
		task.mu.Unlock()

		// Inject result into host conversation. Non-blocking send;
		// if buffer full, the result is logged and dropped.
		select {
		case d.injected <- injectedMsg{text: result, source: "delegate:" + taskID}:
		default:
			if d.logger != nil {
				d.logger.Errorf("delegate: injected channel full, dropping result for %s", taskID)
			} else {
				fmt.Fprintf(os.Stderr, "delegate: injected channel full, dropping result for %s\n", taskID)
			}
		}
	}

	if d.notifier == nil {
		return
	}
	verdict := "ok"
	if runErr != nil {
		verdict = "failed"
	}
	_, _ = d.notifier.Emit(core.Notification{
		Kind:     "chat",
		SourceID: taskID,
		Title:    "chat " + taskID + " finished",
		Verdict:  verdict,
	})
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
