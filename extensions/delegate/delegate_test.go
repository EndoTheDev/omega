package delegate

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// TestCompile verifies the package compiles and the interfaces are satisfied.
func TestCompile(t *testing.T) {
	// ToolProvider interface.
	var _ agent.ToolProvider = (*Delegate)(nil)
	// Plugin interface.
	var _ agent.Plugin = (*Plugin)(nil)
}

// TestTools verifies both tools are registered with correct names.
func TestTools(t *testing.T) {
	d := NewDelegate()
	tools := d.Tools()
	if _, ok := tools["delegate.task"]; !ok {
		t.Fatal("missing delegate.task tool")
	}
	if _, ok := tools["delegate.status"]; !ok {
		t.Fatal("missing delegate.status tool")
	}
	if tools["delegate.task"].Description == "" {
		t.Fatal("delegate.task has empty description")
	}
}

// TestDelegateStatus verifies delegate.status returns correct output
// when no tasks are running.
func TestDelegateStatus(t *testing.T) {
	d := NewDelegate()
	out, err := d.runDelegateStatus(context.Background(), nil)
	if err != nil {
		t.Fatalf("runDelegateStatus: %v", err)
	}
	if !strings.Contains(out, "Running: 0") {
		t.Fatalf("expected 'Running: 0', got %q", out)
	}
	if !strings.Contains(out, "No tasks") {
		t.Fatalf("expected 'No tasks', got %q", out)
	}
}

// TestPendingCount verifies pending count is 0 with no tasks.
func TestPendingCount(t *testing.T) {
	d := NewDelegate()
	if c := d.PendingCount(); c != 0 {
		t.Fatalf("expected 0 pending, got %d", c)
	}
}

// TestPluginMount verifies Mount populates Context correctly.
func TestPluginMount(t *testing.T) {
	p := NewPlugin()
	if p.Name() != "delegate" {
		t.Fatalf("expected name 'delegate', got %q", p.Name())
	}

	ctx := &agent.Context{}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("expected 1 tool provider, got %d", len(ctx.ToolProviders))
	}
	if ctx.InjectedMessages == nil {
		t.Fatal("InjectedMessages not set")
	}
	if ctx.PendingDelegations == nil {
		t.Fatal("PendingDelegations not set")
	}
	if ctx.PendingDelegations() != 0 {
		t.Fatalf("expected 0 pending, got %d", ctx.PendingDelegations())
	}
}