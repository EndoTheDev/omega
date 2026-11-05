//go:build !linux

// Package terminal - the !linux process stub: this build cannot spawn a
// pty, so every spawn refuses and the routes answer the refusal. The
// module still compiles and mounts - an empty session list is the
// contract, and the spawn error names the box it does not work on.
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

var errNoPTY = errors.New("terminal: pty is linux-only on this build")

func spawnProcess(workdir string) (process, error) {
	return nil, errNoPTY
}
