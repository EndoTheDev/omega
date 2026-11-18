//go:build windows

package terminal

import "os/exec"

// Windows has no process groups in the POSIX sense.
func shellKillSetup(*exec.Cmd) {}
