//go:build windows

package terminal

import (
	"context"
	"os/exec"
	"syscall"
)

// shellCommand builds the shell child for command. CreateProcess's lpApplicationName
// (the resolved cmd.exe path Go computes from exec.CommandContext(ctx, "cmd")) pins the
// executable, so the SysProcAttr.CmdLine string reaches the shell byte-for-byte,
// exactly as the agent typed it - no escaping layer, no mangling class.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	c := exec.CommandContext(ctx, "cmd")
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd /C " + command}
	return c
}

// detachedShellCommand builds the registry's shell child: no ctx, so a run cancellation
// never kills it; the caller's detachChild adds the process-group flag and the
// job-object guard.
func detachedShellCommand(command string) *exec.Cmd {
	c := exec.Command("cmd")
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd /C " + command}
	return c
}
