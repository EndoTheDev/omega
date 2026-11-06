package cron

// status_test.go: the capsule round's two extensions (card t_3885c975).
// - the per-job LAST STATUS: every fire return path lands an outcome in
//   cronstate.json, Jobs() carries it, the record survives a restart.
// - the per-job SKILLS: the editor pins names, the fire injects the
//   bodies ahead of the prompt, an unpinnable name is a fire-time
//   error (the bad-agent posture).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// fakeSkills is the skills seam double: reads from a map, errors on
// the unknown names.
type fakeSkills struct{ files map[string]string }

func (f fakeSkills) SkillRead(name string) (description, content string, err error) {
	if c, ok := f.files[name]; ok {
		return "desc " + name, c, nil
	}
	return "", "", errors.New("no such skill")
}

func TestFireOutcomeOkThenError(t *testing.T) {
	home := t.TempDir()
	r := newTestRunner(t, home, Job{Name: "pulse", Schedule: "0 8 * * *", Prompt: "p"})
	runErr := errors.New("provider exploded")
	r.runTurn = func(ctx context.Context, sessionID string, job Job, prompt string) (string, error) {
		return "final", runErr
	}
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())
	row := r.Jobs()[0]
	if row.Status != "error" {
		t.Fatalf("status = %q, want error", row.Status)
	}
	if !strings.Contains(row.StatusNote, "provider exploded") {
		t.Errorf("status note = %q, want the error message", row.StatusNote)
	}
	if row.Last == "" {
		t.Error("last completion empty on a recorded fire")
	}

	// The next fire runs clean: the status flips, the note is gone.
	r.runTurn = func(ctx context.Context, sessionID string, job Job, prompt string) (string, error) {
		return "fine", nil
	}
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())
	row = r.Jobs()[0]
	if row.Status != "ok" || row.StatusNote != "" || row.Last == "" {
		t.Errorf("clean fire = %+v, want ok/empty note/timestamp", row)
	}
}

func TestFireOutcomeSurvivesRestart(t *testing.T) {
	home := t.TempDir()
	r := newTestRunner(t, home, Job{Name: "pulse", Schedule: "0 8 * * *", Prompt: "p"})
	r.runTurn = func(ctx context.Context, sessionID string, job Job, prompt string) (string, error) {
		return "", errors.New("hung turn killed")
	}
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())

	// A fresh runner (a restarted serve) reads the same cronstate.json:
	// the status is a promise the process can keep across restarts.
	fresh := newTestRunner(t, home, Job{Name: "pulse", Schedule: "0 8 * * *", Prompt: "p"})
	fresh.loadCronState()
	row := fresh.Jobs()[0]
	if row.Status != "error" || !strings.Contains(row.StatusNote, "hung turn killed") {
		t.Errorf("restarted status = %+v, want error naming the failure", row)
	}
}

func TestWakeOutcomeRidesTheTick(t *testing.T) {
	home := t.TempDir()
	job := Job{Name: "wake", Schedule: "17 * * * *", Prompt: "patrol", AgentOwned: true, Agent: "scout"}
	r := newTestRunner(t, home, job)

	// No tick emitter (continuous off): inert wake = error status.
	r.fire(context.Background(), job, r.now())
	if row := r.Jobs()[0]; row.Status != "error" || !strings.Contains(row.StatusNote, "continuous not mounted") {
		t.Errorf("inert wake = %+v, want an error status", row)
	}

	// With the spine wired, the same job fires clean.
	emitted := 0
	r.tickEmit = func(p wakeTickPayload) { emitted++ }
	r.fire(context.Background(), job, r.now())
	if emitted != 1 {
		t.Fatalf("wake ticks = %d, want 1", emitted)
	}
	if row := r.Jobs()[0]; row.Status != "ok" || row.StatusNote != "" {
		t.Errorf("wake = %+v, want ok", row)
	}
}

