package http_channel

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
)

func TestHTTPChannelImplementsChannel(t *testing.T) {
	var _ agent.Channel = (*HTTPChannel)(nil)
}

func TestPluginImplementsInterface(t *testing.T) {
	var _ agent.Plugin = (*Plugin)(nil)
}

func TestPluginMetadata(t *testing.T) {
	p := NewPlugin()
	if p.Name() != "http_channel" {
		t.Errorf("Name() = %q, want %q", p.Name(), "http_channel")
	}
	provides := p.Provides()
	if len(provides) != 1 || provides[0] != "channel" {
		t.Errorf("Provides() = %v, want [channel]", provides)
	}
	if len(p.Requires()) != 0 {
		t.Errorf("Requires() = %v, want empty", p.Requires())
	}
}

func TestPluginMount(t *testing.T) {
	cfg := gateway.DefaultConfig()
	cfg.Server.Port = 9999

	p := NewPlugin()
	ctx := &agent.Context{Config: cfg}
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
	cfg := gateway.DefaultConfig()
	ctx := &agent.Context{Config: cfg}

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