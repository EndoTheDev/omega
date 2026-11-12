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
	"github.com/EndoTheDev/omega/core"
	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// catchUpWindow bounds how far back a missed fire is allowed to be.
// A missed scheduled time more recent than this fires once at startup;
// older missed fires are skipped and logged.
const catchUpWindow = 24 * time.Hour

// cronStateFileName persists last-fire time per job under OMEGA_HOME.
const cronStateFileName = "cronstate.json"

// stateFileName is the job source watched for hot reload.
const stateFileName = "crons.yaml"

// runOriginScheduled tags fires as background runs (the launch value
// the verdict tool and the approval gate compare - the word stays
// "cron" on the wire, the identifier stays free of the capsule noun).
const runOriginScheduled = "cron"

// cronState is the on-disk shape of cronstate.json. Jobs keys the
// covered scheduled times (the catch-up contract); Last keys the most
// recent fire's outcome (the next-run row's sibling: status + when).
type cronState struct {
	Jobs map[string]int64       `json:"jobs"` // name -> last covered scheduled time (unix)
	Last map[string]fireOutcome `json:"last,omitempty"`
}

// fireOutcome is one completed fire's record: when the turn (or wake)
// finished, whether it ran clean, and the capped error message when it
// did not. Keyed on the job's StateKey like everything else in here.
type fireOutcome struct {
	At    int64  `json:"at"` // completion wall clock (unix)
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// statusNoteCap bounds the persisted error message: the state file is
// an operator's diagnostic, not a log dump.
const statusNoteCap = 512

// Runner is the scheduler. Safe for concurrent use.
type Runner struct {
	home   string
	cfg    *Config // initial jobs, from Mount
	logger agent.LoggerProvider
	// memory is the consumer-side face of the memory capsule's store
	// (part 3): only the methods the deliver target calls, resolved
	// from the universal registry at Start. Nil-safe: no capsule, no
	// delivery (the loop logs and continues).
	memory memoryStore
	ag     *agent.Agent
	store  agent.StoreProvider
	// notifier is the finished-things emitter face, resolved from the
	// universal registry at Start (card t_5d9032d3). Nil-safe: no
	// capsule, no emission.
	notifier finishedThings
	// skills is the consumer-side face of the skills capsule (part 3):
	// just the read method the pinned-skills injection calls, resolved
	// from the universal registry at Start. Nil-safe: no capsule, no
	// pinning (a job with skills errors at fire, the scheduler keeps
	// standing - the same posture a bad agent name gets).
	skills skillReader
	// tickEmit is the event-spine wake-tick emitter, wired from
	// deps.Ctx at Start (card t_72ecca67). Nil = the spine has no
	// emitter (never started, or no Context): agent wakes go inert.
	tickEmit func(wakeTickPayload)

	// panels is the capsule's canvas surface, set by the plugin at
	// Mount (arc 6). Nil-safe: no canvas, no push - a reload still
	// works, frontends catch up on their next snapshot.
	panels *capsule

	mu        sync.Mutex
	running   map[string]bool        // per-job fire guard
	jobs      []Job                  // current job set
	sched     *cron.Cron             // active schedule; nil until started
	lastFire  map[string]int64       // state from cronstate.json + this run
	last      map[string]fireOutcome // state: the most recent fire's outcome
	scheduled map[string]bool        // names currently scheduled (for logs/tests)
	stop      chan struct{}          // closed by Stop() to end Start

	// Test hooks. runTurn is swappable so guard/catch-up/fire tests
	// don't need a live provider; now isolates time-dependent logic.
	now          func() time.Time
	runTurn      func(ctx context.Context, sessionID string, job Job, prompt string) (string, error)
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
		last:         map[string]fireOutcome{},
		scheduled:    map[string]bool{},
		stop:         make(chan struct{}),
		now:          time.Now,
		pollInterval: 5 * time.Second,
	}
	r.runTurn = r.agentTurn // method value; tests replace the hook
	return r
}

// runnerDeps carries what the Start arms the runner with: the host's
// StartDeps values reshaped by the plugin (the capsule owns the shape;
// the host knows StartDeps).
type runnerDeps struct {
	Ctx   *core.Context
	Agent *agent.Agent
	Store agent.StoreProvider
}

