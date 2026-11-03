package api

// The importability law (restructure part 2, t_905fa502): agent/api is
// the agent's base endpoint set and imports the stdlib plus the three
// mechanism tiers below it - core, agent (and its sub-packages), llm -
// and NEVER harness. The extension pattern is the capsule's
// core.RegisterRoute, not a service import: a harness edge here would
// invert the tier order the tree just learned. The machinery clones
// core's importability test (arc 1c, t_483de955), scoped to production
// files: _test.go fixtures ride the real harness store (the standing
// pattern the cmd tests use), so the shipped graph is what the law
// guards. The shell gate (scripts/gate-deps.sh) backs this up on the
// llm/agent package set.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isStdlib reports whether an import path belongs to the Go standard
// library: its first path segment never contains a dot.
func isStdlib(path string) bool {
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	return first != "" && !strings.ContainsAny(first, ".")
}

// isAgentAPILegalImport reports whether path is legal in production
// agent/api code: the stdlib plus {core, agent, agent/*, llm}.
func isAgentAPILegalImport(path string) bool {
	const mod = "github.com/EndoTheDev/omega"
	switch {
	case isStdlib(path):
		return true
	case path == mod+"/core", path == mod+"/llm":
		return true
	case strings.HasPrefix(path, mod+"/agent"):
		return true
	}
	return false
}

// TestAPIImportsMechanismOnly parses every non-test agent/api/*.go file
// and fails if any import falls outside {stdlib, core, agent, llm}.
// Offenders are listed with file names so the fix is the next edit, not
// a mystery.
func TestAPIImportsMechanismOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read agent/api/: %v", err)
	}
	var offenders []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", e.Name(), perr)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if !isAgentAPILegalImport(path) {
				offenders = append(offenders, e.Name()+" imports "+path)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("agent/api imports outside {stdlib, core, agent, llm} (importability law, restructure part 2):\n%s",
			strings.Join(offenders, "\n"))
	}
}
