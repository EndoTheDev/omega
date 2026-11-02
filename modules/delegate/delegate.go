// Package delegate provides the delegate.task and delegate.status tools.
// delegate.task spawns child omega processes (omega run) to work on
// subtasks in the background. When a child finishes, the result is
// injected into the host conversation as a new turn via InjectedMessages.
// delegate.status returns the current state of all tasks.
//
// Recursion guard: OMEGA_SUBAGENT=1 env var, two layers, both inside
// the module. The spawn sets it on the child's env (spawn_test.go pins
// that); the child's own mount refuses the delegate tools when the var
// is set (plugin.go) - one-shot subagents spawn no children.
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
	// store is the session-persistence seam, captured from the mount
	// Context (nil-safe: no store module, no child session). The store
	// mounts before delegate (slice-order tiebreak: no Requires edge -
	// one naming delegate is the removal recipe's stop signal).
	store core.StoreProvider
	// spawnable reports the recursion guard's mount verdict: false
	// inside a subagent child (OMEGA_SUBAGENT set) - delegate.task
	// never registers there, so depth stays at one level.
	spawnable bool
}

// injectedMsg carries a subagent result to be injected as a new turn.
// It is converted to core.InjectedMessage by the plugin adapter.
type injectedMsg struct {
	text   string
	source string
}

type delegateTask struct {
	id      string
	prompt  string
	session string // child --session id ("" when no store seam) - the stored, searchable surface
	cmd     *exec.Cmd
	out     strings.Builder // child stdout only: the final answer, never stderr noise
	errOut  strings.Builder // child stderr, kept on the task for status depth
	mu      sync.Mutex
	done    bool
}

// NewDelegate creates a Delegate ready to mount. The injected channel
// is created internally; the host reads it via InjectedMessages.
func NewDelegate() *Delegate {
	return &Delegate{
		tasks:     make(map[string]*delegateTask),
		injected:  make(chan injectedMsg, 16),
		spawnable: os.Getenv("OMEGA_SUBAGENT") == "",
	}
}

// The injected summary cap (ruling 2, summary-back): the caller's
// context stays small however chatty the child ran. Head keeps the
// answer's shape, tail keeps the closing verdict, the marker names the
// cut so the turn never reads like a truncated sentence.
const (
	summaryCap  = 6000
	summaryHead = 3500
	summaryTail = 1500
)

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
				"description": "Registered agent profile to run the subtask as (action task): its config, skills, memory and logs. Defaulting law: empty = the default agent.",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Provider model for the child run (rides OMEGA_MODEL). Defaulting law: empty = inherit the session provider model (= the child's own config), never an error.",
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

// resolveAgent applies the defaulting law (Endo, 2026-11-01,
// omega-wide) to delegate.task's agent field: any field naming an
// AGENT defaults to the default agent when empty. Trimmed so a stray
// space is a typo-carrying field, not a ghost agent name.
func resolveAgent(field string) string {
	name := strings.TrimSpace(field)
	if name == "" || name == agent.DefaultAgentName {
		return agent.DefaultAgentName
	}
	return name
}

// resolveModel applies the defaulting law's model half to
// delegate.task's model field: empty = the session provider model,
// never an error. For a spawned child, "the session provider model" is
// the model the child's own config produces - with no OMEGA_MODEL on
// its env, its run session adopts it, which IS the inherit. The parent
// never re-guesses the model value the child would resolve; only an
// explicit field becomes an OMEGA_MODEL override. Trimmed like agent.
func resolveModel(field string) string {
	return strings.TrimSpace(field)
}

// childEnv builds the child process deltas over os.Environ(): the
// recursion guard always; the agent layer env only off-default
// (datadir collapses "default" to unset anyway - the default agent's
// config IS the home config); the model only when explicitly asked -
// the defaulting law's empty = inherit is the ABSENCE of the var.
func childEnv(agentName, model string) []string {
	env := []string{"OMEGA_SUBAGENT=1"}
	if name := resolveAgent(agentName); name != agent.DefaultAgentName {
		env = append(env, "OMEGA_AGENT="+name)
	}
	if model := resolveModel(model); model != "" {
		env = append(env, "OMEGA_MODEL="+model)
	}
	return env
}

func (d *Delegate) runDelegateTask(ctx context.Context, args map[string]any) (string, error) {
	if !d.spawnable {
		return "", fmt.Errorf("delegate: spawn refused - this process is itself a subagent (OMEGA_SUBAGENT), depth stays at one level")
	}
	prompt, _ := args["prompt"].(string)
	agentName, _ := args["agent"].(string)
	model, _ := args["model"].(string)
	timeoutSec := 300
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}

	// Agent target (card t_7e2b7002), under the defaulting law: empty
	// resolves to the default agent - no OMEGA_AGENT on the child, the
	// home config itself is the default agent's layer. A non-default
	// name is validated here so a typo fails immediately instead of
	// quietly running as not-configured defaults a minute later (the
	// child's LoadConfig would only reject a truly malformed layer,
	// not a ghost name).
	if name := resolveAgent(agentName); name != agent.DefaultAgentName {
		home := os.Getenv("OMEGA_HOME")
		if _, err := agent.LoadProfile(home, name); err != nil {
			return "", fmt.Errorf("delegate: unknown agent %q - register it first (%s)", name, agent.AgentDir(home, name))
		}
	}

	taskID := fmt.Sprintf("task-%d", atomic.AddInt64(&d.taskCounter, 1))
	omegaBin := d.findOmegaBinary()
	if omegaBin == "" {
		return "error: could not find omega binary (OMEGA_BIN not set)", fmt.Errorf("omega binary not found")
	}

	// The child session (the stored+searchable ruling): the host
	// pre-creates the session through the store seam - label
	// delegate:<task>, parent link = the calling session when the run
	// context carries one - and the child inherits it via run
	// --session, which makes the child persist its prompt, report and
	// tool results into the store (the --session persist contract).
	// Nil store or a failed create degrades to the old shape: a plain
	// run child, nothing persisted. Bonus, never a hard dependency.
	childArgs := []string{"run"}
	session := ""
	if d.store != nil {
		if sid, err := agent.NewSessionID(); err == nil {
			label := "delegate:" + taskID
			if err := d.store.CreateSession(ctx, sid, agent.SessionIDFromContext(ctx), label); err == nil {
				childArgs = append(childArgs, "--session", sid)
				session = sid
			} else if d.logger != nil {
				d.logger.Errorf("delegate: create child session for %s: %v - degrading to a non-persisted run", taskID, err)
			}
		}
	}
	childArgs = append(childArgs, prompt)

	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	cmd := exec.CommandContext(taskCtx, omegaBin, childArgs...)
	cmd.Env = append(os.Environ(), childEnv(agentName, model)...)
	// Process-group kill, same as the shell tool: without it the
	// timeout kills only the omega child while shells it spawned
	// survive as orphans holding locks (the bug class shellKillSetup
	// was created to fix).
	agent.SetupProcessGroupKill(cmd)

	task := &delegateTask{id: taskID, prompt: prompt, session: session, cmd: cmd}
	cmd.Stdout = &task.out
	cmd.Stderr = &task.errOut

	d.mu.Lock()
	d.tasks[taskID] = task
	d.mu.Unlock()

	go func() {
		defer cancel()
		runErr := cmd.Run()
		d.finish(taskID, runErr)
	}()

	started := fmt.Sprintf("Subagent %s started. The result will appear automatically when it finishes. Use delegate.status to check progress.", taskID)
	if session != "" {
		started = fmt.Sprintf("%s (session %s)", started, session)
	}
	return started, nil
}

