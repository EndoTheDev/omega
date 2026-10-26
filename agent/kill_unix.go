//go:build unix

package agent

import (
	"os/exec"
	"syscall"
	"time"
)

// SetupProcessGroupKill makes ctx cancellation kill the WHOLE process
// tree: CommandContext's default kills only the direct child, while
// grandchildren holding the output pipe stall the caller past any
// timeout (seen live: sh -c "sleep 30" blocked 30s). Setpgid puts the
// child in its own group; Cancel kills the group; WaitDelay bounds the
// pipe copy after the kill as a backstop.
func SetupProcessGroupKill(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 3 * time.Second
}
