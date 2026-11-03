package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bind-time assertion law (part 3 wave 2, the seam constitution):
// NewFromContext resolves and shape-asserts every need at bind. A
// wrong-shaped offer under a need key fails at boot naming the need -
// never a silent nil discovered mid-turn, never a lazy resolve.
// TestCensusEnforcement's complement: the negative case per need key.
func TestBindWrongShapeDiesLoud(t *testing.T) {
	// The store key is bound by the serving surface, not NewFromContext
	// (cmd/omega/main.go asserts the store offer's shape the same way).
	for _, key := range []string{"provider", "loop", "prompt", "logger", "compactor", "approvals", "nudger"} {
		var ctx Context
		ctx.ApplyOffer(key, "test", struct{}{}) // satisfies no need interface
		err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					s, ok := r.(string)
					if !ok {
						err = panicErr{v: r}
						return
					}
					err = &bootBindError{msg: s}
				}
			}()
			NewFromContext(&ctx, AgentOptions{})
			return nil
		}()
		if err == nil {
			t.Fatalf("need %q: a wrong-shaped offer must fail the bind loud", key)
		}
		if !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "wrong shape") {
			t.Errorf("need %q: the boot failure must name the need and the wrong shape, got %q", key, err.Error())
		}
	}
}

// bootBindError carries the bind panic's message.
type bootBindError struct{ msg string }

func (e *bootBindError) Error() string { return e.msg }

type panicErr struct{ v any }

func (e panicErr) Error() string {
	return "panic: not a string panic"
}

// needKeys is the closed need-key namespace (agent/seams.go, the
// census comment in extensionapi.go): the substrate's shopping list.
// Proposing #8 is a design event, argued in a card - this test is the
// mirror of core/census.go's slot list and the Offer call sites; a
// capsule offering outside the closed list is a wave-3 noun finding.
var needKeys = []string{
	"provider",
	"store",
	"loop",
	"logger",
	"compactor",
	"prompt",
	"nudger",
	"approvals",
}

// TestBindMissingStoreOffersFloor pins the missing-supply flip side
// for the needs with documented floors: absence is a legal state - the
// loop degrades (no prompt, no compaction, no nudger, no gate, no
// logger) instead of booting the wrong shape. Supply-side absence of
// "provider" refuses the boot upstream (the unconfigured host posture)
// and is not re-tested here.
func TestBindMissingNeedsAreTheFloor(t *testing.T) {
	var ctx Context
	ctx.ApplyOffer("provider", "test", FakeProvider{})

	// The optional needs stay absent on purpose: zero offers for
	// loop, logger, compactor, prompt, nudger, approvals. The bind
	// resolution must not fabricate one.
	for _, key := range []string{"loop", "logger", "compactor", "prompt", "nudger", "approvals"} {
		if ctx.Services().Resolve(key) != nil {
			t.Fatalf("need %q: nothing offered, the registry must resolve nil", key)
		}
	}
}

// TestClassANounsLeaveTheSubstrate is the wave-2 noun law (part 3, the
// seam constitution): the Class A need-interface names live with their
// consumers (agent/, the loop machinery, the serving surface) - the
// substrate ends as wire types, mount machinery, registry. The scan is
// identifier-level (go/ast), so comments and the accepted residual
// string keys ("store", "provider" as data in the census and resolve
// calls) never trip it.
func TestClassANounsLeaveTheSubstrate(t *testing.T) {
	forbidden := map[string]bool{
		"Provider":              true,
		"StoreProvider":         true,
		"LoggerProvider":        true,
		"PromptBuilder":         true,
		"CompactionProvider":    true,
		"ApprovalProvider":      true,
		"TurnNudger":            true,
		"LoopProvider":          true,
		"RegisterProvider":      true,
		"RegisterStore":         true,
		"RegisterLogger":        true,
		"RegisterCompactor":     true,
		"RegisterPromptBuilder": true,
		"RegisterApprovals":     true,
		"RegisterTurnNudger":    true,
		"RegisterLoop":          true,
	}
	for _, dir := range []string{"../core", "../llm"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
			if err != nil {
				t.Fatalf("parse %s/%s: %v", dir, e.Name(), err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				if forbidden[id.Name] {
					t.Errorf("noun residue: %s:%d names %q - the substrate carries no Class A name", e.Name(), fset.Position(id.Pos()).Line, id.Name)
				}
				return true
			})
		}
	}
}
