package cron

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
	agent_loop "github.com/EndoTheDev/omega/modules/agent_loop"
	"github.com/EndoTheDev/omega/modules/store"
)

// newTestRunner builds a Runner with stubbed time and turn hooks,
// pointed at a temp home dir.
func newTestRunner(t *testing.T, home string, jobs ...Job) *Runner {
	t.Helper()
	r := NewRunner(&Config{Jobs: jobs}, home)
	r.now = func() time.Time { return mustLocal(2026, 10, 17, 10, 0) }
	return r
}

// --- test doubles ---

// fakeMemory records Add calls (core.MemoryProvider).
type fakeMemory struct {
	mu    sync.Mutex
	added []string
}

func (f *fakeMemory) Snapshot() string { return "" }
func (f *fakeMemory) Add(target, content string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, target+":"+content)
	return "", nil
}
func (f *fakeMemory) Replace(target, oldText, content string) (string, error) { return "", nil }
func (f *fakeMemory) Remove(target, oldText string) (string, error)           { return "", nil }
func (f *fakeMemory) List(target string) ([]string, error)                    { return nil, nil }

// --- guard (single instance per job) ---

func TestFireGuardSkipsConcurrentSameJobOthersStillRun(t *testing.T) {
	r := newTestRunner(t, t.TempDir(),
		Job{Name: "slow", Schedule: "0 8 * * *", Prompt: "p"},
		Job{Name: "fast", Schedule: "0 9 * * *", Prompt: "p"},
	)

	// runTurn blocks for "slow" until released.
	entered := make(chan string, 4)
	release := make(chan struct{})
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		entered <- job.Name
		<-release
		return "done " + job.Name, nil
	}

	ctx := context.Background()
	var fired sync.WaitGroup
	fired.Add(1)
	go func() { // slow begins
		defer fired.Done()
		r.fire(ctx, r.jobsSnapshot()[0], time.Now())
	}()
	if got := <-entered; got != "slow" {
		t.Fatalf("first fire = %s, want slow", got)
	}

	// Second fire of the same job skips, logged, never queued.
	r.fire(ctx, r.jobsSnapshot()[0], time.Now())

	// Other jobs still fire in parallel while slow runs.
	fired.Add(1)
	go func() {
		defer fired.Done()
		r.fire(ctx, r.jobsSnapshot()[1], time.Now())
	}()
	if got := <-entered; got != "fast" {
		t.Fatalf("parallel fire = %s, want fast", got)
	}

	close(release)
	// Fires write cronstate.json into the TempDir; wait them out so
	// cleanup never races a straggler write.
	fired.Wait()
}

func TestFireGuardLogsSkip(t *testing.T) {
	rec := newLogRecorder()
	home := t.TempDir()
	r := newTestRunner(t, home, Job{Name: "busy", Schedule: "0 8 * * *", Prompt: "p"})
	r.logger = rec

	release := make(chan struct{})
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		<-release
		return "ok", nil
	}
	ctx := context.Background()
	go r.fire(ctx, r.jobsSnapshot()[0], time.Now())

	// Wait until the job is actually running (guard held), then a
	// synchronous fire must skip without ever entering runTurn.
	r.mu.Lock()
	for !r.running["busy"] {
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		r.mu.Lock()
	}
	r.mu.Unlock()
	r.fire(ctx, r.jobsSnapshot()[0], time.Now())

	if !rec.waitContains("skipped, previous fire still running", 2*time.Second) {
		t.Fatalf("skip not logged: %v", rec.snapshot())
	}
	close(release)
}

// logRecorder is a race-safe LoggerProvider.
type logRecorder struct {
	mu   sync.Mutex
	logs []string
}

func newLogRecorder() *logRecorder { return &logRecorder{} }

