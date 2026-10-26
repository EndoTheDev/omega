package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"gopkg.in/yaml.v3"
)

// setBase returns an extension pointed at the given test server.
func setBase(apiKey, base string) *Extension {
	return &Extension{apiKey: apiKey, baseURL: base}
}

// TestDoSearchSuccess verifies the happy path: auth header, request
// body, and formatted output.
func TestDoSearchSuccess(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		fmt.Fprint(w, `{"results":[{"title":"T","url":"U","content":"C"}]}`)
	}))
	defer srv.Close()

	ext := setBase("test-key", srv.URL)
	result, err := ext.doSearch(context.Background(), "golang", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
	}
	if !strings.Contains(gotBody, `"query":"golang"`) {
		t.Errorf("request body missing query: %s", gotBody)
	}
	for _, want := range []string{"## T", "C", "URL: U"} {
		if !strings.Contains(result, want) {
			t.Errorf("output missing %q:\n%s", want, result)
		}
	}
}

// TestDoSearchZeroResults verifies the empty-results message.
func TestDoSearchZeroResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results":[]}`)
	}))
	defer srv.Close()

	ext := setBase("k", srv.URL)
	result, err := ext.doSearch(context.Background(), "nothing", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No results found." {
		t.Errorf("got %q, want %q", result, "No results found.")
	}
}

// TestRunSearchMaxResults verifies max_results parsing for both
// float64 (JSON-decoded) and int args via the request body.
func TestRunSearchMaxResults(t *testing.T) {
	cases := []struct {
		name string
		arg  any
		want string
	}{
		{"float64", float64(8.0), `"max_results":8`},
		{"int", 3, `"max_results":3`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				gotBody = string(b)
				fmt.Fprint(w, `{"results":[]}`)
			}))
			defer srv.Close()

			ext := setBase("k", srv.URL)
			if _, err := ext.runSearch(context.Background(), map[string]any{
				"query":       "q",
				"max_results": tc.arg,
			}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(gotBody, tc.want) {
				t.Errorf("body %s missing %s", gotBody, tc.want)
			}
		})
	}
}

// TestDoFetchSuccess verifies the formatted page output with links.
func TestDoFetchSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"title":"Page","content":"Body","links":["a","b"]}`)
	}))
	defer srv.Close()

	ext := setBase("k", srv.URL)
	result, err := ext.doFetch(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"# Page", "Body", "## Links", "- a", "- b"} {
		if !strings.Contains(result, want) {
			t.Errorf("output missing %q:\n%s", want, result)
		}
	}
}

// TestDoSearchInvalidJSON verifies parse failures come back as
// (msg, nil), not as a Go error.
func TestDoSearchInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not json")
	}))
	defer srv.Close()

	ext := setBase("k", srv.URL)
	result, err := ext.doSearch(context.Background(), "q", 5)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !strings.Contains(result, "error: parsing search response") {
		t.Errorf("got %q, want parse error message", result)
	}
}

// TestDoFetchInvalidJSON verifies parse failures come back as
// (msg, nil), not as a Go error.
func TestDoFetchInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html>garbage</html>")
	}))
	defer srv.Close()

	ext := setBase("k", srv.URL)
	result, err := ext.doFetch(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !strings.Contains(result, "error: parsing fetch response") {
		t.Errorf("got %q, want parse error message", result)
	}
}

// TestDoSearchHTTP401 verifies non-2xx statuses surface as loud error
// strings — the 401 body must never fall through to a silent
// "No results found."
func TestDoSearchHTTP401(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"unauthorized credentials"}`)
	}))
	defer srv.Close()

	ext := setBase("wrong-key", srv.URL)
	result, err := ext.doSearch(context.Background(), "q", 5)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !strings.Contains(result, "error: search failed: HTTP 401") {
		t.Errorf("got %q, want loud HTTP 401 error", result)
	}
	if !strings.Contains(result, "unauthorized credentials") {
		t.Errorf("got %q, want body snippet in error", result)
	}
	if strings.Contains(result, "No results found") {
		t.Errorf("silent empty-result path leaked through: %q", result)
	}
}

