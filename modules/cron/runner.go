// Package cron runner: scheduling, catch-up, concurrent-fire guard,
// deliver targets, and hot-reload of crons.yaml.
package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/robfig/cron/v3"
)

// catchUpWindow bounds how far back a missed fire is allowed to be.
// A missed scheduled time more recent than this fires once at startup;
// older missed fires are skipped and logged.
const catchUpWindow = 24 * time.Hour

// cronStateFileName persists last-fire time per job under OMEGA_HOME.
const cronStateFileName = "cronstate.json"

// stateFileName is the job source watched for hot reload.
const stateFileName = "crons.yaml"

// cronState is the on-disk shape of cronstate.json.
type cronState struct {
	Jobs map[string]int64 `json:"jobs"` // name -> last covered scheduled time (unix)
}

// Runner implements agent.CronProvider. Safe for concurrent use.
type Runner struct {
	home   string
	cfg    *Config // initial jobs, from Mount
	logger agent.LoggerProvider
	memory agent.MemoryProvider
	ag     *agent.Agent
	store  agent.StoreProvider

	mu        sync.Mutex
	running   map[string]bool  // per-job fire guard
	jobs      []Job            // current job set
	sched     *cron.Cron       // active schedule; nil until started
	lastFire  map[string]int64 // state from cronstate.json + this run
	scheduled map[string]bool  // names currently scheduled (for logs/tests)
	stop      chan struct{}    // closed by Stop() to end Start

	// Test hooks. runTurn is swappable so guard/catch-up/fire tests
	// don't need a live provider; now isolates time-dependent logic.
	now          func() time.Time
	runTurn      func(ctx context.Context, sessionID string, job Job) (string, error)
	pollInterval time.Duration
}

// NewRunner creates the cron Runner for a parsed config and an omega
// home directory (crons.yaml, cronstate.json, journal/).
func NewRunner(cfg *Config, home string) *Runner {
	jobs := cfg.Jobs
	if jobs == nil {
		jobs = []Job{}
	}
	r := &Runner{
		home:         home,
		cfg:          cfg,
		jobs:         jobs,
		running:      map[string]bool{},
		lastFire:     map[string]int64{},
		scheduled:    map[string]bool{},
		stop:         make(chan struct{}),
		now:          time.Now,
		pollInterval: 5 * time.Second,
	}
	r.runTurn = r.agentTurn // method value; tests replace the hook
	return r
}

// Start runs the scheduler until the context is cancelled or Stop is
// called. Implements agent.CronProvider. deps carries the agent,
// store, memory seam, and logger captured at host wiring time.
func (r *Runner) Start(ctx context.Context, deps agent.CronDeps) error {
	if deps.Ctx != nil {
		r.memory = deps.Ctx.Memory
		if r.logger == nil {
			r.logger = deps.Ctx.Logger
		}
	}
	r.ag = deps.Agent
	r.store = deps.Store

	r.loadCronState()
	r.catchUp(ctx, r.now())
	r.scheduleEnabled(ctx)
	if r.sched != nil {
		r.sched.Start()
	}

	// Watcher exits on ctx cancellation or Stop - Start just waits for
	// it after signaling shutdown. No ownership of its done channel
	// (double-close on shutdown was a real crash).
	var watcher sync.WaitGroup
	watcher.Add(1)
	go func() {
		defer watcher.Done()
		r.watchJobsFile(ctx)
	}()

	select {
	case <-ctx.Done():
	case <-r.stop:
	}
	r.stopScheduler()
	watcher.Wait()
	return nil
}

// Stop shuts the scheduler down (agent.CronProvider). Safe to call
// multiple times.
func (r *Runner) Stop() error {
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
	return nil
}

func (r *Runner) logf(format string, args ...any) {
	if r.logger != nil {
		r.logger.Printf("cron: "+format, args...)
	} else {
		fmt.Fprintf(os.Stderr, "cron: "+format+"\n", args...)
	}
}

func (r *Runner) errorf(format string, args ...any) {
	if r.logger != nil {
		r.logger.Errorf("cron: "+format, args...)
	} else {
		fmt.Fprintf(os.Stderr, "cron: "+format+"\n", args...)
	}
}

// --- firing ---

// tryAcquire performs the single-instance-per-job check-and-set.
// Returns false when the job's previous fire is still running.
func (r *Runner) tryAcquire(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running[name] {
		return false
	}
	r.running[name] = true
	return true
}