// finish records a dispatched run's completion (card t_5d9032d3): the
// summary lands on the task, injects into the host conversation, and
// one finished-things record emits - verdict ok on a clean exit,
// failed on error. The notification title is metadata only (the
// dispatch id); prompt and output never enter it.
func (d *Delegate) finish(taskID string, runErr error) {
	d.mu.Lock()
	task := d.tasks[taskID]
	var result string
	if task != nil {
		task.mu.Lock()
		task.done = true
		result = task.renderResult(runErr)
		task.mu.Unlock()
	}
	d.mu.Unlock()

	if task != nil {
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

// renderResult renders the summary-shaped inject (the ruling, ruling
// 2): a clean exit produces the child's own final answer - what omega
// run printed, trimmed to one summary turn; a failed exit produces a
// SHORT failure line and nothing else (the details live in the child
// session and the task record, never in the caller's context).
func (t *delegateTask) renderResult(runErr error) string {
	if runErr != nil {
		return fmt.Sprintf("error: delegate task %s failed: %v", t.id, runErr)
	}
	out := strings.TrimSpace(t.out.String())
	if out == "" {
		// A clean exit with nothing printed is still a turn, but a
		// silent one - the shape never goes blank on the caller.
		return fmt.Sprintf("delegate %s: finished without output", t.id)
	}
	return clipSummary(out)
}

// clipSummary bounds an injected result at summaryCap runes, cutting
// the middle with a named marker once the child overshot the cap: the
// head keeps the answer's opening argument, the tail keeps the closing
// verdict, the marker makes the cut visible instead of subtle.
func clipSummary(s string) string {
	runes := []rune(s)
	if len(runes) <= summaryCap {
		return s
	}
	omitted := len(runes) - summaryHead - summaryTail
	return string(runes[:summaryHead]) +
		fmt.Sprintf("\n...[delegate: %d chars omitted]...\n", omitted) +
		string(runes[len(runes)-summaryTail:])
}

func (d *Delegate) runDelegateStatus(ctx context.Context, args map[string]any) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	type taskInfo struct {
		id      string
		prompt  string
		session string
		status  string
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
		infos = append(infos, taskInfo{id: t.id, prompt: t.prompt, session: t.session, status: status})
		t.mu.Unlock()
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Running: %d\n", running)
	if len(infos) == 0 {
		sb.WriteString("No tasks.\n")
	} else {
		for _, t := range infos {
			line := fmt.Sprintf("- %s [%s]: %s", t.id, t.status, t.prompt)
			if t.session != "" {
				line += fmt.Sprintf(" (session %s)", t.session)
			}
			sb.WriteString(line + "\n")
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
