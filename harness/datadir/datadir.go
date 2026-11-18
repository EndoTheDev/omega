// Package datadir owns the database directory layout inside an omega home.
package datadir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"
)

// dirName is the fixed database directory name inside an omega home.
const dirName = "data"

// Warnf prints a loud migration warning to stderr.
var Warnf = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "omega datadir: "+format+"\n", args...)
}

// Logf prints a one-time migration notice to stderr.
var Logf = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "omega datadir: "+format+"\n", args...)
}

// Dir returns the database directory for an omega home: <home>/data.
func Dir(home string) string {
	return filepath.Join(home, dirName)
}

// DBPath returns the database file path for name (e.g. "omega.db") inside home's data
// directory.
func DBPath(home, name string) string {
	return filepath.Join(Dir(home), name)
}

// Home resolves the omega home the way the module-level fallbacks do.
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

// AgentHome resolves the omega home the DATABASE consumers of this process should use:
// when the process runs as a continuous agent (OMEGA_AGENT set, the spawn contract
// every wake child and delegated child carries), the answer is the agent's own home so
// its sessions store, notifications, and boards resolve under <agent>/data/ - full data
// isolation, of. A malformed OMEGA_AGENT value can never escape agents/: invalid names
// fail the resolution loudly (the caller surfaces the error), never silently fall back
// - a hostile env var must not point the stores at the wrong home.
func AgentHome() (string, error) {
	home := Home()
	name := strings.TrimSpace(os.Getenv("OMEGA_AGENT"))
	if name == "" {
		return home, nil
	}
	if name == "default" {
		// main exists by definition at the root.
		return home, nil
	}
	if err := agent.ValidAgentName(name); err != nil {
		return "", fmt.Errorf("datadir: OMEGA_AGENT %q: %w", name, err)
	}
	return filepath.Join(home, "agents", name), nil
}

// Migrate performs the boot-time data/ layout migration for one database before its.
// dsn must be a file path inside a data directory (parent dir named "data"); anything
// else - empty, ":memory:", DSNs with query params (only the file path part is
// considered), paths outside a data dir - passes through untouched. If <home>/<name>
// exists but data/<name> does not, the database and its -wal/-shm sidecars are MOVED
// into data/ - sidecars first, main file last, then the caller opens the handle.
// Rename, never copy-and-leave: a crash mid-migration leaves no data/<name>, so the
// next boot simply re-runs the (idempotent) migration; two divergent copies can never
// appear.
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
		// Crash-safe order: sidecars first, main file last. A crash between renames leaves no
		// data/<name>, so the next boot re-runs the migration cleanly (stale sidecar
		// leftovers are simply overwritten).
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
	}
	return nil
}
