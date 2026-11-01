// Terminal module tests (card t_55a3fc4b): the store laws (claim
// reattach, the cap, the orphan prune, the lifecycle stop), the route
// surface through the real mux (the wire shapes the webui's Terminal
// tab rides - claim, replay+live SSE stream, input, resize, kill),
// and the pty seam on this machine (a real shell on a real pty pair,
// skipped on boxes without /dev/ptmx).
package terminal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// fakeProc is the pipe-backed process: Read/Write are real pipes (the
// blocking semantics the session loops expect), Done/Kill/ExitCode
// are the test's hand on the child.
type fakeProc struct {
	read    *os.File
	write   *os.File
	mu      sync.Mutex
	resizes [][2]uint16
	done    chan struct{}
	closed  bool
	code    int
	kills   int
}

func newFakeProc() *fakeProc {
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	return &fakeProc{read: r, write: w, done: make(chan struct{})}
}

func (f *fakeProc) Read(b []byte) (int, error)  { return f.read.Read(b) }
func (f *fakeProc) Write(b []byte) (int, error) { return f.write.Write(b) }
func (f *fakeProc) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	f.read.Close()
	f.write.Close()
	return nil
}

func (f *fakeProc) Resize(rows, cols uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resizes = append(f.resizes, [2]uint16{rows, cols})
	return nil
}

func (f *fakeProc) Done() <-chan struct{} { return f.done }

// Kill closes the master and reports a signal death.
func (f *fakeProc) Kill() error {
	f.kills++
	select {
	case <-f.done:
	default:
		f.mu.Lock()
		f.code = -1
		f.mu.Unlock()
		close(f.done)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return nil
}

func (f *fakeProc) ExitCode() int { return f.code }

// exit completes the fake's life the way a real child departing on
// its own reports (code first, then the reap fence).
func (f *fakeProc) exit(code int) {
	f.mu.Lock()
	f.code = code
	f.mu.Unlock()
	close(f.done)
}

// spawnFakes builds a spawn seam handing out fresh fakes; the
// returned func reads their life through.
func spawnFakes() (spawnFunc, func() []*fakeProc) {
	mu := sync.Mutex{}
	held := []*fakeProc{}
	return func(string) (process, error) {
			f := newFakeProc()
			mu.Lock()
			held = append(held, f)
			mu.Unlock()
			return f, nil
		},
		func() []*fakeProc {
			mu.Lock()
			defer mu.Unlock()
			return append([]*fakeProc(nil), held...)
		}
}

// newTestStore mounts a store on the fake seam; tests inject their
// own clock when they need the prune math.
func newTestStore(t *testing.T, now func() time.Time) (*Store, func() []*fakeProc) {
	t.Helper()
	spawn, procs := spawnFakes()
	st := NewStore(spawn, t.TempDir(), now)
	t.Cleanup(func() { _ = st.Stop() })
	return st, procs
}

// mountTerminal mounts the plugin through the real registration path
// and returns the module mux (real pattern matching: PathValue set).
func mountTerminal(t *testing.T, st *Store) *http.ServeMux {
	t.Helper()
	plug := NewPluginWithStore(st)
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{plug}, ctx, nil, nil); err != nil {
		t.Fatalf("mount all: %v", err)
	}
	mux := http.NewServeMux()
	for _, rt := range ctx.ModuleRoutes {
		pattern, handler := rt.Pattern, rt.Handler
		mux.HandleFunc(pattern, handler)
	}
	return mux
}