// release marks the job's fire as finished.
func (r *Runner) release(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running[name] = false
}

// fire runs one job occurrence at time `when` (the scheduled time for
// catch-up fires, approximately now for regular fires). A fire
// arriving while the same job is still running is skipped - logged,
// never queued. Different jobs run in parallel.
func (r *Runner) fire(ctx context.Context, job Job, when time.Time) {
	if !job.IsEnabled() {
		return
	}
	if ctx.Err() != nil {
		return
	}
	if !r.tryAcquire(job.Name) {
		r.logf("job %s: skipped, previous fire still running", job.Name)
		return
	}
	defer r.release(job.Name)

	start := r.now()
	sessionID, err := agent.NewSessionID()
	if err != nil {
		r.errorf("job %s: session id: %v", job.Name, err)
		return
	}

	// Session labeled cron:<name>; prompt + run outputs persist even
	// if the turn itself fails.
	if r.store != nil {
		if err := r.store.CreateSession(ctx, sessionID, "", "cron:"+job.Name); err != nil {
			r.errorf("job %s: create session: %v", job.Name, err)
		}
		if err := r.store.AppendMessage(ctx, sessionID, ai.NewUser(job.Prompt)); err != nil {
			r.errorf("job %s: persist prompt: %v", job.Name, err)
		}
	}

	final, runErr := r.runTurn(ctx, sessionID, job)
	if runErr != nil {
		r.errorf("job %s: run failed after %s: %v", job.Name, sinceStart(start, r.now()), runErr)
	} else {
		r.logf("job %s: fired, session %s, done in %s", job.Name, shortID(sessionID), sinceStart(start, r.now()))
	}

	r.applyDeliver(ctx, job, final)
	r.recordFire(job, when)
}

// agentTurn is the default runTurn: one full agent turn in the cron
// session. Sends the job prompt through the loop, streams to
// completion, and persists assistant + tool-result messages.
func (r *Runner) agentTurn(ctx context.Context, sessionID string, job Job) (string, error) {
	if r.ag == nil {
		return "", fmt.Errorf("no agent wired (cron runner started without deps.Agent)")
	}
	var prompt, completion int
	var final string
	var runErr error
	for event := range r.ag.Run(ctx, []ai.Message{ai.NewUser(job.Prompt)}, nil) {
		switch e := event.(type) {
		case agent.AssistantMessageEvent:
			final = e.Message.Content
			if r.store != nil {
				if err := r.store.AppendMessage(ctx, sessionID, e.Message); err != nil {
					r.errorf("job %s: persist assistant: %v", job.Name, err)
				}
			}
		case agent.ToolResultEvent:
			if r.store != nil {
				if err := r.store.AppendMessage(ctx, sessionID, e.Message); err != nil {
					r.errorf("job %s: persist tool result: %v", job.Name, err)
				}
			}
		case agent.StreamEvent:
			if end, ok := e.Event.(ai.StreamEnd); ok {
				if end.PromptEvalCount != nil {
					prompt += *end.PromptEvalCount
				}
				if end.EvalCount != nil {
					completion += *end.EvalCount
				}
			}
		case agent.AgentEnd:
			if e.Error != "" {
				runErr = fmt.Errorf("%s", e.Error)
			}
		}
	}
	if r.store != nil {
		if err := r.store.AddSessionTokens(ctx, sessionID, prompt, completion); err != nil {
			r.errorf("job %s: persist usage: %v", job.Name, err)
		}
	}
	return final, runErr
}

// recordFire stores the covered fire time and writes cronstate.json.
// Catch-up fires record the missed scheduled time so a restart before
// the next scheduled fire does not fire again.
func (r *Runner) recordFire(job Job, when time.Time) {
	r.mu.Lock()
	r.lastFire[job.Name] = when.Unix()
	r.mu.Unlock()
	if err := r.saveCronState(); err != nil {
		r.errorf("job %s: save state: %v", job.Name, err)
	}
}

