package agent

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The turn-copy persistence law (card t_471f6d4a): the agent run loop
// is the ONLY writer of turn copies. `AppendMessage` calls outside the
// storage packages (agent/ owns the TurnSink seam, modules/store/ is
// the store implementation itself) must be tree operations, session
// metadata entries, or extension-owned audit records - never user /
// assistant / tool-result copies of a run. Channels fetch and render;
// they do not append turn content.
//
// The named allowlist carries the site's class so a new call can land
// only by declaring what kind of record it writes.

var turnCopyAppendAllowlist = map[string]string{
	"http_channel/server_branch.go":   "tree operation - branch summary System marker on the child session",
	"http_channel/server_variants.go": "tree operation - core.NewVariantSelect marker",
	"tui/variant.go":                  "tree operation - core.NewVariantSelect markers",
	"tui/tui.go":                      "session entry markers (persistEntry: model/thinking-level changes) - not turn copies",
	"modules/verdict/hub.go":          "verdict audit records (VerdictCard/Verdict) - extension-owned Q&A records, not model turns",
}

var appendMessageCall = regexp.MustCompile(`\.AppendMessage\(`)

// TestTurnCopyPersistenceLaw walks the repository source and fails on
// any AppendMessage call site outside agent/ and modules/store/ that
// the allowlist has not classified.
func TestTurnCopyPersistenceLaw(t *testing.T) {
	root := repoRootForLaws(t)
	var violations []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".worktrees", "static", "node_modules", "ui":
				// Webui build trees, VCS internals, and co-worker
				// worktree copies: not this repo's Go source.
				// A worktree is a full repo copy - its allowlisted
				// files appear at un-allowlisted rel paths and the
				// walk would count them as violations.
				// SkipDir prunes only this subtree (SkipAll would end
				// the whole walk - a nested <module>/ui would silently
				// hide every later path).
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		// The seam's home and the store implementation are exempt.
		if rel == "agent" || strings.HasPrefix(rel, "agent/") ||
			rel == "modules/store" || strings.HasPrefix(rel, "modules/store/") {
			return nil
		}
		if _, allowed := turnCopyAppendAllowlist[rel]; allowed {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if appendMessageCall.MatchString(line) {
				violations = append(violations, rel+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("turn-copy persistence law violated - the agent loop is the only writer of turn copies:\n%s",
			strings.Join(violations, "\n"))
	}
}

// repoRootForLaws walks up from the package directory until go.mod.
func repoRootForLaws(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found upward from %s", dir)
		}
		dir = parent
	}
}
