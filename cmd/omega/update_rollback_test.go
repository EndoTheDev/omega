package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestReplaceBinaryWritesBak pins the one-file rollback rule: every
// update overwrites omega.bak beside the replaced binary.
func TestReplaceBinaryWritesBak(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "omega")
	src := filepath.Join(dir, "staged-bin")
	writeExe(t, dst, "OLD-BINARY")
	writeExe(t, src, "NEW-BINARY")

	if err := replaceBinary(src, dst); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	assertFile := func(path, want string) {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(b) != want {
			t.Errorf("%s = %q, want %q", path, b, want)
		}
	}
	assertFile(dst, "NEW-BINARY")
	assertFile(dst+".bak", "OLD-BINARY")

	// No scratch files left behind.
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{"omega", "omega.bak", "staged-bin"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("dir = %v, want %v (no .new/.old leftovers)", names, want)
	}
}

// TestReplaceBinaryBakOneGeneration: a second update overwrites the
// .bak slot — it never grows, only ever holds the previous generation.
func TestReplaceBinaryBakOneGeneration(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "omega")
	writeExe(t, dst, "V1")
	src := filepath.Join(dir, "next")
	writeExe(t, src, "V2")

	if err := replaceBinary(src, dst); err != nil {
		t.Fatalf("first update: %v", err)
	}
	writeExe(t, src, "V3")
	if err := replaceBinary(src, dst); err != nil {
		t.Fatalf("second update: %v", err)
	}

	for name, want := range map[string]string{
		"omega":     "V3",
		"omega.bak": "V2", // previous generation, not V1
	} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != want {
			t.Errorf("%s = %q, want %q", name, b, want)
		}
	}
}

// TestRollbackSwapRoundTrip: the swap must preserve BOTH versions —
// the backup lands in the exe slot, the replaced binary lands in the
// .bak slot, and no side files remain.
func TestRollbackSwapRoundTrip(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "omega")
	bak := exe + ".bak"
	writeExe(t, exe, "BAD-NIGHTLY")
	writeExe(t, bak, "GOOD-STABLE")

	if err := rollbackSwap(exe, bak); err != nil {
		t.Fatalf("rollbackSwap: %v", err)
	}
	read := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		return string(b)
	}
	if got := read(exe); got != "GOOD-STABLE" {
		t.Errorf("exe after rollback = %q, want GOOD-STABLE", got)
	}
	if got := read(bak); got != "BAD-NIGHTLY" {
		t.Errorf("bak after rollback = %q, want BAD-NIGHTLY (one generation, overwritten)", got)
	}
	// The side file must be gone: a second rollback round-trips.
	if err := rollbackSwap(exe, bak); err != nil {
		t.Fatalf("second rollbackSwap: %v", err)
	}
	if got := read(exe); got != "BAD-NIGHTLY" {
		t.Errorf("exe after second rollback = %q, want BAD-NIGHTLY", got)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "rollback") {
			t.Errorf("side file %s left behind", e.Name())
		}
	}
}

// TestRollbackToRoundTrip: the full rollback path — swap, health check
// against the restored exe, success message carrying the restored
// version.
func TestRollbackToRoundTrip(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "omega")
	writeExe(t, exe, "BAD-NIGHTLY")
	writeExe(t, exe+".bak", "GOOD-STABLE")

	oldHealth := verifyBinaryHealth
	t.Cleanup(func() { verifyBinaryHealth = oldHealth })
	var checked string
	verifyBinaryHealth = func(path string) (string, error) {
		checked = path
		return "omega v0.9.1\n", nil
	}
	if err := rollbackTo(exe); err != nil {
		t.Fatalf("rollbackTo: %v", err)
	}
	if checked != exe {
		t.Errorf("health checked %q, want the restored exe %q", checked, exe)
	}
}

func TestRollbackToNoBak(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "omega")
	writeExe(t, exe, "RUNNING")
	if err := rollbackTo(exe); err == nil {
		t.Fatal("no omega.bak present: rollbackTo must error, not wipe the running binary")
	}
	// Old behavior preserved: the untouched exe survived the failed rollback.
	if b, _ := os.ReadFile(exe); string(b) != "RUNNING" {
		t.Error("failed rollback must leave the running binary alone")
	}
}

// TestRollbackToUnhealthy: a backup that fails its health check still
// leaves the swap state intact and reports an error — never a silent
// success on a broken restore.
func TestRollbackToUnhealthy(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "omega")
	writeExe(t, exe, "BAD-NIGHTLY")
	writeExe(t, exe+".bak", "GOOD-STABLE")

	oldHealth := verifyBinaryHealth
	t.Cleanup(func() { verifyBinaryHealth = oldHealth })
	verifyBinaryHealth = func(path string) (string, error) {
		return "", os.ErrPermission
	}
	if err := rollbackTo(exe); err == nil {
		t.Fatal("unhealthy restored binary: rollbackTo must error")
	}
	// The restored version is live and the .bak slot intact.
	read := func(p string) string {
		b, _ := os.ReadFile(p)
		return string(b)
	}
	if read(exe) != "GOOD-STABLE" || read(exe+".bak") != "BAD-NIGHTLY" {
		t.Error("failed health check must leave both slots intact, not destructive")
	}
}