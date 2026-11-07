//go:build !windows

package process

import "os/exec"

// shellCommand builds the detached shell child on the unix side: the
// argv contract is correct here, sh -c takes the command as one
// argument. The windows companion carries the raw-command-line
// serializer (card t_82107785).
func shellCommand(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command)
}
