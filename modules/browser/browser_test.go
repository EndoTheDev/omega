package browser

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// TestStoreRoundTrip: the store persists a valid last URL and reloads
// it; corrupt content is a zero state, never a boot failure.
func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "browser.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatalf("open empty: %v", err)
	}
	if s.Last() != "" {
		t.Fatalf("empty home: last = %q", s.Last())
	}
	if err := s.SetLast("https://example.test/docs"); err != nil {
		t.Fatalf("set: %v", err)
	}
	s2, err := OpenStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if s2.Last() != "https://example.test/docs" {
		t.Fatalf("round trip: last = %q", s2.Last())
	}

	// corrupt file: zero state, no error
	if werr := os.WriteFile(path, []byte("{not json"), 0o644); werr != nil {
		t.Fatalf("write corrupt: %v", werr)
	}
	s3, err := OpenStore(path)
	if err != nil {
		t.Fatalf("open corrupt: %v", err)
	}
	if s3.Last() != "" {
		t.Fatalf("corrupt file must read as zero state, got %q", s3.Last())
	}

	// wrong-typed content: the loader never trusts a non-URL
	if werr := os.WriteFile(path, []byte(`{"last": "javascript:alert(1)"}`), 0o644); werr != nil {
		t.Fatalf("write wrong: %v", werr)
	}
	s4, err := OpenStore(path)
	if err != nil {
		t.Fatalf("open wrong: %v", err)
	}
	if s4.Last() != "" {
		t.Fatalf("non-http scheme must be refused on load, got %q", s4.Last())
	}
}

// TestStoreMemoryOnly: a pathless store answers as a zero state and
// swallows persistence (the no-home embed shape).
func TestStoreMemoryOnly(t *testing.T) {
	s, err := OpenStore("")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.SetLast("https://example.test"); err != nil {
		t.Fatalf("set on memory store: %v", err)
	}
	if s.Last() != "https://example.test" {
		t.Fatalf("memory store lost the write: %q", s.Last())
	}
}

// TestValidURL: only absolute http(s) URLs the address bar could
// actually navigate. Shape validation, not an origin gate - any site
// is allowed, any scheme-shape is not.
func TestValidURL(t *testing.T) {
	good := []string{
		"https://example.com",
		"https://example.com/docs/?q=x#anchor",
		"http://127.0.0.1:8098/health",
		"https://10.0.0.3:8080/admin",
	}
	for _, u := range good {
		if !ValidURL(u) {
			t.Errorf("%q: refused, want allowed", u)
		}
	}
	bad := []string{
		"",
		"example.com", // the SPA normalizes the scheme first
		"ftp://example.com",
		"javascript:alert(1)",
		"file:///etc/os-release",
		"data:text/html;base64,xx",
		"https://exa mple.com", // embedded space
		"https://ok\x01.com",   // control bytes
	}
	for _, u := range bad {
		if ValidURL(u) {
			t.Errorf("%q: allowed, want refused", u)
		}
	}
}

// mountPlugin mounts the plugin the api way and returns the live
// Context (the ModuleRoutes ledger rides it).
func mountPlugin(t *testing.T, p *Plugin) *core.Context {
	t.Helper()
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	return ctx
}

// serveRoute finds the module's route on the context and serves one
// request through it.
func serveRoute(t *testing.T, ctx *core.Context, pattern, method, target, body string) (int, string) {
	t.Helper()
	for _, rt := range ctx.ModuleRoutes {
		if rt.Pattern == pattern {
			w := httptest.NewRecorder()
			var reader *bytes.Reader
			if body != "" {
				reader = bytes.NewReader([]byte(body))
			} else {
				reader = bytes.NewReader(nil)
			}
			rt.Handler(w, httptest.NewRequest(method, target, reader))
			return w.Code, w.Body.String()
		}
	}
	t.Fatalf("%s not registered on the mount", pattern)
	return 0, ""
}

// TestRouteRoundTrip is the tab's wire contract: empty resume point
// answers 200 "", a PUT with a good URL persists, a GET round-trips
// it, a wrong-URL PUT is a 400, and the module is on the registry
// (toggleable, route-rooted under /browser).
func TestRouteRoundTrip(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	t.Setenv("OMEGA_AGENT", "")

	p := NewPlugin()
	ctx := mountPlugin(t, p)
	if p.store == nil {
		t.Fatal("mount left the plugin storeless")
	}
	if _, err := os.Stat(filepath.Join(datadir.Home(), "data", StoreName)); err != nil && !os.IsNotExist(err) {
		t.Fatalf("store stat: %v", err)
	}

	code, body := serveRoute(t, ctx, "GET /browser/last", "GET", "/browser/last", "")
	if code != 200 {
		t.Fatalf("GET empty: %d", code)
	}
	var rep struct {
		Last string `json:"last"`
	}
	if err := json.Unmarshal([]byte(body), &rep); err != nil || rep.Last != "" {
		t.Fatalf("GET empty body = %q (%v)", body, err)
	}

	code, _ = serveRoute(t, ctx, "PUT /browser/last", "PUT", "/browser/last", `{"url":"https://example.test/docs"}`)
	if code != 200 {
		t.Fatalf("PUT good: %d", code)
	}
	_, body = serveRoute(t, ctx, "GET /browser/last", "GET", "/browser/last", "")
	if err := json.Unmarshal([]byte(body), &rep); err != nil || rep.Last != "https://example.test/docs" {
		t.Fatalf("GET after PUT: %q (%v)", body, err)
	}
	// persisted to disk (the same store a restart reloads)
	s2, err := OpenStore(filepath.Join(datadir.Home(), "data", StoreName))
	if err != nil {
		t.Fatalf("reopen persisted: %v", err)
	}
	if s2.Last() != "https://example.test/docs" {
		t.Fatalf("disk persistence lost: %q", s2.Last())
	}

	code, body = serveRoute(t, ctx, "PUT /browser/last", "PUT", "/browser/last", `{"url":"javascript:alert(1)"}`)
	if code != 400 || !strings.Contains(body, "error") {
		t.Fatalf("PUT wrong shape: %d %s", code, body)
	}
	if p.store.Last() != "https://example.test/docs" {
		t.Fatalf("refused PUT mutated the store: %q", p.store.Last())
	}

	// the toggleable shape: registry introspection names it
	if !p.Toggleable() {
		t.Fatal("browser capsule must be toggleable")
	}
}