func (l *logRecorder) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, fmt.Sprintf(format, args...))
}
func (l *logRecorder) Errorf(format string, args ...any) { l.Printf(format, args...) }
func (l *logRecorder) Close() error                      { return nil }
func (l *logRecorder) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.logs...)
}
func (l *logRecorder) waitContains(sub string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		for _, line := range l.snapshot() {
			if strings.Contains(line, sub) {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// --- catch-up ---

func TestCatchUpFiresOnceWithin24h(t *testing.T) {
	home := t.TempDir()
	r := newTestRunner(t, home, Job{Name: "daily", Schedule: "30 7 * * *", Prompt: "p"})

	// Last fire 26.5h ago; the scheduled 07:30 today was missed 2.5h ago.
	last := mustLocal(2026, 10, 16, 4, 0)
	r.lastFire["daily"] = last.Unix()

	var fired []time.Time
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		fired = append(fired, r.now())
		return "ok", nil
	}

	now := r.now()
	r.catchUp(context.Background(), now)
	if len(fired) != 1 {
		t.Fatalf("catch-up fired %d times, want 1", len(fired))
	}

	prev := mustLocal(2026, 10, 17, 7, 30)
	got, _ := r.lastFireOf("daily")
	if got != prev.Unix() {
		t.Fatalf("state = %d, want missed scheduled time %d", got, prev.Unix())
	}

	// Restart before the next scheduled fire: no second catch-up.
	r.catchUp(context.Background(), now)
	if len(fired) != 1 {
		t.Fatalf("second catch-up fired again (%d total), want 1", len(fired))
	}
}

func TestCatchUpSkipsOlderThan24h(t *testing.T) {
	home := t.TempDir()
	r := newTestRunner(t, home, Job{Name: "weekly", Schedule: "0 9 * * 0", Prompt: "p"})

	// Last fire 8 days ago; most recent Sunday 09:00 is ~6 days back.
	last := mustLocal(2026, 10, 9, 9, 0)
	r.lastFire["weekly"] = last.Unix()

	fired := false
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		fired = true
		return "ok", nil
	}
	r.catchUp(context.Background(), r.now())
	if fired {
		t.Fatal("missed fire older than 24h fired at startup")
	}
}

func TestCatchUpNeverFiresDisabledJob(t *testing.T) {
	home := t.TempDir()
	disabled := Job{Name: "off", Schedule: "30 7 * * *", Prompt: "p"}
	off := false
	disabled.Enabled = &off
	r := newTestRunner(t, home, disabled)
	r.lastFire["off"] = mustLocal(2026, 10, 16, 4, 0).Unix()

	fired := false
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		fired = true
		return "ok", nil
	}
	r.catchUp(context.Background(), r.now())
	if fired {
		t.Fatal("disabled job catch-up fired")
	}
}

func TestCatchUpSkipsNeverFiredJob(t *testing.T) {
	r := newTestRunner(t, t.TempDir(), Job{Name: "fresh", Schedule: "30 7 * * *", Prompt: "p"})
	fired := false
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		fired = true
		return "ok", nil
	}
	r.catchUp(context.Background(), r.now())
	if fired {
		t.Fatal("job with no recorded last fire catch-up fired")
	}
}

// --- regular fire + state ---

func TestFirePersistsSessionAndState(t *testing.T) {
	home := t.TempDir()
	r := newTestRunner(t, home, Job{Name: "jt", Schedule: "0 8 * * *", Prompt: "hello", Deliver: "none"})
	st, err := store.Open(filepath.Join(home, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	r.store = st

	// The real loop persists the turn copies now (t_471f6d4a): run the
	// default agentTurn against a scripted provider - one tool turn,
	// then the final answer.
	script := []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response_chunk", Content: "thinking"},
		core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "nop"}},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
	final := []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response_chunk", Content: "final answer"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
	// WithDelay spaces the scripted events so the phase meters measure
	// real wall-clock - the zero-delay fake floors every span to 0.
	r.ag = core.NewAgent(ai.NewFakeProviderScripts("test", script, final).WithDelay(5*time.Millisecond), map[string]core.Tool{
		"nop": {
			Description: "test no-op",
			Run:         func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
		},
	}, 0)
	r.ag.SetLoopProvider(agent_loop.Loop{})

	fireTime := r.now()
	r.fire(context.Background(), r.jobsSnapshot()[0], fireTime)

	sessions, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if sessions[0].Label != "cron:jt" {
		t.Fatalf("label = %q, want cron:jt", sessions[0].Label)
	}
	msgs, err := st.GetMessages(context.Background(), sessions[0].ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	// Provider-correct order: user -> assistant(A + calls) -> tool
	// result -> assistant(B).
	if len(msgs) != 4 {
		t.Fatalf("want prompt+assistant+result+answer persisted, got %d messages", len(msgs))
	}
	if u, ok := msgs[0].(core.User); !ok || u.Content != "hello" {
		t.Fatalf("first message not the prompt: %v", msgs[0])
	}
	a, isAssistant := msgs[1].(core.Assistant)
	if !isAssistant || len(a.ToolCalls) != 1 {
		t.Fatalf("second message not the assistant-with-calls: %v", msgs[1])
	}
	if _, ok := msgs[2].(core.ToolResult); !ok {
		t.Fatalf("third message not the tool result: %v", msgs[2])
	}
	if b, ok := msgs[3].(core.Assistant); !ok || agent.MessageText(b) != "final answer" {
		t.Fatalf("fourth message not the answer: %v", msgs[3])
	}
	if a.Timing == nil {
		t.Fatalf("assistant-with-calls carries no timing")
	}

	if got, _ := r.lastFireOf("jt"); got != fireTime.Unix() {
		t.Fatalf("state not persisted: %d != %d", got, fireTime.Unix())
	}
	if _, err := os.Stat(filepath.Join(home, "cronstate.json")); err != nil {
		t.Fatalf("cronstate.json not written: %v", err)
	}
}

// --- deliver targets ---

func TestDeliverJournalCreatesFileAndAppends(t *testing.T) {
	home := t.TempDir()
	r := newTestRunner(t, home, Job{Name: "daily-journal", Schedule: "0 8 * * *", Prompt: "p", Deliver: "journal"})
	r.now = func() time.Time { return mustLocal(2026, 10, 17, 8, 0) }
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		return "journal body", nil
	}
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())

	path := filepath.Join(home, "journal", "2026-10-17.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("journal file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "## daily-journal - 2026-10-17 08:00") || !strings.Contains(text, "journal body") {
		t.Fatalf("journal content wrong: %q", text)
	}
	// Second fire appends, does not overwrite.
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		return "second entry", nil
	}
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())
	data, _ = os.ReadFile(path)
	text = string(data)
	if !strings.Contains(text, "journal body") || !strings.Contains(text, "second entry") {
		t.Fatalf("second fire not appended: %q", text)
	}
}

