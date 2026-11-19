//go:build windows

package terminal

import (
	"os/exec"
	"syscall"
)

// detachedShellCommand builds the registry's shell child: no ctx, so a
// run cancellation never kills it; the caller's detachChild adds the
// process-group flag and the job-object guard. The command rides a RAW
// child command line 
// process registry carries): CreateProcess's lpApplicationName (the
// resolved cmd.exe path Go computes) pins the executable, so the
// SysProcAttr.CmdLine string reaches the shell byte-for-byte, exactly as
// the agent typed it - no argv re-escaping, no quote-mangling class.
func detachedShellCommand(command string) *exec.Cmd {
	c := exec.Command("cmd")
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd /C " + command}
	return c
}
