package api

// The importability law: api/ is the serving shell. NEVER harness (extensions are
// in-process plugins reached through the RegisterRoute seam, never a service import: a
// harness edge here would invert the tier order the tree just learned) and NEVER llm
// (the model client is agent vocabulary; the chat pipeline lives in agent/api exactly
// so the shell does not need it).

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isAPILegalImport reports whether path is legal in production api/ code.
func isAPILegalImport(path string) bool {
	const mod = "github.com/EndoTheDev/omega"
	if !strings.HasPrefix(path, mod+"/") {
		// external module (stdlib or go.mod dep): not a tier edge
		return true
	}
	rest := path[len(mod)+1:]
	return rest == "core" || strings.HasPrefix(rest, "agent")
}

// TestAPIImportsShellVocabularyOnly parses every non-test api/*.go file and fails if.
func TestAPIImportsShellVocabularyOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read api/: %v", err)
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
			if !isAPILegalImport(path) {
				offenders = append(offenders, e.Name()+" imports "+path)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("api/ imports outside {stdlib, core, agent} (importability law, restructure part 2):\n%s",
			strings.Join(offenders, "\n"))
	}
}
