//go:build windows

package agent

import "os/exec"

// Windows has no POSIX process groups; CommandContext's default child
// kill is all we get. cmd.exe /C children usually die with their
// console.
func SetupProcessGroupKill(*exec.Cmd) {}