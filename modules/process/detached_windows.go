//go:build windows

package process

import (
	"fmt"
	"os/exec"
	"syscall"
)

// CREATE_NEW_PROCESS_GROUP keeps windows shell children out of omega's
// console group so no stray Ctrl+Broadcast reaches them.
const createNewProcessGroup = 0x00000200

const processQueryLimitedInformation = 0x00001000

// detachChild: windows has no POSIX process groups; a new console
// process group is all we get. TerminateProcess (os.Process.Kill) is
// the only kill - TERM and KILL collapse into one hard stop.
func detachChild(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNewProcessGroup
}

// groupAlive probes the pid through the process table handle.
func groupAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	syscall.CloseHandle(handle)
	return true
}

// signalTerm on windows is TerminateProcess: a TERM is a KILL (the
// graceful-escalation contract degrades on windows, the OS gives us
// nothing else for detached children).
func signalTerm(pid int) error {
	return killProcess(pid)
}

// signalKill on windows is the same TerminateProcess.
func signalKill(pid int) error {
	return killProcess(pid)
}

// killProcess hard-stops one pid. An already-dead pid answers nil -
// the caller's liveness re-check settles it.
func killProcess(pid int) error {
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(handle)
	if err := syscall.TerminateProcess(handle, 1); err != nil {
		return fmt.Errorf("terminate pid %d: %w", pid, err)
	}
	return nil
}