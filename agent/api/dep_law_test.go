package api

// The importability law: agent/api is the agent's base endpoint set and imports the
// stdlib plus the three mechanism tiers below it - core, agent (and its sub-packages),
// llm - and NEVER harness.

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

// isAgentAPILegalImport reports whether path is legal in production agent/api code.
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

// TestAPIImportsMechanismOnly parses every non-test agent/api/*.go file and fails on an
// import outside {stdlib, core, agent, llm}.
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