// TestDoFetchHTTP401 verifies non-2xx statuses surface as loud error
// strings for fetch too.
func TestDoFetchHTTP401(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"unauthorized credentials"}`)
	}))
	defer srv.Close()

	ext := setBase("wrong-key", srv.URL)
	result, err := ext.doFetch(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !strings.Contains(result, "error: fetch failed: HTTP 401") {
		t.Errorf("got %q, want loud HTTP 401 error", result)
	}
}

// TestDoSearchHTTP500 verifies 5xx responses surface as loud errors
// through the ai.RetryHTTP path: RetryHTTP returns the last (500)
// response rather than a Go error, so doSearch reads its body and
// reports the status. OMEGA_MAX_RETRIES=0 keeps the test fast.
func TestDoSearchHTTP500(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "0")
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ext := setBase("k", srv.URL)
	result, err := ext.doSearch(context.Background(), "q", 5)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if hits != 1 {
		t.Errorf("expected 1 attempt with OMEGA_MAX_RETRIES=0, got %d", hits)
	}
	if !strings.Contains(result, "error: search failed: HTTP 500") {
		t.Errorf("got %q, want loud HTTP 500 error", result)
	}
}

// TestDoFetchHTTP500 verifies 5xx responses surface as loud error
// strings for fetch too.
func TestDoFetchHTTP500(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ext := setBase("k", srv.URL)
	result, err := ext.doFetch(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !strings.Contains(result, "error: fetch failed: HTTP 500") {
		t.Errorf("got %q, want loud HTTP 500 error", result)
	}
}

// TestHTTPErrorMessage verifies the snippet truncation rules.
func TestHTTPErrorMessage(t *testing.T) {
	long := strings.Repeat("x", 250)
	msg := httpError(502, []byte("  "+long+"  "), "search")
	if msg != "error: search failed: HTTP 502: "+strings.Repeat("x", 200)+"..." {
		t.Errorf("long-snippet case: got %q", msg)
	}
	if msg := httpError(204, []byte("ignored"), "search"); msg != "" {
		t.Errorf("2xx case returned %q, want empty", msg)
	}
	if msg := httpError(500, nil, "fetch"); msg != "error: fetch failed: HTTP 500" {
		t.Errorf("empty-body case: got %q", msg)
	}
}

// TestNewDefaultBaseURL verifies the fallback to the default upstream
// mirror and trailing-slash trimming.
func TestNewDefaultBaseURL(t *testing.T) {
	if got := New(Config{}).baseURL; got != defaultBaseURL {
		t.Errorf("empty BaseURL -> %q, want %q", got, defaultBaseURL)
	}
	if got := New(Config{BaseURL: "http://localhost:11435/"}).baseURL; got != "http://localhost:11435" {
		t.Errorf("trailing slash kept: %q", got)
	}
}

// TestPluginInterface verifies the Plugin satisfies agent.Plugin.
func TestPluginInterface(t *testing.T) {
	var _ agent.Plugin = (*Plugin)(nil)
}

// TestExtensionImplementsToolProvider verifies Extension satisfies
// core.ToolProvider.
func TestExtensionImplementsToolProvider(t *testing.T) {
	var _ core.ToolProvider = (*Extension)(nil)
}

// TestToolsShape checks the namespaced tool pair registers with
// correct names and per-action parameters.
func TestToolsShape(t *testing.T) {
	ext := New(Default()) // no API key, tools still registered
	tools := ext.Tools()
	for _, name := range []string{"web.search", "web.fetch"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("missing %s tool", name)
		}
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
	// Each verb carries its own parameters (the schema split that came
	// with the Q10 rename); web.search wants query, web.fetch wants url.
	if _, ok := tools["web.search"].Parameters["properties"].(map[string]any)["query"]; !ok {
		t.Error("web.search schema lost the query property")
	}
	if _, ok := tools["web.fetch"].Parameters["properties"].(map[string]any)["url"]; !ok {
		t.Error("web.fetch schema lost the url property")
	}
}

// TestRunWebRoutesBothActions is the fold gate: both verbs are
// reachable through the shared dispatcher (validation errors, no HTTP
// calls).
func TestRunWebRoutesBothActions(t *testing.T) {
	ext := New(Default())
	// search routes
	result, err := ext.runWeb(context.Background(), map[string]any{"action": "search"})
	if err != nil || result != "error: query is required" {
		t.Fatalf("search route: %q %v", result, err)
	}
	// fetch routes
	result, err = ext.runWeb(context.Background(), map[string]any{"action": "fetch"})
	if err != nil || result != "error: url is required" {
		t.Fatalf("fetch route: %q %v", result, err)
	}
	// the registered namespaced tools route through the same dispatcher
	if out, err := ext.Tools()["web.search"].Run(context.Background(), map[string]any{}); err != nil || out != "error: query is required" {
		t.Fatalf("web.search: %q %v", out, err)
	}
	// unknown action
	if _, err := ext.runWeb(context.Background(), map[string]any{}); err == nil {
		t.Fatal("empty action accepted")
	}
}

// TestSearchRequiresQuery verifies validation without making HTTP calls.
func TestSearchRequiresQuery(t *testing.T) {
	ext := New(Default())
	result, err := ext.runSearch(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "error: query is required" {
		t.Errorf("expected query-required error, got %q", result)
	}
}

// TestFetchRequiresURL verifies validation without making HTTP calls.
func TestFetchRequiresURL(t *testing.T) {
	ext := New(Default())
	result, err := ext.runFetch(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "error: url is required" {
		t.Errorf("expected url-required error, got %q", result)
	}
}

// TestSearchNoAPIKey verifies the no-key guard fires before HTTP.
func TestSearchNoAPIKey(t *testing.T) {
	ext := New(Default()) // no key
	result, err := ext.doSearch(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "error: API key not set" {
		t.Errorf("expected no-key error, got %q", result)
	}
}

// TestFetchNoAPIKey verifies the no-key guard fires before HTTP.
func TestFetchNoAPIKey(t *testing.T) {
	ext := New(Default())
	result, err := ext.doFetch(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "error: API key not set" {
		t.Errorf("expected no-key error, got %q", result)
	}
}

// TestMount verifies the api mount registers the ToolProvider on the
// Context - the ExtensionAPI path is web's only mount since the v0.12
// phase-C2 migration.
func TestMount(t *testing.T) {
	p := NewPlugin()
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil); err != nil {
		t.Fatalf("api mount failed: %v", err)
	}
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("expected 1 ToolProvider, got %d", len(ctx.ToolProviders))
	}
	tools := ctx.ToolProviders[0].Tools()
	if _, ok := tools["web.search"]; !ok {
		t.Error("mounted provider missing web.search")
	}
	if _, ok := tools["web.fetch"]; !ok {
		t.Error("mounted provider missing web.fetch")
	}
	if _, ok := tools["web"]; ok {
		t.Error("the unprefixed consolidated web tool must be retired")
	}
	if err := p.Mount(ctx); err == nil {
		t.Error("the retired classic mount must fail loud")
	}
}

// TestMountWithConfig verifies the API key and base URL flow from
// config to extension.
func TestMountWithConfig(t *testing.T) {
	p := NewPlugin()
	ctx := &core.Context{
		Configs: map[string]any{
			"web": Config{APIKey: "sk-test", BaseURL: "http://host:1"},
		},
	}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil); err != nil {
		t.Fatalf("api mount failed: %v", err)
	}
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("expected 1 tool provider, got %d", len(ctx.ToolProviders))
	}
	tools := ctx.ToolProviders[0].Tools()
	ws, ok := tools["web.search"]
	if !ok {
		t.Fatal("web.search tool not found")
	}
	if ws.Description == "" {
		t.Error("web.search tool has empty description")
	}
}

// TestConfigYAMLTags verifies the yaml tags round-trip, so a
// config.yaml stanza lands in the right fields.
func TestConfigYAMLTags(t *testing.T) {
	var c Config
	src := []byte("api_key: k\nbase_url: http://h:1\n")
	if err := yaml.Unmarshal(src, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.APIKey != "k" || c.BaseURL != "http://h:1" {
		t.Errorf("yaml tags wrong: %+v", c)
	}
}
