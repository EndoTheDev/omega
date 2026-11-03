package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
// Proposing #9 is a design event, argued in a card - this test is the
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

// capsuleServiceKeys are the non-need class of the closed namespace
// (part 3 wave 1): owner-offered capsule services, consumed through
// consumer-owned local interfaces. kanban is a Provides claim only
// (no service is offered under it - the census carries it no slot).
var capsuleServiceKeys = []string{
	"skills",
	"memory",
	"trust",
	"cron",
	"notifications",
	"verdicts",
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
// seam constitution): the need-interface names live with their
// consumers (agent/, the loop machinery, the serving surface) - the
// substrate ends as wire types, mount machinery, registry. The scan is
// identifier-level (go/ast), so comments and the accepted residual
// string keys ("store", "provider" as data in the census and resolve
// calls) never trip it. agent/ is not scanned: it declares the faces.
func TestClassANounsLeaveTheSubstrate(t *testing.T) {
	forbidden := map[string]bool{
		"Provider":           true,
		"StoreProvider":      true,
		"LoggerProvider":     true,
		"PromptBuilder":      true,
		"CompactionProvider": true,
		"ApprovalProvider":   true,
		"TurnNudger":         true,
		"LoopProvider":       true,
		// Class B owner faces (part 3 wave 1) plus the slot-era types
		// that referenced them: deleted everywhere, the consumer keeps
		// only its own local interface.
		"CronProvider":          true,
		"SkillsProvider":        true,
		"MemoryProvider":        true,
		"TrustProvider":         true,
		"VerdictsProvider":      true,
		"NotificationsProvider": true,
		"CronDeps":              true,
		"CronJobInfo":           true,
		"CronLister":            true,
		"SkillsLister":          true,
		"Frontend":              true,
		"RegisterProvider":      true,
		"RegisterStore":         true,
		"RegisterLogger":        true,
		"RegisterCompactor":     true,
		"RegisterPromptBuilder": true,
		"RegisterApprovals":     true,
		"RegisterTurnNudger":    true,
		"RegisterLoop":          true,
	}
	// The scan covers core/ and llm/ - the substrate rings. agent/ and
	// the shells (api/, agent/api/) declare or consume the faces: the
	// same identifiers there are the legal consult sites, not residue.
	for _, dir := range []struct {
		path      string
		skipTests bool
	}{
		{"../core", false},
		{"../llm", false},
	} {
		entries, err := os.ReadDir(dir.path)
		if err != nil {
			t.Fatalf("read %s: %v", dir.path, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			if dir.skipTests && strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, filepath.Join(dir.path, e.Name()), nil, 0)
			if err != nil {
				t.Fatalf("parse %s/%s: %v", dir.path, e.Name(), err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				if forbidden[id.Name] {
					t.Errorf("noun residue: %s:%d names %q - the substrate carries no capsule name", e.Name(), fset.Position(id.Pos()).Line, id.Name)
				}
				return true
			})
		}
	}
}

// TestClassANounsLiveAtTheirConsumers pins the flip side of the wave-2
// noun law: the eight need interfaces exist, at their ONE consumer
// home (agent/seams.go - the loop machinery's own need statements). A
// face moving house re-lands the docs, agent/extensionapi.go's census
// comment and the harness offers with it, so the drift cannot be
// silent.
func TestClassANounsLiveAtTheirConsumers(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "seams.go", nil, 0)
	if err != nil {
		t.Fatalf("parse seams.go: %v", err)
	}
	declared := map[string]bool{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts := spec.(*ast.TypeSpec)
			if _, ok := ts.Type.(*ast.InterfaceType); ok {
				declared[ts.Name.Name] = true
			}
		}
	}
	// The need face per need key: key -> the consumer interface name.
	faces := map[string]string{
		"provider":  "Provider",
		"store":     "StoreProvider",
		"loop":      "LoopProvider",
		"logger":    "LoggerProvider",
		"compactor": "CompactionProvider",
		"prompt":    "PromptBuilder",
		"nudger":    "TurnNudger",
		"approvals": "ApprovalProvider",
	}
	for key, face := range faces {
		if !declared[face] {
			t.Errorf("need %q: the consumer face %q is not declared in agent/seams.go - the need statement lives with the consumer, sync the census and the docs if it moved", key, face)
		}
	}
}

// TestCapsuleOwnerFacesLeaveTheTree is the deletability proof-as-test
// (part 3, the seam constitution): the Class B owner-face names and the
// slot-era types they referenced must be dead in every ring outside
// harness/ - core, llm, agent (loop + base endpoints included), api and
// the host. No type, field or local identifier may resurrect them
// there; a consumer declares its own local interface with its own
// name, the owner noun is gone. Comments and string keys are data
// (go/ast) and never trip.
func TestCapsuleOwnerFacesLeaveTheTree(t *testing.T) {
	forbidden := map[string]bool{
		"CronProvider":          true,
		"CronDeps":              true,
		"CronJobInfo":           true,
		"CronLister":            true,
		"SkillsProvider":        true,
		"SkillsLister":          true,
		"MemoryProvider":        true,
		"TrustProvider":         true,
		"VerdictsProvider":      true,
		"NotificationsProvider": true,
		"Frontend":              true,
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	// The substrate rings the constitution names: core, llm, agent
	// (incl. the loop + base endpoints), api (the shell ring) and the
	// host (cmd). harness/ is excluded - capsules own their folders.
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "webui", "node_modules", "dist", ".nuxt", ".worktrees", "harness", "deploy", "diag", "scripts":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if forbidden[id.Name] {
				t.Errorf("owner-face residue: %s:%d names %q - nobody may carry the deleted slot vocabulary", rel, fset.Position(id.Pos()).Line, id.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		if err == filepath.SkipDir {
			return
		}
		t.Fatalf("walk: %v", err)
	}
}

// TestOffersStayInClosedKeys walks every Offer call site in the tree
// and holds the namespace shut: the only legal offer keys are the 8
// need keys (needKeys above) plus the 6 capsule-service keys
// (capsuleServiceKeys). Offering a 15th key is a design event, argued
// in a card - not a line a capsule may add silently.
func TestOffersStayInClosedKeys(t *testing.T) {
	closed := map[string]bool{}
	for _, k := range needKeys {
		closed[k] = true
	}
	for _, k := range capsuleServiceKeys {
		closed[k] = true
	}
	offered := map[string]int{}
	offerRE := regexp.MustCompile(`\.Offer\("([a-z_]+)",`)
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	werr := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr2 error) error {
		if werr2 != nil {
			return werr2
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "webui", "node_modules", "dist", ".nuxt", ".worktrees":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for _, m := range offerRE.FindAllStringSubmatch(string(data), -1) {
			offered[m[1]]++
		}
		return nil
	})
	if werr != nil && werr != filepath.SkipDir {
		t.Fatalf("walk: %v", werr)
	}
	if len(offered) == 0 {
		t.Fatal("no Offer call site found in the walk - the scan cannot see the tree")
	}
	for key := range offered {
		if !closed[key] {
			t.Errorf("offer key %q offered outside the closed namespace - a design event, not a registration (agent/seams_test.go needKeys + the census list)", key)
		}
	}
	for _, key := range needKeys {
		if offered[key] == 0 {
			t.Errorf("need %q: no capsule offers it - the bind resolves nil on a key nobody offers, resync needKeys and the census", key)
		}
	}
}
