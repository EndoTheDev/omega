package api

// The wake-path tests (async agent loop phase 1, card t_f3f953d0):
// the runaway guard's park-and-reset, the user-send reset, the
// simulated restart (boot recovery over a REAL store table), and the
// end-to-end slice - dispatch, turn end, re-entry wake - through the
// real loop, the server's wake path and a scripted provider.

import (
	"context"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/EndoTheDev/omega/llm"
)

// TestWakeGuardParksAt25AndResets: unit - the cap counts launches, a
// park refuses every further wake, a user reset re-arms.
func TestWakeGuardParksAt25AndResets(t *testing.T) {
	h := newWakeHub()
	for i := 0; i < maxConsecutiveWakes; i++ {
		if !h.allow("s1") {
			t.Fatalf("wake %d refused, want %d admitted", i+1, maxConsecutiveWakes)
		}
	}
	if h.allow("s1") {
		t.Errorf("wake 26 admitted - the 25 cap leaked")
	}
	// The 26th was refused: park latches the refusal permanently.
	if !h.park("s1") {
		t.Fatal("park refused to latch")
	}
	if !h.parkedNow("s1") {
		t.Fatal("parked flag lost")
	}
	// A user message reset re-arms fully: parked flag gone, counter at 0.
	h.reset("s1")
	if h.parkedNow("s1") {
		t.Error("reset left the parked flag")
	}
	if !h.allow("s1") {
		t.Error("post-reset admit refused")
	}
}

// TestBootRecoveryInjectsTimeoutResults: the simulated restart. The
// durable table (a real store) carries one pending call across
// NewServer; the sweep settles it and injects the interrupted result
// as a wake run.
func TestBootRecoveryInjectsTimeoutResults(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.CreateSession(context.Background(), "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.Register(agent.PendingCall{
		SessionID: "s1", CallID: "cLost", ToolName: "probe",
		Arguments: `{}`, DispatchedAt: core.NowISO(),
	}); err != nil {
		t.Fatalf("seed pending call: %v", err)
	}

	// The wake run's first turn: the recovered "interrupted" result is
	// in its context; the script closes the stream and ends the run.
	scripts := [][]core.ProviderStreamEvent{finalStep("recovering")}
	prov := newWakeScripted("step", scripts)
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	s := NewServer(a, nil, st, nil, nil, nil)

	// The sweep runs synchronously inside NewServer: by the time it
	// returns, every pending row is settled and the launch has been
	// admitted or rolled back. Both facts are read as RECORDS, never
	// polled - the wake run's live slot measures 2-4ms, shorter than any
	// poll interval, so a poll for s.runs.active misses the run whole
	// whenever the test goroutine is descheduled here and the wait
	// deadline, not the launch, would decide the verdict (t_32c3fce2).
	if left := s.yieldLedger.All(); len(left) != 0 {
		t.Fatalf("boot sweep left entries: %+v", left)
	}
	if n := s.wakeLaunchCount("s1"); n != 1 {
		t.Fatalf("boot recovery launched %d wake runs, want 1 - the wake launch never happened", n)
	}
	// The injected result persisted as a first-class yield_result row
	// once the run lands it (the loop's input persist). A row is durable,
	// so this poll is a latch, not a sampled state.
	deadline := time.Now().Add(testWaitTimeout)
	seen := false
	for time.Now().Before(deadline) && !seen {
		msgs, err := st.GetMessages(context.Background(), "s1")
		if err == nil {
			for _, m := range msgs {
				if y, ok := m.(core.YieldResult); ok && y.Timeout && y.ToolCallID == "cLost" {
					seen = true
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !seen {
		t.Error("the injected timeout result never persisted")
	}
	waitFor(t, "wake run finished", func() bool { return !s.runs.active("s1") })
}

// TestBootRecoveryDoesNotReattachPendingShell is the shell-unification
// probe pin (card t_5a49790b): a pending shell.run call whose row
// survives a restart is NOT re-attached. The row is durable (a real
// file-backed pending_calls table, re-read by a fresh store handle =
// the restart), and boot recovery SETTLES it and injects the
// interrupted result - the wake path never rejoins the call.
//
// That is the accepted degradation the card names: the detached child
// keeps running and its log stays readable through the shell registry
// (shell.list / shell.read), and the model learns the call was cut by
// the restart from the injected result, then re-reads the shell by id.
func TestBootRecoveryDoesNotReattachPendingShell(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "omega.db")
	st, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.CreateSession(context.Background(), "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.Register(agent.PendingCall{
		SessionID: "s1", CallID: "cShell", ToolName: "shell.run",
		Arguments: `{"command":"sleep 300"}`, DispatchedAt: core.NowISO(),
	}); err != nil {
		t.Fatalf("seed pending call: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store (the omega exit): %v", err)
	}

	// The restart: a fresh handle over the same file. The pending row
	// is still there - that is the half the card asked us to probe.
	st2, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { st2.Close() })
	if rows := st2.All(); len(rows) != 1 || rows[0].ToolName != "shell.run" {
		t.Fatalf("pending_calls across the restart = %+v, want the one shell.run row", rows)
	}

	prov := newWakeScripted("step", [][]core.ProviderStreamEvent{finalStep("recovering")})
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	s := NewServer(a, nil, st2, nil, nil, nil)

	// The sweep settles the row: no re-attach, the call is not re-run.
	waitFor(t, "boot sweep settles the pending shell call", func() bool {
		return len(s.yieldLedger.All()) == 0
	})
	// ... and what re-enters is the interrupted TIMEOUT result, not the
	// shell's live output - the wake path cannot reach the call again.
	waitFor(t, "the interrupted result persists", func() bool {
		msgs, err := st2.GetMessages(context.Background(), "s1")
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if y, ok := m.(core.YieldResult); ok && y.ToolCallID == "cShell" {
				if !y.Timeout || !strings.Contains(y.Content, "interrupted") {
					t.Errorf("recovered result = %+v, want the interrupted timeout shape (no re-attach)", y)
				}
				return true
			}
		}
		return false
	})
	waitFor(t, "wake run finished", func() bool { return !s.runs.active("s1") })
}

