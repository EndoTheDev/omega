// Package datadir owns the database directory layout inside an omega
// home (Endo 2026-10-20): databases (omega.db, kanban.db) live in
// <home>/data/, not the home root. It is the single source for the
// data-dir path and performs the boot-time migration that moves legacy
// home-root databases into data/ before the db handle is opened.
package datadir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// dirName is the fixed database directory name inside an omega home.
// Endo's ruling: the folder is `data/` - no config knob.
const dirName = "data"

// Warnf prints a loud migration warning to stderr. Package-level so
// tests can capture it.
var Warnf = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "omega datadir: "+format+"\n", args...)
}

// Logf prints a one-time migration notice to stderr. Package-level so
// tests can capture it.
var Logf = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "omega datadir: "+format+"\n", args...)
}

// Dir returns the database directory for an omega home: <home>/data.
func Dir(home string) string {
	return filepath.Join(home, dirName)
}

// DBPath returns the database file path for name (e.g. "omega.db")
// inside home's data directory: <home>/data/<name>.
func DBPath(home, name string) string {
	return filepath.Join(Dir(home), name)
}

// Home resolves the omega home the way the module-level fallbacks do:
// OMEGA_HOME when set, else ~/.omega. Returns "" when neither is
// determinable (no OMEGA_HOME, no user home). The omega binary itself
// also supports exe-dir homes (cmd/omega homeForExe), but the host
// resolves db paths for that case before mounting plugins, so plugins
// only ever need this fallback.
func Home() string {
	if home := os.Getenv("OMEGA_HOME"); home != "" {
		return home
	}
	u, err := os.UserHomeDir()
	if err != nil || u == "" {
		return ""
	}
	return filepath.Join(u, ".omega")
}

// Migrate performs the boot-time data/ layout migration for one
// database before its handle is opened. dsn must be a file path inside
// a data directory (parent dir named "data"); anything else - empty,
// ":memory:", DSNs with query params (only the file path part is
// considered), paths outside a data dir - passes through untouched.
//
// If <home>/<name> exists but data/<name> does not, the database and
// its -wal/-shm sidecars are MOVED into data/ - sidecars first, main
// file last, then the caller opens the handle. Rename, never
// copy-and-leave: a crash mid-migration leaves no data/<name>, so the
// next boot simply re-runs the (idempotent) migration; two divergent
// copies can never appear. If BOTH spellings exist, neither file is
// touched - the migration warns loudly and data/ wins at runtime,
// leaving the operator to resolve the legacy copy.
//
// The data directory is created whenever missing, so a fresh home
// opens cleanly too.
func Migrate(dsn string) error {
	path := dsn
	if i := strings.IndexByte(dsn, '?'); i >= 0 {
		path = dsn[:i]
	}
	if path == "" || path == ":memory:" || filepath.Base(filepath.Dir(path)) != dirName {
		return nil
	}
	dataDir := filepath.Dir(path)
	name := filepath.Base(path)
	legacy := filepath.Join(filepath.Dir(dataDir), name)

	// Ensure the directory exists before anything else - a fresh home
	// has no data/ yet and the open that follows Migrate must succeed.
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("datadir migration: create %s: %w", dataDir, err)
	}

	_, dataExists := os.Stat(path)
	_, legacyExists := os.Stat(legacy)
	switch {
	case dataExists == nil && legacyExists == nil:
		Warnf("both %s and legacy %s exist; using %s - resolve the legacy copy by hand", path, legacy, path)
	case dataExists != nil && legacyExists == nil:
		// Crash-safe order: sidecars first, main file last. A crash
		// between renames leaves no data/<name>, so the next boot
		// re-runs the migration cleanly (stale sidecar leftovers are
		// simply overwritten).
		for _, side := range []string{name + "-wal", name + "-shm", name} {
			from := filepath.Join(filepath.Dir(dataDir), side)
			if err := os.Rename(from, filepath.Join(dataDir, side)); err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("datadir migration: move %s into %s: %w", from, dataDir, err)
				}
				continue
			}
			if side == name {
				Logf("migrated %s -> %s", from, filepath.Join(dataDir, side))
			}
		}
	default:
		// data/<name> exists, legacy may not: already migrated or
		// fresh - nothing to do.
	}
	return nil
}