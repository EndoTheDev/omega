//go:build !windows

package terminal

import "os/exec"

// detachedShellCommand: no ctx, so a run cancellation never kills the shell child.
func detachedShellCommand(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command)
}
