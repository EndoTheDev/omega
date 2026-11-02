package cron

// schema_test.go: the async cron overhaul (card t_ecec864e) - the
// interval kind alongside the 5-field cron grammar, the per-job
// agent/model fields under the defaulting law, and the fire target
// dispatch (in-process default agent, continuous-agent wake, bad agent
// = clear logged error). The save/delete editor write face is pinned
// in save_test.go.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/llm"
	agent_loop "github.com/EndoTheDev/omega/modules/agent_loop"
	"github.com/EndoTheDev/omega/modules/store"
)

// --- schema: kinds, minutes, schedule laws ---

func TestIntervalKindParses(t *testing.T) {
	raw := `jobs:
  - name: every-five
    kind: interval
    minutes: 5
    prompt: tick
  - name: explicit-cron
    kind: cron
    schedule: "0 8 * * *"
    prompt: daily
  - name: implied-cron
    schedule: "30 4 * * 0"
    prompt: weekly
`
	cfg, err := ParseConfig([]byte(raw))
	if err != nil {
		t.Fatalf("interval yaml rejected: %v", err)
	}
	if len(cfg.Jobs) != 3 {
		t.Fatalf("want 3 jobs, got %d", len(cfg.Jobs))
	}
	if k := cfg.Jobs[0].KindResolved(); k != kindInterval || cfg.Jobs[0].Minutes != 5 || cfg.Jobs[0].Schedule != "" {
		t.Errorf("interval job misparsed: %+v", cfg.Jobs[0])
	}
	if k := cfg.Jobs[1].KindResolved(); k != kindCron {
		t.Errorf("explicit cron kind lost: %+v", cfg.Jobs[1])
	}
	if k := cfg.Jobs[2].KindResolved(); k != kindCron {
		t.Errorf("absent kind must default to cron: %+v", cfg.Jobs[2])
	}
}

func TestIntervalKindValidation(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"interval without minutes", "jobs:\n  - name: x\n    kind: interval\n    prompt: p\n", "minutes"},
		{"interval with zero minutes", "jobs:\n  - name: x\n    kind: interval\n    minutes: 0\n    prompt: p\n", "minutes"},
		{"interval with schedule", "jobs:\n  - name: x\n    kind: interval\n    schedule: \"0 8 * * *\"\n    minutes: 5\n    prompt: p\n", "minutes, not schedule"},
		{"cron with minutes", "jobs:\n  - name: x\n    schedule: \"0 8 * * *\"\n    minutes: 5\n    prompt: p\n", "schedule, not minutes"},
		{"cron without schedule", "jobs:\n  - name: x\n    prompt: p\n", "missing schedule"},
		{"unknown kind", "jobs:\n  - name: x\n    kind: hourly\n    prompt: p\n", "kind must be cron or interval"},
	}
	for _, c := range cases {
		_, err := ParseConfig([]byte(c.yaml))
		if err == nil {
			t.Errorf("%s: accepted, want rejection naming %q", c.name, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to name %q", c.name, err, c.want)
		}
	}
}

// --- schema: the deliver enum + legacy aliases ---

