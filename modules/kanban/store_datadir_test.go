package kanban

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/modules/datadir"
)

// TestOpenMigratesLegacyHome: opening the board db migrates a legacy
// home-root kanban.db (+ sidecars) into data/ before the handle opens,
// and the opened store still serves the board. The legacy fixture is a
// real SQLite database.
func TestOpenMigratesLegacyHome(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "kanban.db")

	// Real legacy db at the root (root path is outside a data dir, so
	// setup itself triggers no migration).
	s0, err := Open(legacy)
	if err != nil {
		t.Fatalf("create legacy db: %v", err)
	}
	s0.Close()

	dsn := datadir.DBPath(home, "kanban.db")
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(dsn); err != nil {
		t.Errorf("data board db not created: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("root copy survived migration, stat err = %v", err)
	}
}