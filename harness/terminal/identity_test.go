// The capsule identity law (card t_9ae9a25a): the webui's composition
// manifests derive every dockable id from the capsule's ui/ dir
// (`harness/<name>/ui` -> `{ module: "<name>" }`), and the dock's
// runtime module gate joins those ids against the module list GET
// /modules serves out of the mount registry. So the plugin Name(), the
// package dir, the ui dir and the manifest id are ONE identity.
//
// The capsule once answered "shell" while its dir and dockable id said
// "terminal": the gate's `off` set held "shell" and its `owned` set held
// "terminal", the two halves never met, and the Terminal chip survived a
// module toggle-off (the defect class t_103e30a6 killed everywhere
// else). A hardcoded alias table in the webui was rejected - the names
// agree by construction instead, and this test is the tripwire.
package terminal

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCapsuleIdentityAgreesWithDir(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed - cannot locate the capsule dir")
	}
	capsuleDir := filepath.Dir(file)
	name := filepath.Base(capsuleDir)

	if got := NewShellPlugin().Name(); got != name {
		t.Fatalf("plugin Name() = %q, capsule dir = %q: the dock's module gate joins the registry row against the manifest id generated from this dir, so they must agree", got, name)
	}

	// the ui/ dir is what the build's scan turns into the dockable
	// manifest entry; a capsule without one has no dockable to gate.
	if _, err := os.Stat(filepath.Join(capsuleDir, "ui")); err != nil {
		t.Fatalf("capsule %q has no ui/ dir (the dockable manifest source): %v", name, err)
	}

	// the generated manifest (tracked, nuxt generate rewrites it) must
	// carry this capsule under the same id - the join the runtime gate
	// performs, read off the artifact itself.
	manifest := filepath.Join(capsuleDir, "..", "..", "webui", "app", "composables", "module-tabs.generated.ts")
	body, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("read the dockable manifest: %v", err)
	}
	if want := `{ module: "` + name + `"`; !strings.Contains(string(body), want) {
		t.Fatalf("module-tabs.generated.ts carries no %s entry: the capsule's dockable id no longer matches its plugin name (re-run nuxt generate, or the rename broke the join)", want)
	}
}
