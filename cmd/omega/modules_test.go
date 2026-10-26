package main

// The E2 host-side tests: the omega.json write boot shares with the
// UI toggle (Q7's one code path - the file LoadDisabled reads is the
// file SaveDisabled writes), and the disabled-list derivation from
// the live registry.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveDisabledRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omega.json")
	if err := SaveDisabled(path, []string{"kanban", "web"}); err != nil {
		t.Fatal(err)
	}
	// LoadDisabled reads the same file boot reads: the write must be
	// legible to it (one code path, Q7).
	off, err := LoadDisabled(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(off) != 2 || off[0] != "kanban" || off[1] != "web" {
		t.Fatalf("LoadDisabled read %v, want [kanban web]", off)
	}
}

func TestSaveDisabledPreservesOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omega.json")
	if err := os.WriteFile(path, []byte(`{"note":"hand-written"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveDisabled(path, []string{"kanban"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !contains(body, "hand-written") || !contains(body, "kanban") {
		t.Fatalf("the write lost keys: %s", body)
	}
}

func TestSaveDisabledEmptyRemovesKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omega.json")
	if err := SaveDisabled(path, []string{"kanban"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveDisabled(path, nil); err != nil {
		t.Fatal(err)
	}
	off, err := LoadDisabled(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(off) != 0 {
		t.Fatalf("the empty list must clear the toggle state, got %v", off)
	}
}

func TestSaveDisabledLoudOnBrokenFile(t *testing.T) {
	// A malformed omega.json must fail the write loud: boot parses
	// this file, and a silent overwrite would destroy the state boot
	// needs.
	path := filepath.Join(t.TempDir(), "omega.json")
	if err := os.WriteFile(path, []byte(`{disabled`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveDisabled(path, []string{"kanban"}); err == nil {
		t.Fatal("a broken omega.json must fail the write loud")
	}
}