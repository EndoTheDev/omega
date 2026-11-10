// The shell tool rides the terminal capsule (the shell-ring shrink
// wave 1, Endo: "bash could go into the terminal capsule"). The Tool's
// NAME is builtin vocabulary: "shell.run" parses as the module.verb
// pair ("shell", "run") and was the built-in tools capsule's natural
// name - so it registers through its own non-toggleable shell plugin
// (isBuiltin: machine-critical vocabulary, not module toggleable),
// keeping the model-facing name and the approval/gate hooks byte-exact.
// The capsule law still holds: the tool's SOURCE is harness/terminal,
// and deleting the capsule removes the tool and the terminal pane
// together (the removal proof).
//
// ONE shell domain (card t_5a49790b): shell.run, shell.list,
// shell.read, shell.kill. There are no flags and no modes - shell.run
// starts its command detached under the registry (own process group,
// a state row, a per-shell log), so a call that runs past the loop's
// inline budget converts to a pending call and its child keeps
// running; shell.list is the addressing surface, shell.read tails one
// shell's log (live, and after it exits - retention), shell.kill stops
// one by id. The retired process.spawn/list/read/kill verbs are gone;
// the registry machinery they were built on lives here underneath.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
	"github.com/EndoTheDev/omega/harness/store"
)

// ShellProvider serves the shell verb family as an additive
// tools-seam contributor. The guard refuses any command that resolves
// into the instance database (ENDO'S LAW 2026-10-18); nil is
// pass-through.
type ShellProvider struct {
	guard *datadir.Guard
	// reg is the shell registry: the process registry machinery under
	// its one remaining name. It is REQUIRED and always explicit -
	// MountWithAPI builds it from the process: section, and a host or
	// test mounting the family itself builds one over a home it names.
	// There is deliberately no lazy default: a default would resolve
	// datadir.Home() and write registry state into the installed
	// instance (~/.omega) from whatever happened to call a verb.
	reg *Registry
}

// errNoRegistry is the loud answer for an unmounted provider. It is a
// wiring bug, never a runtime condition: MountWithAPI always sets the
// registry, so a nil one names itself instead of quietly writing into
// some home nobody chose.
var errNoRegistry = errors.New("shell provider has no registry: the shell plugin mounts it (MountWithAPI) or NewShellProvider supplies one")

// NewShellProvider builds the shell tool face over an explicit registry
// and guard. MountWithAPI is the production door (it derives both from
// the config sections); a host or test that mounts the family itself
// passes a registry rooted at a home it owns. A nil guard is
// pass-through (datadir.Guard's nil receiver never denies).
func NewShellProvider(guard *datadir.Guard, reg *Registry) *ShellProvider {
	return &ShellProvider{guard: guard, reg: reg}
}

// Tools returns the shell verb family (the capsule's whole tool face).
func (p *ShellProvider) Tools() map[string]core.Tool {
	return map[string]core.Tool{
		"shell.run": {
			Description: "Run a shell command and return its stdout and stderr.\nThe command runs detached: a call still running when the turn ends keeps running past it, its result re-enters when the shell settles, and shell.list / shell.read / shell.kill address it meanwhile.",
			// Escalation-eligible (card t_1501cb04, Endo ruling
			// 2026-11-08 - the static Yield class is superseded): a
			// fast command runs INLINE in the turn (zero overhead -
			// previously EVERY shell.run paid the yield round trip),
			// a long one converts at the loop's inline budget and
			// its output re-enters as the call's tool result through
			// the wake path. A wakeless host blocks to completion
			// (the documented degradation).
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "The command to run."},
					"dir":     map[string]any{"type": "string", "description": "Optional working directory for this call: absolute, or relative to the session workspace. Overrides the run's default directory."},
				},
				"required": []string{"command"},
			},
			Run: p.RunShell,
		},
		"shell.list": {
			Description: "List running shells: id, command, age, log path.\nThe addressing surface - shell.read and shell.kill take one of these ids.",
			Parameters:  emptyParams(),
			Run:         p.List,
		},
		"shell.read": {
			Description: "Tail one shell's log by id - live while it runs, and still there after it exits (the log survives the shell; that post-mortem read is the point of keeping it).\nThe tail, never the whole log (context law).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":    map[string]any{"type": "string", "description": "Shell id from shell.list (required)."},
					"lines": map[string]any{"type": "integer", "description": "Tail count (default 100, max 2000)."},
				},
				"required": []string{"id"},
			},
			Run: p.Read,
		},
		"shell.kill": {
			Description: "Stop one running shell by id: SIGTERM first, escalating to SIGKILL after the grace period.\nIts log stays readable afterwards.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Shell id from shell.list (required)."},
				},
				"required": []string{"id"},
			},
			Run: p.Kill,
		},
	}
}

