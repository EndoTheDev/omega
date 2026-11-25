//go:build windows

package main

// The Windows host mode exercised through its own seams: swap, detached spawn, the
// supervised window, and the automatic revert. No test spawns a real process or waits the
// real window.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// closedPortAddr reserves and releases a loopback port, so waitPortFree sees a free port.
func closedPortAddr(t *testing.T) string {
	t.Helper()
	port, err := freeLoopbackPort()
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	return fmt.Sprintf("127.0.0.1:%d", port)
}

type countingChild struct{ kills int }

func (c *countingChild) Kill() error { c.kills++; return nil }

// TestFinalizePlatformSwapSpawnSupervise: the Windows mechanism installs the staged
// binary, stops the parent, spawns the new binary detached, and passes the window.
func TestFinalizePlatformSwapSpawnSupervise(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "omega.exe")
	staged := exe + stagedSuffix
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatalf("exe: %v", err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o700); err != nil {
		t.Fatalf("staged: %v", err)
	}
	var stopped, spawned bool
	req := finalizeRequest{
		ExePath:    exe,
		StagedPath: staged,
		NewVersion: "v9.9.9",
		ConfigPath: filepath.Join(dir, "config.yaml"),
		Origin:     "http://127.0.0.1:1",
		PortAddr:   closedPortAddr(t),
		Window:     time.Second,
		Stop:       func() { stopped = true },
		Probe:      func(context.Context, string, string) error { return nil },
		Spawn: func(string) (finalizeChild, error) {
			spawned = true
			return fakeChild{}, nil
		},
		Swap:     replaceBinary,
		Rollback: rollbackSwap,
		Sleep:    func(time.Duration) {},
	}
	out, err := finalizePlatform(req)
	if err != nil {
		t.Fatalf("finalizePlatform: %v", err)
	}
	if out.Serving {
		t.Fatal("a successful finalize claimed the old process still serves")
	}
	if !stopped || !spawned {
		t.Fatalf("stopped=%v spawned=%v, want both (the parent must release the port before the child boots)", stopped, spawned)
	}
	if body, _ := os.ReadFile(exe); string(body) != "new" {
		t.Fatalf("installed binary = %q, want the staged one", body)
	}
	if body, _ := os.ReadFile(exe + ".bak"); string(body) != "old" {
		t.Fatalf("omega.bak = %q, want the replaced binary", body)
	}
}

// TestFinalizePlatformWindowFailReverts: a stale server answering /health never passes
// (version mismatch), the child is killed, and the old binary is back in place.
func TestFinalizePlatformWindowFailReverts(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "omega.exe")
	staged := exe + stagedSuffix
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatalf("exe: %v", err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o700); err != nil {
		t.Fatalf("staged: %v", err)
	}
	child := &countingChild{}
	req := finalizeRequest{
		ExePath:    exe,
		StagedPath: staged,
		NewVersion: "v9.9.9",
		ConfigPath: filepath.Join(dir, "config.yaml"),
		Origin:     "http://127.0.0.1:1",
		PortAddr:   closedPortAddr(t),
		Window:     40 * time.Millisecond,
		Stop:       func() {},
		Probe: func(context.Context, string, string) error {
			return errors.New(`/health version "v0.19.0", want "v9.9.9"`)
		},
		Spawn:    func(string) (finalizeChild, error) { return child, nil },
		Swap:     replaceBinary,
		Rollback: rollbackSwap,
		Sleep:    func(time.Duration) {},
	}
	out, err := finalizePlatform(req)
	if err == nil {
		t.Fatal("a window that never reported the new version passed")
	}
	if out.Serving {
		t.Fatal("a failed window left the caller thinking the old process still serves")
	}
	if child.kills != 1 {
		t.Fatalf("child kills = %d, want 1 (an unsupervised new process would be a defect)", child.kills)
	}
	if body, _ := os.ReadFile(exe); string(body) != "old" {
		t.Fatalf("installed binary = %q after the revert, want the old one", body)
	}
	if body, _ := os.ReadFile(exe + ".bak"); string(body) != "new" {
		t.Fatalf("omega.bak = %q after the revert, want the failed release recorded one generation back", body)
	}
}
