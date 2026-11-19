package kanban

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/harness/datadir"
)

// TestOpenMigratesLegacyHome: opening the board db migrates a legacy home-root.
func TestOpenMigratesLegacyHome(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "kanban.db")

	// Real legacy db at the root.
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