// applyDeliver applies the run's deliver target to the final assistant
// message. An empty final message (failed or tool-only run) delivers
// nothing and is logged.
func (r *Runner) applyDeliver(ctx context.Context, job Job, final string) {
	if strings.TrimSpace(final) == "" {
		if job.DeliverTarget() != deliverNone {
			r.logf("job %s: no final message, deliver target %q skipped", job.Name, job.DeliverTarget())
		}
		return
	}
	switch job.DeliverTarget() {
	case deliverJournal:
		if err := appendJournal(r.home, job, r.now(), final); err != nil {
			r.errorf("job %s: journal: %v", job.Name, err)
		} else {
			r.logf("job %s: journal updated", job.Name)
		}
	case deliverMemory:
		if r.memory == nil {
			r.errorf("job %s: deliver memory but no memory extension loaded", job.Name)
			return
		}
		if _, err := r.memory.Add("memory", final); err != nil {
			r.errorf("job %s: memory store: %v", job.Name, err)
		} else {
			r.logf("job %s: memory updated", job.Name)
		}
	default:
		// none: session only.
	}
}

// appendJournal appends the run summary to <home>/journal/YYYY-MM-DD.md,
// creating the directory and file on demand.
func appendJournal(home string, job Job, when time.Time, final string) error {
	dir := filepath.Join(home, "journal")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create journal dir: %w", err)
	}
	path := filepath.Join(dir, when.Format("2006-01-02")+".md")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open journal: %w", err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "## %s - %s\n\n%s\n\n", job.Name, when.Format("2006-01-02 15:04"), final)
	return err
}

// --- catch-up ---

// catchUp fires missed jobs at startup. For each enabled job: the most
// recent scheduled time before now is checked against lastFire; a
// missed fire within the last 24h fires once (state records the missed
// scheduled time, so a restart before the next scheduled fire does not
// double-fire). Older missed fires are skipped and logged. A job with
// no recorded last fire (new or never fired) gets no catch-up.
func (r *Runner) catchUp(ctx context.Context, now time.Time) {
	for _, job := range r.jobsSnapshot() {
		if !job.IsEnabled() {
			continue // a missed fire of a disabled job never fires
		}
		last, ok := r.lastFireOf(job.Name)
		if !ok {
			r.logf("job %s: no recorded last fire, no catch-up", job.Name)
			continue
		}
		sched, err := scheduleParser.Parse(job.Schedule)
		if err != nil {
			continue // validated at load; unreachable on a fresh parse
		}
		prev := schedulePrev(sched, now)
		if prev.IsZero() || prev.Unix() <= last {
			continue // nothing missed
		}
		if now.Sub(prev) > catchUpWindow {
			r.logf("job %s: missed fire %s skipped (older than 24h)", job.Name, prev.Format(time.RFC3339))
			continue
		}
		r.logf("job %s: catch-up for missed fire %s", job.Name, prev.Format(time.RFC3339))
		r.fire(ctx, job, prev)
	}
}

// schedulePrev returns the most recent scheduled occurrence at or
// before `now` by walking Schedule.Next forward over the last 7 days.
// robfig Schedule exposes Next only. Seven days is ample: catch-up
// only fires misses inside 24h, so anything older is skipped anyway.
// Returns the zero time when no occurrence falls inside the window.
func schedulePrev(sched cron.Schedule, now time.Time) time.Time {
	start := now.Add(-7 * 24 * time.Hour).Truncate(time.Minute)
	prev := time.Time{}
	t := start
	for i := 0; i < 7*24*60; i++ {
		next := sched.Next(t)
		if next.IsZero() || next.After(now) {
			break
		}
		prev = next
		t = next
	}
	return prev
}

// --- scheduling ---

// scheduleEnabled rebuilds the scheduler from the current job set,
// scheduling enabled jobs only. Used at startup and on hot reload.
func (r *Runner) scheduleEnabled(ctx context.Context) {
	c := cron.New(cron.WithParser(scheduleParser))
	r.mu.Lock()
	r.scheduled = make(map[string]bool)
	for _, job := range r.jobs {
		if !job.IsEnabled() {
			continue
		}
		_, err := c.AddJob(job.Schedule, cron.FuncJob(func() { r.fire(ctx, job, time.Now()) }))
		if err != nil {
			r.errorf("job %s: schedule: %v (validated at load, unexpected)", job.Name, err)
			continue
		}
		r.scheduled[job.Name] = true
	}
	r.sched = c
	r.mu.Unlock()
	c.Start()
}

