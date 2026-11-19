package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/EndoTheDev/omega/agent"
	agentapi "github.com/EndoTheDev/omega/agent/api"
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

// explicit bind list creates one channel per address.
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

// runtimeTitleModel is the wire boundary for the title knob.
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

// TestTitleModelDefaultFollowsTheRunModel walks the REAL wire path (plugin Config ->
// HTTPChannel.wire -> SetChannelKnobs) with title_model: default and a chat whose title
// call follows it: the detached title call must stream through the model the chat ran,
// never through a model literally NAMED "default".
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
	// The title call is a detached goroutine fired once the exchange lands. The wait
	// condition must be the observable the assertions read - the store's label - not the
	// provider's request count: the second request is served before the title goroutine
	// has parsed the response and written the label, and a loaded box widens that gap (the
	// nightly flake this test shipped with).
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
	if sess, err := st.GetSession(context.Background(), "title-default"); err != nil || sess.Label == "" {
		t.Fatalf("no title written (session=%+v, err=%v) - the title call never ran", sess, err)
	}
}

// TestMultiBindWebsocketSharesOneHub pins the multi-bind push seam on the real start
// path: two loopback binds, one websocket client per bind, a chat run started on the
// FIRST bind - and the chat frames must land on BOTH clients.
func TestMultiBindWebsocketSharesOneHub(t *testing.T) {
	prov := newMultiProvider("step")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	p := NewPlugin()
	ctx := &core.Context{Configs: map[string]any{
		"http_channel": Config{Port: port, Bind: []string{"127.0.0.1", "127.0.0.2"}},
	}}
	ctx.ApplyOffer("provider", "test", prov)
	ctx.ApplyOffer("loop", "test", testLoop{})
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if len(p.mounted) != 2 {
		t.Fatalf("binds = %d, want one channel per bind", len(p.mounted))
	}
	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	deps := core.ChannelDeps{Ctx: ctx, Store: st}
	for _, ch := range ctx.Channels {
		go func(c core.Channel) { _ = c.Start(runCtx, deps) }(ch)
	}
	binds := []string{"127.0.0.1", "127.0.0.2"}
	for _, b := range binds {
		waitMirrorHealthy(t, fmt.Sprintf("http://%s:%d", b, port))
	}

	chA, ok := p.mounted[0].(*HTTPChannel)
	if !ok {
		t.Fatalf("mounted channel %T is not an HTTPChannel", p.mounted[0])
	}
	chB, ok := p.mounted[1].(*HTTPChannel)
	if !ok {
		t.Fatalf("mounted channel %T is not an HTTPChannel", p.mounted[1])
	}

	connA := dialSocketURL(t, fmt.Sprintf("ws://%s:%d/ws", binds[0], port))
	connB := dialSocketURL(t, fmt.Sprintf("ws://%s:%d/ws", binds[1], port))

	// The seam under test: both binds serve ONE hub, and both clients are attached to it.
	if hubA, hubB := chA.srv.Sockets(), chB.srv.Sockets(); hubA != hubB {
		t.Fatalf("bind %s serves hub %p, bind %s serves hub %p: one hub cannot be per-bind and shared", binds[0], hubA, binds[1], hubB)
	}
	waitFor(t, "both binds' /ws clients attached to the one hub", func() bool {
		return chA.srv.Sockets().SubscriberCount() == 2
	})

	sbody, _ := json.Marshal(map[string]any{"id": "mb-ws"})
	sresp, err := http.Post(fmt.Sprintf("http://%s:%d/sessions", binds[0], port), "application/json", bytes.NewReader(sbody))
	if err != nil {
		t.Fatalf("create session on bind A: %v", err)
	}
	sresp.Body.Close()
	if sresp.StatusCode != http.StatusCreated {
		t.Fatalf("create session status = %d, want 201", sresp.StatusCode)
	}
	muteTitles(t, st, "mb-ws")
	chatCtx, chatCancel := context.WithCancel(context.Background())
	t.Cleanup(chatCancel)
	chatReq, err := http.NewRequestWithContext(chatCtx, http.MethodPost,
		fmt.Sprintf("http://%s:%d/chat", binds[0], port),
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}],"session_id":"mb-ws"}`))
	if err != nil {
		t.Fatalf("new chat request: %v", err)
	}
	chatResp, err := (&http.Client{}).Do(chatReq)
	if err != nil {
		t.Fatalf("chat on bind A: %v", err)
	}
	t.Cleanup(func() { chatResp.Body.Close() })
	prov.send(finalStep("cross-bind hello")...)
	prov.done()

	for name, conn := range map[string]*websocket.Conn{"bind A": connA, "bind B": connB} {
		frame := readFrameTopic(t, conn, agentapi.TopicChat)
		if frame.Session != "mb-ws" {
			t.Fatalf("%s chat frame session = %q, want mb-ws", name, frame.Session)
		}
	}
}

// dialSocketURL connects a websocket client to one bind's /ws.
func dialSocketURL(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial %s: %v (status %d)", url, err, status)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// readFrameTopic reads frames until one carries the wanted topic, skipping the rest.
// The deadline bounds a HANG, never correctness.
func readFrameTopic(t *testing.T, conn *websocket.Conn, topic string) agentapi.SocketMessage {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("no %q frame arrived on this bind's /ws: %v", topic, err)
		}
		var msg agentapi.SocketMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("decode frame: %v (%s)", err, raw)
		}
		if msg.Topic == topic {
			return msg
		}
	}
}
