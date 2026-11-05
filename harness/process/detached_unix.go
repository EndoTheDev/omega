//go:build unix

package process

import (
	"errors"
	"os/exec"
	"syscall"
)

// detachChild puts the child in its own process group: no omega signal
// can reach it, and the group kill targets only this child's tree. A
// dying omega does not kill the child - detachment is the design (long
// transfers survive agent restarts).
func detachChild(cmd *exec.Cmd) treeKiller {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return treeKiller{}
}

// treeKiller is the windows job-object guard; on unix the process
// GROUP is the tree (signalTerm/signalKill target -pid), so every
// operation is the pid-level call and attach/close are no-ops.
type treeKiller struct{}

func (treeKiller) attach(cmd *exec.Cmd) {}

// term is the pid-level TERM: the detached shell is its group leader,
// so the signal reaches the command's children too.
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

// signalTerm signals the child's whole process group. The detached
// shell is the group leader, so TERM reaches the command's children -
// a spawned pipeline must not leave grandchildren running after a
// kill.
func signalTerm(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}

// signalKill escalates to SIGKILL on the process group. Return
// ESRCH is already-dead - callers re-check liveness either way.
func signalKill(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}