// emptyParams is shell.list's no-argument schema.
func emptyParams() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []string{},
	}
}

// dirOverride resolves the optional working-directory argument (card
// t_ccf74d88): an absolute path passes through cleaned; a relative one
// roots at the run's session workspace, or at the process working
// directory when the run has none. A missing or non-directory path is
// a named tool error, never a cryptic exec chdir failure.
func dirOverride(ctx context.Context, args map[string]any) (string, error) {
	v, _ := args["dir"].(string)
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	abs := v
	if !filepath.IsAbs(v) {
		if root := core.RunWorkspace(ctx); root != "" {
			abs = filepath.Join(root, v)
		} else {
			a, err := filepath.Abs(v)
			if err != nil {
				return "", fmt.Errorf("dir %q: %v", v, err)
			}
			abs = a
		}
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("dir %s: %v", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("dir %s: not a directory", abs)
	}
	return abs, nil
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
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("argument %q must not be empty", key)
	}
	return s, nil
}

// argInt extracts an optional integer argument.
func argInt(args map[string]any, key string) (int, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, nil
	}
	switch n := v.(type) {
	case int:
		return n, nil
	case float64:
		return int(n), nil
	default:
		return 0, fmt.Errorf("argument %q must be an integer, got %T", key, v)
	}
}

// shellOutputCap bounds how much of a shell's log one call reads back.
// The pre-unification shape buffered a command's WHOLE output in memory
// before the agent loop's MaxToolOutput truncation ever ran, so 'cat
// 500MB' or a forever-streaming command ballooned omega's RSS into an
// OOM that killed the server holding the store. The log on disk takes
// the whole stream; the call reads the head, the model tails the rest.
const shellOutputCap = 1 << 20 // 1 MB