func TestDeliverEnumResolution(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"", deliverNewSession},
		{"none", deliverNewSession},
		{"new-session", deliverNewSession},
		{"journal", deliverAppendToFile},
		{"append-to-file", deliverAppendToFile},
		{"continuous-agent", deliverContinuous},
		{"memory", deliverMemory},
	}
	for _, c := range cases {
		j := Job{Name: "x", Schedule: "0 8 * * *", Prompt: "p", Deliver: c.raw}
		if got := j.DeliverTarget(); got != c.want {
			t.Errorf("DeliverTarget(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
	// unknown value rejects at load
	if _, err := ParseConfig([]byte("jobs:\n  - name: x\n    schedule: \"0 8 * * *\"\n    prompt: p\n    deliver: smoke-signal\n")); err == nil {
		t.Error("unknown deliver accepted")
	}
	// legacy journal keeps its byte-identical shape: empty file, the
	// journal date naming
	j := Job{Name: "x", Schedule: "0 8 * * *", Prompt: "p", Deliver: deliverJournal}
	if j.DeliverTarget() != deliverAppendToFile || j.File != "" {
		t.Errorf("journal alias target = %q, want append-to-file with empty file", j.DeliverTarget())
	}
}

// --- schema: agent + model fields, the defaulting law ---

func TestAgentDefaultLaw(t *testing.T) {
	if got := (Job{Name: "x", Schedule: "0 8 * * *", Prompt: "p"}).ResolveAgent(); got != agent.DefaultAgentName {
		t.Errorf("empty agent resolves to %q, want the default agent %q", got, agent.DefaultAgentName)
	}
	if got := (Job{Name: "x", Schedule: "0 8 * * *", Prompt: "p", Agent: "scout"}).ResolveAgent(); got != "scout" {
		t.Errorf("authored agent lost: %q", got)
	}
	// syntax-only load-time validation
	if _, err := ParseConfig([]byte("jobs:\n  - name: x\n    schedule: \"0 8 * * *\"\n    prompt: p\n    agent: SCOUT!!\n")); err == nil {
		t.Error("malformed agent name accepted at load")
	}
	// the model field rides through parse untouched
	cfg, err := ParseConfig([]byte("jobs:\n  - name: x\n    kind: interval\n    minutes: 5\n    prompt: p\n    model: glm-5.3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jobs[0].Model != "glm-5.3" {
		t.Errorf("model field lost: %+v", cfg.Jobs[0])
	}
}

func TestFileFieldValidation(t *testing.T) {
	if _, err := ParseConfig([]byte("jobs:\n  - name: x\n    schedule: \"0 8 * * *\"\n    prompt: p\n    file: notes/a.md\n")); err == nil {
		t.Error("file on a non-append deliver accepted")
	}
	if _, err := ParseConfig([]byte("jobs:\n  - name: x\n    schedule: \"0 8 * * *\"\n    prompt: p\n    deliver: append-to-file\n    file: ../../outside.md\n")); err == nil {
		t.Error("escaping file path accepted")
	}
	if _, err := ParseConfig([]byte("jobs:\n  - name: x\n    schedule: \"0 8 * * *\"\n    prompt: p\n    deliver: append-to-file\n    file: /absolute\n")); err == nil {
		t.Error("absolute file path accepted")
	}
	cfg, err := ParseConfig([]byte("jobs:\n  - name: x\n    schedule: \"0 8 * * *\"\n    prompt: p\n    deliver: append-to-file\n    file: journal/report.md\n"))
	if err != nil {
		t.Fatalf("legal append-to-file rejected: %v", err)
	}
	if cfg.Jobs[0].File != "journal/report.md" {
		t.Errorf("file lost: %+v", cfg.Jobs[0])
	}
}

// --- the interval schedule ---

func TestEveryScheduleNext(t *testing.T) {
	// the grid is aligned to epoch minutes: an exact boundary returns
	// the NEXT boundary (the robfig contract: strictly after t)
	s := everySchedule{minutes: 5}
	base := time.Unix(0, 0)
	if got := s.Next(base); !got.Equal(base.Add(5 * time.Minute)) {
		t.Errorf("Next(exact boundary) = %v, want +5m", got)
	}
	mid := base.Add(3 * time.Minute)
	if got := s.Next(mid); !got.Equal(base.Add(5 * time.Minute)) {
		t.Errorf("Next(mid-interval) = %v, want the next grid point", got)
	}
	// sub-second noise truncates onto the grid, never drifts
	noisy := base.Add(4*time.Minute + 59*time.Second + 500*time.Millisecond)
	if got := s.Next(noisy); !got.Equal(base.Add(5 * time.Minute)) {
		t.Errorf("Next(noisy) = %v, want the next grid point", got)
	}
	// crossing hours and days on uneven intervals
	cases := []struct {
		minutes int
		from    time.Time
		want    time.Time
	}{
		{60, base.Add(59 * time.Minute), base.Add(time.Hour)},
		{1440, base.Add(23 * time.Hour), base.Add(24 * time.Hour)},
	}
	for _, c := range cases {
		got := everySchedule{minutes: c.minutes}.Next(c.from)
		if !got.Equal(c.want) {
			t.Errorf("Next(%dm from %v) = %v, want %v", c.minutes, c.from, got, c.want)
		}
	}
}

func TestScheduleForKinds(t *testing.T) {
	cronSched, err := scheduleFor(Job{Name: "x", Schedule: "0 8 * * *"})
	if err != nil {
		t.Fatal(err)
	}
	intSched, err := scheduleFor(Job{Name: "x", Kind: kindInterval, Minutes: 5})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Minute).Add(30 * time.Second)
	if got := intSched.Next(now); got.Sub(now) > 5*time.Minute || got.Before(now.Add(time.Second)) {
		t.Errorf("interval Next = %v from %v, want a point <= 5m ahead", got, now)
	}
	if next := cronSched.Next(now); next.Before(now) {
		t.Errorf("cron schedule broken: %v before %v", next, now)
	}
}

func TestIntervalJobCatchUpFiresOnGrid(t *testing.T) {
	r := newTestRunner(t, t.TempDir(), Job{Name: "pulse", Kind: kindInterval, Minutes: 10, Prompt: "p"})
	fired := make(chan struct{}, 4)
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		fired <- struct{}{}
		return "done", nil
	}
	// evaluate the catch-up from a point OFF the 10-minute grid (the
	// runner's fixed `now`, on whole hours, sits exactly on it)
	now := r.now().Add(-4 * time.Minute)
	// last fire 25 minutes ago: one grid point was missed
	r.lastFire["pulse"] = now.Add(-25 * time.Minute).Unix()
	r.catchUp(context.Background(), now)
	select {
	case <-fired:
	default:
		t.Fatal("catch-up did not fire the interval job")
	}
	// the recorded fire is the missed grid time: a restart before the
	// next point does not fire again
	last, _ := r.lastFireOf("pulse")
	if last >= now.Unix() {
		t.Fatalf("catch-up recorded %d, want the missed grid time < %d", last, now.Unix())
	}
	r.catchUp(context.Background(), now)
	select {
	case <-fired:
		t.Fatal("double catch-up fired")
	default:
	}
}

func TestIntervalJobNextFireInInspector(t *testing.T) {
	r := newTestRunner(t, t.TempDir(), Job{Name: "pulse", Kind: kindInterval, Minutes: 10, Prompt: "p"})
	jobs := r.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("want 1 row, got %d", len(jobs))
	}
	row := jobs[0]
	if row.Kind != kindInterval || row.Minutes != 10 || row.Schedule != "" || row.Next == "" {
		t.Errorf("interval row fields wrong: %+v", row)
	}
	if row.Deliver != deliverNewSession || row.Agent != "" || row.Model != "" {
		t.Errorf("defaulting-law fields wrong: %+v", row)
	}
	off := false
	r.jobs = []Job{{Name: "pulse", Kind: kindInterval, Minutes: 10, Prompt: "p", Enabled: &off}}
	row = r.Jobs()[0]
	if row.Enabled || row.Next != "" {
		t.Errorf("disabled interval row still promises fires: %+v", row)
	}
}

