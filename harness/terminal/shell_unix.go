//go:build !windows

package terminal

import (
	"context"
	"os/exec"
)

// shellCommand builds the shell child on the unix side: the argv
// contract is correct here, sh -c takes the command as one argument.
// The windows companion carries the raw-command-line serializer
// (card t_82107785).
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	c := exec.CommandContext(ctx, "sh", "-c", command)
	shellKillSetup(c)
	return c
}

// detachedShellCommand builds the registry's shell child: no ctx, so a
// run cancellation never kills it. Detachment is the design - a long
// command outlives the turn (and the omega process) that started it.
// The argv contract is correct here, sh -c takes the command as one
// argument; the windows companion carries the raw-command-line
// serializer (card t_82107785).
func detachedShellCommand(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command)
}
