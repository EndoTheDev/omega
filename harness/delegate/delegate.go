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
	"github.com/EndoTheDev/omega/agent"
)

// finishedThings is the consumer-side face of the finished-things
// store (part 3 seam constitution): just the emit method the
// dispatched-run reports call. The notifications capsule satisfies it
// structurally; no shared type exists.
type finishedThings interface {
	Emit(n core.Notice) (core.Notice, error)
}

// Delegate manages background subagent tasks. It implements ToolProvider
// and supplies InjectedMessages (the run-affinity InjectedStream) +
// PendingDelegations to the agent Context.
type Delegate struct {
	mu          sync.Mutex
	tasks       map[string]*delegateTask
	taskCounter int64
	logger      agent.LoggerProvider
	// notifier is the finished-things consumers face (card
	// t_5d9032d3), wired by the plugin at mount from the universal
	// registry. Nil-safe: no capsule, no notification.
	notifier finishedThings
	// store is the session-persistence seam, captured from the mount
	// Context (nil-safe: no store module, no child session). The store
	// mounts before delegate (slice-order tiebreak: no Requires edge -
	// one naming delegate is the removal recipe's stop signal).
	store agent.StoreProvider
	// spawnable reports the recursion guard's mount verdict: false
	// inside a subagent child (OMEGA_SUBAGENT set) - delegate.task
	// never registers there, so depth stays at one level.
	spawnable bool

	// The injected results (t_f3fb7c52, run affinity): a queue keyed by
	// the spawning Run's affinity key, with per-key wake signals. Poll
	// and Wait drain ONLY their key's results - a concurrent run (the
	// title turn, a sibling /chat) can never take another run's result.
	// qmu guards everything below, including the cap.
	qmu     sync.Mutex
	queues  map[string][]core.InjectedMessage
	waiters map[string][]chan struct{}
	qTotal  int
}

type delegateTask struct {
	id            string
	prompt        string
	affinity      string // the spawning run's affinity key; "" = unaddressed (no-key runs)
	session       string // child --session id ("" when no store seam) - the stored, searchable surface
	cmd           *exec.Cmd
	out           strings.Builder // child stdout only: the final answer, never stderr noise
	errOut        strings.Builder // child stderr, kept on the task for status depth
	mu            sync.Mutex
	done          bool
}

