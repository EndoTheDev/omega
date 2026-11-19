//go:build windows

package agent

import "os/exec"

func SetupProcessGroupKill(*exec.Cmd) {}
