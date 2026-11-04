// ENDO'S LAW 2026-10-18: omega must never touch its database through the
// filesystem - only through the store seam and the task/session tools.
// The deny guard lives beside the db-path layout it guards (datadir is
// the instance-database's path authority), consumed by the tool
// capsules (harness/files for the file tools, harness/terminal for
// shell.run). The shell matcher also greps the raw command string for
// the db basename - a deny-list is inherently heuristic (a determined
// agent can base64 around it); this guard kills the accidental class,
// the prompt law covers intent, and WAL + backups are the recovery
// floor.

package datadir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DBDenyLaw is the law named in every refusal.
const DBDenyLaw = "omega.db is the store's door: use the task/session tools, not the filesystem."

// DBDenied builds the loud refusal error for a hit.
func DBDenied(what string) error {
	return fmt.Errorf("REFUSED by ENDO'S LAW 2026-10-18: %s (requested: %s)", DBDenyLaw, what)
}

// Guard holds the real filesystem paths of the instance database
// family (omega.db, omega.db-wal, omega.db-shm), symlink-resolved the
// same way the files endpoints resolve paths. A nil Guard means no
// instance home was determinable and everything passes.
type Guard struct {
	reals map[string]bool
}

// dbSuffixed lists the database family members for one db path.
// The WAL/SHM siblings live and die with the db, so the law covers
// them too: writing the WAL side door corrupts the store just as well.
func dbSuffixed(dbPath string) []string {
	return []string{dbPath, dbPath + "-wal", dbPath + "-shm"}
}

// NewGuard builds the deny set from the instance db path. The db
// file may not exist yet (fresh home, created lazily by the store),
// so resolution falls back to the real parent directory like the
// files endpoints do. If path is empty, the standard install layout
// (data db under OMEGA_HOME or ~/.omega) is used.
func NewGuard(dbPath string) *Guard {
	if strings.TrimSpace(dbPath) == "" {
		datadirHome := Home()
		if datadirHome == "" {
			return nil
		}
		dbPath = DBPath(datadirHome, "omega.db")
	}
	g := &Guard{reals: map[string]bool{}}
	for _, cand := range dbSuffixed(dbPath) {
		abs, err := filepath.Abs(cand)
		if err != nil {
			abs = filepath.Clean(cand)
		}
		// The unresolved cleaned path is a member too: a symlinked
		// home parent (macOS /var, containers) must still be caught
		// when EvalSymlinks of the leaf fails and only one of the two
		// spellings is known.
		g.reals[abs] = true
		if real := realResolve(abs); real != "" {
			g.reals[real] = true
		}
	}
	return g
}

// realResolve resolves a path through symlinks, tolerating a missing
// leaf by resolving the real parent first - the same contract as
// http_channel's resolveUnderRoot. Never fails: returns the cleaned
// path when nothing better is known.
func realResolve(path string) string {
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		return real
	}
	dir, base := filepath.Split(filepath.Clean(path))
	if dir == "" {
		dir = "."
	}
	if realDir, derr := filepath.EvalSymlinks(dir); derr == nil {
		return filepath.Join(realDir, base)
	}
	return filepath.Clean(path)
}

// DenyFile reports whether a file-tool path resolves into the
// instance database family.
func (g *Guard) DenyFile(path string) bool {
	if g == nil {
		return false
	}
	for _, cand := range dbSuffixed(path) {
		real := realResolve(cand)
		if real != "" && g.reals[real] {
			return true
		}
	}
	return false
}

// DenyShell reports whether a shell command is denied: either the raw
// string spells the db basename, or some path-like token in it resolves
// into the instance database family. Tokens are examined individually
// so a harmless mention of the basename in a long command is still
// evaluated by the stronger resolver-based check - but a bare basename
// hit denies outright, since the command could be anything.
func (g *Guard) DenyShell(command string) bool {
	if g == nil {
		return false
	}
	for _, base := range []string{"omega.db-wal", "omega.db-shm", "omega.db"} {
		if strings.Contains(command, base) {
			return true
		}
	}
	home := ""
	if u, err := os.UserHomeDir(); err == nil {
		home = u
	}
	for _, tok := range strings.Fields(command) {
		if !strings.ContainsAny(tok, `~/\`) {
			continue
		}
		path := strings.Trim(tok, `"'`)
		if path == "" || strings.Contains(path, "$(") || strings.Contains(path, "`") {
			continue // unresolved expansion - the basename grep's problem
		}
		// Expand $HOME and leading ~, resolve, compare.
		path = strings.ReplaceAll(path, "$HOME", home)
		if strings.HasPrefix(path, "~") {
			if home == "" {
				continue
			}
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
		if !filepath.IsAbs(path) {
			wd, err := os.Getwd()
			if err != nil {
				continue
			}
			path = filepath.Join(wd, path)
		}
		if g.DenyFile(path) {
			return true
		}
	}
	return false
}