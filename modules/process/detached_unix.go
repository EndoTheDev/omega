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
func detachChild(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

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