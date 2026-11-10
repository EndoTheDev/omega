//go:build windows

package terminal

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// CREATE_NEW_PROCESS_GROUP keeps windows shell children out of omega's
// console group so no stray Ctrl+Broadcast reaches them.
const createNewProcessGroup = 0x00000200

const (
	processTerminate               = 0x0001
	processSetQuota                = 0x0100
	processQueryLimitedInformation = 0x00001000
)

// detachChild: windows has no POSIX process groups. The tree guard is
// a JOB object with KILL_ON_JOB_CLOSE: every descendant of the shell
// lands in it, TerminateJobObject tears the tree down in one stop, and
// a job handle nobody releases (a crashed omega) still reaps what
// survived inside. TERM and KILL collapse into one hard stop for the
// tree - the OS offers a detached grandchild nothing else.
func detachChild(cmd *exec.Cmd) treeKiller {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNewProcessGroup
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		// The floor contract holds without the job: a per-pid
		// TerminateProcess, like every orphan this registry reaps.
		return treeKiller{}
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return treeKiller{}
	}
	return treeKiller{job: job}
}

// treeKiller is the detached child's tree handle: attach right after
// Start, term/kill tear the tree down in one stop, close releases the
// job (which kills whatever still lives inside). The zero treeKiller
// is valid: every operation degrades to the pid floor.
type treeKiller struct{ job windows.Handle }

// attach binds the started child (and through it every descendant) to
// the job. A binding that fails leaves the pid-level kill standing.
func (t treeKiller) attach(cmd *exec.Cmd) {
	if t.job == 0 || cmd.Process == nil {
		return
	}
	h, err := windows.OpenProcess(
		processTerminate|processSetQuota|processQueryLimitedInformation,
		false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.AssignProcessToJobObject(t.job, h)
}

// term is the tree kill: one TerminateJobObject. pid is the fallback
// floor when the child never joined a job.
func (t treeKiller) term(pid int) error {
	if t.job != 0 {
		return windows.TerminateJobObject(t.job, 1)
	}
	return killProcess(pid)
}

// kill is the same hard stop as term: windows offers no graceful
// escalation for a detached tree.
func (t treeKiller) kill(pid int) error { return t.term(pid) }

// close releases the job. KILL_ON_JOB_CLOSE kills whatever the child
// left inside (a grandchild still holding its log file) - the reaper
// calls it once the shell itself has exited, so the log rename and a
// later directory removal never race a live handle.
func (t treeKiller) close() {
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
	}
}

// stillActive is the windows sentinel for "no exit code yet"
// (STATUS_PENDING); every other value means the process has shot.
const stillActive = 259

// groupAlive probes the pid's exit code: an openable handle alone is a
// false verdict on windows - a terminated-but-not-yet-released pid
// still opens, so Kill would report a process it just killed as
// "still alive". STILL_ACTIVE means alive; any exit code means dead.
func groupAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}

// signalTerm on windows is TerminateProcess: a TERM is a KILL (the
// graceful-escalation contract degrades on windows, the OS gives us
// nothing else for detached children). Orphans only - an owned child
// goes through its treeKiller.
func signalTerm(pid int) error {
	return killProcess(pid)
}

// signalKill on windows is the same TerminateProcess.
func signalKill(pid int) error {
	return killProcess(pid)
}

// killProcess hard-stops one pid. An already-dead pid answers nil -
// the caller's liveness re-check settles it. The handle carries
// PROCESS_TERMINATE: a query-only handle answers Access denied on
// every TerminateProcess.
func killProcess(pid int) error {
	handle, err := syscall.OpenProcess(
		processTerminate|processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(handle)
	if err := syscall.TerminateProcess(syscall.Handle(handle), 1); err != nil {
		return fmt.Errorf("terminate pid %d: %w", pid, err)
	}
	return nil
}
