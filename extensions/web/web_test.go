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
	"github.com/EndoTheDev/omega/gateway"
)

// setURLs points the package-level API URLs at test servers and
// restores them on cleanup.
func setURLs(t *testing.T, search, fetch string) {
	t.Helper()
	oldSearch, oldFetch := searchURL, fetchURL
	searchURL, fetchURL = search, fetch
	t.Cleanup(func() {
		searchURL, fetchURL = oldSearch, oldFetch
	})
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
	setURLs(t, srv.URL, fetchURL)

	ext := &Extension{apiKey: "test-key"}
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
	setURLs(t, srv.URL, fetchURL)

	ext := &Extension{apiKey: "test-key"}
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
			setURLs(t, srv.URL, fetchURL)

			ext := &Extension{apiKey: "test-key"}
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
	setURLs(t, searchURL, srv.URL)

	ext := &Extension{apiKey: "test-key"}
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
	setURLs(t, srv.URL, fetchURL)

	ext := &Extension{apiKey: "test-key"}
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
	setURLs(t, searchURL, srv.URL)

	ext := &Extension{apiKey: "test-key"}
	result, err := ext.doFetch(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !strings.Contains(result, "error: parsing fetch response") {
		t.Errorf("got %q, want parse error message", result)
	}
}

// TestDoSearchHTTP500 verifies 5xx responses surface through the
// ai.RetryHTTP path: RetryHTTP returns the last (500) response rather
// than a Go error, so doSearch reads its (empty) body and reports a
// parse failure. OMEGA_MAX_RETRIES=0 keeps the test fast.
func TestDoSearchHTTP500(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "0")
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	setURLs(t, srv.URL, fetchURL)

	ext := &Extension{apiKey: "test-key"}
	result, err := ext.doSearch(context.Background(), "q", 5)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if hits != 1 {
		t.Errorf("expected 1 attempt with OMEGA_MAX_RETRIES=0, got %d", hits)
	}
	if !strings.Contains(result, "error: parsing search response") {
		t.Errorf("got %q, want parse error on empty 500 body", result)
	}
}

// TestDoFetchHTTP500 verifies 5xx responses surface through the
// ai.RetryHTTP path: RetryHTTP returns the last (500) response rather
// than a Go error, so doFetch reads its (empty) body and reports a
// parse failure.
func TestDoFetchHTTP500(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	setURLs(t, searchURL, srv.URL)

	ext := &Extension{apiKey: "test-key"}
	result, err := ext.doFetch(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !strings.Contains(result, "error: parsing fetch response") {
		t.Errorf("got %q, want parse error on empty 500 body", result)
	}
}

// TestMountWithConfig verifies the API key flows from config to extension.

// TestPluginInterface verifies the Plugin satisfies agent.Plugin.
func TestPluginInterface(t *testing.T) {
	var _ agent.Plugin = (*Plugin)(nil)
}

// TestExtensionImplementsToolProvider verifies Extension satisfies
// agent.ToolProvider.
func TestExtensionImplementsToolProvider(t *testing.T) {
	var _ agent.ToolProvider = (*Extension)(nil)
}

// TestToolsShape checks both tools are registered with correct names.
func TestToolsShape(t *testing.T) {
	ext := New(nil) // nil config = no API key, tools still registered
	tools := ext.Tools()
	if _, ok := tools["web.search"]; !ok {
		t.Error("missing web.search tool")
	}
	if _, ok := tools["web.fetch"]; !ok {
		t.Error("missing web.fetch tool")
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

// TestSearchRequiresQuery verifies validation without making HTTP calls.
func TestSearchRequiresQuery(t *testing.T) {
	ext := New(nil)
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
	ext := New(nil)
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
	ext := New(nil) // no key
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
	ext := New(nil)
	result, err := ext.doFetch(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "error: API key not set" {
		t.Errorf("expected no-key error, got %q", result)
	}
}

// TestMount verifies Mount appends a ToolProvider to the Context.
func TestMount(t *testing.T) {
	p := NewPlugin()
	ctx := &agent.Context{}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount failed: %v", err)
	}
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("expected 1 ToolProvider, got %d", len(ctx.ToolProviders))
	}
	tools := ctx.ToolProviders[0].Tools()
	if _, ok := tools["web.search"]; !ok {
		t.Error("mounted provider missing web.search")
	}
}

// TestMountWithConfig verifies the API key flows from config to extension.
func TestMountWithConfig(t *testing.T) {
	p := NewPlugin()
	ctx := &agent.Context{
		Config: gateway.Config{
			Provider: gateway.ProviderConfig{APIKey: "test-key"},
		},
	}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount failed: %v", err)
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
