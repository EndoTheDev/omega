package store

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// TestPluginImplementsInterface verifies the Plugin type satisfies
// agent.Plugin at compile time.
func TestPluginImplementsInterface(t *testing.T) {
	var _ agent.Plugin = (*Plugin)(nil)
}

// TestNewStoreOpensAndSearches verifies the store opens, accepts
// messages, and FTS5 search returns results.
func TestNewStoreOpensAndSearches(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", "test session"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("hello searchable world")); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	results, err := s.SearchMessages(ctx, "searchable")
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SessionID != "s1" {
		t.Fatalf("expected session s1, got %s", results[0].SessionID)
	}
}

// TestSearchTool verifies the sessions.search tool returns formatted
// results with the session label.
func TestSearchTool(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	s.CreateSession(ctx, "s1", "", "my labeled session")
	s.AppendMessage(ctx, "s1", ai.NewUser("unique searchable content"))

	tp := &searchToolProvider{store: s}
	tools := tp.Tools()
	tool, ok := tools["sessions.search"]
	if !ok {
		t.Fatal("expected sessions.search tool in tool map")
	}

	result, err := tool.Run(ctx, map[string]any{"query": "unique"})
	if err != nil {
		t.Fatalf("tool.Run: %v", err)
	}
	if result == "" || result == "no results" {
		t.Fatalf("expected search results, got %q", result)
	}
	// The tool formats results as "[label] snippet" — verify the label
	// from the session appears.
	if !strings.Contains(result, "my labeled session") {
		t.Fatalf("expected label 'my labeled session' in result, got %q", result)
	}
}

// TestSearchToolNoResults verifies the tool returns "no results" when
// the query matches nothing.
func TestSearchToolNoResults(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	tp := &searchToolProvider{store: s}
	tools := tp.Tools()
	result, err := tools["sessions.search"].Run(ctx, map[string]any{"query": "nonexistent"})
	if err != nil {
		t.Fatalf("tool.Run: %v", err)
	}
	if result != "no results" {
		t.Fatalf("expected 'no results', got %q", result)
	}
}

// TestPluginMount verifies Mount populates ctx.Store and adds a
// sessions.search tool provider, reading DSN from Config.
func TestPluginMount(t *testing.T) {
	p := NewPlugin()
	ctx := &agent.Context{
		Configs: map[string]any{
			"store": Config{DBPath: ":memory:"},
		},
	}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	defer ctx.Store.Close()

	if ctx.Store == nil {
		t.Fatal("Store slot not populated")
	}
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("expected 1 tool provider, got %d", len(ctx.ToolProviders))
	}
	tools := ctx.ToolProviders[0].Tools()
	if _, ok := tools["sessions.search"]; !ok {
		t.Fatal("expected sessions.search tool from mounted provider")
	}
}