// --- the defaulting law at fire time: model override ---

// streamSpy records the ModelName of the provider each Stream rides,
// into a recorder shared by every WithModel derivation - so the test
// sees both the inherited and the overridden stream against one log.
// WithModel derives a NEW spy around the copy (the copy discipline).
type modelLog struct {
	mu   sync.Mutex
	seen []string
}

func (m *modelLog) push(name string) {
	m.mu.Lock()
	m.seen = append(m.seen, name)
	m.mu.Unlock()
}

func (m *modelLog) last() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.seen) == 0 {
		return ""
	}
	return m.seen[len(m.seen)-1]
}

type streamSpy struct {
	inner core.Provider
	log   *modelLog
}

func (s *streamSpy) Stream(ctx context.Context, msgs []core.Message, tools []core.ToolSchema) <-chan core.ProviderStreamEvent {
	s.log.push(s.inner.ModelName())
	return s.inner.Stream(ctx, msgs, tools)
}
func (s *streamSpy) ModelName() string         { return s.inner.ModelName() }
func (s *streamSpy) SetThinkingLevel(l string) { s.inner.SetThinkingLevel(l) }
func (s *streamSpy) WithModel(m string) core.Provider {
	return &streamSpy{inner: s.inner.WithModel(m), log: s.log}
}
func (s *streamSpy) WithThinkingLevel(l string) core.Provider { return s.inner.WithThinkingLevel(l) }
func (s *streamSpy) ListModels() ([]string, error)            { return s.inner.ListModels() }
func (s *streamSpy) ModelInfo() (core.ModelInfo, error)       { return s.inner.ModelInfo() }

