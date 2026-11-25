//go:build !windows

package main

// The Unix host mode: the exec shape. The exec itself cannot be exercised inside a test
// binary, so the seam records the call and the rollback path is proven with real files.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFinalizePlatformExecsInPlace(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "omega")
	staged := exe + stagedSuffix
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatalf("exe: %v", err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o700); err != nil {
		t.Fatalf("staged: %v", err)
	}
	prev := execSelf
	defer func() { execSelf = prev }()
	var execExe, execConfig string
	execSelf = func(e, c string) error {
		execExe, execConfig = e, c
		return nil
	}
	req := finalizeRequest{
		ExePath:    exe,
		StagedPath: staged,
		NewVersion: "v9.9.9",
		ConfigPath: filepath.Join(dir, "config.yaml"),
		Swap:       replaceBinary,
		Rollback:   rollbackSwap,
	}
	out, err := finalizePlatform(req)
	if err != nil {
		t.Fatalf("finalizePlatform: %v", err)
	}
	if out.Serving {
		t.Fatal("the exec shape must replace the process, not leave the old one serving")
	}
	if execExe != exe || execConfig != req.ConfigPath {
		t.Fatalf("exec(%q, %q), want the installed binary and the booted config", execExe, execConfig)
	}
	if body, _ := os.ReadFile(exe); string(body) != "new" {
		t.Fatalf("installed binary = %q, want the staged one", body)
	}
}

func TestFinalizePlatformExecFailureRestoresBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "omega")
	staged := exe + stagedSuffix
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatalf("exe: %v", err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o700); err != nil {
		t.Fatalf("staged: %v", err)
	}
	prev := execSelf
	defer func() { execSelf = prev }()
	execSelf = func(string, string) error { return errors.New("exec: permission denied") }

	req := finalizeRequest{
		ExePath:    exe,
		StagedPath: staged,
		NewVersion: "v9.9.9",
		ConfigPath: filepath.Join(dir, "config.yaml"),
		Swap:       replaceBinary,
		Rollback:   rollbackSwap,
	}
	out, err := finalizePlatform(req)
	if err == nil {
		t.Fatal("a failed exec read as a successful restart")
	}
	if !out.Serving {
		t.Fatal("a failed exec must report the old process still serving (nothing was replaced)")
	}
	if body, _ := os.ReadFile(exe); string(body) != "old" {
		t.Fatalf("installed binary = %q after a failed exec, want omega.bak restored", body)
	}
}
