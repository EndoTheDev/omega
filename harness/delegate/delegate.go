// Package delegate provides the delegate.task and delegate.status tools.
// delegate.task spawns child omega processes (omega run) to work on
// subtasks in the background. The tool is yield-class (async agent loop
// phase 2, card t_0805d438; design of record Workshop/omega/async-agent-loop.md):
// the call ends the turn, the report re-enters as a tool result through
// the phase-1 wake path, and no user-shaped delegate message is ever
// written. delegate.status returns the current state of all tasks, and
// for a done task the task's final answer text (the queryable home).
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

// Delegate manages background subagent tasks. It implements ToolProvider.
// Its tools carry no seam beyond the registry: the report's re-entry is
// the loop's own yield machinery (the tool's Yield registration), not a
// delegate-owned surface.
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
	result  string // the report render (renderResult) a done task keeps for delegate.status
}

// NewDelegate creates a Delegate ready to mount.
func NewDelegate() *Delegate {
	return &Delegate{
		tasks:     make(map[string]*delegateTask),
		spawnable: os.Getenv("OMEGA_SUBAGENT") == "",
	}
}

// The result cap (the summary-back ruling): the caller's context stays
// small however chatty the child ran. Head keeps the answer's shape,
// tail keeps the closing verdict, the marker names the cut so the turn
// never reads like a truncated sentence.
const (
	summaryCap  = 6000
	summaryHead = 3500
	summaryTail = 1500
)

// Description constants for the delegate tools (the machine-facing
// identities, Q10 namespace pairs since the v0.12 mount migration).
const (
	delegateTaskDesc   = "Spawn a subagent to work on a task in the background. The result re-enters as this call's tool result when the subagent finishes."
	delegateStatusDesc = "Check the status of running and completed subagent tasks."
)

// taskIDShape (scope addition 4, card t_0805d438): task ids stay short
// session-local names ("task-1", "task-2") with one counter per Delegate
// instance, so an id alone is unsafe to match on. The chosen shape
// carries the session reference beside the id: every status row renders
// "task-N [status] (session <sid>)". The id shape itself never changed
// (consumers match by it); the session is the disambiguator, in the
// same payload.

// statusSummaryClip bounds the prompt digest a status row carries
// (scope addition A4): the FULL prompt never re-enters on a poll - a
// prompt-sized line every poll is the payload bloat the finding names.
// The digest exists to identify the task; the child's session carries
// the prompt itself.
const statusSummaryClip = 120

func (d *Delegate) Tools() map[string]core.Tool {
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
		// Yield: async agent loop phase 2 - the call ends the turn and
		// the report re-enters as this call's tool result. A wakeless
		// host (the CLI loop, a bare run) runs it loop-back instead:
		// the blocking wait the one-shot shape always had.
		"delegate.task": {
			Description: delegateTaskDesc,
			Yield:       true,
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

// runDelegateTask spawns the child and waits it out. Under the yield
// engine the call runs on the dispatched executor's goroutine - the
// conversation is never parked: the turn (and run) end, the user
// messages enter the home freely, and this return value re-enters as
// the call's tool result. Loop-back (the wakeless CLI loop) blocks the
// run here instead - the one-shot subagent shape `omega run` always
// had. Either way the report is the tool result, and no user-shaped
// delegate message is ever written.
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
	defer cancel()
	cmd := exec.CommandContext(taskCtx, omegaBin, childArgs...)
	cmd.Env = append(os.Environ(), childEnv(agentName, model)...)
	cmd.Env = append(cmd.Env, workerStamp(taskID, omegaBin)...)
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

	// The wait IS the tool call: the dispatched executor (or the
	// loop-back run) blocks here until the child exits or its timeout
	// kills the process tree. The task's wall clock is its own; the
	// run's cap above adds nothing.
	runErr := cmd.Run()
	task.mu.Lock()
	task.done = true
	task.result = task.renderResult(runErr)
	task.mu.Unlock()

	// The finished-things record (card t_5d9032d3): verdict ok on a
	// clean exit, failed on error. The notification title is metadata
	// only (the dispatch id); prompt and output never enter it.
	if d.notifier != nil {
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

	return task.result, nil
}

// renderResult renders the report-shaped tool result (ruling 2): a
// clean exit produces the child's own final answer - what omega run
// printed, trimmed to one summary turn; a failed exit produces a SHORT
// failure line and nothing else (the details live in the child
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

// runDelegateStatus renders the task listing. Payload shape (scope
// addition A4, card t_0805d438): every row is
//
//	- task-N [running|done] (session <sid>) <prompt digest, 120 runes>
//
// and a done row carries the task's final answer as its own indented
// line - the queryable home the injected-user seam used to be the only
// door for. The FULL prompt never enters the payload; the digest
// identifies, the child session holds the prompt.
func (d *Delegate) runDelegateStatus(ctx context.Context, args map[string]any) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	type taskInfo struct {
		id      string
		prompt  string
		session string
		status  string
		answer  string
	}

	var infos []taskInfo
	running, done := 0, 0
	for _, t := range d.tasks {
		t.mu.Lock()
		status := "running"
		if t.done {
			status = "done"
			done++
		} else {
			running++
		}
		infos = append(infos, taskInfo{id: t.id, prompt: t.prompt, session: t.session, status: status, answer: t.result})
		t.mu.Unlock()
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Running: %d, Done: %d\n", running, done)
	if len(infos) == 0 {
		sb.WriteString("No tasks.\n")
		return sb.String(), nil
	}
	for _, t := range infos {
		line := fmt.Sprintf("- %s [%s]", t.id, t.status)
		if t.session != "" {
			line += fmt.Sprintf(" (session %s)", t.session)
		}
		line += " " + taskSummary(t.prompt)
		sb.WriteString(line + "\n")
		if t.answer != "" {
			sb.WriteString("  answer: " + t.answer + "\n")
		}
	}
	return sb.String(), nil
}

// taskSummary renders the status row's prompt digest: the first
// prompt line, clipped, whitespace-flattened to one compact strip.
func taskSummary(prompt string) string {
	line := strings.Join(strings.Fields(strings.TrimSpace(prompt)), " ")
	runes := []rune(line)
	if len(runes) > statusSummaryClip {
		return string(runes[:statusSummaryClip]) + "..."
	}
	return line
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
