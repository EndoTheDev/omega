package http_channel

// The E2 /modules handler tests bind at the wire shape the settings
// pane consumes: the engine ring never appears (tier law), the pane's
// toggle applies through the seam, and a rejection comes back 400
// carrying the ledger text (Q11/Q12). A host without the seam answers
// 501 (the Settings precedent).

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// fakeModules records the toggle calls and plays a canned answer.
type fakeModules struct {
	mods     []core.ModuleStatus
	refusals []core.Refusal
	err      error
	toggled  []string // "name:true"/"name:false" in call order
}

func (f *fakeModules) Modules() []core.ModuleStatus { return f.mods }
func (f *fakeModules) Toggle(_ context.Context, name string, enabled bool) error {
	f.toggled = append(f.toggled, name+":"+boolWord(enabled))
	return f.err
}
func (f *fakeModules) Refusals() []core.Refusal { return f.refusals }

func boolWord(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// fakeModule builds one introspection entry.
func fakeModule(name, state string, toggleable bool) core.ModuleStatus {
	return core.ModuleStatus{Name: name, State: state, Provides: []string{name}, Toggleable: toggleable}
}

// contains reports whether the body carries substr (the local helper -
// the settings tests read bodies the same way).
func contains(body, substr string) bool { return strings.Contains(body, substr) }

// stringBody wraps a JSON literal in an io.Reader for httptest.
func stringBody(s string) io.Reader { return strings.NewReader(s) }

func TestModulesListHidesEngineRing(t *testing.T) {
	// http_channel and tui are engine ring: never in the list.
	fm := &fakeModules{mods: []core.ModuleStatus{
		fakeModule("kanban", "mounted", true),
		fakeModule("http_channel", "mounted", false),
		fakeModule("tui", "mounted", false),
		fakeModule("store", "mounted", false),
	}}
	s := NewServer(nil, nil, nil, nil, fm)
	req := httptest.NewRequest(http.MethodGet, "/modules", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /modules answered %d", rec.Code)
	}
	body := rec.Body.String()
	if want := `"name":"kanban"`; !contains(body, want) {
		t.Fatalf("the module list lost kanban: %s", body)
	}
	for _, never := range []string{"http_channel", "tui"} {
		if contains(body, `"name":"`+never+`"`) {
			t.Fatalf("engine ring module %q leaked into the list", never)
		}
	}
}

func TestModulesListCarriesRefusals(t *testing.T) {
	// Q12: the toggle ledger rides the introspection, the rejection
	// error as text (an error interface marshals empty - a lie).
	fm := &fakeModules{
		refusals: []core.Refusal{{Module: "kanban", Seam: "toggle", Predicate: "toggle.enable", Err: errors.New("module \"kanban\" panicked: boom"), At: time.Now()}},
	}
	s := NewServer(nil, nil, nil, nil, fm)
	req := httptest.NewRequest(http.MethodGet, "/modules", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !contains(body, "toggle.enable") || !contains(body, "kanban") {
		t.Fatalf("the refusal tail is missing: %s", body)
	}
	if !contains(body, "boom") {
		t.Fatalf("the refusal error must ride the wire as text: %s", body)
	}
}

func TestModuleToggleAppliesThroughSeam(t *testing.T) {
	fm := &fakeModules{mods: []core.ModuleStatus{fakeModule("kanban", "mounted", true)}}
	s := NewServer(nil, nil, nil, nil, fm)
	req := httptest.NewRequest(http.MethodPut, "/modules/kanban", stringBody(`{"enabled":false}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT answered %d: %s", rec.Code, rec.Body.String())
	}
	if len(fm.toggled) != 1 || fm.toggled[0] != "kanban:false" {
		t.Fatalf("the seam saw %v, want [kanban:false]", fm.toggled)
	}
	if !contains(rec.Body.String(), `"state":"mounted"`) {
		t.Fatalf("the refresh payload must carry the refreshed list: %s", rec.Body.String())
	}
}

func TestModuleToggleRejectionCarriesLedgerText(t *testing.T) {
	// Q11: the roll-back rejection is the body; the pane surfaces it.
	fm := &fakeModules{mods: []core.ModuleStatus{fakeModule("kanban", "disabled", true)}, err: errors.New(`module "kanban" panicked: boom (hint: add "kanban" to "disabled" in config.yaml)`)}
	s := NewServer(nil, nil, nil, nil, fm)
	req := httptest.NewRequest(http.MethodPut, "/modules/kanban", stringBody(`{"enabled":true}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("the rejection must answer 400, got %d", rec.Code)
	}
	if !contains(rec.Body.String(), "panicked") || !contains(rec.Body.String(), "config.yaml") {
		t.Fatalf("the rejection text lost the ModulePanicError shape: %s", rec.Body.String())
	}
}

func TestModulesEngineToggleRejected(t *testing.T) {
	fm := &fakeModules{mods: []core.ModuleStatus{fakeModule("http_channel", "mounted", false)}}
	s := NewServer(nil, nil, nil, nil, fm)
	req := httptest.NewRequest(http.MethodPut, "/modules/http_channel", stringBody(`{"enabled":false}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("toggling the engine must reject, got %d", rec.Code)
	}
	if len(fm.toggled) != 0 {
		t.Fatal("the engine rejection must not reach the seam")
	}
	if !contains(rec.Body.String(), "engine ring") {
		t.Fatalf("the tier-law rejection must name itself: %s", rec.Body.String())
	}
}

func TestModulesMissingSeam501(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/modules"},
		{http.MethodPut, "/modules/kanban"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, stringBody(`{"enabled":true}`))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("%s %s answered %d, want 501", tc.method, tc.path, rec.Code)
		}
	}
}

func TestModuleToggleMalformedBody(t *testing.T) {
	fm := &fakeModules{mods: []core.ModuleStatus{fakeModule("kanban", "mounted", true)}}
	s := NewServer(nil, nil, nil, nil, fm)
	req := httptest.NewRequest(http.MethodPut, "/modules/kanban", stringBody(`{"enabled":"yes"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a malformed body must answer 400, got %d", rec.Code)
	}
	// Missing enabled is the same lie the diff-only settings PUT
	// rejects: an absent field is not a toggle.
	req = httptest.NewRequest(http.MethodPut, "/modules/kanban", stringBody(`{}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || len(fm.toggled) != 0 {
		t.Fatalf("an absent enabled must not toggle: %d %v", rec.Code, fm.toggled)
	}
}

// The live re-wire (v0.12 E2): after a toggle the server re-reads the
// Context instead of the mount-time copies - a disabled module's
// routes die without a restart, a re-enabled one's return.
func TestRewireSeamsFollowsTheContext(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil)
	served := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ctx := &core.Context{ModuleRoutes: []core.ModuleRoute{{Module: "kanban", Pattern: "GET /tasks", Handler: served}}}

	// mount-shaped: the wired routes answer the module paths
	if err := s.rewireSeams(ctx); err != nil {
		t.Fatalf("rewire (mounted): %v", err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tasks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks after wire = %d", rec.Code)
	}

	// the disable cascade removed the route: one rewire, the engine
	// holds nothing module-shaped - the path falls through to the SPA
	// shell like any unknown browser route (the phase-F law: the route
	// surface lives exactly while its module mounts; the modules
	// inventory, not a 404 guard, is what tells the API caller)
	ctx.ModuleRoutes = nil
	if err := s.rewireSeams(ctx); err != nil {
		t.Fatalf("rewire (dismounted): %v", err)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tasks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks after dismount rewire = %d, want the SPA fallthrough", rec.Code)
	}

	// the remount restores it - same call, no restart
	if err := s.rewireSeams(&core.Context{ModuleRoutes: []core.ModuleRoute{{Module: "kanban", Pattern: "GET /tasks", Handler: served}}}); err != nil {
		t.Fatalf("rewire (remounted): %v", err)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tasks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks after remount rewire = %d", rec.Code)
	}
}

// TestRewireRefreshesToolInventory pins the agent half: the /tools
// inventory is the union the loop runs with, so a module's tools must
// vanish from it when the module toggles off and return when it
// toggles back - the agent re-reads the Context on every rewire.
func TestRewireRefreshesToolInventory(t *testing.T) {
	provider := &fakeToolProvider{tools: map[string]core.Tool{
		"kanban.create": {Description: "create a card"},
	}}
	ctx := &core.Context{ToolProviders: []core.ToolProvider{provider}}
	ag := core.NewFromContext(ctx, core.AgentOptions{})
	s := NewServer(ag, nil, nil, nil, nil)
	if err := s.rewireSeams(ctx); err != nil {
		t.Fatalf("rewire: %v", err)
	}
	assertToolsListed(t, s, "kanban.create", true)

	// the module went: its provider is off the Context
	ctx.ToolProviders = nil
	if err := s.rewireSeams(ctx); err != nil {
		t.Fatalf("rewire after disable: %v", err)
	}
	assertToolsListed(t, s, "kanban.create", false)

	// ... and back on
	ctx.ToolProviders = []core.ToolProvider{provider}
	if err := s.rewireSeams(ctx); err != nil {
		t.Fatalf("rewire after enable: %v", err)
	}
	assertToolsListed(t, s, "kanban.create", true)
}

type fakeToolProvider struct{ tools map[string]core.Tool }

func (f *fakeToolProvider) Tools() map[string]core.Tool { return f.tools }

// assertToolsListed reads the /tools inventory the webui's ToolsList
// renders and checks one tool's presence.
func assertToolsListed(t *testing.T, s *Server, name string, want bool) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tools", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tools = %d", rec.Code)
	}
	if want != contains(rec.Body.String(), name) {
		t.Fatalf("tool %q listed=%v, want %v (%s)", name, contains(rec.Body.String(), name), want, rec.Body.String())
	}
}