// tapLog streams a running shell's log to the run's output tap as the
// file grows - the pending chip's live tail (push channel, card
// t_7624bab1). RunShell starts it immediately after Spawn, before it
// waits, so the whole inline wait streams; the shell's log is the ONE
// writer (the child holds a file descriptor on it), so the tap tails
// from its own offset: an in-process pipe would die with omega and take
// the shell down with it, which the detachment contract forbids. A
// context without a tap makes core.EmitToolOutput a no-op, and the tail
// stops when the shell exits or the call ends. Live bytes are bounded
// by the same head cap the call's own result uses: past it the tap
// announces the truncation once and stops - the log on disk keeps the
// rest for shell.read.
func tapLog(ctx context.Context, e *Entry) {
	const (
		interval = 50 * time.Millisecond
		chunk    = 32 * 1024
	)
	f, err := os.Open(e.LogPath)
	if err != nil {
		return
	}
	defer f.Close()
	buf := make([]byte, chunk)
	sent := 0
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			room := shellOutputCap - sent
			if room <= 0 {
				core.EmitToolOutput(ctx, "", true)
				return
			}
			keep := n
			if keep > room {
				keep = room
			}
			core.EmitToolOutput(ctx, string(buf[:keep]), keep < n)
			sent += keep
			if keep < n {
				return
			}
			continue
		}
		if rerr != nil && rerr != io.EOF {
			return
		}
		select {
		case <-e.done:
			return
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// RunShell executes a shell command and returns combined output. The
// command is started DETACHED under the shell registry: its own
// process group, a state row, and a log file. The call waits for it
// here, but a run cancellation (run timeout, or a converted call's
// yield deadline) never kills the shell - detachment is the contract:
// the call returns naming the id, the shell keeps running, and
// shell.read / shell.kill reach it.
func (p *ShellProvider) RunShell(ctx context.Context, args map[string]any) (string, error) {
	command, err := argString(args, "command")
	if err != nil {
		return "", err
	}
	if p.guard.DenyShell(command) {
		return "", datadir.DBDenied(command)
	}
	// Per-call workdir override (card t_ccf74d88): an explicit dir
	// wins over the run's session workspace. Empty = the workspace,
	// else the process working directory - today's behavior.
	workdir, err := dirOverride(ctx, args)
	if err != nil {
		return "", err
	}
	reg := p.reg
	if reg == nil {
		return "", errNoRegistry
	}
	e, err := reg.Spawn(ctx, command, "", workdir)
	if err != nil {
		return "", err
	}
	// Live tail for the push channel (card t_7624bab1): the pending
	// chip streams a running call's output as it lands. The tap starts
	// HERE, before the wait, so the whole inline wait streams - started
	// after it, the shell would already be done and the tap would
	// replay the finished file in one burst. The log is the child's own
	// file descriptor, so the tap reads it rather than owning a pipe (a
	// pipe dies with omega and takes the shell with it - the
	// detachment contract forbids that). The tap stops on its own when
	// the shell exits or the call is cut.
	go tapLog(ctx, e)
	select {
	case <-e.done:
		// The reaper records the exit code before it closes done.
	case <-ctx.Done():
		// The call was cut short (a run timeout, or the converted
		// call's yield deadline). The shell is detached by design: it
		// keeps running and stays addressable.
		out, total, _ := readLogHead(e.LogPath)
		return shellOutput(out, total, e.ID), fmt.Errorf(
			"shell %s is still running (pid %d): the call was interrupted (%v) - the shell outlives it; shell.read id=%s tails it, shell.kill id=%s stops it",
			e.ID, e.PID, ctx.Err(), e.ID, e.ID)
	}
	out, total, err := readLogHead(e.LogPath)
	if err != nil {
		return "", err
	}
	if code := e.ExitCode; code != nil && *code != 0 {
		return shellOutput(out, total, e.ID), fmt.Errorf("command failed: exit status %d", *code)
	}
	return shellOutput(out, total, e.ID), nil
}

// List reports the running shells (id, command, age, log path) - the
// addressing surface. A shell that already exited keeps its log for
// the retention window, but it is not listed: the id from the call's
// own result is the post-mortem handle.
func (p *ShellProvider) List(_ context.Context, _ map[string]any) (string, error) {
	if p.reg == nil {
		return "", errNoRegistry
	}
	var running []Entry
	for _, e := range p.reg.List() {
		if !e.Exited {
			running = append(running, e)
		}
	}
	if len(running) == 0 {
		return "[no running shells]", nil
	}
	idWidth := len("ID")
	for _, e := range running {
		if len(e.ID) > idWidth {
			idWidth = len(e.ID)
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-*s  %-8s  %-40s  %s\n", idWidth, "ID", "AGE", "COMMAND", "LOG")
	for i := range running {
		e := &running[i]
		fmt.Fprintf(&sb, "%-*s  %-8s  %-40s  %s\n", idWidth, e.ID, age(e.StartedAt), shorten(e.Command, 40), e.LogPath)
	}
	return sb.String(), nil
}

// Read tails one shell's log (shell.read). The read is live: the
// registry re-reads the file on every call, so a running shell's
// newest output is there - and a finished shell's log is there too.
func (p *ShellProvider) Read(_ context.Context, args map[string]any) (string, error) {
	id, err := argString(args, "id")
	if err != nil {
		return "", err
	}
	lines, err := argInt(args, "lines")
	if err != nil {
		return "", err
	}
	if p.reg == nil {
		return "", errNoRegistry
	}
	text, _, cut, err := p.reg.Read(id, lines)
	if err != nil {
		return "", err
	}
	if cut {
		text += "\n... [log truncated: showing only the tail - the agent reads the tail, never the whole log]"
	}
	return text, nil
}

// Kill stops one running shell by id (shell.kill).
func (p *ShellProvider) Kill(_ context.Context, args map[string]any) (string, error) {
	id, err := argString(args, "id")
	if err != nil {
		return "", err
	}
	if p.reg == nil {
		return "", errNoRegistry
	}
	return p.reg.Kill(id)
}

// readLogHead reads at most shellOutputCap bytes from the head of a
// shell log and reports the file's real size, so the truncation note
// can be honest about both numbers. A missing log (a shell that has
// not written anything yet) is empty output, never an error.
func readLogHead(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, nil
		}
		return "", 0, fmt.Errorf("read shell log: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", 0, fmt.Errorf("stat shell log: %w", err)
	}
	buf := make([]byte, shellOutputCap)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", 0, fmt.Errorf("read shell log: %w", err)
	}
	return string(buf[:n]), info.Size(), nil
}

// shellOutput renders a capped read: the kept bytes plus an honest
// note when output was actually dropped. The cap keeps the HEAD (the
// first shellOutputCap bytes) and drops the tail; the note names both,
// the shell's id, and the retrieval path - shell.read tails the log
// live and the log survives the shell (post-mortem retention, card
// t_304afb4e), so truncation here is a display choice, not the only
// copy.
func shellOutput(out string, total int64, id string) string {
	if total > int64(len(out)) {
		out += fmt.Sprintf("\n... [output truncated: kept the first %d bytes of %d - shell.read id=%s tails the log, which survives the shell]",
			len(out), total, id)
	}
	return out
}

// age renders a shell's elapsed wall clock compactly.
func age(startedAt int64) string {
	if startedAt <= 0 {
		return "-"
	}
	return time.Since(time.Unix(startedAt, 0)).Round(time.Second).String()
}

// shorten clips a command for the list's fixed column.
func shorten(s string, n int) string {
	if len(s) <= n || n < 4 {
		return s
	}
	return s[:n-3] + "..."
}

// ShellPlugin is the shell tool's registration face: a builtin-tier
// capsule (no Toggleable) - its tool names keep their natural "shell.*"
// spelling on the one-tenant ledger.
type ShellPlugin struct {
	provider *ShellProvider
}

// Name returns the extension name shown in /extensions and the prompt's
// tools section (the shell tool's group).
func (p *ShellPlugin) Name() string { return "shell" }

// Description implements agent.Describer.
func (p *ShellPlugin) Description() string {
	return "built-in tools: shell.run / shell.list / shell.read / shell.kill"
}

// Provides lists seam names this plugin mounts: the additive tools
// rail only (the shell family needs no other seam).
func (p *ShellPlugin) Provides() []string { return []string{"tools"} }

// Requires lists seam names that must be mounted first.
func (p *ShellPlugin) Requires() []string { return nil }

// ConfigKey names the config section this plugin reads from
// ctx.Configs. The `process:` section is the shell registry's knobs
// (log_max_bytes, kill_grace_seconds) - the section name is history,
// the machinery is the shell domain's (card t_5a49790b).
func (p *ShellPlugin) ConfigKey() string { return "process" }

// MountWithAPI implements agent.APIPlugin: registers the shell verb
// family over one registry. The guard is built from the store config
// (the instance db path); the registry from the process section (its
// home, log cap and kill grace).
func (p *ShellPlugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	var guard *datadir.Guard
	if c, ok := ctx.Configs["store"].(store.Config); ok {
		guard = datadir.NewGuard(c.DBPath)
	} else {
		guard = datadir.NewGuard("")
	}
	cfg := RegistryConfig{}
	if c, ok := ctx.Configs["process"].(RegistryConfig); ok {
		cfg = c
	}
	p.provider = NewShellProvider(guard, NewRegistry(cfg))
	for name, t := range p.provider.Tools() {
		if err := api.RegisterTool(name, t); err != nil {
			return err
		}
	}
	return nil
}

// NewShellPlugin creates the shell tool plugin.
func NewShellPlugin() *ShellPlugin { return &ShellPlugin{} }
