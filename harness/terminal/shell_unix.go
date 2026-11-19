//go:build !windows

package terminal

import "os/exec"

// detachedShellCommand builds the registry's shell child: no ctx, so a run cancellation
// never kills it. The argv contract is correct here, sh -c takes the command as one
// argument; the windows companion carries the raw-command-line serializer.
func detachedShellCommand(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command)
}
