// The capsule identity law. The capsule once answered "shell" while its dir and
// dockable id said "terminal": the gate's `off` set held "shell" and its `owned` set
// held "terminal", the two halves never met, and the Terminal chip survived a module
// toggle-off.
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

	// the ui/ dir is what the build's scan turns into the dockable manifest entry.
	if _, err := os.Stat(filepath.Join(capsuleDir, "ui")); err != nil {
		t.Fatalf("capsule %q has no ui/ dir (the dockable manifest source): %v", name, err)
	}

	// the generated manifest (tracked, nuxt generate rewrites it) must carry this capsule
	// under the same id.
	manifest := filepath.Join(capsuleDir, "..", "..", "webui", "app", "composables", "module-tabs.generated.ts")
	body, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("read the dockable manifest: %v", err)
	}
	if want := `{ module: "` + name + `"`; !strings.Contains(string(body), want) {
		t.Fatalf("module-tabs.generated.ts carries no %s entry: the capsule's dockable id no longer matches its plugin name (re-run nuxt generate, or the rename broke the join)", want)
	}
}