func TestFireModelOverrideAndInherit(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	spawner := &streamSpy{
		inner: llm.NewFakeProviderScripts("session-model", []core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "done"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		}),
		log: &modelLog{},
	}
	r := newTestRunner(t, t.TempDir(), Job{Name: "m", Schedule: "0 8 * * *", Prompt: "p"})
	r.store = st
	r.ag = core.NewAgent(spawner, nil, 0)
	r.ag.SetLoopProvider(agent_loop.Loop{})

	// the defaulting law, model side: empty model = the session
	// provider model - the base provider streams, no derivation
	if _, err := r.agentTurn(context.Background(), "s1", Job{Name: "m", Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if got := spawner.log.last(); got != "session-model" {
		t.Fatalf("inherited model = %q, want session-model", got)
	}
	// a named model derives a per-run provider via WithModel
	if _, err := r.agentTurn(context.Background(), "s2", Job{Name: "m", Prompt: "p", Model: "cron-model"}); err != nil {
		t.Fatal(err)
	}
	// the derived spy sees the override; the base provider still
	// reports the session model afterwards (no shared-state mutation)
	for i := 0; i < 200; i++ {
		if got := spawner.log.last(); got == "cron-model" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := spawner.log.last(); got != "cron-model" {
		t.Fatalf("override model = %q, want cron-model via RunWithProvider", got)
	}
	if got := spawner.inner.ModelName(); got != "session-model" {
		t.Fatalf("session provider model mutated: %q", got)
	}
}

// --- wake-target dispatch ---

func TestWakeTickCarriesModel(t *testing.T) {
	var got wakeTickPayload
	r := newTestRunner(t, t.TempDir())
	r.tickEmit = func(p wakeTickPayload) { got = p }
	job := Job{Name: "w", Schedule: "0 8 * * *", Prompt: "p", Model: "wake-model", Enabled: boolPtr()}
	r.fireAgentWake(job, "scout", "w")
	if got.Agent != "scout" || got.Job != "w" || got.Prompt != "p" || got.Model != "wake-model" {
		t.Fatalf("wake payload wrong: %+v", got)
	}
}

func boolPtr() *bool {
	v := true
	return &v
}

func TestContinuousAgentTargetWakeAndBadAgent(t *testing.T) {
	rec := newLogRecorder()
	home := t.TempDir()
	if err := agent.ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	r := newTestRunner(t, home)
	r.logger = rec
	var ticks []wakeTickPayload
	r.tickEmit = func(p wakeTickPayload) { ticks = append(ticks, p) }
	ctx := context.Background()

	// a main-home job aimed at a continuous agent: the wake fires
	r.fire(ctx, Job{Name: "patrol", Schedule: "0 8 * * *", Prompt: "go", Deliver: deliverContinuous, Agent: "scout", Enabled: boolPtr()}, r.now())
	if len(ticks) != 1 || ticks[0].Agent != "scout" {
		t.Fatalf("continuous-agent fire produced %d ticks, want one aimed at scout: %+v", len(ticks), ticks)
	}

	// unknown agent: clear error in the job log, scheduler standing
	r.fire(ctx, Job{Name: "ghost", Schedule: "0 8 * * *", Prompt: "go", Deliver: deliverContinuous, Agent: "nobody-here", Enabled: boolPtr()}, r.now())
	if len(ticks) != 1 {
		t.Fatalf("a bad agent wake must not emit a tick: %+v", ticks)
	}
	if !rec.waitContains("unknown agent", 2*time.Second) || !rec.waitContains("nobody-here", 2*time.Second) {
		t.Errorf("bad agent error not logged clearly: %v", rec.snapshot())
	}

	// an agent with no continuous home (main): same clear-error lane
	r.fire(ctx, Job{Name: "main-wake", Schedule: "0 8 * * *", Prompt: "go", Deliver: deliverContinuous, Enabled: boolPtr()}, r.now())
	if len(ticks) != 1 {
		t.Fatalf("the default agent must not wake as a fire: %+v", ticks)
	}
	if !rec.waitContains("no continuous session", 2*time.Second) {
		t.Errorf("non-continuous agent error not logged: %v", rec.snapshot())
	}

	// an in-process deliver with a non-default agent: clear error,
	// never a wrong-persona run
	r.fire(ctx, Job{Name: "misplaced", Schedule: "0 8 * * *", Prompt: "go", Deliver: deliverNewSession, Agent: "scout", Enabled: boolPtr()}, r.now())
	if !rec.waitContains("runs in-process as the default agent", 2*time.Second) {
		t.Errorf("wrong-target error not logged: %v", rec.snapshot())
	}

	// in-process fires with an EMPTY agent field just run: the
	// defaulting law's silent inheritance case
	r.store = nil
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) { return "ok", nil }
	r.fire(ctx, Job{Name: "plain", Schedule: "0 8 * * *", Prompt: "go", Enabled: boolPtr()}, r.now())
}

func TestAppendToFileDeliver(t *testing.T) {
	home := t.TempDir()
	r := newTestRunner(t, home)
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) { return "the report", nil }
	ctx := context.Background()
	when := r.now()
	r.fire(ctx, Job{Name: "filer", Schedule: "0 8 * * *", Prompt: "p", Deliver: deliverAppendToFile, File: "notes/report.md", Enabled: boolPtr()}, when)
	text, err := os.ReadFile(filepath.Join(home, "notes", "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "## filer - ") || !strings.Contains(string(text), "the report") {
		t.Errorf("file appendix wrong: %q", text)
	}
	// the legacy journal alias: empty file = the journal/date naming
	r.fire(ctx, Job{Name: "journaled", Schedule: "0 8 * * *", Prompt: "p", Deliver: deliverJournal, Enabled: boolPtr()}, when)
	jt, err := os.ReadFile(filepath.Join(home, "journal", when.Format("2006-01-02")+".md"))
	if err != nil {
		t.Fatalf("journal alias must keep the journal naming: %v", err)
	}
	if !strings.Contains(string(jt), "## journaled - ") {
		t.Errorf("journal entry wrong: %q", jt)
	}
}
