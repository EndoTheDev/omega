//go:build windows

package terminal

import (
	"os/exec"
	"syscall"
)

// detachedShellCommand: no ctx, so a run cancellation never kills the shell child
// (detachChild adds the process group + job object). The command rides a RAW child
// command line, unescaped and byte-for-byte as typed - argv is never re-quoted.
func detachedShellCommand(command string) *exec.Cmd {
	c := exec.Command("cmd")
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd /C " + command}
	return c
}
