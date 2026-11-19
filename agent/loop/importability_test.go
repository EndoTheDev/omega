package loop

// The loop's allowance list: agent/loop imports exactly {core, llm, agent, stdlib}.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoopImportsAreExactlyTheAllowance fails on any agent/loop import outside. The
// promotion added no new import rights - the loop imports the parent mechanism, the
// substrate, and the contract layer it always did.
func TestLoopImportsAreExactlyTheAllowance(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read agent/loop: %v", err)
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
			first := path
			if i := strings.IndexByte(path, '/'); i >= 0 {
				first = path[:i]
			}
			switch {
			case first != "" && !strings.ContainsAny(first, "."):
				continue // stdlib
			case path == "github.com/EndoTheDev/omega/core",
				path == "github.com/EndoTheDev/omega/llm",
				path == "github.com/EndoTheDev/omega/agent":
				continue
			default:
				offenders = append(offenders, e.Name()+" imports "+path)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("agent/loop imports outside its allowance {stdlib, core, llm, agent} (constitutional promotion, no new rights):\n%s",
			strings.Join(offenders, "\n"))
	}
}