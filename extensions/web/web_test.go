package web

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
)

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
	if p.cfg.Provider.APIKey != "test-key" {
		t.Errorf("expected API key %q, got %q", "test-key", p.cfg.Provider.APIKey)
	}
}