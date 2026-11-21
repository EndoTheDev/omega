package api

// The multi-bind mirror pin. The capsule's server pin follows the last STARTED bind, so
// an action-pipe launch on any mirror registers the run there - with per-bind hubs that
// run was invisible to the transcript and events reads on every other mirror (journey
// 30-retry-single-user went red on exactly that).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	agentapi "github.com/EndoTheDev/omega/agent/api"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

func TestChatActionPipeMirrorKeepsTheRunVisible(t *testing.T) {
	prov := newMultiProvider("step")
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	sA := NewServer(a, nil, st, nil, nil, nil)
	aB := agent.NewAgent(prov, nil, 0)
	aB.SetLoopProvider(testLoop{})
	sB := NewServer(aB, nil, st, nil, nil, nil)

	hub := &agentapi.RunHub{}
	chat := agentapi.NewChatCapsule()
	chat.PinServer(sB.Server)
	shared := &sharedCanvas{}
	for _, s := range []*Server{sA, sB} {
		s.BindRunHub(hub)
		s.BindChatCapsule(chat)
		canvas, err := shared.build(&core.Context{PanelProviders: []core.PanelProvider{chat}}, s.CanvasEmit)
		if err != nil {
			t.Fatalf("canvas: %v", err)
		}
		s.BindCanvas(canvas)
	}
	tsA := httptest.NewServer(sA.Handler())
	t.Cleanup(tsA.Close)
	tsB := httptest.NewServer(sB.Handler())
	t.Cleanup(tsB.Close)

	createSession(t, tsA, "mb1")
	muteTitles(t, st, "mb1")
	startExchange(t, prov, sA, tsA, "mb1")

	call := fmt.Sprintf(`{"module":%q,"action":%q,"args":{"session":"mb1"}}`, agentapi.ChatModuleNamespace, agentapi.ChatActionRetry)
	code, resp := postJSON(t, tsA.URL+"/ui/actions", call)
	if code != http.StatusOK {
		t.Fatalf("retry action status %d: %v", code, resp)
	}
	waitFor(t, "retry run live on mirror A", func() bool { return sA.ActiveRun("mb1") })
	_, tx := transcriptOf(t, tsA, "mb1")
	if tx["running"] != true {
		t.Fatalf("transcript through mirror A: running = %v, want true", tx["running"])
	}
	prov.send(finalStep("second answer")...)
	prov.done()
	waitFor(t, "retry replay done", func() bool { return !sA.ActiveRun("mb1") })
	_, tx = transcriptOf(t, tsA, "mb1")
	if tx["running"] != false {
		t.Fatalf("transcript through mirror A post-run: running = %v, want false", tx["running"])
	}
}

// The multi-bind canvas pin: the panel state a mirror's GET /ui/canvas serves must. The
// channel start used to build one canvas per bind, and the capsule's push closure is
// last-one-wins - the mirror that lost the start race served its boot-time seed
// forever, so any repaint whose durable path is the snapshot refetch (the variant
// switch back) never flipped on that mirror.
func TestMultiBindServesOneCanvas(t *testing.T) {
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
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if len(ctx.Channels) != 2 {
		t.Fatalf("channels = %d, want one per bind", len(ctx.Channels))
	}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := core.ChannelDeps{Ctx: ctx, Store: st}
	for _, ch := range ctx.Channels {
		go func(c core.Channel) { c.Start(runCtx, deps) }(ch)
	}
	mirrors := []string{
		fmt.Sprintf("http://127.0.0.1:%d", port),
		fmt.Sprintf("http://127.0.0.2:%d", port),
	}
	for _, base := range mirrors {
		waitMirrorHealthy(t, base)
	}

	// One push through the capsule. Every mirror must serve it.
	p.chat.Push(core.Panel{
		ID:      agentapi.ChatPanelID,
		Title:   "Chat",
		Kind:    core.PanelKindChat,
		Payload: core.ChatPayload{Session: "pin-session", Running: false},
	})
	for _, base := range mirrors {
		if sess := canvasChatSession(t, base); sess != "pin-session" {
			t.Fatalf("mirror %s serves chat session %q, want the pushed pin-session", base, sess)
		}
	}
}

// waitMirrorHealthy polls a bind's /health until the lane is up.
func waitMirrorHealthy(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for time.Now().Before(deadline) {
		res, err := http.Get(base + "/health")
		if err == nil {
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if strings.Contains(string(body), `"ok"`) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("mirror %s never healthy", base)
}

// canvasChatSession reads the chat panel's payload session off a mirror's snapshot GET.
func canvasChatSession(t *testing.T, base string) string {
	t.Helper()
	res, err := http.Get(base + "/ui/canvas")
	if err != nil {
		t.Fatalf("snapshot %s: %v", base, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("snapshot %s: status %d", base, res.StatusCode)
	}
	var panels []core.Panel
	if err := json.NewDecoder(res.Body).Decode(&panels); err != nil {
		t.Fatalf("snapshot %s: decode: %v", base, err)
	}
	for _, pn := range panels {
		if pn.ID == agentapi.ChatPanelID {
			payload, _ := pn.Payload.(map[string]any)
			sess, _ := payload["session"].(string)
			return sess
		}
	}
	t.Fatalf("no chat panel in %s snapshot: %+v", base, panels)
	return ""
}
