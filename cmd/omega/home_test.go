package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCwd verifies that cwd() returns a non-empty string matching
// the actual working directory.
func TestCwd(t *testing.T) {
	got := cwd()
	if got == "" {
		t.Fatal("cwd() returned empty string, want non-empty")
	}
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if got != want {
		t.Errorf("cwd() = %q, want %q", got, want)
	}
}

// TestOmegaHomeEnvVar verifies the OMEGA_HOME env var branch.
func TestOmegaHomeEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OMEGA_HOME", dir)
	if got := omegaHome(); got != dir {
		t.Errorf("omegaHome() = %q, want %q", got, dir)
	}
}

// TestOmegaHomeBinaryDir verifies the os.Executable() branch when
// OMEGA_HOME is unset.
func TestOmegaHomeBinaryDir(t *testing.T) {
	t.Setenv("OMEGA_HOME", "")
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	want := filepath.Dir(exe)
	if got := omegaHome(); got != want {
		t.Errorf("omegaHome() = %q, want %q", got, want)
	}
}

// TestOmegaHomeFallback verifies that omegaHome() returns a non-empty
// string when OMEGA_HOME is unset (the os.Executable() branch should
// succeed in the test binary, so this confirms a valid path is returned).
func TestOmegaHomeFallback(t *testing.T) {
	t.Setenv("OMEGA_HOME", "")
	got := omegaHome()
	if got == "" {
		t.Fatal("omegaHome() returned empty string, want non-empty")
	}
}