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
package terminal

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
	"github.com/EndoTheDev/omega/harness/store"
)

// ShellProvider serves the shell tool as an additive tools-seam
// contributor. The guard refuses any command that resolves into the
// instance database (ENDO'S LAW 2026-10-18); nil is pass-through.
type ShellProvider struct {
	guard *datadir.Guard
}

// Tools returns the shell tool map (the capsule's one tool).
func (p *ShellProvider) Tools() map[string]core.Tool {
	return map[string]core.Tool{
		"shell.run": {
			Description: "Run a shell command and return its stdout and stderr.",
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
	return s, nil
}

// shellOutputCap bounds how much of a command's combined output
// buffers in memory: CombinedOutput buffered EVERYTHING before the
// agent loop's MaxToolOutput truncation ever ran, so 'cat 500MB' or a
// forever-streaming command ballooned omega's RSS into an OOM that
// killed the server holding the store.
const shellOutputCap = 1 << 20 // 1 MB

// cappedBuffer accepts all writes but keeps at most shellOutputCap
// bytes (the head), counting only the DROPPED tail in over for an
// honest truncation note. The pre-fix shape counted every written
// byte in over (card t_304afb4e): over was total-written, not
// overflow, so the marker fired on ANY output and the reported total
// double-counted the kept bytes - an 8-byte echo read as a
// truncation, and a 2096-byte `omega --help` reported "4192 bytes
// total". The condition (over > 0) was honest; the accumulator
// underneath it lied.
type cappedBuffer struct {
	// ctx is the call's context: its run output tap (core.EmitToolOutput)
	// is the live-stream source for the pending chip (push channel, card
	// t_7624bab1). A context without a tap streams nothing - the shell
	// tool itself never knows whether anyone listens.
	ctx  context.Context
	mu   sync.Mutex
	buf  bytes.Buffer
	over int64
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	room := shellOutputCap - b.buf.Len()
	kept := len(p)
	if room <= 0 {
		kept = 0
	} else if kept > room {
		kept = room
	}
	if kept > 0 {
		b.buf.Write(p[:kept]) //nolint:errcheck // bytes.Buffer never errors
	}
	b.over += int64(len(p) - kept)
	b.mu.Unlock()
	// The kept bytes are the chip's tail as they land; a write that lost
	// bytes carries the truncation marker on the streaming path too
	// (t_304afb4e's law, same as shellOutput's final note).
	if kept > 0 || kept < len(p) {
		core.EmitToolOutput(b.ctx, string(p[:kept]), kept < len(p))
	}
	return len(p), nil
}

// RunShell executes a shell command and returns combined output. The
// command runs under the call ctx: a cancelled ctx (the detached run's
// wall-clock cap) kills the process - without this, a command that
// never returns hangs the tool loop past any run timeout. Output is
// capped at shellOutputCap bytes (cappedBuffer), not buffered whole.
func (p *ShellProvider) RunShell(ctx context.Context, args map[string]any) (string, error) {
	command, err := argString(args, "command")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command must not be empty")
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

	cmd := shellCommand(ctx, command)
	if runtime.GOOS == "windows" {
		// Windows exec carries no process-group kill (the 09828e16
		// law): the ctx kill takes cmd.exe, but a pipe child (sleep,
		// a long compiler) outlives it and holds cmd.Wait() past any
		// cap. WaitDelay closes the pipes a bounded grace after the
		// shell dies - the cancelled run actually returns.
		cmd.WaitDelay = 500 * time.Millisecond
	}
	// A session-rooted run executes INSIDE its workspace (card
	// t_9de5b009): relative commands (ls, grep, go test...) operate on
	// the session's folder. No workspace = the process working
	// directory, exactly as before.
	cmd.Dir = core.RunWorkspace(ctx)
	if workdir != "" {
		cmd.Dir = workdir
	}
	outBuf := &cappedBuffer{ctx: ctx}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("command failed: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return shellOutput(outBuf), fmt.Errorf("command failed: %w", err)
	}
	return shellOutput(outBuf), nil
}

// shellOutput renders a capped buffer: the kept bytes plus an honest
// note when output was actually dropped. The cap keeps the HEAD (the
// first shellOutputCap bytes) and drops the tail; the note names both
// and the retrieval path (process.spawn persists the full log to
// disk, readable after the process exits - truncation here is a
// display choice, not the only copy).
func shellOutput(b *cappedBuffer) string {
	out := b.buf.String()
	if b.over > 0 {
		out += fmt.Sprintf("\n... [output truncated: kept the first %d bytes of %d - rerun via process.spawn to keep the full log on disk]",
			b.buf.Len(), b.over+int64(b.buf.Len()))
	}
	return out
}

// ShellPlugin is the shell tool's registration face: a builtin-tier
// capsule (no Toggleable) - its tool name keeps its natural "shell.run"
// spelling on the one-tenant ledger.
type ShellPlugin struct {
	provider *ShellProvider
}

// Name returns the extension name shown in /extensions and the prompt's
// tools section (the shell tool's group).
func (p *ShellPlugin) Name() string { return "shell" }

// Description implements agent.Describer.
func (p *ShellPlugin) Description() string {
	return "built-in tool: shell.run"
}

// Provides lists seam names this plugin mounts: the additive tools
// rail only (the shell tool needs no other seam).
func (p *ShellPlugin) Provides() []string { return []string{"tools"} }

// Requires lists seam names that must be mounted first.
func (p *ShellPlugin) Requires() []string { return nil }

// ConfigKey names the config section this plugin reads from
// ctx.Configs. The shell tool reads no section of its own (the db-door
// guard reads the STORE config from the same mount map).
func (p *ShellPlugin) ConfigKey() string { return "" }

// MountWithAPI implements agent.APIPlugin: registers the shell tool.
// The guard is built from the store config (the instance db path).
func (p *ShellPlugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	var guard *datadir.Guard
	if c, ok := ctx.Configs["store"].(store.Config); ok {
		guard = datadir.NewGuard(c.DBPath)
	} else {
		guard = datadir.NewGuard("")
	}
	p.provider = &ShellProvider{guard: guard}
	for name, t := range p.provider.Tools() {
		if err := api.RegisterTool(name, t); err != nil {
			return err
		}
	}
	return nil
}

// NewShellPlugin creates the shell tool plugin.
func NewShellPlugin() *ShellPlugin { return &ShellPlugin{} }
