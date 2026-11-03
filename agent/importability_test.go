package agent

// The tier law (three-tier restructure, card t_768c6cc1): agent/ is THE
// MECHANISM. It imports the substrate (core, llm) and itself - never a
// harness capsule. Same machinery as core's importability_test.go: the
// law was true when the test landed; the test keeps it true.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowedPath reports whether an import path is legal inside the
// mechanism tier. Stdlib first: paths without a dot in their first
// segment. Then the tier's omega allowance {core, llm, agent}, then
// the vetted third-party list - a new third-party dep lands only by
// adding it here (in review, on its own), never by the side door.
func allowedPath(path string) bool {
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	if first == "" || !strings.ContainsAny(first, ".") {
		return true // stdlib
	}
	switch path {
	case "github.com/EndoTheDev/omega/core",
		"github.com/EndoTheDev/omega/llm",
		"github.com/EndoTheDev/omega/agent",
		"gopkg.in/yaml.v3":
		return true
	}
	return false
}

// checkImports parses every .go file in dir (test files included,
// subdirectories excluded) and lists imports the tier law refuses.
// The offender list carries file names so the fix is the next edit,
// not a mystery.
func checkImports(t *testing.T, dir string) []string {
	t.Helper()
	var offenders []string
	err := filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if path != dir {
				return filepath.SkipDir // exactly this directory, no tiers below
			}
			return nil
		}
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if !allowedPath(p) {
				rel, rerr := filepath.Rel(dir, path)
				if rerr != nil {
					rel = path
				}
				offenders = append(offenders, filepath.ToSlash(rel)+" imports "+p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return offenders
}

// TestAgentImportsLaw: agent/ is the mechanism. core, llm, agent,
// the vetted third-party list, stdlib - nothing else. A harness or
// modules import anywhere under agent/ fails here.
func TestAgentImportsLaw(t *testing.T) {
	offenders := checkImports(t, ".")
	if len(offenders) > 0 {
		t.Fatalf("agent/ imports outside its tier law {stdlib, core, llm, agent, vetted third-party} (harness -> agent -> llm, never reversed):\n%s",
			strings.Join(offenders, "\n"))
	}
}