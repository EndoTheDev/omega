package cron

// run.go's roundtrip: Run now starts one fire the schedule did not order, Stop run cancels
// the live one, and both routes answer the fresh job set (running carries the live mark).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// waitUntil is the package's bounded wait: it catches a HANG, never correctness (the
// gate law's test-wait rule).
func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached within " + testWaitTimeout.String())
}

func blockingRunner(t *testing.T) (*Runner, *atomic.Int32, chan struct{}, chan string) {
	t.Helper()
	r := newTestRunner(t, t.TempDir(), Job{Name: "pulse", Schedule: "0 8 * * *", Prompt: "p"})
	var calls atomic.Int32
	started := make(chan struct{}, 4)
	canceled := make(chan string, 4)
	r.runTurn = func(ctx context.Context, sessionID string, job Job, prompt string) (string, error) {
		calls.Add(1)
		started <- struct{}{}
		<-ctx.Done()
		canceled <- job.Name
		return "", ctx.Err()
	}
	return r, &calls, started, canceled
}

func TestRunNowStartsOneFireAndStopCancelsIt(t *testing.T) {
	r, calls, started, canceled := blockingRunner(t)

	if err := r.RunNow("pulse"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	select {
	case <-started:
	case <-time.After(testWaitTimeout):
		t.Fatal("RunNow never started a fire")
	}
	// the live mark rides the job row the console renders
	waitUntil(t, func() bool {
		rows := r.Jobs()
		return len(rows) == 1 && rows[0].Running
	})

	// a second run-now while the fire is in flight is a loud refusal, never a queue
	if err := r.RunNow("pulse"); !errors.Is(err, ErrJobRunning) {
		t.Fatalf("second RunNow = %v, want ErrJobRunning", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("runTurn calls = %d, want 1", calls.Load())
	}

	if err := r.StopRun("pulse"); err != nil {
		t.Fatalf("StopRun: %v", err)
	}
	select {
	case name := <-canceled:
		if name != "pulse" {
			t.Fatalf("canceled job = %q, want pulse", name)
		}
	case <-time.After(testWaitTimeout):
		t.Fatal("StopRun never canceled the live turn")
	}
	waitUntil(t, func() bool {
		rows := r.Jobs()
		return len(rows) == 1 && !rows[0].Running
	})

	// the stopped fire still landed its last-status record (the row's voice)
	if row := r.Jobs()[0]; row.Status == "" {
		t.Fatalf("stopped fire left no status record: %+v", row)
	}
	if err := r.StopRun("pulse"); !errors.Is(err, ErrJobNotRunning) {
		t.Fatalf("StopRun on an idle job = %v, want ErrJobNotRunning", err)
	}
}

func TestRunNowBypassesTheEnabledFlag(t *testing.T) {
	off := false
	r := newTestRunner(t, t.TempDir(), Job{Name: "paused", Schedule: "0 8 * * *", Prompt: "p", Enabled: &off})
	var ran atomic.Bool
	r.runTurn = func(context.Context, string, Job, string) (string, error) {
		ran.Store(true)
		return "done", nil
	}
	if err := r.RunNow("paused"); err != nil {
		t.Fatalf("RunNow on a paused job: %v", err)
	}
	waitUntil(t, ran.Load)
	if err := r.RunNow("ghost"); err == nil {
		t.Fatal("RunNow on an unknown key answered nil")
	}
}

func TestCronRunRoutesRoundtrip(t *testing.T) {
	r, _, started, _ := blockingRunner(t)
	p := &Plugin{runner: r}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/cron/run", bytes.NewBufferString(`{"key":"pulse"}`))
	p.handleRun(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /cron/run = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var rows []JobInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("run answer decode: %v", err)
	}
	if len(rows) != 1 || !rows[0].Running {
		t.Fatalf("run answer = %+v, want one running row", rows)
	}
	select {
	case <-started:
	case <-time.After(testWaitTimeout):
		t.Fatal("the route never started the fire")
	}

	// stop answers the fresh set with the live mark gone
	stop := httptest.NewRecorder()
	p.handleStop(stop, httptest.NewRequest(http.MethodPost, "/cron/stop", bytes.NewBufferString(`{"key":"pulse"}`)))
	if stop.Code != http.StatusOK {
		t.Fatalf("POST /cron/stop = %d (%s), want 200", stop.Code, stop.Body.String())
	}

	// refusals: unknown key 404, missing key 400, GET 405, idle stop 409
	idle := httptest.NewRecorder()
	p.handleStop(idle, httptest.NewRequest(http.MethodPost, "/cron/stop", bytes.NewBufferString(`{"key":"pulse"}`)))
	if idle.Code != http.StatusConflict {
		t.Fatalf("idle stop = %d, want 409", idle.Code)
	}
	ghost := httptest.NewRecorder()
	p.handleRun(ghost, httptest.NewRequest(http.MethodPost, "/cron/run", bytes.NewBufferString(`{"key":"ghost"}`)))
	if ghost.Code != http.StatusNotFound {
		t.Fatalf("unknown key run = %d, want 404", ghost.Code)
	}
	missing := httptest.NewRecorder()
	p.handleRun(missing, httptest.NewRequest(http.MethodPost, "/cron/run", bytes.NewBufferString(`{}`)))
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing key run = %d, want 400", missing.Code)
	}
	nope := httptest.NewRecorder()
	p.handleRun(nope, httptest.NewRequest(http.MethodGet, "/cron/run", nil))
	if nope.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /cron/run = %d, want 405", nope.Code)
	}
	unmounted := httptest.NewRecorder()
	(&Plugin{}).handleRun(unmounted, httptest.NewRequest(http.MethodPost, "/cron/run", bytes.NewBufferString(`{"key":"pulse"}`)))
	if unmounted.Code != http.StatusNotImplemented {
		t.Fatalf("unmounted run = %d, want 501", unmounted.Code)
	}
}
