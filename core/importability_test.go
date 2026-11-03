package core

// The importability law (factory-flow ADR): core is zero behavior and
// imports the Go stdlib only. This test was the convention's teeth
// before it was a test (arc 1c, t_483de955): a non-stdlib import anywhere
// in core fails the suite, so the strangler arcs cannot let a forbidden
// edge land.

import (
	"go/ast"
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
// ("github.com/...", "omega/llm"), including every golang.org/x and
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

// TestCoreCarriesNoCapsuleNouns is the substrate noun law (part 3, the
// seam constitution): core carries zero capsule nouns - wire types,
// mount machinery, the registry, nothing capsule-shaped. The ban
// covers both noun classes plus the slot-era types that rode out with
// them:
//
//   - Class A need faces (wave 2): the loop's needs live with their
//     consumers (agent/seams.go, agent/api, the host); suppliers offer
//     under need keys and never import these names.
//   - Class B owner faces (wave 1): deleted everywhere; a consumer
//     declares its own local interface and resolves by the string key.
//   - the slot-era types that referenced them (CronDeps, Frontend, ...).
//
// Identifier-level via go/ast: comments and the accepted residual
// string keys never trip. The complement (the faces pinned at their
// consumer homes and the closed offer list) lives in agent/seams_test.go.
func TestCoreCarriesNoCapsuleNouns(t *testing.T) {
	forbidden := map[string]bool{
		// Class A need faces (part 3 wave 2).
		"Provider":           true,
		"StoreProvider":      true,
		"LoopProvider":       true,
		"LoggerProvider":     true,
		"CompactionProvider": true,
		"PromptBuilder":      true,
		"TurnNudger":         true,
		"ApprovalProvider":   true,
		// Class B owner faces (part 3 wave 1).
		"CronProvider":          true,
		"SkillsProvider":        true,
		"MemoryProvider":        true,
		"TrustProvider":         true,
		"VerdictsProvider":      true,
		"NotificationsProvider": true,
		// The slot-era types that referenced them.
		"CronDeps":     true,
		"CronJobInfo":  true,
		"CronLister":   true,
		"SkillsLister": true,
		"Frontend":     true,
		// The retired Register* slot machinery.
		"RegisterProvider":      true,
		"RegisterStore":         true,
		"RegisterLogger":        true,
		"RegisterCompactor":     true,
		"RegisterPromptBuilder": true,
		"RegisterApprovals":     true,
		"RegisterTurnNudger":    true,
		"RegisterLoop":          true,
		"RegisterCron":          true,
		"RegisterTrust":         true,
		"RegisterMemory":        true,
		"RegisterSkills":        true,
		"RegisterVerdicts":      true,
		"RegisterNotifications": true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read core/: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, e.Name(), nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", e.Name(), perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if forbidden[id.Name] {
				t.Errorf("noun residue: %s:%d names %q - the substrate carries no capsule noun",
					e.Name(), fset.Position(id.Pos()).Line, id.Name)
			}
			return true
		})
	}
}