// TestWakeSliceEndToEnd: the scripted slice (dispatch, turn end,
// re-entry wake) through the REAL loop over the /chat wire. One user
// send's completion calls a yield tool; the run ends yielded; the
// settled result wakes the home - a second run answers it - and the
// flush-before-result order holds in the store.
func TestWakeSliceEndToEnd(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	prov := newWakeScripted("step", [][]core.ProviderStreamEvent{
		// Run 1: one yield-class call - the turn ends yielded.
		{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c1", Name: "probe", Arguments: map[string]any{"msg": "hi"},
			}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// Run 2 (the wake): answer and close.
		finalStep("done"),
	})
	tools := map[string]core.Tool{"probe": {
		Description: "probe (yield-class)",
		Yield:       true,
		Run: func(_ context.Context, args map[string]any) (string, error) {
			msg, _ := args["msg"].(string)
			return "async:" + msg, nil
		},
	}}
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	s := NewServer(a, tools, st, nil, nil, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	// Run 1: a scripted yield call; the agent_end must carry yielded.
	resp, _, sc := postChatStream(t, ts, "s1")
	lines := readFrames(t, sc, "agent_end", 1)
	// The frame's data line trails its event line.
	sc.Scan()
	dataLine := sc.Text()
	resp.Body.Close()
	endEvent, endData := "", ""
	for i := 0; i < len(lines)-1; i++ {
		if lines[i] == "event: agent_end" {
			endEvent, endData = lines[i], lines[i+1]
		}
	}
	if endData == "" {
		endData = dataLine
	}
	if !contains(endData, `"finish_reason":"yielded"`) {
		t.Errorf("agent_end (%s / %s) carried no yielded finish_reason", endEvent, endData)
	}

	// The result re-enters: the wake run (a second run on the same
	// home, through the same launchRun slot) lands the synthetic row.
	deadline := time.Now().Add(testWaitTimeout)
	seen := false
	for time.Now().Before(deadline) && !seen {
		msgs, err := st.GetMessages(context.Background(), "s1")
		if err == nil {
			seen = hasYieldResult(msgs, "c1", core.YieldResult{Content: "async:hi"})
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !seen {
		dumpSessionRows(t, st, "s1")
		t.Fatal("the wake run never landed the probe result")
	}
	// Flush-before-result held: the assistant that carries the call is
	// durable BEFORE the synthetic result row.
	msgs, err := st.GetMessages(context.Background(), "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	callIdx, resultIdx := -1, -1
	for i, m := range msgs {
		if as, ok := m.(core.Assistant); ok {
			for _, c := range as.ToolCalls {
				if c.ID == "c1" {
					callIdx = i
				}
			}
		}
		if y, ok := m.(core.YieldResult); ok && y.ToolCallID == "c1" {
			resultIdx = i
		}
	}
	if callIdx == -1 || resultIdx == -1 || callIdx > resultIdx {
		t.Errorf("order broken: assistant(call) index %d, yield_result index %d", callIdx, resultIdx)
	}
	// The slice ended in both runs - nothing parked, no live goroutine.
	waitFor(t, "wake run finished", func() bool { return !s.runs.active("s1") })
	if s.wakes.parkedNow("s1") {
		t.Error("a 1-wake slice parked the home")
	}
}

// TestWakeEmptyTurnLeavesNote: a wake run whose only completion is
// all-empty must never end with zero persisted output (card t_954a67a6).
// The store sink's all-empty-assistant skip is right for a blank stream
// before an error end on a normal turn; on a wake run it ate the
// re-entry turn whole - no row, no log line, and the transcript read as
// a conversation that simply stopped. The run leaves a visible
// bookkeeping note instead: the diagnostic breadcrumb and the honest UI
// state in one row.
func TestWakeEmptyTurnLeavesNote(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	// The wake run's only script: a completion with nothing in it -
	// no content delta, no thinking, no calls. Exactly the shape the
	// live lane recorded (a ~1s empty answer).
	prov := newWakeScripted("step", [][]core.ProviderStreamEvent{
		{core.StreamEnd{Type: "stream_end", FinishReason: "stop"}},
	})
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	s := NewServer(a, nil, st, nil, nil, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	// The settled result re-enters through the real wake path.
	s.wakeHome("s1", core.YieldResult{
		ToolCallID: "call_5b16qor0",
		ToolName:   "files.glob",
		Content:    "a.txt\nb.txt",
		Timestamp:  core.NowISO(),
	})
	waitFor(t, "wake run launched and finished", func() bool {
		return s.wakeLaunchCount("s1") == 1 && !s.runs.active("s1")
	})

	msgs, err := st.GetMessages(context.Background(), "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	note := ""
	for _, m := range msgs {
		if u, ok := m.(core.User); ok && contains(u.Content, "ended with no output") {
			note = u.Content
		}
	}
	if note == "" {
		dumpSessionRows(t, st, "s1")
		t.Fatal("the empty wake turn left no note - the run vanished with zero trace")
	}
	if !contains(note, "files.glob") {
		t.Errorf("note %q does not name the tool that settled", note)
	}
	// The note is the run's last word: the settled result still carries
	// its own row before it.
	if !hasYieldResult(msgs, "call_5b16qor0", core.YieldResult{}) {
		dumpSessionRows(t, st, "s1")
		t.Error("the settled result row went missing")
	}
}

// TestWakeStreamErrorLeavesNote: the adjacent break in the same
// contract (card t_9821e39b). A wake run killed mid-stream by a
// provider stream error (StreamEnd with FinishReason="error") never
// reaches the loop's persistence epilogue, so the all-empty note
// wrapper never sees an Assistant at all: the live UI painted the
// agent_end error frame, the lane log wrote the "stream error" line,
// and the SESSION kept no row - after a reload the re-entry turn
// vanished with zero trace. The run must leave a visible bookkeeping
// note naming the tool that settled instead.
func TestWakeStreamErrorLeavesNote(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	// The wake run's only script: the provider stream dies before a
	// single completion - no chunk, no calls, just the error end.
	prov := newWakeScripted("step", [][]core.ProviderStreamEvent{
		{core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "provider 500: upstream exploded"}},
	})
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	s := NewServer(a, nil, st, nil, nil, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	// The settled result re-enters through the real wake path.
	s.wakeHome("s1", core.YieldResult{
		ToolCallID: "call_9err0r",
		ToolName:   "files.glob",
		Content:    "a.txt",
		Timestamp:  core.NowISO(),
	})
	waitFor(t, "wake run launched and finished", func() bool {
		return s.wakeLaunchCount("s1") == 1 && !s.runs.active("s1")
	})

	msgs, err := st.GetMessages(context.Background(), "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	note := ""
	for _, m := range msgs {
		if u, ok := m.(core.User); ok && contains(u.Content, "provider error") {
			note = u.Content
		}
		if _, ok := m.(core.Assistant); ok {
			t.Errorf("the errored wake turn persisted an assistant row the live stream never finished: %s", sessionRowDump(msgs))
		}
	}
	if note == "" {
		dumpSessionRows(t, st, "s1")
		t.Fatal("the stream-errored wake turn left no note - the run vanished with zero trace")
	}
	if !contains(note, "files.glob") {
		t.Errorf("note %q does not name the tool that settled", note)
	}
	// The note is the run's last word: the settled result still carries
	// its own row before it.
	if !hasYieldResult(msgs, "call_9err0r", core.YieldResult{}) {
		dumpSessionRows(t, st, "s1")
		t.Error("the settled result row went missing")
	}
}

// TestNonWakeStreamErrorLeavesNoRow pins the untouched half (scope
// law, card t_9821e39b): a NON-wake run killed by a stream error keeps
// today's behavior - the live error frame is the whole trace, nothing
// extra is written behind the sender's back.
func TestNonWakeStreamErrorLeavesNoRow(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	prov := newWakeScripted("step", [][]core.ProviderStreamEvent{
		{core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "provider 500: upstream exploded"}},
	})
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	s := NewServer(a, nil, st, nil, nil, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	resp, cancel, sc := postChatStream(t, ts, "s1")
	defer cancel()
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	resp.Body.Close()
	waitFor(t, "run finished", func() bool { return !s.runs.active("s1") })

	// The error really reached the live wire - otherwise this pin
	// would pass on a run that never errored at all.
	if !contains(strings.Join(lines, "\n"), `"finish_reason":"error"`) {
		t.Fatalf("no error-end frame on the wire; frames: %v", lines)
	}
	msgs, err := st.GetMessages(context.Background(), "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	for _, m := range msgs {
		if u, ok := m.(core.User); ok && contains(u.Content, "provider error") {
			t.Fatalf("a non-wake stream error wrote a bookkeeping note: %q", u.Content)
		}
		if _, ok := m.(core.Assistant); ok {
			t.Errorf("the errored non-wake run persisted an assistant row: %s", sessionRowDump(msgs))
		}
	}
}

// TestWakeGuardUserSendResets: parkHome parks with a persisted visible
// note; a plain user send through the /chat pipe resets the guard
// (launchRun's non-wake reset) and runs to its own end. The session
// carries no pending calls - a park on a dry home must not error.
func TestWakeGuardUserSendResets(t *testing.T) {
	// One batch ready: the reset run's script closes its own stream.
	prov := newWakeScripted("step", [][]core.ProviderStreamEvent{finalStep("round trip")})
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := NewServer(a, nil, st, nil, nil, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	// Park through the real path: the note lands on the transcript and
	// the parked state latches.
	s.parkHome("s1")
	if !s.wakes.parkedNow("s1") {
		t.Fatal("parkHome left no parked flag")
	}
	msgs, err := st.GetMessages(context.Background(), "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	note := false
	for _, m := range msgs {
		if u, ok := m.(core.User); ok && contains(u.Content, "pending calls parked") {
			note = true
		}
	}
	if !note {
		t.Errorf("park note never persisted: %s", sessionRowDump(msgs))
	}

	// A user send through the wire: the run's launch resets the guard.
	resp, cancel, sc := postChatStream(t, ts, "s1")
	readFrames(t, sc, "agent_end", 1)
	detachClient(cancel, resp)
	waitFor(t, "run finished after user send", func() bool { return !s.runs.active("s1") })
	waitFor(t, "guard reset after user send", func() bool { return !s.wakes.parkedNow("s1") })
	if !s.wakes.allow("s1") {
		t.Error("post-reset admit refused")
	}
}

// wakeScripted streams ONE prewritten script per Stream call (runs sit
// serially on the one-run-per-session slot) and closes the stream at
// script end - the loop's stream range unwinds exactly like a real
// provider's, so each run finishes on its own script. Unlike
// stepProvider, several turns across several runs never race one
// shared receive channel.
type wakeScripted struct {
	modelName string
	mu        sync.Mutex
	idx       int
	scripts   [][]core.ProviderStreamEvent
}

func newWakeScripted(modelName string, scripts [][]core.ProviderStreamEvent) *wakeScripted {
	return &wakeScripted{modelName: modelName, scripts: scripts}
}

func (p *wakeScripted) ModelName() string { return p.modelName }

func (p *wakeScripted) SetThinkingLevel(level string) {}

// Copies of a scripted provider share the script deck and the index
// (one serial stream per slot makes runs strictly sequential), so a
// new wakeScripted is built instead of a lock-carrying struct copy.
func (p *wakeScripted) nextCopy() *wakeScripted {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &wakeScripted{modelName: p.modelName, scripts: p.scripts, idx: p.idx}
}

func (p *wakeScripted) WithThinkingLevel(level string) agent.Provider {
	return p.nextCopy()
}

func (p *wakeScripted) WithModel(model string) agent.Provider {
	c := p.nextCopy()
	c.modelName = model
	return c
}

func (p *wakeScripted) ListModels() ([]string, error) {
	return []string{p.modelName}, nil
}

func (p *wakeScripted) ModelInfo() (llm.ModelInfo, error) {
	return llm.ModelInfo{}, nil
}

func (p *wakeScripted) Stream(_ context.Context, _ []core.Message, _ []llm.ToolSchema) <-chan core.ProviderStreamEvent {
	p.mu.Lock()
	i := p.idx
	if i < len(p.scripts) {
		p.idx++
	}
	p.mu.Unlock()
	events := make(chan core.ProviderStreamEvent)
	if i >= len(p.scripts) {
		close(events)
		return events
	}
	script := p.scripts[i]
	go func() {
		defer close(events)
		for _, e := range script {
			events <- e
		}
	}()
	return events
}

// hasYieldResult reports whether one row is the named tool call's
// settled result; the `want` value carries only the fields to match
// (a zero field matches anything).
func hasYieldResult(msgs []core.Message, callID string, want core.YieldResult) bool {
	for _, m := range msgs {
		y, ok := m.(core.YieldResult)
		if !ok || y.ToolCallID != callID {
			continue
		}
		if want.Timeout && !y.Timeout {
			continue
		}
		if want.Content != "" && y.Content != want.Content {
			continue
		}
		return true
	}
	return false
}

// sessionRowDump renders persisted rows for a failure diagnosis.
func sessionRowDump(msgs []core.Message) string {
	out := ""
	for i, m := range msgs {
		out += fmt.Sprintf("\n  row %d: %T %q", i, m, contentOf(m))
	}
	return out
}

// contentOf reads the text of the message kinds the wake tests meet.
func contentOf(m core.Message) string {
	switch v := m.(type) {
	case core.User:
		return v.Content
	case core.Assistant:
		return v.Content
	case core.YieldResult:
		return v.Content
	case core.ToolResult:
		return v.Content
	}
	return ""
}

// dumpSessionRows prints the persisted rows of one session - the
// failure diagnosis for a slice that never completes.
func dumpSessionRows(t *testing.T, st *store.Store, id string) {
	t.Helper()
	msgs, err := st.GetMessages(context.Background(), id)
	if err != nil {
		t.Logf("dump failed: %v", err)
		return
	}
	t.Logf("session %s rows:%s", id, sessionRowDump(msgs))
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// waitFor is defined in server_runs_test.go; the tests in this file
// reuse it.