func TestDeliverMemoryStoresFinalViaMemorySeam(t *testing.T) {
	r := newTestRunner(t, t.TempDir(), Job{Name: "m", Schedule: "0 8 * * *", Prompt: "p", Deliver: "memory"})
	fm := &fakeMemory{}
	r.memory = fm
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		return "memory note", nil
	}
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if len(fm.added) != 1 || fm.added[0] != "memory:memory note" {
		t.Fatalf("memory deliver wrong: %v", fm.added)
	}
}

func TestDeliverNoneWritesNothing(t *testing.T) {
	home := t.TempDir()
	r := newTestRunner(t, home, Job{Name: "n", Schedule: "0 8 * * *", Prompt: "p"})
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		return "ignored", nil
	}
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())
	if _, err := os.Stat(filepath.Join(home, "journal")); !os.IsNotExist(err) {
		t.Fatalf("journal dir created under deliver none: %v", err)
	}
}

func TestDeliverSkipsEmptyFinal(t *testing.T) {
	home := t.TempDir()
	r := newTestRunner(t, home, Job{Name: "e", Schedule: "0 8 * * *", Prompt: "p", Deliver: "journal"})
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		return "", fmt.Errorf("provider down")
	}
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())
	if _, err := os.Stat(filepath.Join(home, "journal")); !os.IsNotExist(err) {
		t.Fatalf("journal written for empty final message: %v", err)
	}
}

// --- hot reload ---

