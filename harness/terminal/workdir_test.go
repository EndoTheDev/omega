// The spawn workspace law's pin (card t_62d72028): a freshly spawned
// terminal session starts with pwd == the resolved workspace - the
// focused session's pinned root (?session=), else the
// default_workspace knob, else omega's process cwd. The spawn seam
// records the workdir it was handed, so the pin reads the SPAWN
// layer, not a pty's echo.
package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// fakeSessionStores is the workspace-pin face under test: a fixed
// session table, errors on the unknown id (the fall-through shape).
type fakeSessionStores struct {
	rows map[string]core.Session
}

func (f fakeSessionStores) GetSession(_ context.Context, id string) (core.Session, error) {
	if s, ok := f.rows[id]; ok {
		return s, nil
	}
	return core.Session{}, errors.New("no such session")
}

// The production offer satisfies the capsule's consumer-owned face:
// the store capsule offers agent.StoreProvider under "store", and the
// claim's live seam resolve shape-asserts it into sessionStores - the
// assertion here is the compile-time half of that assert.
var _ sessionStores = agent.StoreProvider(nil)

// spawnRecorder is the spawn seam under test: hands out fakes and
// records every workdir it was handed.
type spawnRecorder struct {
	mu   sync.Mutex
	dirs []string
}

func (r *spawnRecorder) spawn(workdir string) (process, error) {
	r.mu.Lock()
	r.dirs = append(r.dirs, workdir)
	r.mu.Unlock()
	return newFakeProc(), nil
}

func (r *spawnRecorder) spawned() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.dirs...)
}

// newWorkspaceStore builds a store on the recording spawn seam with
// the given construction fallback (omega's process cwd in production).
func newWorkspaceStore(t *testing.T, fallback string) (*Store, *spawnRecorder) {
	t.Helper()
	rec := &spawnRecorder{}
	st := NewStore(rec.spawn, fallback, nil)
	t.Cleanup(func() { _ = st.Stop() })
	return st, rec
}

// mountWorkspaceMux mounts the plugin through the real registration
// path with the given session table offered under the store seam (the
// resolution law's step 1 source) and returns the module mux.
func mountWorkspaceMux(t *testing.T, st *Store, sess fakeSessionStores) (*Plugin, *http.ServeMux) {
	t.Helper()
	plug := NewPluginWithStore(st)
	ctx := &core.Context{}
	ctx.ApplyOffer("store", "test", sess)
	if _, err := agent.MountAllAPI([]agent.Plugin{plug}, ctx, nil, nil); err != nil {
		t.Fatalf("mount all: %v", err)
	}
	mux := http.NewServeMux()
	for _, rt := range ctx.ModuleRoutes {
		mux.HandleFunc(rt.Pattern, rt.Handler)
	}
	return plug, mux
}

// claimOnMux drives POST /terminal/sessions (with the optional
// ?session= id) and returns the claimed session id.
func claimOnMux(t *testing.T, mux *http.ServeMux, session string) string {
	t.Helper()
	url := "/terminal/sessions"
	if session != "" {
		url += "?session=" + session
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", url, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", w.Code, w.Body.String())
	}
	var rep struct {
		ID      string `json:"id"`
		Claimed bool   `json:"claimed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("claim body: %v", err)
	}
	if rep.ID == "" {
		t.Fatal("claim: empty id")
	}
	return rep.ID
}

// TestClaimWorkdirFallback pins the store layer: an empty resolved
// workdir spawns in the construction fallback (the process cwd), an
// explicit one passes through to the spawn seam untouched.
func TestClaimWorkdirFallback(t *testing.T) {
	st, rec := newWorkspaceStore(t, "/proc/cwd/fallback")
	if _, _, err := st.Claim(""); err != nil {
		t.Fatalf("claim with empty workdir: %v", err)
	}
	if _, _, err := st.Claim("/explicit/dir"); err != nil {
		t.Fatalf("claim with explicit workdir: %v", err)
	}
	got := rec.spawned()
	if len(got) != 2 || got[0] != "/proc/cwd/fallback" || got[1] != "/explicit/dir" {
		t.Fatalf("spawn workdirs: %v, want [/proc/cwd/fallback /explicit/dir]", got)
	}
}

// TestSpawnWorkspaceChain pins the resolution law end to end through
// the claim route: the focused session's pinned workspace, else the
// default_workspace knob (live: a hot SetDefaultWorkspace lands on
// the next spawn), else the construction fallback. A session the
// store does not know falls through the chain - resolution never
// fails on it. A reattach never re-spawns.
func TestSpawnWorkspaceChain(t *testing.T) {
	fallback := t.TempDir()
	st, rec := newWorkspaceStore(t, fallback)
	sess := fakeSessionStores{rows: map[string]core.Session{
		"pinned": {ID: "pinned", Workspace: "/ws/session-root"},
	}}
	plug, mux := mountWorkspaceMux(t, st, sess)
	plug.SetDefaultWorkspace("/ws/default")

	// step 1: the focused session's pinned workspace wins over the knob
	claimOnMux(t, mux, "pinned")
	// fall-through: a session the store does not know rides the knob
	claimOnMux(t, mux, "unknown-id")
	// step 2: no session id = the knob
	claimOnMux(t, mux, "")
	// step 3: no session, no knob = the construction fallback
	plug.SetDefaultWorkspace("")
	claimOnMux(t, mux, "")
	// the hot knob lands on the NEXT spawn live
	plug.SetDefaultWorkspace("/ws/hot")
	claimOnMux(t, mux, "")

	got := rec.spawned()
	want := []string{
		"/ws/session-root",
		"/ws/default",
		"/ws/default",
		fallback,
		"/ws/hot",
	}
	if len(got) != len(want) {
		t.Fatalf("spawn count: %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("spawn %d workdir: %q, want %q (chain: %v)", i, got[i], want[i], got)
		}
	}

	// reattach: a claim for a live unattached shell adopts it - no
	// new spawn, its cwd never re-resolves under the user. The chain
	// claims above never subscribed, so they stay taken (the claim
	// mark law) - this claim spawns fresh (number 6)...
	id := claimOnMux(t, mux, "pinned")
	if n := len(rec.spawned()); n != len(want)+1 {
		t.Fatalf("spawn count after claim: %d, want %d (a fresh spawn was expected)", n, len(want)+1)
	}
	// ...then the adopt (subscribe clears the claim mark) and the
	// detach hand the shell back, and the next claim REATTACHES to
	// it - no re-spawn, no workdir re-resolution
	s := st.Get(id)
	sub, _, _ := s.subscribe()
	s.unsubscribe(sub)
	claimOnMux(t, mux, "pinned")
	if n := len(rec.spawned()); n != len(want)+1 {
		t.Fatalf("spawn count after reattach: %d, want %d (a reattach must never re-spawn)", n, len(want)+1)
	}
}
