package http_channel

// The multi-bind mirror pin (t_c6f13a82): one process can serve several
// binds (loopback + a LAN address), one Server per bind but ONE run hub
// and ONE chat capsule. The capsule's server pin follows the last
// STARTED bind, so an action-pipe launch on any mirror registers the
// run there - with per-bind hubs that run was invisible to the
// transcript and events reads on every other mirror (journey
// 30-retry-single-user went red on exactly that).

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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
	sA := NewServer(a, nil, st, nil)
	// The second mirror gets its own agent (each channel start builds
	// one) over the same provider + store: the mirror executes runs.
	aB := core.NewAgent(prov, nil, 0)
	aB.SetLoopProvider(testLoop{})
	sB := NewServer(aB, nil, st, nil)

	// The channel-start wiring (HTTPChannel.Start): one process-wide
	// hub, one shared capsule; the LAST-started bind holds the capsule's
	// server pin.
	hub := &runHub{}
	chat := newChatCapsule()
	chat.srv = sB
	for _, s := range []*Server{sA, sB} {
		s.runs = hub
		s.chat = chat
		if err := s.newCanvas(&core.Context{PanelProviders: []core.PanelProvider{chat}}); err != nil {
			t.Fatalf("newCanvas: %v", err)
		}
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