package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
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

// runtimeTitleModel is the wire boundary for the title knob (card
// t_b403ad88): the persisted word stays in config.yaml, the server only
// ever sees a model name or the empty "no knob" value.
func TestRuntimeTitleModelKnob(t *testing.T) {
	cases := []struct{ configured, want string }{
		{"", ""},
		{"default", ""},
		{" default ", ""},
		{"gemcheap", "gemcheap"},
		{"big:gemcheap", "big:gemcheap"},
	}
	for _, c := range cases {
		if got := runtimeTitleModel(c.configured); got != c.want {
			t.Errorf("runtimeTitleModel(%q) = %q, want %q", c.configured, got, c.want)
		}
	}
}

// TestTitleModelDefaultFollowsTheRunModel walks the REAL wire path
// (plugin Config -> HTTPChannel.wire -> SetChannelKnobs) with
// title_model: default and a chat whose title call follows it: the
// detached title call must stream through the model the chat ran, never
// through a model literally NAMED "default". Before the boundary
// normalisation the word rode the server verbatim, so this red as a title
// call to "default".
func TestTitleModelDefaultFollowsTheRunModel(t *testing.T) {
	prov := newMockProvider("run-model", scripted(
		core.ResponseChunk{Type: "response_chunk", Content: "hello"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	))
	plugin := NewPlugin()
	ctx := &core.Context{Configs: map[string]any{
		"http_channel": Config{Port: 1, TitleModel: agent.DefaultModelSlot},
	}}
	if _, err := agent.MountAllAPI([]agent.Plugin{plugin}, ctx, nil, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	ctx.ApplyOffer("provider", "test", prov)
	// The loop seam's floor (no loop mounted) answers nothing; the api
	// test double drives the streamed turn deterministically.
	ctx.ApplyOffer("loop", "test", testLoop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	if len(plugin.mounted) != 1 {
		t.Fatalf("binds = %d, want 1", len(plugin.mounted))
	}
	ch, ok := plugin.mounted[0].(*HTTPChannel)
	if !ok {
		t.Fatalf("mounted channel %T is not an HTTPChannel", plugin.mounted[0])
	}
	if err := ch.wire(core.ChannelDeps{Ctx: ctx, Store: st, Version: "test"}); err != nil {
		t.Fatalf("wire: %v", err)
	}
	ts := httptest.NewServer(ch.srv.Handler())
	t.Cleanup(ts.Close)

	createSession(t, ts, "title-default")
	resp, cancel, _ := postChatStream(t, ts, "title-default")
	t.Cleanup(func() {
		resp.Body.Close()
		cancel()
	})
	// The title call is a detached goroutine fired once the exchange
	// lands: wait for it, then judge every model that served. The wait
	// condition must be the observable the assertions read - the store's
	// label - not the provider's request count: the second request is
	// served before the title goroutine has parsed the response and
	// written the label, and a loaded box widens that gap (the nightly
	// flake this test shipped with).
	waitFor(t, "the chat and its detached title call", func() bool {
		if len(prov.servedModelNames()) < 2 {
			return false
		}
		sess, err := st.GetSession(context.Background(), "title-default")
		return err == nil && sess.Label != ""
	})
	models := prov.servedModelNames()
	for i, model := range models {
		if model == agent.DefaultModelSlot {
			t.Fatalf("call %d routed to a model named %q - the knob's word must resolve to the run's model (served: %v)", i+1, agent.DefaultModelSlot, models)
		}
		if model != "run-model" {
			t.Fatalf("call %d served by %q, want the run's model (served: %v)", i+1, model, models)
		}
	}
	// The title really landed: the detached call ran, not just the chat.
	if sess, err := st.GetSession(context.Background(), "title-default"); err != nil || sess.Label == "" {
		t.Fatalf("no title written (session=%+v, err=%v) - the title call never ran", sess, err)
	}
}
