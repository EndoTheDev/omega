package datadir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestMigrateFreshHome creates nothing but the data directory.
func TestMigrateFreshHome(t *testing.T) {
	home := t.TempDir()
	dsn := DBPath(home, "omega.db")
	if err := Migrate(dsn); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := os.Stat(Dir(home)); err != nil {
		t.Errorf("data dir not created: %v", err)
	}
	if _, err := os.Stat(dsn); !os.IsNotExist(err) {
		t.Errorf("fresh migration must not fabricate a db file, stat err = %v", err)
	}
}

// TestMigrateLegacyHome moves db + sidecars into data/, root copies gone.
func TestMigrateLegacyHome(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "omega.db"), "MAIN")
	writeFile(t, filepath.Join(home, "omega.db-wal"), "WAL")
	writeFile(t, filepath.Join(home, "omega.db-shm"), "SHM")

	var logs []string
	prevLog, prevWarn := Logf, Warnf
	Logf = func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	Warnf = func(string, ...any) {}
	defer func() { Logf, Warnf = prevLog, prevWarn }()

	dsn := DBPath(home, "omega.db")
	if err := Migrate(dsn); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got := readFile(t, dsn); got != "MAIN" {
		t.Errorf("data db content = %q, want MAIN", got)
	}
	if got := readFile(t, dsn+"-wal"); got != "WAL" {
		t.Errorf("data wal content = %q, want WAL", got)
	}
	if got := readFile(t, dsn+"-shm"); got != "SHM" {
		t.Errorf("data shm content = %q, want SHM", got)
	}
	for _, root := range []string{"omega.db", "omega.db-wal", "omega.db-shm"} {
		if _, err := os.Stat(filepath.Join(home, root)); !os.IsNotExist(err) {
			t.Errorf("legacy root copy %s still present, stat err = %v", root, err)
		}
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "migrated") {
		t.Errorf("want exactly one migration notice, got %q", logs)
	}
}

// TestMigratePartialSidecars recovers the crash-mid-migration state:
// sidecars already in data/ (stale), main still at the root.
func TestMigratePartialSidecars(t *testing.T) {
	home := t.TempDir()
	dsn := DBPath(home, "omega.db")
	if err := os.MkdirAll(Dir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dsn+"-wal", "STALE-WAL")
	writeFile(t, dsn+"-shm", "STALE-SHM")
	writeFile(t, filepath.Join(home, "omega.db"), "MAIN")
	writeFile(t, filepath.Join(home, "omega.db-wal"), "WAL")

	if err := Migrate(dsn); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got := readFile(t, dsn); got != "MAIN" {
		t.Errorf("data db content = %q, want MAIN", got)
	}
	if got := readFile(t, dsn+"-wal"); got != "WAL" {
		t.Errorf("stale sidecar not overwritten with live wal, got %q", got)
	}
	for _, root := range []string{"omega.db", "omega.db-wal"} {
		if _, err := os.Stat(filepath.Join(home, root)); !os.IsNotExist(err) {
			t.Errorf("legacy root copy %s still present, stat err = %v", root, err)
		}
	}
}

// TestMigrateBothExist touches neither and warns loudly.
func TestMigrateBothExist(t *testing.T) {
	home := t.TempDir()
	dsn := DBPath(home, "omega.db")
	if err := os.MkdirAll(Dir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dsn, "DATASIDE")
	writeFile(t, filepath.Join(home, "omega.db"), "LEGACY")

	var warns []string
	prevWarn := Warnf
	Warnf = func(f string, a ...any) { warns = append(warns, fmt.Sprintf(f, a...)) }
	defer func() { Warnf = prevWarn }()

	if err := Migrate(dsn); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got := readFile(t, dsn); got != "DATASIDE" {
		t.Errorf("data db was touched in both-exist case: %q", got)
	}
	if got := readFile(t, filepath.Join(home, "omega.db")); got != "LEGACY" {
		t.Errorf("legacy db was touched in both-exist case: %q", got)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "legacy") || !strings.Contains(warns[0], "using") {
		t.Errorf("want one loud both-exist warning, got %q", warns)
	}
}

// TestMigrateIdempotent: a second boot over a migrated home is a no-op.
func TestMigrateIdempotent(t *testing.T) {
	home := t.TempDir()
	dsn := DBPath(home, "omega.db")
	writeFile(t, filepath.Join(home, "omega.db"), "MAIN")

	for i := 0; i < 2; i++ {
		if err := Migrate(dsn); err != nil {
			t.Fatalf("Migrate run %d: %v", i+1, err)
		}
	}
	if got := readFile(t, dsn); got != "MAIN" {
		t.Errorf("data db content = %q, want MAIN", got)
	}
	if _, err := os.Stat(filepath.Join(home, "omega.db")); !os.IsNotExist(err) {
		t.Errorf("legacy root copy reappeared or never moved, stat err = %v", err)
	}
}

// TestMigrateIgnoresNonDataPaths: dsn outside a data dir is untouched.
func TestMigrateIgnoresNonDataPaths(t *testing.T) {
	for _, dsn := range []string{":memory:", "", filepath.Join(t.TempDir(), "omega.db") + "?_journal_mode=WAL"} {
		t.Run(dsn, func(t *testing.T) {
			if err := Migrate(dsn); err != nil {
				t.Fatalf("Migrate(%q): %v", dsn, err)
			}
		})
		// The query-param case must not fabricate anything.
		if dsn == "" || dsn == ":memory:" {
			continue
		}
		base, _, _ := strings.Cut(dsn, "?")
		if _, err := os.Stat(base); !os.IsNotExist(err) {
			t.Errorf("query-param dsn fabricated %s, stat err = %v", base, err)
		}
	}
}

// TestMigrateOtherName moves a differently-named db (kanban.db).
func TestMigrateOtherName(t *testing.T) {
	home := t.TempDir()
	dsn := DBPath(home, "kanban.db")
	writeFile(t, filepath.Join(home, "kanban.db"), "BOARD")
	if err := Migrate(dsn); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got := readFile(t, dsn); got != "BOARD" {
		t.Errorf("data kanban.db content = %q, want BOARD", got)
	}
}

func TestDBPath(t *testing.T) {
	if got, want := DBPath("/home/x/.omega", "omega.db"), "/home/x/.omega/data/omega.db"; got != want {
		t.Errorf("DBPath = %q, want %q", got, want)
	}
	if got, want := Dir("/home/x/.omega"), "/home/x/.omega/data"; got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestHome(t *testing.T) {
	t.Setenv("OMEGA_HOME", "/explicit/home")
	if got := Home(); got != "/explicit/home" {
		t.Errorf("Home with OMEGA_HOME = %q, want /explicit/home", got)
	}
	t.Setenv("OMEGA_HOME", "")
	if got := Home(); got == "" || got == "." {
		t.Errorf("Home fallback = %q, want a ~/.omega path", got)
	}
}