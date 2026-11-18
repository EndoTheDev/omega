// The shell tool rides the terminal capsule (the shell-ring shrink wave 1, Endo. The
// Tool's NAME is builtin vocabulary: "shell.run" parses as the module.verb pair
// ("shell", "run") and was the built-in tools capsule's natural name - so it registers
// on the built-in VOCABULARY rail (api.RegisterBuiltinTool: machine-critical vocabulary
// the prompt and the approval gate address byte-exact, not a module-namespaced verb),
// keeping the model-facing name unchanged across the 0.17.3 modules fold and the
// capsule's rename to `terminal`.
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

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// ShellProvider serves the shell verb family as an additive tools-seam contributor. The
// guard refuses any command that resolves into the instance database; nil is
// pass-through.
type ShellProvider struct {
	guard *datadir.Guard
	reg *Registry
}

// errNoRegistry is the loud answer for an unmounted provider. It is a wiring bug, never
// a runtime condition: MountWithAPI always sets the registry, so a nil one names itself
// instead of quietly writing into some home nobody chose.
var errNoRegistry = errors.New("shell provider has no registry: the shell plugin mounts it (MountWithAPI) or NewShellProvider supplies one")

// NewShellProvider builds the shell tool face over an explicit registry and guard. A
// nil guard is pass-through (datadir.Guard's nil receiver never denies).
func NewShellProvider(guard *datadir.Guard, reg *Registry) *ShellProvider {
	return &ShellProvider{guard: guard, reg: reg}
}

// Tools returns the shell verb family (the capsule's whole tool face).
func (p *ShellProvider) Tools() map[string]core.Tool {
	return map[string]core.Tool{
		"shell.run": {
			Description: "Run a shell command and return its stdout and stderr.\nThe command runs detached: a call still running when the turn ends keeps running past it, its result re-enters when the shell settles, and shell.list / shell.read / shell.kill address it meanwhile.",
			// Escalation-eligible: a fast command runs INLINE in the turn (zero overhead.
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

// dirOverride resolves the optional working-directory argument. A missing or
// non-directory path is a named tool error, never a cryptic exec chdir failure.
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
const shellOutputCap = 1 << 20 // 1 MB

// tapLog streams a running shell's log to the run's output tap as the file grows.
// RunShell starts it immediately after Spawn, before it waits, so the whole inline wait
// streams; the shell's log is the ONE writer (the child holds a file descriptor on it),
// so the tap tails from its own offset: an in-process pipe would die with omega and
// take the shell down with it, which the detachment contract forbids.
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

// RunShell executes a shell command and returns combined output. The call waits for it
// here, but a run cancellation (run timeout, or a converted call's yield deadline)
// never kills the shell - detachment is the contract: the call returns naming the id,
// the shell keeps running, and shell.read / shell.kill reach it.
func (p *ShellProvider) RunShell(ctx context.Context, args map[string]any) (string, error) {
	command, err := argString(args, "command")
	if err != nil {
		return "", err
	}
	if p.guard.DenyShell(command) {
		return "", datadir.DBDenied(command)
	}
	// Per-call workdir override: an explicit dir wins over the run's session workspace.
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
	// Live tail for the push channel. The log is the child's own file descriptor, so the
	// tap reads it rather than owning a pipe (a pipe dies with omega and takes the shell
	// with it - the detachment contract forbids that).
	go tapLog(ctx, e)
	select {
	case <-e.done:
	case <-ctx.Done():
		out, total, _ := readLogHead(e.LogPath)
		if core.RunStopped(ctx) {
			note := "the shell was killed with the turn"
			if msg, kerr := p.reg.Kill(e.ID); kerr != nil {
				note = "killing the shell failed: " + kerr.Error()
			} else if msg != "" {
				note = msg
			}
			return shellOutput(out, total, e.ID), fmt.Errorf(
				"shell %s stopped: the user ended the turn (%v) - %s; the log stays readable (shell.read id=%s)",
				e.ID, ctx.Err(), note, e.ID)
		}
		// The call was cut short (a run timeout, or the converted call's yield deadline).
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

// List reports the running shells (id, command, age, log path).
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

// Read tails one shell's log (shell.read).
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

// readLogHead reads at most shellOutputCap bytes from the head of a shell log and. A
// missing log (a shell that has not written anything yet) is empty output, never an
// error.
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

// shellOutput renders a capped read.
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

// ShellPlugin is gone: the tool family and the pane are ONE registration. The tool
// names never moved: shell.run / shell.list / shell.read / shell.kill stay the API
// surface the model and the approval gate know.
func NewShellPlugin() *Plugin { return NewPlugin("", "") }
