package http_channel

// The multi-bind mirror pin (t_c6f13a82): one process can serve several
// binds (loopback + a LAN address), one Server per bind but ONE run hub
// and ONE chat capsule. The capsule's server pin follows the last
// STARTED bind, so an action-pipe launch on any mirror registers the
// run there - with per-bind hubs that run was invisible to the
// transcript and events reads on every other mirror (journey
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
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/store"
)

func TestChatActionPipeMirrorKeepsTheRunVisible(t *testing.T) {
	prov := newMultiProvider("step")
	a := core.NewAgent(prov, nil, 0)
	a.SetLoopProvider(testLoop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	sA := NewServer(a, nil, st, nil, nil)
	// The second mirror gets its own agent (each channel start builds
	// one) over the same provider + store: the mirror executes runs.
	aB := core.NewAgent(prov, nil, 0)
	aB.SetLoopProvider(testLoop{})
	sB := NewServer(aB, nil, st, nil, nil)

	// The channel-start wiring (HTTPChannel.Start): one process-wide
	// hub, one shared capsule, ONE canvas behind every mirror
	// (t_1c9793a8); the LAST-started bind holds the capsule's server
	// pin.
	hub := &runHub{}
	chat := newChatCapsule()
	chat.srv.Store(sB)
	shared := &sharedCanvas{}
	for _, s := range []*Server{sA, sB} {
		s.runs = hub
		s.chat = chat
		canvas, err := shared.build(&core.Context{PanelProviders: []core.PanelProvider{chat}}, s.canvasEmit)
		if err != nil {
			t.Fatalf("canvas: %v", err)
		}
		s.canvas = canvas
	}
	tsA := httptest.NewServer(sA.Handler())
	t.Cleanup(tsA.Close)
	tsB := httptest.NewServer(sB.Handler())
	t.Cleanup(tsB.Close)

	// History lands through mirror A; the run registers in the shared
	// hub either way.
	createSession(t, tsA, "mb1")
	muteTitles(t, st, "mb1")
	startExchange(t, prov, sA, tsA, "mb1")

	// The retry rides the action pipe from mirror A while the capsule
	// pins mirror B: prepareRun + launchRun execute on B (chat.srv).
	// The pin under test - A's hub sees the run (per-bind wiring answered
	// "session not active" here, journey 30's exact failure).
	call := fmt.Sprintf(`{"module":%q,"action":%q,"args":{"session":"mb1"}}`, chatModuleNamespace, chatActionRetry)
	code, resp := postJSON(t, tsA.URL+"/ui/actions", call)
	if code != http.StatusOK {
		t.Fatalf("retry action status %d: %v", code, resp)
	}
	waitFor(t, "retry run live on mirror A", func() bool { return sA.runs.active("mb1") })
	_, tx := transcriptOf(t, tsA, "mb1")
	if tx["running"] != true {
		t.Fatalf("transcript through mirror A: running = %v, want true", tx["running"])
	}
	// finish the replay through mirror B's agent (the capsule pin's
	// provider is shared, so either scripter drives it)
	prov.send(finalStep("second answer")...)
	prov.done()
	waitFor(t, "retry replay done", func() bool { return !sA.runs.active("mb1") })
	_, tx = transcriptOf(t, tsA, "mb1")
	if tx["running"] != false {
		t.Fatalf("transcript through mirror A post-run: running = %v, want false", tx["running"])
	}
}

// The multi-bind canvas pin (t_1c9793a8): the panel state a mirror's
// GET /ui/canvas serves must track the capsule's pushes on EVERY
// bind. The channel start used to build one canvas per bind, and the
// capsule's push closure is last-one-wins - the mirror that lost the
// start race served its boot-time seed forever, so any repaint whose
// durable path is the snapshot refetch (journey 30's variant switch
// back) never flipped on that mirror. The REAL channel start is the
// path under test: both binds start in goroutines like cmdServe does,
// against live listeners on two loopback addresses.
func TestMultiBindServesOneCanvas(t *testing.T) {
	prov := newMultiProvider("step")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	// A private port for this test's two binds.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	// The plugin's Mount: one hub, one capsule, one canvas builder
	// carried by every bind's channel.
	p := NewPlugin()
	ctx := &core.Context{
		Provider: prov,
		Configs: map[string]any{
			"http_channel": Config{Port: port, Bind: []string{"127.0.0.1", "127.0.0.2"}},
		},
	}
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

	// One push through the capsule - the same face beginRun, endRun
	// and the variant-select re-push ride. Every mirror must serve it.
	p.chat.push(core.Panel{
		ID:      chatPanelID,
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

// waitMirrorHealthy polls a bind's /health until the lane is up - the
// channel's listener is live once Serve accepts.
func waitMirrorHealthy(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
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

// canvasChatSession reads the chat panel's payload session off a
// mirror's snapshot GET.
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
		if pn.ID == chatPanelID {
			payload, _ := pn.Payload.(map[string]any)
			sess, _ := payload["session"].(string)
			return sess
		}
	}
	t.Fatalf("no chat panel in %s snapshot: %+v", base, panels)
	return ""
}
