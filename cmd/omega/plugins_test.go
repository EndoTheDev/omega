package main

import (
	"testing"
)

// TestBuildPlugins verifies buildPlugins returns all 15 in-process
// extensions, no error, and every plugin is non-nil, when given a
// default Config.
func TestBuildPlugins(t *testing.T) {
	cfg := DefaultConfig()

	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins returned error: %v", err)
	}

	const want = 15
	if len(plugins) != want {
		t.Fatalf("buildPlugins returned %d plugins, want %d", len(plugins), want)
	}

	for i, p := range plugins {
		if p == nil {
			t.Errorf("plugins[%d] is nil", i)
		}
	}
}

// TestSignalContext verifies signalContext returns a non-nil context
// and cancel func, the context is not initially done, and cancel does
// not panic.
func TestSignalContext(t *testing.T) {
	ctx, cancel := signalContext()
	if ctx == nil {
		t.Fatal("signalContext returned nil context")
	}
	if cancel == nil {
		t.Fatal("signalContext returned nil cancel func")
	}

	select {
	case <-ctx.Done():
		t.Fatal("context is already done before cancel")
	default:
	}

	// Calling cancel must not panic.
	cancel()

	// After cancel, the context should become done.
	select {
	case <-ctx.Done():
	default:
		t.Fatal("context not done after cancel")
	}
}