func TestPinnedSkillsRideTheFirePrompt(t *testing.T) {
	r := newTestRunner(t, t.TempDir(), Job{
		Name: "report", Schedule: "0 8 * * *", Prompt: "write the digest",
		Skills: []string{"omega-dev"},
	})
	r.skills = fakeSkills{files: map[string]string{"omega-dev": "the omega procedure\n"}}

	// The injection: the pinned body ahead of the prompt.
	got, err := r.pinnedPrompt(r.jobsSnapshot()[0])
	if err != nil {
		t.Fatalf("pinnedPrompt: %v", err)
	}
	if !strings.Contains(got, "<skill name=\"omega-dev\">") ||
		!strings.Contains(got, "the omega procedure") ||
		!strings.HasSuffix(got, "write the digest") {
		t.Errorf("pinned prompt = %q, want skill body ahead of the prompt", got)
	}
	// An unpinned job's prompt is byte-identical to the authored one.
	plain := newTestRunner(t, t.TempDir(), Job{Name: "plain", Schedule: "0 8 * * *", Prompt: "just the prompt"})
	got, err = r.pinnedPrompt(plain.jobsSnapshot()[0])
	if err != nil || got != "just the prompt" {
		t.Errorf("unpinned prompt = %q (%v), want the authored prompt", got, err)
	}
}

func TestPinnedSkillsUnknownNameIsAFireError(t *testing.T) {
	r := newTestRunner(t, t.TempDir(), Job{
		Name: "report", Schedule: "0 8 * * *", Prompt: "p",
		Skills: []string{"ghost-skill"},
	})
	turned := false
	r.runTurn = func(ctx context.Context, sessionID string, job Job, prompt string) (string, error) {
		turned = true
		return "x", nil
	}
	// The skills face is mounted, the name just is not there: no run
	// on a half-set table, the error lands in the status.
	r.skills = fakeSkills{files: map[string]string{}}
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())
	if turned {
		t.Error("the turn ran despite an unreadable pinned skill")
	}
	if row := r.Jobs()[0]; row.Status != "error" || !strings.Contains(row.StatusNote, "ghost-skill") {
		t.Errorf("status = %+v, want error naming the skill", row)
	}

	// The face itself is missing (skills module off): same loud error.
	r2 := newTestRunner(t, t.TempDir(), Job{Name: "r2", Schedule: "0 8 * * *", Prompt: "p", Skills: []string{"x"}})
	r2.runTurn = func(ctx context.Context, sessionID string, job Job, prompt string) (string, error) { return "x", nil }
	r2.fire(context.Background(), r2.jobsSnapshot()[0], r.now())
	if row := r2.Jobs()[0]; row.Status != "error" || !strings.Contains(row.StatusNote, "skills module is not mounted") {
		t.Errorf("no-face status = %+v, want the unmounted error", row)
	}
}