// memoryStore is the consumer-side face of the memory capsule (part 3
// seam constitution): just the method the deliver target calls. The
// memory capsule satisfies it structurally; no shared type exists.
type memoryStore interface {
	Add(target, content string) (usage string, err error)
}

// finishedThings is the consumer-side face of the finished-things
// store: just the emit method.
type finishedThings interface {
	Emit(n core.Notice) (core.Notice, error)
}

// skillReader is the consumer-side face of the skills capsule (part 3,
// the seam constitution): just the read the pinned-skills injection
// calls. The skills capsule satisfies it structurally (SkillRead);
// no shared type exists, absence is the floor.
type skillReader interface {
	SkillRead(name string) (description, content string, err error)
}

// Start runs the scheduler until the context is cancelled or Stop is
// called. deps carries the agent, store, memory, and finished-things
// faces resolved at host wiring time.
func (r *Runner) Start(ctx context.Context, deps runnerDeps) error {
	if deps.Ctx != nil {
		r.memory, _ = deps.Ctx.Services().Resolve("memory").(memoryStore)
		if r.logger == nil && deps.Ctx.Services().Resolve("logger") != nil {
			if lg, ok := deps.Ctx.Services().Resolve("logger").(agent.LoggerProvider); ok {
				r.logger = lg
			}
		}
		// The wake-tick emitter rides the Context's event spine (card
		// t_72ecca67): cron owns WHEN, the continuous capsule owns
		// WHAT happens to the tick. Nil observer set = empty spine,
		// the wake goes inert by construction.
		r.tickEmit = func(p wakeTickPayload) {
			deps.Ctx.EmitTick("cron", "wake", p)
		}
	}
	r.ag = deps.Agent
	r.store = deps.Store
	if deps.Ctx != nil {
		r.notifier, _ = deps.Ctx.Services().Resolve("notifications").(finishedThings)
		// The pinned-skills face: resolved live at Start like every
		// other registry consumer here. Wrong shape = nil (the shape
		// panic lives behind the mount's own seam asserts; a resolved
		//-but-non-conforming offer degrades to "no skills face" and
		// jobs with a pinned list error at fire, loudly, in the log).
		r.skills, _ = deps.Ctx.Services().Resolve("skills").(skillReader)
	}

	r.loadCronState()
	// The state GC rule (card t_a99ffcd7): sweep stale entries BEFORE
	// catch-up reads them - a phantom catch-up for a removed actor is
	// exactly the drift the sweep exists to kill.
	r.sweepStaleState()
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

// Stop shuts the scheduler down (the scheduler). Safe to call
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
// Returns false when the job's previous fire is still running. Keys on
// the job's StateKey - agent jobs share the name space with main's.
func (r *Runner) tryAcquire(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running[key] {
		return false
	}
	r.running[key] = true
	return true
}

// release marks the job's fire as finished.
func (r *Runner) release(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running[key] = false
}

// fire runs one job occurrence at time `when` (the scheduled time for
// catch-up fires, approximately now for regular fires). A fire
// arriving while the same job is still running is skipped - logged,
// never queued. Different jobs run in parallel.
//
// Agent jobs (card t_72ecca67) never turn in-process: they are WAKES,
// one tick through the event spine (payload law: raw JSON); the
// continuous capsule owns what the tick means. Cron owns WHEN only -
// no session, no notification, no deliver on this side. With the
// continuous capsule toggled off the tick lands on an empty spine and
// the wake goes inert; the schedule survives (ruling 14).
func (r *Runner) fire(ctx context.Context, job Job, when time.Time) {
	if !job.IsEnabled() {
		return
	}
	if ctx.Err() != nil {
		return
	}
	key := job.StateKey()
	if !r.tryAcquire(key) {
		r.logf("job %s: skipped, previous fire still running", key)
		return
	}
	defer r.release(key)

	// The last-status contract (t_3885c975): every fire return path
	// lands an outcome - ok / error with the message - so the tab's
	// status line always tells the truth about the last run. A wake
	// with no tick emitter is an error, not an invisible success.
	if job.IsAgent() {
		wakeErr := r.fireAgentWake(job, job.Agent, key)
		r.recordFire(job, when)
		r.recordOutcome(job, wakeErr)
		return
	}
	if job.DeliverTarget() == deliverContinuous {
		// A main-home job naming deliver: continuous-agent is the same
		// wake, aimed by the job's own agent field (the defaulting law
		// resolves empty to the default agent). The wake validation is
		// FIRE-TIME by law: agent homes are dynamic, so a bad or
		// non-continuous name is a clear logged error - the scheduler
		// keeps standing and the schedule survives (the acceptance
		// law: bad agent name never crashes the runner).
		name := job.ResolveAgent()
		prof, err := agent.LoadProfile(r.home, name)
		if err != nil {
			r.errorf("job %s: agent %q unknown: %v", key, name, err)
			r.recordFire(job, when)
			r.recordOutcome(job, fmt.Errorf("agent %q unknown: %v", name, err))
			return
		}
		if !prof.Continuous {
			r.errorf("job %s: agent %q has no continuous session (main never wakes); deliver continuous-agent needs an agent home", key, name)
			r.recordFire(job, when)
			r.recordOutcome(job, fmt.Errorf("agent %q has no continuous session", name))
			return
		}
		wakeErr := r.fireAgentWake(job, name, key)
		r.recordFire(job, when)
		r.recordOutcome(job, wakeErr)
		return
	}
	// In-process targets run under the default agent. A job naming a
	// non-default agent with an in-process deliver is a wrong-target
	// reference, not a crash: clear error in the job log, the run never
	// starts under the wrong persona, the schedule survives.
	if name := job.ResolveAgent(); name != agent.DefaultAgentName {
		r.errorf("job %s: agent %q: deliver %q runs in-process as the default agent; use deliver: continuous-agent to fire agent %s", key, name, job.DeliverTarget(), name)
		r.recordFire(job, when)
		r.recordOutcome(job, fmt.Errorf("deliver %q runs in-process as the default agent; agent %q needs deliver: continuous-agent", job.DeliverTarget(), name))
		return
	}

	start := r.now()
	sessionID, err := agent.NewSessionID()
	if err != nil {
		r.errorf("job %s: session id: %v", job.Name, err)
		r.recordOutcome(job, fmt.Errorf("session id: %v", err))
		return
	}

	// The pinned skills resolve BEFORE the session exists (the bad-agent
	// posture, now for skills): a half-set table never opens a session,
	// the error lands in the log and in the last-status record, the
	// scheduler keeps standing.
	promptText, pinErr := r.pinnedPrompt(job)
	if pinErr != nil {
		r.errorf("%v", pinErr)
		r.recordOutcome(job, pinErr)
		return
	}

	// Session labeled cron:<name>; prompt + run outputs persist even
	// if the turn itself fails. Session creation stays here; the
	// turn-copy persistence (prompt, assistant, tool results) rides
	// the loop's sink (t_471f6d4a).
	if r.store != nil {
		if err := r.store.CreateSession(ctx, sessionID, "", "cron:"+job.Name); err != nil {
			r.errorf("job %s: create session: %v", job.Name, err)
		}
	}

	// Wall-clock cap on the turn itself: without it, one hung tool
	// call (shell sleep, wedged provider) never returns, every later
	// fire logs "skipped, previous fire still running", and the job's
	// journal/memory delivery stops until a process restart. The cap
	// is the configured server.run_timeout - the same knob the /chat
	// runs carry - 30 minutes when it is unset (card t_ef885fd4).
	turnCtx, cancel := context.WithTimeout(ctx, r.turnCap())
	defer cancel()
	// Verdict cards v1 (card t_2ecf9c51): a cron fire is a background
	// turn - the verdict tool refuses to hard-pause from this origin.
	turnCtx = core.WithRunOrigin(turnCtx, runOriginScheduled)

	final, runErr := r.runTurn(turnCtx, sessionID, job, promptText)
	if runErr != nil {
		r.errorf("job %s: run failed after %s: %v", job.Name, sinceStart(start, r.now()), runErr)
	} else {
		r.logf("job %s: fired, session %s, done in %s", job.Name, shortID(sessionID), sinceStart(start, r.now()))
	}

	r.applyDeliver(ctx, job, final)
	r.recordFire(job, when)
	r.recordOutcome(job, runErr)
	r.emitFinished(job.Name, runErr == nil)
}

// turnCapDefault is the unset server.run_timeout fallback: 30 minutes,
// the same 1800s the detached /chat run cap falls back to.
const turnCapDefault = 30 * time.Minute

// serverRunTimeout is the ONE host knob a fire reads out of config.yaml
// - the server section's run_timeout, in seconds. One parse shape per
// concern (the agent/profiles.go law): every other server key is the
// host's business and stays unparsed here.
type serverRunTimeout struct {
	Server struct {
		RunTimeout int `yaml:"run_timeout"`
	} `yaml:"server"`
}

// turnCap resolves one fire's wall-clock cap: the configured
// server.run_timeout when the host sets one, turnCapDefault otherwise
// (unset, zero, negative, or no readable config). The knob is the
// shell's - server.run_timeout caps every detached /chat run - and the
// scheduler runs in-process beside that same shell, so the fire reads
// the config the process booted on rather than inventing a cron-side
// section. The two candidate paths mirror cmd/omega's resolveConfigPath
// (the legacy home root first, the config/ layout second). Re-read per
// fire: the cap bounds a hung turn, it is not a boot-time identity, and
// fires are minutes apart.
func (r *Runner) turnCap() time.Duration {
	if r.home == "" {
		return turnCapDefault // no home: nothing to read, never a relative-path hit
	}
	for _, path := range []string{
		filepath.Join(r.home, "config.yaml"),
		filepath.Join(r.home, "config", "config.yaml"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg serverRunTimeout
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			// A malformed config.yaml already failed the host's own boot
			// parse; a fire degrades to the default instead of inventing
			// a second opinion on the operator's file.
			return turnCapDefault
		}
		if cfg.Server.RunTimeout > 0 {
			return time.Duration(cfg.Server.RunTimeout) * time.Second
		}
		return turnCapDefault
	}
	return turnCapDefault
}

// emitFinished records one finished-things record for the fire (card
// t_5d9032d3). Metadata only - the job name goes in the title, never
// prompt or run content. Catch-up fires travel through fire too, so
// one occurrence = one record regardless of how it fired. Agent wakes
// never ride this: ruling 10 bans cross-home notification writes and
// unasked messaging - the wake report lands in the agent's own chat,
// the roster's unread flag is the human's signal.
func (r *Runner) emitFinished(jobName string, ok bool) {
	if r.notifier == nil {
		return
	}
	verdict := "ok"
	if !ok {
		verdict = "failed"
	}
	_, _ = r.notifier.Emit(core.Notice{
		Kind:     "cron",
		SourceID: jobName,
		Title:    "cron " + jobName + " finished",
		Verdict:  verdict,
	})
}

// wakeTickPayload is the raw-JSON shape of cron's wake ticks (card
// t_72ecca67). Defined here, parsed over there (harness/continuous):
// the payload law keeps the two capsules decoupled - raw JSON, no
// shared Go types across the module boundary.
type wakeTickPayload struct {
	Agent  string `json:"agent"`
	Job    string `json:"job"`
	Prompt string `json:"prompt"`
	// Model rides the defaulting law: empty = inherit (the payload
	// law keeps the capsule decoupled - the field is optional JSON,
	// the continuous capsule decides what a model override means,
	// today OMEGA_MODEL on the wake child's env).
	Model string `json:"model,omitempty"`
}

// fireAgentWake is the agent-wake fire body: one tick through the
// event spine, then done. agent is the resolved target (the owning
// agent home's name, or the job's agent field via the defaulting law).
// The error return feeds the fire's last-status record: nil tickEmit =
// the continuous capsule is toggled off - the tick lands on an empty
// spine, the wake goes inert (the log names it), the schedule stays.
func (r *Runner) fireAgentWake(job Job, target string, key string) error {
	if r.tickEmit == nil {
		r.logf("wake %s: continuous not mounted, wake inert", key)
		return fmt.Errorf("continuous not mounted, wake inert")
	}
	r.tickEmit(wakeTickPayload{Agent: target, Job: job.Name, Prompt: job.Prompt, Model: job.Model})
	r.logf("wake %s: tick emitted", key)
	return nil
}

// pinnedPrompt builds the fire's user message: the pinned skills'
// bodies ahead of the job prompt (t_3885c975). A scheduled job runs
// one fixed procedure unwatched - making it hunt the skill index is
// noise, the editor pins the names and the fire ships the content.
// A pinned name the skills capsule cannot read (unknown, unmounted,
// unreadable) is an error the fire records - no run on a half-set
// table, the scheduler keeps standing (the bad-agent posture).
func (r *Runner) pinnedPrompt(job Job) (string, error) {
	if len(job.Skills) == 0 {
		return job.Prompt, nil
	}
	if r.skills == nil {
		return "", fmt.Errorf("job %s: skills pinned but the skills module is not mounted", job.Name)
	}
	var sb strings.Builder
	sb.WriteString("<pinned-skills>\n")
	for _, name := range job.Skills {
		_, content, err := r.skills.SkillRead(name)
		if err != nil {
			return "", fmt.Errorf("job %s: pinned skill %q: %w", job.Name, name, err)
		}
		sb.WriteString("<skill name=\"" + name + "\">\n")
		sb.WriteString(strings.TrimRight(content, "\n"))
		sb.WriteString("\n</skill>\n")
	}
	sb.WriteString("</pinned-skills>\n\n")
	return sb.String() + job.Prompt, nil
}

// agentTurn is the default runTurn: one full agent turn in the cron
// session. The prompt arrives pre-resolved - fire() pinned the skills
// in before the session existed (the sessionless half-set-table law);
// agentTurn sends it through the loop. The loop's sink persists
// prompt, assistant, and tool results (t_471f6d4a) - the runner only
// adds the session's token totals.
func (r *Runner) agentTurn(ctx context.Context, sessionID string, job Job, prompt string) (string, error) {
	if r.ag == nil {
		return "", fmt.Errorf("no agent wired (cron runner started without deps.Agent)")
	}
	// The defaulting law, model field: empty = the session provider
	// model (this exact wire), non-empty = a per-run provider derived
	// with WithModel - the copy discipline keeps parallel fires of
	// different jobs from racing shared provider state; the turn's
	// model dies with the run, the serve session's model never moves.
	//
	// The turn-copy sink wraps ctx BEFORE the run starts: Run captures
	// the context it is given, a sink applied after the call would
	// miss the whole turn's persistence.
	ctx = agent.WithTurnSink(ctx, agent.NewStoreTurnSink(r.store, sessionID, ""))
	ctx = agent.WithPersistInput(ctx, []core.Message{core.NewUser(prompt)})
	promptMsgs := []core.Message{core.NewUser(prompt)}
	var events <-chan core.Event
	if model := strings.TrimSpace(job.Model); model != "" {
		if p := r.ag.Provider(); p != nil {
			events = r.ag.RunWithProvider(ctx, p.WithModel(model), promptMsgs, nil)
		} else {
			r.logf("job %s: model %q ignored, no provider wired", job.Name, model)
			events = r.ag.Run(ctx, promptMsgs, nil)
		}
	} else {
		events = r.ag.Run(ctx, promptMsgs, nil)
	}
	var promptTokens, completionTokens int
	var final string
	var runErr error
	for event := range events {
		switch e := event.(type) {
		case core.AssistantMessageEvent:
			final = e.Message.Content
		case core.StreamEvent:
			if end, ok := e.Event.(core.StreamEnd); ok {
				if end.PromptEvalCount != nil {
					promptTokens += *end.PromptEvalCount
				}
				if end.EvalCount != nil {
					completionTokens += *end.EvalCount
				}
			}
		case core.AgentEnd:
			if e.Error != "" {
				runErr = fmt.Errorf("%s", e.Error)
			}
		}
	}
	if r.store != nil {
		if err := r.store.AddSessionTokens(ctx, sessionID, promptTokens, completionTokens); err != nil {
			r.errorf("job %s: persist usage: %v", job.Name, err)
		}
	}
	return final, runErr
}

// recordFire stores the covered fire time and writes cronstate.json.
// Catch-up fires record the missed scheduled time so a restart before
// the next scheduled fire does not fire again. Keys on StateKey -
// agent jobs name-space under <agent>/.
func (r *Runner) recordFire(job Job, when time.Time) {
	r.mu.Lock()
	r.lastFire[job.StateKey()] = when.Unix()
	r.mu.Unlock()
	if err := r.saveCronState(); err != nil {
		r.errorf("job %s: save state: %v", job.Name, err)
	}
}

// recordOutcome lands the fire's last-status record (t_3885c975): ok,
// or error with the message. Every fire return path calls it, so the
// tab's status line never lies about the last run. The record is the
// COMPLETION wall clock (when the turn or wake finished), not the
// scheduled slot the catch-up keys on - the two live as separate
// entries in cronstate.json.
func (r *Runner) recordOutcome(job Job, err error) {
	r.mu.Lock()
	if r.last == nil {
		r.last = map[string]fireOutcome{}
	}
	r.last[job.StateKey()] = fireOutcome{
		At:    r.now().Unix(),
		Ok:    err == nil,
		Error: truncateNote(err),
	}
	r.mu.Unlock()
	if serr := r.saveCronState(); serr != nil {
		r.errorf("job %s: save state: %v", job.Name, serr)
	}
}

// truncateNote caps an error message for the state record.
func truncateNote(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > statusNoteCap {
		msg = msg[:statusNoteCap]
	}
	return msg
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
	case deliverAppendToFile:
		if err := appendDeliverFile(r.home, job, r.now(), final); err != nil {
			r.errorf("job %s: file append: %v", job.Name, err)
		} else {
			r.logf("job %s: file updated", job.Name)
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
		// new-session: session only.
	}
}

// appendDeliverFile appends the run summary to the job's `file`
// (home-relative), creating directories on demand. An empty file keeps
// the legacy journal naming (<home>/journal/YYYY-MM-DD.md) - that is
// the journal alias's exact shape, and the append-to-file default so a
// job without a file still lands somewhere sane.
func appendDeliverFile(home string, job Job, when time.Time, final string) error {
	name := job.File
	if name == "" {
		name = filepath.Join("journal", when.Format("2006-01-02")+".md")
	}
	path := filepath.Join(home, filepath.FromSlash(name))
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
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
// no recorded last fire (new or never fired) gets no catch-up. Interval
// jobs ride the same walk: schedulePrev steps everySchedule.Next
// minute-by-minute over the window, an interval miss aligns onto the
// same epoch-minute grid a live fire would have hit.
func (r *Runner) catchUp(ctx context.Context, now time.Time) {
	for _, job := range r.jobsSnapshot() {
		if !job.IsEnabled() {
			continue // a missed fire of a disabled job never fires
		}
		last, ok := r.lastFireOf(job.StateKey())
		if !ok {
			r.logf("job %s: no recorded last fire, no catch-up", job.Name)
			continue
		}
		sched, err := scheduleFor(job)
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

// scheduleFor returns the cron.Schedule a job fires on: the parsed
// 5-field grammar for kind cron, the epoch-minute interval for kind
// interval. Both satisfy cron.Schedule, so the live scheduler, the
// catch-up walk and the next-fire inspector share one mechanism.
func scheduleFor(job Job) (cron.Schedule, error) {
	if job.KindResolved() == kindInterval {
		return everySchedule{minutes: job.Minutes}, nil
	}
	return scheduleParser.Parse(job.Schedule)
}

// everySchedule fires on minute multiples of N aligned to the epoch:
// time.Truncate is absolute, so the grid is timezone-independent and
// restart-stable (a job with minutes: 5 fires at :00, :05, :10 ... on
// every wall clock whose offset is whole minutes). Implements
// cron.Schedule - interval jobs are ordinary robfig schedules, nothing
// in the scheduler knows the kind exists. Next always returns a time
// STRICTLY after t (the robfig contract).
type everySchedule struct{ minutes int }

func (s everySchedule) Next(t time.Time) time.Time {
	dur := time.Duration(s.minutes) * time.Minute
	if t.Truncate(dur).Equal(t) {
		return t.Add(dur)
	}
	return t.Truncate(dur).Add(dur)
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
		run := cron.FuncJob(func() { r.fire(ctx, job, time.Now()) })
		if job.KindResolved() == kindInterval {
			// Interval jobs are their own cron.Schedule: the live
			// scheduler cannot tell the two kinds apart.
			c.Schedule(everySchedule{minutes: job.Minutes}, run)
		} else {
			if _, err := c.AddJob(job.Schedule, run); err != nil {
				r.errorf("job %s: schedule: %v (validated at load, unexpected)", job.Name, err)
				continue
			}
		}
		r.scheduled[job.StateKey()] = true
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

// Jobs is the read-only inspector snapshot (, card
// t_db430fea): every job in the current config with its enablement and
// next fire time in RFC3339 local time, sorted by name. A disabled job
// and a job whose schedule does not parse (validated at load, so
// unexpected) report an empty Next. Works before Start - the job set
// exists the moment the plugin mounts, whether or not the scheduler
// has been started by the host.
func (r *Runner) Jobs() []JobInfo {
	jobs := r.jobsSnapshot()
	now := r.now()
	out := make([]JobInfo, 0, len(jobs))
	for _, j := range jobs {
		row := JobInfo{
			Name:     j.Name,
			Kind:     j.KindResolved(),
			Schedule: j.Schedule,
			Minutes:  j.Minutes,
			Deliver:  j.DeliverTarget(),
			Agent:    j.Agent,
			Model:    j.Model,
			File:     j.File,
			Prompt:   j.Prompt,
			Skills:   j.Skills,
			Enabled:  j.IsEnabled(),
		}
		// The last-status fields ride the same snapshot: the record
		// survives restarts, so a fresh serve still shows the truth
		// about the previous process's fires.
		if last, ok := r.outcomeOf(j.StateKey()); ok {
			row.Last = time.Unix(last.At, 0).Local().Format(time.RFC3339)
			if last.Ok {
				row.Status = "ok"
			} else {
				row.Status = "error"
				row.StatusNote = last.Error
			}
		}
		if row.Enabled {
			if sched, err := scheduleFor(j); err == nil {
				if next := sched.Next(now); !next.IsZero() {
					row.Next = next.Format(time.RFC3339)
				}
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, k int) bool {
		if out[i].Agent != out[k].Agent {
			return out[i].Agent < out[k].Agent
		}
		return out[i].Name < out[k].Name
	})
	return out
}

// --- state (cronstate.json) ---

// sweepStaleState is the cronstate.json GC rule (card t_a99ffcd7): a
// state entry whose key matches no job in the CURRENT declared set -
// main's crons.yaml plus every agent home's - is a leftover pointing
// at a removed runtime actor (a deleted roster agent's wake, a job
// edited out of the file) and gets dropped, logged by name. The sweep
// is conservative by construction: a job that still declares itself
// keeps its state whatever its enablement ("never drop a job that
// still declares itself"), and it only ever runs against a job set
// that loaded clean (Start's config and applyReload's reload both
// arrive post-validation; a failed reload keeps the previous jobs and
// never reaches here). Runs at Start (before catch-up - a stale entry
// must not drive a phantom catch-up) and on every hot reload (the
// watcher's poll reaches this the moment an agent home disappears).
func (r *Runner) sweepStaleState() {
	r.mu.Lock()
	declared := make(map[string]bool, len(r.jobs))
	for _, j := range r.jobs {
		declared[j.StateKey()] = true
	}
	var droppedFire, droppedLast []string
	for k := range r.lastFire {
		if !declared[k] {
			droppedFire = append(droppedFire, k)
		}
	}
	for k := range r.last {
		if !declared[k] {
			droppedLast = append(droppedLast, k)
		}
	}
	if len(droppedFire) == 0 && len(droppedLast) == 0 {
		r.mu.Unlock()
		return
	}
	for _, k := range droppedFire {
		delete(r.lastFire, k)
	}
	for _, k := range droppedLast {
		delete(r.last, k)
	}
	r.mu.Unlock()
	for _, k := range droppedFire {
		r.logf("cronstate GC: dropped stale fire record %q (job not declared in any crons.yaml)", k)
	}
	for _, k := range droppedLast {
		r.logf("cronstate GC: dropped stale fire outcome %q (job not declared in any crons.yaml)", k)
	}
	if err := r.saveCronState(); err != nil {
		r.errorf("cronstate GC: save state: %v", err)
	}
}

// loadCronState reads cronstate.json. A missing or corrupt file is a
// fresh state - no catch-up, regular schedule unaffected. The Last map
// is optional (older state files carry jobs only): absent outcomes
// read as the job never fired.
func (r *Runner) loadCronState() {
	r.mu.Lock()
	r.lastFire = map[string]int64{}
	r.last = map[string]fireOutcome{}
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
	r.last = st.Last
	if r.last == nil {
		r.last = map[string]fireOutcome{}
	}
	r.mu.Unlock()
}

// saveCronState atomically writes cronstate.json.
func (r *Runner) saveCronState() error {
	r.mu.Lock()
	st := cronState{
		Jobs: make(map[string]int64, len(r.lastFire)),
		Last: make(map[string]fireOutcome, len(r.last)),
	}
	for k, v := range r.lastFire {
		st.Jobs[k] = v
	}
	for k, v := range r.last {
		st.Last[k] = v
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

// outcomeOf returns the recorded last-fire outcome for a job.
func (r *Runner) outcomeOf(key string) (fireOutcome, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.last[key]
	return o, ok
}

// --- hot reload ---

// cronFiles returns every crons.yaml this runner owns (card
// t_72ecca67, ruling 12): the main home's file AND every agent home's
// config/crons.yaml. The watcher and the reload both walk this list.
func (r *Runner) cronFiles() map[string]string {
	paths := map[string]string{}
	if p := homeFile(r.home); p != "" {
		paths[p] = "main"
	}
	for _, prof := range agent.LoadProfiles(r.home) {
		if !prof.Continuous {
			continue
		}
		paths[filepath.Join(prof.Dir, "config", stateFileName)] = prof.Name
	}
	return paths
}

// watchJobsFile polls every crons.yaml (main + agent homes, ruling 12)
// for mtime changes and reloads the full job set without restart,
// logging the diff. Mirrors cmd/omega's WatchConfig pattern (poll, not
// fsnotify). Two laws keep the previous job set in effect: a reload
// that fails to parse, and a reload that reads an empty file
// (loadAllConfigsForReload - a truncated write must never apply as an
// empty job set, card t_8be45d55). Returns when the context is cancelled
// or Stop is called.
func (r *Runner) watchJobsFile(ctx context.Context) {
	// The signature is path -> mtime; a new agent home or a new agent
	// file changes the set, the mtime change changes the value.
	last := map[string]time.Time{}
	snapshot := func() map[string]time.Time {
		now := map[string]time.Time{}
		for path := range r.cronFiles() {
			if fi, err := os.Stat(path); err == nil {
				now[path] = fi.ModTime()
			}
		}
		return now
	}
	last = snapshot()
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
		nowSig := snapshot()
		if sameMtimes(last, nowSig) {
			continue
		}
		last = nowSig
		cfg, err := loadAllConfigsForReload(r.home)
		if err != nil {
			r.errorf("crons reload failed (keeping previous jobs): %v", err)
			continue
		}
		r.applyReload(ctx, cfg)
	}
}

// sameMtimes compares two mtime maps including key sets.
func sameMtimes(a, b map[string]time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || !v.Equal(w) {
			return false
		}
	}
	return true
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
		oldSet[j.StateKey()] = j
	}
	newJobs := cfg.Jobs
	newSet := make(map[string]Job, len(newJobs))
	for _, j := range newJobs {
		newSet[j.StateKey()] = j
	}
	for _, j := range newJobs {
		prev, existed := oldSet[j.StateKey()]
		// Diff keys are the StateKey: two agents (and main) may carry
		// the same job name; only a change in the same home logs here.
		switch {
		case !existed:
			r.logf("reload: job added: %s (%s)", j.StateKey(), j.Schedule)
		case !prev.IsEnabled() && j.IsEnabled():
			r.logf("reload: job enabled: %s", j.StateKey())
		case prev.IsEnabled() && !j.IsEnabled():
			r.logf("reload: job disabled: %s", j.StateKey())
		}
	}
	for _, j := range old {
		if _, ok := newSet[j.StateKey()]; !ok {
			r.logf("reload: job removed: %s", j.StateKey())
		}
	}

	// The state GC rule (card t_a99ffcd7): a reload means the declared
	// set changed - the moment a removed actor's state becomes sweepable.
	// The watcher's 5s poll makes an agent-home removal reach this path
	// live (the journey recipe that rm -rf's the home never writes
	// cronstate.json - the runner owns the file, the runner reaps it).
	r.sweepStaleState()

	r.stopScheduler()
	r.scheduleEnabled(ctx)
	// The panel refresh rides the same reload: the watcher is the
	// module's one internal mutation face, so its change pushes the
	// refreshed jobs panel through the canvas sink (arc 6 push law).
	// Nil-safe; a failed push never fails the reload - the job set is
	// already rescheduled, the snapshot is the catch-up.
	r.panels.push()
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