// claimSession drives POST /terminal/sessions and unmarshals the id.
func claimSession(t *testing.T, mux *http.ServeMux) (id string, claimed bool) {
	t.Helper()
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/terminal/sessions", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", w.Code, w.Body.String())
	}
	var rep struct {
		ID      string `json:"id"`
		Claimed bool   `json:"claimed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("claim body: %v", err)
	}
	if rep.ID == "" {
		t.Fatal("claim: empty id")
	}
	return rep.ID, rep.Claimed
}

// sseFrames splits one SSE body into its "data:" payloads.
func sseFrames(body string) []string {
	out := []string{}
	for block := range strings.SplitSeq(body, "\n\n") {
		for line := range strings.SplitSeq(block, "\n") {
			if frame, ok := strings.CutPrefix(line, "data: "); ok {
				out = append(out, frame)
			}
		}
	}
	return out
}

// sseWithCancel streams with a deadline-backed context and records the
// SSE frames the stream had written by then.
func sseWithCancel(t *testing.T, mux *http.ServeMux, id string, d time.Duration) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, "GET", "/terminal/"+id+"/stream", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return sseFrames(w.Body.String())
}

// --- the store laws ------------------------------------------------------

func TestClaimReattachOneShellPerPane(t *testing.T) {
	st, procs := newTestStore(t, nil)
	s1, claimed, err := st.Claim()
	if err != nil || claimed {
		t.Fatalf("first claim: claimed=%v err=%v", claimed, err)
	}
	s2, claimed, err := st.Claim()
	if err != nil || !claimed || s2 != s1 {
		t.Fatalf("second claim: want reattach of %s, got reattach=%v err=%v", s1.id, claimed, err)
	}
	// an ATTACHED session is no longer claimable - the next pane gets
	// a fresh shell (one terminal per pane). The sub stays attached
	// for the rest of the test.
	sub, replay, _ := s1.subscribe()
	if len(replay) != 0 {
		t.Fatalf("empty session replay: %d bytes", len(replay))
	}
	defer s1.unsubscribe(sub)
	s3, claimed, err := st.Claim()
	if err != nil || claimed || s3 == s1 {
		t.Fatalf("third claim while s1 attached: claimed=%v err=%v", claimed, err)
	}
	if got := len(procs()); got != 2 {
		t.Fatalf("spawn count: %d, want 2", got)
	}
}

func TestCapLawReapsThenRefuses(t *testing.T) {
	st, procs := newTestStore(t, nil)
	// fill the cap with live+attached sessions: every claim rides a
	// fresh shell because the previous one is already attached
	first := ""
	for i := range maxSessions {
		s, _, err := st.Claim()
		if err != nil {
			t.Fatalf("claim %d: %v", i+1, err)
		}
		if i == 0 {
			first = s.id
		}
		s.subscribe()
	}
	_, _, err := st.Claim()
	if err == nil {
		t.Fatal("claim 5: want the cap refusal")
	}
	var capErr *capError
	if !errors.As(err, &capErr) {
		t.Fatalf("claim 5: %v is not the cap refusal", err)
	}
	// the reap order: an EXITED session dies first, attached or not
	all := procs()
	all[0].exit(3)
	waitExited(t, st.Get(first))
	s, _, err := st.Claim()
	if err != nil || s.id == first {
		t.Fatalf("post-cap claim: err=%v id=%s (wanted a new shell)", err, s.id)
	}
	// the reaped shell shows the kill; the live table holds maxSessions
	if after := procs(); after[0].kills == 0 {
		t.Fatal("cap law: the exited session's shell not reaped")
	}
	if got := len(st.List()); got != maxSessions {
		t.Fatalf("cap law: %d live rows, want %d", got, maxSessions)
	}
}

func TestOrphanPrune(t *testing.T) {
	clock := time.Now()
	frozen := func() time.Time { return clock }
	st, procs := newTestStore(t, frozen)
	s, _, err := st.Claim()
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	// live + unattached + untouched past the TTL = the orphan shape
	clock = clock.Add(2 * noOrphanTTL)
	st.prune()
	if st.Get(s.id) != nil {
		t.Fatal("prune: the idle orphan survived")
	}
	if got := procs(); len(got) != 1 || got[0].kills == 0 {
		t.Fatal("prune: the orphan's shell not killed")
	}
	// an ATTACHED session is no orphan regardless of its age
	s2, _, _ := st.Claim()
	sub, _, _ := s2.subscribe()
	clock = clock.Add(2 * noOrphanTTL)
	st.prune()
	if st.Get(s2.id) == nil {
		t.Fatal("prune: an attached session was reaped")
	}
	s2.unsubscribe(sub)
}

func TestLifecycleStopKillsEveryShell(t *testing.T) {
	st, procs := newTestStore(t, nil)
	for range 2 {
		if _, _, err := st.Claim(); err != nil {
			t.Fatalf("claim: %v", err)
		}
	}
	if err := st.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	for _, f := range procs() {
		if f.kills == 0 {
			t.Fatal("stop: a shell survived the lifecycle fence")
		}
	}
	if len(st.List()) != 0 {
		t.Fatalf("stop: the table still lists %d rows", len(st.List()))
	}
}

func TestStopIsIdempotent(t *testing.T) {
	st, _ := newTestStore(t, nil)
	if err := st.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := st.Stop(); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

// --- the route surface through the real mux ------------------------------

func TestRoutesClaimStreamInputKill(t *testing.T) {
	st, _ := newTestStore(t, nil)
	mux := mountTerminal(t, st)
	id, claimed := claimSession(t, mux)
	if claimed {
		t.Fatal("first claim reported a reattach")
	}
	sess := st.Get(id)
	if sess == nil {
		t.Fatal("the store lost the claimed session")
	}

	// input lands in the shell (the fake's write side is the proof)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/terminal/"+id+"/input", strings.NewReader("echo hi\n")))
	if w.Code != http.StatusOK {
		t.Fatalf("input: %d %s", w.Code, w.Body.String())
	}
	if n := w.Body.String(); !strings.Contains(n, `"written":8`) {
		t.Fatalf("input body: %s", n)
	}

	// record output nobody watches, then exit: the stream replays the
	// tail and carries the exit frame with the real code
	sess.record([]byte("hello\n"))
	st.Get(id).proc.(*fakeProc).exit(7)
	waitExited(t, sess)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/terminal/"+id+"/stream", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("stream: %d", w.Code)
	}
	frames := sseFrames(w.Body.String())
	if len(frames) < 2 {
		t.Fatalf("stream: %d frames only: %q", len(frames), frames)
	}
	replayed := false
	for _, f := range frames {
		if strings.Contains(sseDecoded(f), "hello") {
			replayed = true
		}
	}
	if !replayed {
		t.Fatalf("replay frame missing the output: %q", frames)
	}
	if last := frames[len(frames)-1]; !strings.Contains(last, `"t":"x"`) || !strings.Contains(last, `"c":7`) {
		t.Fatalf("exit frame: %q", last)
	}

	// input against the exited shell = 410
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/terminal/"+id+"/input", strings.NewReader("x")))
	if w.Code != http.StatusGone {
		t.Fatalf("dead-shell input: %d, want 410", w.Code)
	}

	// kill twice: both 204 (idempotent), then the stream is 404
	for range 2 {
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("DELETE", "/terminal/"+id, nil))
		if w.Code != http.StatusNoContent {
			t.Fatalf("kill: %d, want 204", w.Code)
		}
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/terminal/"+id+"/stream", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("stream unknown: %d, want 404", w.Code)
	}

	// the killed shell does not row in the listing
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/terminal/sessions", nil))
	if strings.Contains(w.Body.String(), id) {
		t.Fatal("list: the killed session still rows")
	}
}

func TestStreamReplaysTailAfterDetach(t *testing.T) {
	st, _ := newTestStore(t, nil)
	mux := mountTerminal(t, st)
	id, _ := claimSession(t, mux)
	st.Get(id).record([]byte("written while detached"))
	// a claim with the session attached by nobody: the SAME shell comes
	// back (the detaching is the lazy unmount's SSE drop)
	id2, claimed := claimSession(t, mux)
	if !claimed || id2 != id {
		t.Fatalf("reattach claim: id=%s claimed=%v (want %s true)", id2, claimed, id)
	}
	frames := sseWithCancel(t, mux, id, 300*time.Millisecond)
	if len(frames) == 0 || !strings.Contains(sseDecoded(frames[0]), "written while detached") {
		t.Fatalf("reattach replay: %v", frames)
	}
}

func TestResizeRouteShapes(t *testing.T) {
	st, _ := newTestStore(t, nil)
	mux := mountTerminal(t, st)
	id, _ := claimSession(t, mux)
	// the real handler path carries the resize to the process seam
	proc := st.Get(id).proc.(*fakeProc)
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/terminal/"+id+"/resize", strings.NewReader(body)))
		return w
	}
	if w := post(`{"rows":40,"cols":120}`); w.Code != http.StatusOK {
		t.Fatalf("resize: %d %s", w.Code, w.Body.String())
	}
	if last := proc.resizes[len(proc.resizes)-1]; last != [2]uint16{40, 120} {
		t.Fatalf("resize: %v landed", last)
	}
	for _, body := range []string{
		`{"rows":0,"cols":80}`,
		`{"rows":40}`,
		`{"rows":40,"cols":-1}`,
		`{"rows":40,"cols":20001}`,
		`not json`,
	} {
		if w := post(body); w.Code != http.StatusBadRequest {
			t.Fatalf("resize %q: %d, want 400", body, w.Code)
		}
	}
	// unknown id = 404; exited shell = 410
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/terminal/unknown-id/resize", strings.NewReader(`{"rows":4,"cols":4}`)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("resize unknown: %d, want 404", w.Code)
	}
	proc.exit(0)
	waitExited(t, st.Get(id))
	if w := post(`{"rows":4,"cols":4}`); w.Code != http.StatusGone {
		t.Fatalf("resize exited: %d, want 410", w.Code)
	}
}

func TestClaimCapAtTheWire(t *testing.T) {
	// a fully-attached cap: the refusal is 409 at the wire
	st, _ := newTestStore(t, nil)
	attacher := make([]*fakeProc, 0, maxSessions)
	for range maxSessions {
		s, _, err := st.Claim()
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if f, ok := s.proc.(*fakeProc); ok {
			attacher = append(attacher, f)
		}
		s.subscribe()
	}
	mux := mountTerminal(t, st)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/terminal/sessions", nil))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "cap") {
		t.Fatalf("cap at the wire: %d %s", w.Code, w.Body.String())
	}
	// a broken spawn seam: 503 with the seam's own words
	broken := NewStore(func(string) (process, error) {
		return nil, errors.New("no pty on this box")
	}, ".", nil)
	mux2 := mountTerminal(t, broken)
	w2 := httptest.NewRecorder()
	mux2.ServeHTTP(w2, httptest.NewRequest("POST", "/terminal/sessions", nil))
	if w2.Code != http.StatusServiceUnavailable || !strings.Contains(w2.Body.String(), "no pty") {
		t.Fatalf("broken spawn: %d %s", w2.Code, w2.Body.String())
	}
}

func TestInputCeiling(t *testing.T) {
	st, _ := newTestStore(t, nil)
	mux := mountTerminal(t, st)
	id, _ := claimSession(t, mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/terminal/"+id+"/input", strings.NewReader(strings.Repeat("x", maxInput+1))))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("input ceiling: %d, want 413", w.Code)
	}
}

// sseDecoded returns the base64-decoded payload of one wire frame's
// "o" field (the shell never rides the wire raw).
func sseDecoded(frame string) string {
	var f wireFrame
	if err := json.Unmarshal([]byte(frame), &f); err != nil {
		return ""
	}
	if f.T != "o" || f.D == "" {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(f.D)
	if err != nil {
		return ""
	}
	return string(b)
}

// waitExited spins until a session's exit mark landed (the watchLoop
// is its own goroutine; handler-side assertions need the settled
// state, not the race).
func waitExited(t *testing.T, s *Session) {
	t.Helper()
	for range 200 {
		s.mu.Lock()
		doneExited := s.exited
		s.mu.Unlock()
		if doneExited {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the session never reported its exit")
}

// drainUntil reads the output stream until the needle appears or the
// deadline passes (the prompt/echo dance needs a window, not a poll).
func drainUntil(t *testing.T, proc process, needle string, d time.Duration) error {
	t.Helper()
	deadline := time.Now().Add(d)
	acc := ""
	for {
		if time.Now().After(deadline) {
			return errors.New(needle + " never arrived; saw: " + acc)
		}
		buf := make([]byte, 4096)
		n, err := proc.Read(buf)
		if n > 0 {
			acc += string(buf[:n])
			if strings.Contains(acc, needle) {
				return nil
			}
		}
		if err != nil {
			return errors.New("stream ended before " + needle + ": " + acc)
		}
	}
}

func TestRealPTYShellEcho(t *testing.T) {
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Skipf("no /dev/ptmx on this box: %v", err)
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("no /bin/sh: %v", err)
	}
	proc, err := spawnProcess(t.TempDir())
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer proc.Kill()
	if err := proc.Resize(30, 100); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := drainUntil(t, proc, "$ ", 5*time.Second); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if _, err := proc.Write([]byte("echo omega-term-proof\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := drainUntil(t, proc, "omega-term-proof", 5*time.Second); err != nil {
		t.Fatalf("echo: %v", err)
	}
}
