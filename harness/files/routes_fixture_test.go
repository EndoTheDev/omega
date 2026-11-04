package files

// The route-surface test fixture: the capsule's routes mounted EXACTLY
// the way the serve path does (the real MountAllAPI over the plugin,
// the module routes muxed from the live Context's ModuleRoutes with
// their registered patterns) - the tests drive the route registration,
// never a hand-wired handler list.

import (
	"net/http"
	"os"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// chdirToWorkdir points the file routes at a temp directory: the root
// chain falls to the process cwd when no session/workspace exists, so
// the tests chdir into it. Idempotent per test: a test that already
// chdir'd gets the SAME dir back (the mux builder below also calls it,
// and a second chdir would strand the test's fixtures in the first
// dir while the routes serve the second - every write-then-request
// test read empty and 404'd).
var testWorkdir string

func chdirToWorkdir(t *testing.T) string {
	t.Helper()
	if testWorkdir == "" {
		dir := t.TempDir()
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		testWorkdir = dir
		t.Cleanup(func() {
			testWorkdir = ""
			os.Chdir("/") //nolint:errcheck // test cleanup only
		})
	}
	return testWorkdir
}

// claimTestWorkdir registers an already-chdir'd directory as this
// test's workdir, so the mux builder below reuses it instead of
// chdir-ing away (the symlink-root test needs the process to stay in
// the directory it entered).
func claimTestWorkdir(t *testing.T, dir string) {
	t.Helper()
	testWorkdir = dir
	t.Cleanup(func() { testWorkdir = "" })
}

// filesRouteMux mounts one files plugin (no store; the chain ends at
// cwd) and returns the module-route mux the http channel serves.
func filesRouteMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux, _, _, _ := filesRouteMuxWithStore(t)
	return mux
}

// filesRouteMuxWithStore mounts one files plugin with an in-memory
// store offered (the root-resolution law's step 1 source) and returns
// the module route mux, the capsule's Routes handle (the knob's hot
// setter), that store (the same handle the routes resolve), and the
// cwd.
func filesRouteMuxWithStore(t *testing.T) (*http.ServeMux, *Routes, agent.StoreProvider, string) {
	t.Helper()
	cwd := chdirToWorkdir(t)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := &core.Context{Configs: map[string]any{}}
	ctx.ApplyOffer("store", "test", st)
	p := NewPlugin()
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	mux := http.NewServeMux()
	for _, rt := range ctx.ModuleRoutes {
		mux.HandleFunc(rt.Pattern, rt.Handler)
	}
	return mux, p.routes, st, cwd
}

// filesDefaultSetter returns the Routes' SetDefaultWorkspace as the
// hot-apply path would call it.
func filesDefaultSetter(r *Routes) func(string) {
	return r.SetDefaultWorkspace
}