// NewDelegate creates a Delegate ready to mount. The affinity-keyed
// result queues are created internally; the host reads them through the
// InjectedStream seam (ctx.InjectedMessages).
func NewDelegate() *Delegate {
	return &Delegate{
		tasks:     make(map[string]*delegateTask),
		queues:    make(map[string][]core.InjectedMessage),
		waiters:   make(map[string][]chan struct{}),
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
	// injectedCap bounds the TOTAL buffered injected results across
	// every affinity queue (t_f3fb7c52) - the old shared channel's cap
	// - so a dead run's undrained results cannot grow the buffer
	// unboundedly. A full cap drops the newest result (dropResult).
	injectedCap = 16
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

// PendingCount implements the pending-delegations seam per run
// (t_f3fb7c52): the count includes only the running tasks the RUN named
// by affinity spawned. A serve-wide count here would keep a run's
// blocking drain parked on foreign children (and, with run affinity,
// forever - results for foreign children can never enter this run's
// queue). delegate.status's own listing stays serve-wide on purpose:
// it is the user's read surface, not the drain's gate.
func (d *Delegate) PendingCount(affinity string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	count := 0
	for _, t := range d.tasks {
		t.mu.Lock()
		if !t.done && t.affinity == affinity {
			count++
		}
		t.mu.Unlock()
	}
	return count
}

// Poll implements core.InjectedStream (t_f3fb7c52): every result
// queued for affinity comes back FIFO and the queue empties. A
// different key's results are unreachable from this call - the drain
// cannot cross runs.
func (d *Delegate) Poll(affinity string) []core.InjectedMessage {
	d.qmu.Lock()
	defer d.qmu.Unlock()
	msgs := d.queues[affinity]
	delete(d.queues, affinity)
	d.qTotal -= len(msgs)
	return msgs
}

// takeOrArm pops the oldest queued result for affinity, leaving any
// later results queued (Wait returns ONE result - a Poll here would
// empty the queue and silently drop the siblings queued in the same
// window). Nothing queued: it arms and returns a wake signal - the pop
// check and the arming share ONE qmu hold, so no push can land between
// them (that gap would strand a queued result behind a waiter that
// never wakes).
func (d *Delegate) takeOrArm(affinity string) (core.InjectedMessage, chan struct{}, bool) {
	wake := make(chan struct{}, 1)
	d.qmu.Lock()
	defer d.qmu.Unlock()
	msgs := d.queues[affinity]
	if len(msgs) > 0 {
		m := msgs[0]
		if len(msgs) == 1 {
			delete(d.queues, affinity)
		} else {
			d.queues[affinity] = msgs[1:]
		}
		d.qTotal--
		return m, nil, true
	}
	d.waiters[affinity] = append(d.waiters[affinity], wake)
	return core.InjectedMessage{}, wake, false
}

// Wait implements core.InjectedStream (t_f3fb7c52): blocks until one
// result for affinity arrives or ctx ends.
func (d *Delegate) Wait(ctx context.Context, affinity string) (core.InjectedMessage, bool) {
	for {
		m, wake, ok := d.takeOrArm(affinity)
		if ok {
			return m, true
		}
		select {
		case <-wake:
			// woken by a push (or a popped-empty wake a sibling
			// waiter already took) - re-check and re-arm.
			continue
		case <-ctx.Done():
			// deregister the wake slot so a later push does not leak it
			d.qmu.Lock()
			ws := d.waiters[affinity]
			for i, w := range ws {
				if w == wake {
					d.waiters[affinity] = append(ws[:i], ws[i+1:]...)
					break
				}
			}
			if len(d.waiters[affinity]) == 0 {
				delete(d.waiters, affinity)
			}
			d.qmu.Unlock()
			return core.InjectedMessage{}, false
		}
	}
}

// pushResult adds one finished child's result to the affinity-keyed
// queue and wakes its waiters. The buffer is still capped (the
// injectedCap law, formerly the channel's cap): a full cap drops the
// new result with the same logged shape as before.
func (d *Delegate) pushResult(affinity, source, text string) {
	d.qmu.Lock()
	if d.qTotal >= injectedCap {
		d.qmu.Unlock()
		d.dropResult(source)
		return
	}
	d.queues[affinity] = append(d.queues[affinity], core.InjectedMessage{Text: text, Source: source})
	d.qTotal++
	wake := d.waiters[affinity]
	d.waiters[affinity] = nil
	d.qmu.Unlock()
	for _, w := range wake {
		select {
		case w <- struct{}{}:
		default:
		}
	}
}

// dropResult logs (or stderrs) the cap-full drop - the classic
// non-blocking send's overflow arm, keyless now.
func (d *Delegate) dropResult(source string) {
	if d.logger != nil {
		d.logger.Errorf("delegate: injected queue full, dropping result for %s", source)
	} else {
		fmt.Fprintf(os.Stderr, "delegate: injected queue full, dropping result for %s\n", source)
	}
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

// workerStamp builds the machine-fence env deltas for a delegate child
// (card t_8b32a09a). Every delegate child is a worker session, so it is
// stamped OMEGA_WORKER=1 (the machine fence is env-scoped: the tick and
// the main session run unstamped) and OMEGA_WORKER_CARD=<task>-tagged,
// and the repo's git fence shim (bin/shims) rides the front of the
// child's PATH when it sits next to the omega binary (bin/omega beside
// bin/shims in a checkout). An installed home has no repo tree: the
// child runs stamped but the environment alone fences nothing - the
// shim does - so the PATH delta is simply absent there.
func workerStamp(taskID, omegaBin string) []string {
	env := []string{"OMEGA_WORKER=1", "OMEGA_WORKER_CARD=" + taskID}
	if dir := fenceShimDir(omegaBin); dir != "" {
		env = append(env, "PATH="+dir+":"+os.Getenv("PATH"))
	}
	return env
}

// fenceShimDir returns the bin/shims dir sitting next to the omega
// binary, when that dir holds an executable `git` shim. Empty when it
// does not - an installed instance never fakes a fence.
func fenceShimDir(omegaBin string) string {
	if omegaBin == "" {
		return ""
	}
	dir := filepath.Join(filepath.Dir(omegaBin), "shims")
	fi, err := os.Stat(filepath.Join(dir, "git"))
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return ""
	}
	return dir
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
	cmd.Env = append(cmd.Env, workerStamp(taskID, omegaBin)...)
	// Process-group kill, same as the shell tool: without it the
	// timeout kills only the omega child while shells it spawned
	// survive as orphans holding locks (the bug class shellKillSetup
	// was created to fix).
	agent.SetupProcessGroupKill(cmd)

	task := &delegateTask{id: taskID, prompt: prompt, affinity: agent.RunAffinityFromContext(ctx), session: session, cmd: cmd}
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
		// Queue the result for the spawning run's affinity key and wake
		// its waiters (t_f3fb7c52). Non-blocking; if the global buffer
		// is full, the result is logged and dropped.
		d.pushResult(task.affinity, "delegate:"+taskID, result)
	}

	if d.notifier == nil {
		return
	}
	verdict := "ok"
	if runErr != nil {
		verdict = "failed"
	}
	_, _ = d.notifier.Emit(core.Notice{
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
