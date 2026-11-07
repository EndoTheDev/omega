//go:build windows

package terminal

import (
	"context"
	"os/exec"
	"syscall"
)

// shellCommand builds the shell child for command: cmd /C with the
// shell string riding a RAW child command line (card t_82107785, the
// same serializer law the process registry carries). The old path
// handed command to exec.CommandContext as one argv string, and Go's
// argv escaping re-wrapped it: embedded quotes became \" sequences,
// cmd passed the backslashes through verbatim, and the child
// re-parsed them as literal quote characters - `powershell -Command
// "Write-Output hi"` arrived as a string literal PowerShell echoed
// instead of running, and $vars inside quoted scripts vanished into
// the same mangling. CreateProcess's lpApplicationName (the resolved
// cmd.exe path Go computes from exec.CommandContext(ctx, "cmd"))
// pins the executable, so the SysProcAttr.CmdLine string reaches the
// shell byte-for-byte, exactly as the agent typed it - no escaping
// layer, no mangling class.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	c := exec.CommandContext(ctx, "cmd")
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd /C " + command}
	return c
}
