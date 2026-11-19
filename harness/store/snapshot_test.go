package store

// snapshot_test.go: the clone law's store step. The failure this pins is the one a byte
// copy cannot survive: rows living in the -wal of a store that is still OPEN (the
// serving daemon's own store, or a wake child's while it runs) must land in the
// snapshot.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

func TestSnapshotStoreCopiesALiveWALStore(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "omega.db")
	st, err := Open(src)
	if err != nil {
		t.Fatalf("open source store: %v", err)
	}
	// Deliberately NOT closed before the snapshot. The rows below sit in the -wal until a
	// checkpoint, which a clone never gets to wait.
	ctx := context.Background()
	if err := st.CreateAgentSession(ctx, "scout-sess", "", "probe", "scout"); err != nil {
		st.Close()
		t.Fatalf("create session: %v", err)
	}
	if err := st.AppendMessage(ctx, "scout-sess", core.NewUser("wake prompt")); err != nil {
		st.Close()
		t.Fatalf("append message: %v", err)
	}

	dst := filepath.Join(dir, "clone", "omega.db")
	if err := SnapshotStore(src, dst); err != nil {
		st.Close()
		t.Fatalf("snapshot: %v", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dst + suffix); err == nil {
			t.Errorf("snapshot left a %s sidecar - the copy is not self-contained", suffix)
		}
	}
	db, err := sql.Open("sqlite", dst)
	if err != nil {
		st.Close()
		t.Fatalf("open snapshot: %v", err)
	}
	defer db.Close()
	var agentTag, count string
	if err := db.QueryRow(`SELECT agent FROM sessions WHERE id = 'scout-sess'`).Scan(&agentTag); err != nil {
		st.Close()
		t.Fatalf("snapshot lost the session row: %v", err)
	}
	if agentTag != "scout" {
		t.Errorf("snapshot session agent = %q, want scout", agentTag)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = 'scout-sess'`).Scan(&count); err != nil {
		st.Close()
		t.Fatalf("snapshot lost the message rows: %v", err)
	}
	if count != "1" {
		t.Errorf("snapshot messages = %s, want 1", count)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close source store: %v", err)
	}
	// The clone is independent after the source dies (clone + delete is the rename path).
	db2, err := sql.Open("sqlite", dst)
	if err != nil {
		t.Fatalf("reopen snapshot: %v", err)
	}
	defer db2.Close()
	if err := db2.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil || count != "1" {
		t.Errorf("snapshot after source close: count=%q err=%v", count, err)
	}
}

func TestSnapshotStoreMissingSourceIsANoOp(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "clone", "omega.db")
	if err := SnapshotStore(filepath.Join(dir, "absent.db"), dst); err != nil {
		t.Fatalf("missing source: %v", err)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Errorf("a missing source must not create a destination store")
	}
}

func TestSnapshotStoreRefusesAnExistingDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "omega.db")
	st, err := Open(src)
	if err != nil {
		t.Fatalf("open source store: %v", err)
	}
	defer st.Close()
	dst := filepath.Join(dir, "omega-copy.db")
	if err := os.WriteFile(dst, []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SnapshotStore(src, dst); err == nil {
		t.Errorf("an existing destination must be refused, never overwritten")
	}
	if body, _ := os.ReadFile(dst); string(body) != "occupied" {
		t.Errorf("the refused destination was clobbered: %q", body)
	}
}
