package core

// The importability law (factory-flow ADR): core is zero behavior and
// imports the Go stdlib only. This test was the convention's teeth
// before it was a test (arc 1c, t_483de955): a non-stdlib import anywhere
// in core fails the suite, so the strangler arcs cannot let a forbidden
// edge land.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isStdlib reports whether an import path belongs to the Go standard
// library. Stdlib paths never contain a dot in their first path
// segment ("encoding/json", "context"); everything else does
// ("github.com/...", "omega/ai"), including every golang.org/x and
// every omega-internal package.
func isStdlib(path string) bool {
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	return first != "" && !strings.ContainsAny(first, ".")
}

// TestCoreImportsStdlibOnly parses every core/*.go file and fails if
// any import is outside the stdlib. Non-stdlib offenders are listed
// with file names so the fix is the next edit, not a mystery.
func TestCoreImportsStdlibOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read core/: %v", err)
	}
	var offenders []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", e.Name(), perr)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if !isStdlib(path) {
				offenders = append(offenders, e.Name()+" imports "+path)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("core imports outside the stdlib (importability law, factory-flow ADR):\n%s",
			strings.Join(offenders, "\n"))
	}
}
