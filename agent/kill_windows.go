//go:build windows

package agent

import "os/exec"

// Windows has no POSIX process groups.
func SetupProcessGroupKill(*exec.Cmd) {}