func TestHotReloadAddsAndDisablesWithoutRestart(t *testing.T) {
	home := t.TempDir()
	r := newTestRunner(t, home, Job{Name: "starter", Schedule: "0 5 1 * *", Prompt: "p"})
	cfgPath := filepath.Join(home, "crons.yaml")
	if err := os.WriteFile(cfgPath, []byte("jobs:\n  - name: starter\n    schedule: \"0 5 1 * *\"\n    prompt: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.pollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var watcher sync.WaitGroup
	watcher.Add(1)
	go func() {
		defer watcher.Done()
		r.watchJobsFile(ctx)
	}()
	defer func() {
		cancel()
		watcher.Wait()
	}()

	// Add a job + disable starter: one reload carries both changes.
	updated := `
jobs:
  - name: starter
    schedule: "0 5 1 * *"
    prompt: p
    enabled: false
  - name: added
    schedule: "0 6 1 * *"
    prompt: p
`
	waitFor(t, 3*time.Second, func() bool {
		return contains(r.scheduledNames(), "added") && !contains(r.scheduledNames(), "starter")
	}, func() error {
		if err := os.WriteFile(cfgPath, []byte(updated), 0o644); err != nil {
			return err
		}
		return nil
	})

	names := r.scheduledNames()
	if len(names) != 1 || names[0] != "added" {
		t.Fatalf("scheduled = %v, want [added]", names)
	}

	// Invalid yaml keeps the previous job set.
	if err := os.WriteFile(cfgPath, []byte("jobs:\n  - name: broken\n    schedule: \"oops\"\n    prompt: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if got := r.scheduledNames(); len(got) != 1 || got[0] != "added" {
		t.Fatalf("failed reload dropped previous jobs: %v", got)
	}
	if jobs := r.jobsSnapshot(); len(jobs) != 2 || jobs[0].Name != "starter" {
		t.Fatalf("internal job set wrongly replaced: %v", jobNames(jobs))
	}
}

func jobNames(jobs []Job) []string {
	out := make([]string, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.Name)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// waitFor retries cond until true, retrying the (idempotent) trigger
// each interval; on timeout it fails with the last cond state.
func waitFor(t *testing.T, limit time.Duration, cond func() bool, trigger func() error) {
	t.Helper()
	deadline := time.Now().Add(limit)
	_ = trigger()
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		_ = trigger()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition not reached within %s", limit)
}

// --- plugin mount ---

// notificationsPlugin provides the presence the cron Requires edge
// declares (Q3): the runner's notifier arrives through CronDeps.Ctx at
// Start, but the mount pins need the seam provided to mount at all.
type notificationsPlugin struct{}

func (notificationsPlugin) Name() string                                                  { return "notifications" }
func (notificationsPlugin) Provides() []string                                            { return []string{"notifications"} }
func (notificationsPlugin) Requires() []string                                            { return nil }
func (notificationsPlugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error { return nil }

// TestPluginMountSetsCronSeam pins the api mount (the only mount since
// the v0.12 phase-C2 migration).
func TestPluginMountSetsCronSeam(t *testing.T) {
	p := NewPluginWithConfig(&Config{Jobs: []Job{{Name: "a", Schedule: "0 8 * * *", Prompt: "p"}}}, t.TempDir())
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{notificationsPlugin{}, p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if ctx.Cron == nil {
		t.Fatal("ctx.Cron not populated")
	}
	if p.Provides()[0] != "cron" {
		t.Fatalf("provides = %v", p.Provides())
	}
}

func TestPluginMountInvalidConfigIsError(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "crons.yaml"), []byte("jobs:\n  - name: bad\n    schedule: \"oops\"\n    prompt: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := NewPluginWithConfig(nil, home)
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{notificationsPlugin{}, p}, ctx, nil, nil); err == nil {
		t.Fatal("invalid crons.yaml mounted silently")
	} else if !strings.Contains(err.Error(), `job "bad"`) {
		t.Fatalf("mount error missing job name: %v", err)
	}
}

// --- inspector snapshot (card t_db430fea) ---

// enabledOff returns the disabled flag a crons.yaml `enabled: false`
// decodes to.
func enabledOff() *bool {
	b := false
	return &b
}

// Compile-time proof that the Runner satisfies the read-only inspector
// seam the http_channel asserts.
var _ core.CronLister = (*Runner)(nil)

func TestJobsInspectorSnapshot(t *testing.T) {
	r := newTestRunner(t, t.TempDir(),
		Job{Name: "zz-daily", Schedule: "0 8 * * *", Prompt: "p"},
		Job{Name: "aa-minute", Schedule: "*/5 * * * *", Prompt: "p", Enabled: enabledOff()},
	)
	jobs := r.Jobs()
	if len(jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(jobs))
	}
	// sorted by name regardless of config order
	if jobs[0].Name != "aa-minute" || jobs[1].Name != "zz-daily" {
		t.Fatalf("order = %s,%s, want aa-minute,zz-daily", jobs[0].Name, jobs[1].Name)
	}
	// disabled job: Enabled false, no next fire promised
	if jobs[0].Enabled || jobs[0].Next != "" {
		t.Fatalf("disabled job = %+v", jobs[0])
	}
	// enabled job: next fire parses as RFC3339 and is after now
	if !jobs[1].Enabled {
		t.Fatalf("enabled job flag missing: %+v", jobs[1])
	}
	next, err := time.Parse(time.RFC3339, jobs[1].Next)
	if err != nil {
		t.Fatalf("next not RFC3339: %q", jobs[1].Next)
	}
	if !next.After(mustLocal(2026, 10, 17, 10, 0)) {
		t.Fatalf("next %s not after now", jobs[1].Next)
	}
}

func TestJobsInspectorEmptyConfig(t *testing.T) {
	r := newTestRunner(t, t.TempDir())
	jobs := r.Jobs()
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v, want empty", jobs)
	}
}
