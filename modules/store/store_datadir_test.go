package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/modules/datadir"
)

// datadir integration tests: Open triggers the boot-time data-dir
// migration (modules/datadir) before the handle is created. The legacy
// fixtures are real SQLite databases - the migration renames them and
// the migrated copy must still open as a db.

// TestOpenFreshHomeDataDir: opening a fresh home's data db creates
// data/omega.db, nothing at the root.
func TestOpenFreshHomeDataDir(t *testing.T) {
	home := t.TempDir()
	s, err := Open(datadir.DBPath(home, "omega.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if _, err := os.Stat(datadir.DBPath(home, "omega.db")); err != nil {
		t.Errorf("data db not created on open: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "omega.db")); !os.IsNotExist(err) {
		t.Errorf("root db exists on fresh home, stat err = %v", err)
	}
}

// TestOpenMigratesLegacyHome: a home with a root-level db migrates on
// open - the db (with any sidecars) moves into data/, root copies are
// gone, and the store opens from the migrated copy. A second boot is
// idempotent.
func TestOpenMigratesLegacyHome(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "omega.db")

	// Real legacy db at the root (created by a normal open - the root
	// path is outside a data dir, so no migration fires during setup).
	s0, err := Open(legacy)
	if err != nil {
		t.Fatalf("create legacy db: %v", err)
	}
	s0.Close()
	// Fake-but-moveable sidecars: the migration must carry them along.
	for _, side := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(legacy+side, []byte("SIDE"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	dsn := datadir.DBPath(home, "omega.db")
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Close()
	if _, err := os.Stat(dsn); err != nil {
		t.Errorf("migrated db missing in data/: %v", err)
	}
	for _, root := range []string{"omega.db", "omega.db-wal", "omega.db-shm"} {
		if _, err := os.Stat(filepath.Join(home, root)); !os.IsNotExist(err) {
			t.Errorf("root copy %s survived migration, stat err = %v", root, err)
		}
	}
	if _, err := os.Stat(dsn + "-wal"); !os.IsNotExist(err) {
		t.Errorf("sidecar not carried into data/, stat err = %v", err)
	}

	// Second boot over the migrated home: no-op, still opens.
	s2, err := Open(dsn)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	s2.Close()
}

// TestOpenBothExistPicksData: both spellings present (real dbs) ->
// data/ wins, a loud warning fires, and neither file is modified.
func TestOpenBothExistPicksData(t *testing.T) {
	home := t.TempDir()
	dsn := datadir.DBPath(home, "omega.db")
	legacy := filepath.Join(home, "omega.db")

	sData, err := Open(dsn)
	if err != nil {
		t.Fatalf("seed data db: %v", err)
	}
	sData.Close()
	sLegacy, err := Open(legacy)
	if err != nil {
		t.Fatalf("seed legacy db: %v", err)
	}
	sLegacy.Close()

	var warns []string
	prevWarn := datadir.Warnf
	datadir.Warnf = func(f string, a ...any) { warns = append(warns, f) }
	defer func() { datadir.Warnf = prevWarn }()

	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if len(warns) != 1 || !strings.Contains(warns[0], "legacy") {
		t.Errorf("want one both-exist warning, got %q", warns)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("legacy copy must survive the both-exist case untouched: %v", err)
	}
}