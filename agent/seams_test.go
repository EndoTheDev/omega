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

// The bind-time assertion law: a wrong-shaped offer under a need key fails at
// boot naming the need - never a silent nil discovered mid-turn, never a lazy
// resolve.
func TestBindWrongShapeDiesLoud(t *testing.T) {
	// The store key is bound by the serving surface, not NewFromContext.
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

// needKeys is the closed need-key namespace.
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

// capsuleServiceKeys are the non-need class of the closed namespace.
// chat-runs is the first SHELL-offered key: owner = the serving shell, offered through
// the proper api.Offer surface at channel wire time; consumers = any capsule reading
// live-run state through its own interface; a missing offer = honest never-running.
var capsuleServiceKeys = []string{
	"skills",
	"memory",
	"trust",
	"cron",
	"notifications",
	"verdicts",
	"chat-runs",
}

// TestBindMissingStoreOffersFloor pins the missing-supply flip side for the needs.
func TestBindMissingNeedsAreTheFloor(t *testing.T) {
	var ctx Context
	ctx.ApplyOffer("provider", "test", FakeProvider{})

	// The optional needs stay absent on purpose. The bind resolution must not fabricate
	// one.
	for _, key := range []string{"loop", "logger", "compactor", "prompt", "nudger", "approvals"} {
		if ctx.Services().Resolve(key) != nil {
			t.Fatalf("need %q: nothing offered, the registry must resolve nil", key)
		}
	}
}

// TestClassANounsLeaveTheSubstrate is the Class A noun law. The scan
// is identifier-level (go/ast), so comments and the accepted residual string keys
// ("store", "provider" as data in the census and resolve calls) never trip it.
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
		// Class B owner faces plus the slot-era types that referenced them.
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

// TestClassANounsLiveAtTheirConsumers pins the flip side of the wave-2 noun law. A face
// moving house re-lands the docs, agent/extensionapi.go's census comment and the
// harness offers with it, so the drift cannot be silent.
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

// TestCapsuleOwnerFacesLeaveTheTree is the deletability proof-as-test: the Class B
// owner-face names and the slot-era types they referenced
// must be dead in every ring outside harness/ - core, llm, agent (loop + base endpoints
// included), api and the host. Comments and string keys are data (go/ast) and never
// trip.
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

// TestOffersStayInClosedKeys walks every Offer AND ApplyOffer call site in the tree.
// Offering a 16th key is a design event, argued in a card - not a line a capsule may
// add silently. The ApplyOffer walk closes the one-abstraction bypass: a census that
// only pins.Offer( call sites is decoration - the raw core primitive is the mount
// machinery's own, and every literal-key caller (the host's store offer, the shell's
// chat-runs) must sit in the same closed list.
func TestOffersStayInClosedKeys(t *testing.T) {
	closed := map[string]bool{}
	for _, k := range needKeys {
		closed[k] = true
	}
	for _, k := range capsuleServiceKeys {
		closed[k] = true
	}
	offered := map[string]int{}
	offerRE := regexp.MustCompile(`\.(?:Offer|ApplyOffer)\("([a-z_-]+)",`)
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
