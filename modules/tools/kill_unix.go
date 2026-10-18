package tools

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// setProcessGroup + killProcessGroup make ctx cancellation kill the
// WHOLE process tree: CommandContext's default kills only the direct
// child (sh), while grandchildren holding the output pipe stall
// CombinedOutput past any timeout - the exact hang the wall-clock cap
// exists to stop (seen live on CI: sh -c "sleep 30" blocked 30s).
// WaitDelay bounds the pipe copy after the kill as a backstop.
func shellKillSetup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 3 * time.Second
}

// compile-time: keep the context import honest for future callers
var _ = context.Background
