package http_channel

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func TestPluginMount(t *testing.T) {
	p := NewPlugin()
	ctx := &agent.Context{
		Configs: map[string]any{
			"http_channel": Config{Port: 9999},
		},
	}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if len(ctx.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(ctx.Channels))
	}
	ch, ok := ctx.Channels[0].(*HTTPChannel)
	if !ok {
		t.Fatalf("expected *HTTPChannel, got %T", ctx.Channels[0])
	}
	if ch.addr != ":9999" {
		t.Errorf("addr = %q, want :9999", ch.addr)
	}
}

func TestPluginMountDefaultConfig(t *testing.T) {
	// No config on the Context — plugin falls back to defaults.
	p := NewPlugin()
	ctx := &agent.Context{}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if len(ctx.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(ctx.Channels))
	}
	ch, ok := ctx.Channels[0].(*HTTPChannel)
	if !ok {
		t.Fatalf("expected *HTTPChannel, got %T", ctx.Channels[0])
	}
	// DefaultConfig port is 8099.
	if ch.addr != ":8099" {
		t.Errorf("addr = %q, want :8099", ch.addr)
	}
}

func TestPluginMountMultiple(t *testing.T) {
	// Channels are additive: two mounts produce two channels.
	ctx := &agent.Context{
		Configs: map[string]any{
			"http_channel": Default(),
		},
	}

	p := NewPlugin()
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount 1: %v", err)
	}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount 2: %v", err)
	}
	if len(ctx.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(ctx.Channels))
	}
}

func TestStartNoProvider(t *testing.T) {
	// A Context with no provider must fail Start, not panic.
	ch := &HTTPChannel{addr: ":0"}
	err := ch.Start(context.Background(), agent.ChannelDeps{
		Ctx:   &agent.Context{},
		Store: nil,
	})
	if err == nil {
		t.Fatal("expected error with no provider configured")
	}
}