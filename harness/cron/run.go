package cron

// run.go: the console's manual fire control - Run now (start one fire the schedule did
// not order) and Stop run (cancel the fire that is in flight). The schedule flag stays
// the toggle route's business; these two reach the runner, never the yaml.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// ErrJobRunning is the run-now refusal: a fire is already in flight for that key, and
// the runner's law is skip-not-queue.
var ErrJobRunning = errors.New("job is already running")

// ErrJobNotRunning is the stop refusal: there is no live fire to cancel.
var ErrJobNotRunning = errors.New("job is not running")

// RunNow starts one fire immediately, in the background. A manual fire bypasses the
// enabled flag (the operator asked for this occurrence) and a fire already in flight is
// refused, never queued.
func (r *Runner) RunNow(key string) error {
	job, ok := r.jobByKey(key)
	if !ok {
		return UnknownJobError{Key: key}
	}
	if !r.tryAcquire(job.StateKey()) {
		return ErrJobRunning
	}
	go r.fireHeld(context.Background(), job, r.now(), true)
	return nil
}

// StopRun cancels the fire in flight for one key. Its session keeps whatever the turn
// wrote; the outcome lands in the last-status record like any other fire.
func (r *Runner) StopRun(key string) error {
	if _, ok := r.jobByKey(key); !ok {
		return UnknownJobError{Key: key}
	}
	r.mu.Lock()
	cancel, ok := r.cancels[key]
	if ok {
		// drop the handle under the same lock: a second stop must read "not running"
		// even before the fire's goroutine finishes unwinding.
		delete(r.cancels, key)
	}
	r.mu.Unlock()
	if !ok {
		return ErrJobNotRunning
	}
	cancel()
	return nil
}

// cronRunControl is the handler's consumer-owned face: the routes need the two verbs,
// never the whole scheduler.
type cronRunControl interface {
	RunNow(key string) error
	StopRun(key string) error
}

type jobRunRequest struct {
	Key string `json:"key"`
}

// handleRun serves POST /cron/run {key}: start one fire now and answer the fresh job set
// (the toggle route's one-response contract; `running` carries the live mark).
func (p *Plugin) handleRun(w http.ResponseWriter, r *http.Request) {
	p.serveRunControl(w, r, "cron run", func(c cronRunControl, key string) error {
		return c.RunNow(key)
	})
}

// handleStop serves POST /cron/stop {key}: cancel the fire in flight and answer the fresh
// job set.
func (p *Plugin) handleStop(w http.ResponseWriter, r *http.Request) {
	p.serveRunControl(w, r, "cron stop", func(c cronRunControl, key string) error {
		return c.StopRun(key)
	})
}

func (p *Plugin) serveRunControl(
	w http.ResponseWriter,
	r *http.Request,
	label string,
	act func(c cronRunControl, key string) error,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if p.runner == nil {
		http.Error(w, label+": no cron runner mounted", http.StatusNotImplemented)
		return
	}
	control := cronRunControl(p.runner)
	var req jobRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
		http.Error(w, label+": key is required", http.StatusBadRequest)
		return
	}
	if err := act(control, req.Key); err != nil {
		var unknown UnknownJobError
		switch {
		case errors.As(err, &unknown):
			http.Error(w, label+": "+err.Error(), http.StatusNotFound)
		case errors.Is(err, ErrJobRunning), errors.Is(err, ErrJobNotRunning):
			http.Error(w, label+": "+err.Error(), http.StatusConflict)
		default:
			http.Error(w, label+": "+err.Error(), http.StatusBadRequest)
		}
		return
	}
	rows := p.runner.JobRows()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rows); err != nil {
		return
	}
}
