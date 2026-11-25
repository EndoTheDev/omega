//go:build windows

package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// instanceJob is the desktop's ownership handle: one Job Object armed with
// KILL_ON_JOB_CLOSE and held open for the desktop's lifetime. The last handle closing
// - quit, crash, hard kill - terminates every process in the job.
type instanceJob struct {
	handle windows.Handle
}

// release closes the handle, which is the kill; tests call it, production never does.
func (j *instanceJob) release() {
	if j == nil || j.handle == 0 {
		return
	}
	// A live handle fails only mid-teardown, when the kill has already happened.
	_ = windows.CloseHandle(j.handle)
	j.handle = 0
}

// newInstanceJob creates the job and arms kill-on-close on it.
func newInstanceJob() (*instanceJob, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create the instance job: %w", err)
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(handle, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return nil, fmt.Errorf("arm kill-on-close on the instance job: %w", err)
	}
	return &instanceJob{handle: handle}, nil
}

// assign puts a started child into the job. The child inherits the caller's job, so
// this is a nested job (Windows 8+): closing either job still terminates the child.
func (j *instanceJob) assign(proc *os.Process) error {
	var assignErr error
	if err := proc.WithHandle(func(handle uintptr) {
		// The CreateProcess handle carries the access AssignProcessToJobObject needs.
		assignErr = windows.AssignProcessToJobObject(j.handle, windows.Handle(handle))
	}); err != nil {
		return fmt.Errorf("read the spawned agent's handle: %w", err)
	}
	return assignErr
}