// stopScheduler cancels the active cron scheduler.
func (r *Runner) stopScheduler() {
	r.mu.Lock()
	sched := r.sched
	r.sched = nil
	r.mu.Unlock()
	if sched != nil {
		sched.Stop()
	}
}

// scheduledNames returns the currently scheduled job names (tests and
// diagnostics).
func (r *Runner) scheduledNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.scheduled))
	for name := range r.scheduled {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// jobsSnapshot returns a copy of the current job list.
func (r *Runner) jobsSnapshot() []Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Job(nil), r.jobs...)
}

// --- state (cronstate.json) ---

// loadCronState reads cronstate.json. A missing or corrupt file is a
// fresh state - no catch-up, regular schedule unaffected.
func (r *Runner) loadCronState() {
	r.mu.Lock()
	r.lastFire = map[string]int64{}
	r.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(r.home, cronStateFileName))
	if err != nil {
		return
	}
	var st cronState
	if err := json.Unmarshal(data, &st); err != nil {
		r.errorf("read %s: corrupt state ignored: %v", cronStateFileName, err)
		return
	}
	r.mu.Lock()
	r.lastFire = st.Jobs
	if r.lastFire == nil {
		r.lastFire = map[string]int64{}
	}
	r.mu.Unlock()
}

// saveCronState atomically writes cronstate.json.
func (r *Runner) saveCronState() error {
	r.mu.Lock()
	st := cronState{Jobs: make(map[string]int64, len(r.lastFire))}
	for k, v := range r.lastFire {
		st.Jobs[k] = v
	}
	r.mu.Unlock()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(r.home, cronStateFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// lastFireOf returns the recorded last fire for a job.
func (r *Runner) lastFireOf(name string) (int64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.lastFire[name]
	return v, ok
}

// --- hot reload ---

// watchJobsFile polls crons.yaml for mtime changes and reloads the job
// set without restart, logging the diff. Mirrors cmd/omega's
// WatchConfig pattern (poll, not fsnotify). A reload that fails to
// parse keeps the previous job set in effect. Returns when the context
// is cancelled or Stop is called.
func (r *Runner) watchJobsFile(ctx context.Context) {
	path := homeFile(r.home)
	var lastMod time.Time
	if fi, err := os.Stat(path); err == nil {
		lastMod = fi.ModTime()
	}
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stop:
			return
		case <-ticker.C:
		}
		fi, err := os.Stat(path)
		if err != nil || fi.ModTime().Equal(lastMod) {
			continue
		}
		lastMod = fi.ModTime()
		cfg, err := LoadConfig(path)
		if err != nil {
			r.errorf("%s reload failed (keeping previous jobs): %v", stateFileName, err)
			continue
		}
		r.applyReload(ctx, cfg)
	}
}

// applyReload diffs the old job set against the new one, logs it, and
// reschedules. Removed/disabled jobs stop scheduling immediately; an
// already-running fire of a removed job finishes naturally.
func (r *Runner) applyReload(ctx context.Context, cfg *Config) {
	old := r.jobsSnapshot()
	r.mu.Lock()
	r.jobs = cfg.Jobs
	if r.jobs == nil {
		r.jobs = []Job{}
	}
	r.mu.Unlock()

	oldSet := make(map[string]Job, len(old))
	for _, j := range old {
		oldSet[j.Name] = j
	}
	newJobs := cfg.Jobs
	newSet := make(map[string]Job, len(newJobs))
	for _, j := range newJobs {
		newSet[j.Name] = j
	}
	for _, j := range newJobs {
		prev, existed := oldSet[j.Name]
		switch {
		case !existed:
			r.logf("reload: job added: %s (%s)", j.Name, j.Schedule)
		case !prev.IsEnabled() && j.IsEnabled():
			r.logf("reload: job enabled: %s", j.Name)
		case prev.IsEnabled() && !j.IsEnabled():
			r.logf("reload: job disabled: %s", j.Name)
		}
	}
	for _, j := range old {
		if _, ok := newSet[j.Name]; !ok {
			r.logf("reload: job removed: %s", j.Name)
		}
	}

	r.stopScheduler()
	r.scheduleEnabled(ctx)
}

// --- helpers ---

// sinceStart formats a duration for fire logs.
func sinceStart(start, end time.Time) string {
	return end.Sub(start).Round(100 * time.Millisecond).String()
}

// shortID shortens a session id for log lines.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}