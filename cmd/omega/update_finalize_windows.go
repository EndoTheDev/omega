//go:build windows

package main

// The Windows finalize mechanism: swap the staged exe into place, spawn the new
// binary DETACHED, supervise the same 30s window from the parent, then exit. Windows has
// no syscall.Exec, so the parent is the supervisor: a finalize that left a half-swapped
// install or an unsupervised process would be a defect, not a platform variance.

import (
	"fmt"
	"time"
)

// finalizePlatform implements the host-mode seam for Windows.
func finalizePlatform(req finalizeRequest) (finalizeOutcome, error) {
	// The swap is atomic per file: the running image is renamed into omega.bak and the
	// staged binary copied into its place, exactly like the CLI's replaceBinary.
	if err := req.Swap(req.StagedPath, req.ExePath); err != nil {
		return finalizeOutcome{Serving: true}, fmt.Errorf("swap the staged binary: %w", err)
	}
	if req.Stop != nil {
		req.Stop()
	}
	// The child binds the same port: the parent's listener must be released first.
	if err := waitPortFree(req.PortAddr, 20*time.Second); err != nil {
		_ = req.Rollback(req.ExePath, req.ExePath+".bak")
		return finalizeOutcome{}, fmt.Errorf("release the serving port: %w", err)
	}
	child, err := req.Spawn(req.ConfigPath)
	if err != nil {
		_ = req.Rollback(req.ExePath, req.ExePath+".bak")
		return finalizeOutcome{}, fmt.Errorf("start the new binary: %w", err)
	}
	if err := superviseWindow(req); err != nil {
		if child != nil {
			_ = child.Kill()
		}
		_ = req.Rollback(req.ExePath, req.ExePath+".bak")
		return finalizeOutcome{}, err
	}
	return finalizeOutcome{}, nil
}

// spawnFinalizeChild starts the installed binary detached, exactly like the desktop's
// instance spawn but with no job object: the replacement server outlives the parent
// that supervises the swap.
func spawnFinalizeChild(configPath string) (finalizeChild, error) {
	proc, err := startDetachedServe(configPath)
	if err != nil {
		return nil, err
	}
	return proc, nil
}
