package http_channel

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

func TestPluginMount(t *testing.T) {
	p := NewPlugin()
	ctx := &core.Context{
		Configs: map[string]any{
			"http_channel": Config{Port: 9999},
		},
	}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if len(ctx.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(ctx.Channels))
	}
	ch, ok := ctx.Channels[0].(*HTTPChannel)
	if !ok {
		t.Fatalf("expected *HTTPChannel, got %T", ctx.Channels[0])
	}
	if ch.addr != "127.0.0.1:9999" {
		t.Errorf("addr = %q, want 127.0.0.1:9999", ch.addr)
	}
}

func TestPluginMountDefaultConfig(t *testing.T) {
	// No config on the Context — plugin falls back to defaults.
	p := NewPlugin()
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
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
	if ch.addr != "127.0.0.1:8099" {
		t.Errorf("addr = %q, want 127.0.0.1:8099", ch.addr)
	}
}

func TestPluginMountMultiple(t *testing.T) {
	// Channels are additive: two mounts produce two channels.
	ctx := &core.Context{
		Configs: map[string]any{
			"http_channel": Default(),
		},
	}

	p := NewPlugin()
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("Mount 1: %v", err)
	}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("Mount 2: %v", err)
	}
	if len(ctx.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(ctx.Channels))
	}
}

// explicit bind list creates one channel per address; empty list falls
// back to loopback only (TestPluginMountDefaultConfig covers that).
func TestPluginMountBindList(t *testing.T) {
	ctx := &core.Context{Configs: map[string]any{
		"http_channel": Config{Port: 9999, Bind: []string{"127.0.0.1", "100.101.126.30"}},
	}}
	p := NewPlugin()
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if len(ctx.Channels) != 2 {
		t.Fatalf("channels = %d, want 2", len(ctx.Channels))
	}
	addrs := []string{}
	for _, c := range ctx.Channels {
		ch, ok := c.(*HTTPChannel)
		if !ok {
			t.Fatalf("expected *HTTPChannel, got %T", c)
		}
		addrs = append(addrs, ch.addr)
	}
	if addrs[0] != "127.0.0.1:9999" || addrs[1] != "100.101.126.30:9999" {
		t.Fatalf("addrs = %v", addrs)
	}
}

func TestStartNoProvider(t *testing.T) {
	// A Context with no provider must fail Start, not panic.
	ch := &HTTPChannel{addr: ":0"}
	err := ch.Start(context.Background(), core.ChannelDeps{
		Ctx:   &core.Context{},
		Store: nil,
	})
	if err == nil {
		t.Fatal("expected error with no provider configured")
	}
}
