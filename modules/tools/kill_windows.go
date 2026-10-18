package tools

import "os/exec"

// Windows has no process groups in the POSIX sense; CommandContext's
// default child kill is all we get. cmd.exe /C children usually die
// with their console.
func shellKillSetup(*exec.Cmd) {}