func TestSchemaSkillsValidation(t *testing.T) {
	// The happy path: names parse, trim and survive the round trip.
	cfg, err := ParseConfig([]byte("jobs:\n  - name: pinned\n    schedule: \"0 8 * * *\"\n    prompt: p\n    skills:\n      - omega-dev\n      - ponytail\n"))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if got := cfg.Jobs[0].Skills; len(got) != 2 || got[0] != "omega-dev" || got[1] != "ponytail" {
		t.Fatalf("skills = %v, want [omega-dev ponytail]", got)
	}
	for _, bad := range []string{
		"jobs:\n  - name: j\n    schedule: \"0 8 * * *\"\n    prompt: p\n    skills:\n      - Bad_Name\n",
		"jobs:\n  - name: j\n    schedule: \"0 8 * * *\"\n    prompt: p\n    skills:\n      - twice\n      - twice\n",
	} {
		if _, err := ParseConfig([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	// Empty entries trim away rather than reject - whitespace noise
	// from an editor form is not a schema crime.
	cfg, err = ParseConfig([]byte("jobs:\n  - name: tr\n    schedule: \"0 8 * * *\"\n    prompt: p\n    skills:\n      - \"\"\n      -  padded \n"))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if got := cfg.Jobs[0].Skills; len(got) != 1 || got[0] != "padded" {
		t.Fatalf("normalized skills = %v, want [padded]", got)
	}
}

func TestAgentHomeRejectsSkills(t *testing.T) {
	home := t.TempDir()
	if err := agent.ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	// A wake's fire prompt is the continuous capsule's property - an
	// authored skills list would parse and never take effect (the
	// silent-persist hole). Loud rejection, like deliver targets.
	if err := os.WriteFile(filepath.Join(home, "agents", "scout", "config", "crons.yaml"),
		[]byte("jobs:\n  - name: wake\n    schedule: \"17 * * * *\"\n    prompt: patrol\n    skills:\n      - omega-dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAllConfigs(home); err == nil {
		t.Fatal("agent job with pinned skills accepted")
	}
}

func TestJobsListingCarriesThePrompt(t *testing.T) {
	// t_3885c975 review fix: the editor drafts the WHOLE job from the
	// listing row, so the row must carry the authored prompt - an
	// empty-prompt draft dies at "prompt is required" and editing
	// forces a full re-type of the message.
	r := newTestRunner(t, t.TempDir(),
		Job{Name: "ed", Schedule: "0 12 * * *", Prompt: "the authored message"})
	if got := r.Jobs()[0].Prompt; got != "the authored message" {
		t.Fatalf("listing prompt = %q, want the authored prompt", got)
	}
}

func TestEditorSaveRoundTripsSkills(t *testing.T) {
	r := newTestRunner(t, t.TempDir(), Job{Name: "fresh", Schedule: "0 8 * * *", Prompt: "p"})
	jobs, err := r.SaveJob(Job{
		Name: "fresh", Schedule: "0 8 * * *", Prompt: "p",
		Skills:  []string{"omega-dev", "grill-me"},
		Enabled: boolPtr(),
	})
	if err != nil {
		t.Fatalf("SaveJob: %v", err)
	}
	if got := jobs[0].Skills; len(got) != 2 || got[0] != "omega-dev" {
		t.Fatalf("response skills = %v, want the authored list", got)
	}
	// Persisted on disk: a reload reads it back.
	cfg, err := LoadAllConfigs(r.home)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	fresh := newTestRunner(t, r.home, cfg.Jobs...)
	if got := fresh.jobsSnapshot()[0].Skills; len(got) != 2 {
		t.Fatalf("reloaded skills = %v, want the persisted list", got)
	}
	// A later edit without skills clears the list - the editor sends
	// the whole job, the upsert replaces the entry.
	jobs, err = r.SaveJob(Job{Name: "fresh", Schedule: "0 9 * * *", Prompt: "p", Enabled: boolPtr()})
	if err != nil {
		t.Fatalf("SaveJob #2: %v", err)
	}
	if got := jobs[0].Skills; len(got) != 0 {
		t.Fatalf("skills after clearing edit = %v, want empty", got)
	}
}

func TestCronTableRendersLastStatus(t *testing.T) {
	rows := []JobInfo{
		{Name: "a", Schedule: "0 8 * * *", Enabled: true},
		{Name: "b", Schedule: "0 9 * * *", Enabled: true, Status: "ok", Last: time.Date(2026, 11, 7, 2, 10, 0, 0, time.Local).Format(time.RFC3339)},
		{Name: "c", Schedule: "0 9 * * *", Enabled: true, Status: "error", Last: time.Date(2026, 11, 6, 23, 0, 0, 0, time.Local).Format(time.RFC3339)},
	}
	table := renderCronTable(rows)
	if !strings.Contains(table, "ok 11-07 02:10") {
		t.Errorf("table = %q, want the ok status with the local time", table)
	}
	if !strings.Contains(table, "error 11-06 23:00") {
		t.Errorf("table = %q, want the error status", table)
	}
	// The never-fired row keeps its dash - "-" means no fire, not an
	// unknown status.
	if !strings.Contains(table, "  -  ") {
		t.Errorf("table = %q, want a dash for the never-fired row", table)
	}
}
