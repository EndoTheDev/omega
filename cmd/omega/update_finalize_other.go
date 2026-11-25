//go:build !windows

package main

// The Unix finalize mechanism (R2, the exec shape): install the staged binary, then
// syscall.Exec it in place - PID stays, the restart is atomic, and no supervisor outlives
// it. The supervised boot on this host is the pre-flight copy-boot; an exec that
// fails restores omega.bak and the caller restores the snapshots.

import (
	"fmt"
	"os"
	"syscall"
)

// finalizePlatform implements the host-mode seam for Unix.
func finalizePlatform(req finalizeRequest) (finalizeOutcome, error) {
	if err := req.Swap(req.StagedPath, req.ExePath); err != nil {
		return finalizeOutcome{Serving: true}, fmt.Errorf("install the staged binary: %w", err)
	}
	if err := execSelf(req.ExePath, req.ConfigPath); err != nil {
		_ = req.Rollback(req.ExePath, req.ExePath+".bak")
		return finalizeOutcome{Serving: true}, fmt.Errorf("exec the new binary: %w", err)
	}
	// Unreachable on success: the image was replaced.
	return finalizeOutcome{}, nil
}

// execSelf replaces this process with the newly installed binary. A test seam: the exec
// itself cannot be exercised without ending the test binary.
var execSelf = func(exe, configPath string) error {
	args := []string{exe, "serve"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	return syscall.Exec(exe, args, os.Environ())
}

// spawnFinalizeChild never runs on Unix: the exec path needs no detached spawn.
func spawnFinalizeChild(string) (finalizeChild, error) {
	return nil, fmt.Errorf("the Unix finalize execs in place; there is no detached spawn")
}
