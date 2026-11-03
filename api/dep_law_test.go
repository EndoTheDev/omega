package api

// The importability law (restructure part 2, t_905fa502): api/ is the
// serving shell - listener, mux, hostguard, the module route fallback,
// the static embed - and imports the stdlib plus core and the agent
// tiers. NEVER harness (extensions are in-process plugins reached
// through the RegisterRoute seam, never a service import: a harness
// edge here would invert the tier order the tree just learned) and
// NEVER llm (the model client is agent vocabulary; the chat pipeline
// lives in agent/api exactly so the shell does not need it). The
// machinery clones core's importability test (arc 1c, t_483de955),
// scoped to production files: _test.go fixtures ride the real harness
// store (the standing cmd-test pattern), so the shipped graph is what
// the law guards.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isAPILegalImport reports whether path is legal in production api/
// code. The law guards the omega TIER edges, not vendored modules:
// any external module path rides go.mod as everywhere else. Inside
// the omega module, only {core, agent} - neither harness/ nor llm/
// may appear.
func isAPILegalImport(path string) bool {
	const mod = "github.com/EndoTheDev/omega"
	if !strings.HasPrefix(path, mod+"/") {
		// external module (stdlib or go.mod dep): not a tier edge
		return true
	}
	rest := path[len(mod)+1:]
	return rest == "core" || strings.HasPrefix(rest, "agent")
}

// TestAPIImportsShellVocabularyOnly parses every non-test api/*.go file
// and fails if any import falls outside {stdlib, core, agent}. Offenders
// are listed with file names so the fix is the next edit, not a mystery.
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
