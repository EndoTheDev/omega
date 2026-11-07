//go:build !linux && !windows

// Package terminal - the stub build: linux carries the pty door in
// pty_linux.go and windows carries ConPTY in pty_windows.go; on every
// other platform the module still compiles and mounts - spawn refuses,
// the routes answer the refusal, an empty session list is the contract,
// and the spawn error names the box it does not work on.
package terminal

import "errors"

type stubProcess struct{}

func (stubProcess) Read([]byte) (int, error)    { return 0, errNoPTY }
func (stubProcess) Write([]byte) (int, error)   { return 0, errNoPTY }
func (stubProcess) Close() error                { return nil }
func (stubProcess) Resize(uint16, uint16) error { return errNoPTY }
func (stubProcess) Done() <-chan struct{}       { return nil }
func (stubProcess) Kill() error                 { return nil }
func (stubProcess) ExitCode() int               { return -1 }

var errNoPTY = errors.New("terminal: pty is not supported on this platform build")

func spawnProcess(workdir string) (process, error) {
	return nil, errNoPTY
}
