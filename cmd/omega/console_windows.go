//go:build windows

package main

import (
	"errors"
	"os"
	"sync"

	"golang.org/x/sys/windows"
)

// attachParentConsole is ATTACH_PARENT_PROCESS: the console of the process that
// launched this one.
const attachParentConsole = 0xFFFFFFFF

var (
	callerConsoleOnce     sync.Once
	callerConsoleAttached bool
)

// attachCallerConsole attaches this GUI-subsystem exe to its launcher's console.
// Success means a terminal launch: the CLI loop keeps running in that terminal. A
// shell launch (Explorer, a file manager) has no parent console, so the attach fails
// and a bare `omega` opens the window instead.
//
// The answer is memoized. main attaches before its first write and the bare-launch
// routing asks again; AttachConsole fails once the process is attached, so a second
// attempt would read a terminal launch as a shell launch.
func attachCallerConsole() bool {
	callerConsoleOnce.Do(func() {
		// Sampled BEFORE the attach: AttachConsole fills the process's own std handle
		// table on success, while os.Stdin/Stdout/Stderr keep the values captured at
		// startup. A redirect or a pipe is already a usable handle and must be kept;
		// only a handle the process never had is rebound.
		missingIn := stdHandleMissing(windows.STD_INPUT_HANDLE)
		missingOut := stdHandleMissing(windows.STD_OUTPUT_HANDLE)
		missingErr := stdHandleMissing(windows.STD_ERROR_HANDLE)
		if err := attachParentProcessConsole(); err != nil {
			return
		}
		callerConsoleAttached = true
		rebindConsoleStdio(missingIn, missingOut, missingErr)
	})
	return callerConsoleAttached
}

// attachParentProcessConsole is kernel32!AttachConsole(ATTACH_PARENT_PROCESS), which
// x/sys/windows does not wrap.
func attachParentProcessConsole() error {
	ok, _, callErr := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole").Call(attachParentConsole)
	if ok != 0 {
		return nil
	}
	if callErr == nil {
		callErr = errors.New("AttachConsole failed with no error code")
	}
	return callErr
}

// rebindConsoleStdio points the streams the process had no handle for at the freshly
// attached console. A GUI-subsystem child starts with none, so without this every write
// is lost and the first stdin read is EOF.
func rebindConsoleStdio(missingIn, missingOut, missingErr bool) {
	if missingIn {
		if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
			os.Stdin = in
		}
	}
	if missingOut {
		if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
			os.Stdout = out
		}
	}
	if missingErr {
		if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
			os.Stderr = out
		}
	}
}

// stdHandleMissing reports whether the process has no usable handle for one stream.
func stdHandleMissing(kind uint32) bool {
	h, err := windows.GetStdHandle(kind)
	return err != nil || h == 0 || h == windows.InvalidHandle
}
