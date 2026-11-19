//go:build unix

package terminal

import (
	"errors"
	"os/exec"
	"syscall"
)

// detachChild puts the child in its own process group.
func detachChild(cmd *exec.Cmd) treeKiller {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return treeKiller{}
}

// treeKiller is the windows job-object guard.
type treeKiller struct{}

func (treeKiller) attach(cmd *exec.Cmd) {}

// term is the pid-level TERM.
func (treeKiller) term(pid int) error { return signalTerm(pid) }

func (treeKiller) kill(pid int) error { return signalKill(pid) }

func (treeKiller) close() {}

// groupAlive asks the kernel whether the pid exists (signal 0).
func groupAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// signalTerm signals the child's whole process group. The detached shell is the group
// leader, so TERM reaches the command's children - a spawned pipeline must not leave
// grandchildren running after a kill.
func signalTerm(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}

// signalKill escalates to SIGKILL on the process group. Return
// ESRCH is already-dead - callers re-check liveness either way.
func signalKill(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
