package cron

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseConfigValid(t *testing.T) {
	raw := `
jobs:
  - name: daily-journal
    schedule: "0 8 * * *"
    prompt: "Summarize yesterday."
    deliver: journal
    enabled: true
  - name: backup
    schedule: "0 3 * * *"
    prompt: "Back up files."
  - name: quiet
    schedule: "30 4 * * 0"
    prompt: "Reflect."
    enabled: false
`
	cfg, err := ParseConfig([]byte(raw))
	if err != nil {
		t.Fatalf("valid yaml rejected: %v", err)
	}
	if len(cfg.Jobs) != 3 {
		t.Fatalf("want 3 jobs, got %d", len(cfg.Jobs))
	}
	j := cfg.Jobs[0]
	if j.Name != "daily-journal" || j.Schedule != "0 8 * * *" || j.DeliverTarget() != "journal" || !j.IsEnabled() {
		t.Fatalf("job 0 misparsed: %+v", j)
	}
	// Defaults: deliver none, enabled true.
	d := cfg.Jobs[1]
	if d.DeliverTarget() != "none" || !d.IsEnabled() {
		t.Fatalf("defaults wrong: %+v", d)
	}
	// enabled: false honored.
	q := cfg.Jobs[2]
	if q.IsEnabled() {
		t.Fatalf("enabled: false not honored: %+v", q)
	}
}

func TestParseConfigEmpty(t *testing.T) {
	for _, raw := range []string{"", "\n", "jobs: null"} {
		cfg, err := ParseConfig([]byte(raw))
		if err != nil {
			t.Fatalf("empty %q rejected: %v", raw, err)
		}
		if len(cfg.Jobs) != 0 {
			t.Fatalf("want 0 jobs, got %d", len(cfg.Jobs))
		}
	}
}

func TestParseConfigBadScheduleNamesJobAndLine(t *testing.T) {
	raw := `
jobs:
  - name: nightly
    schedule: "not a cron"
    prompt: "hi"
`
	_, err := ParseConfig([]byte(raw))
	if err == nil {
		t.Fatal("bad schedule accepted")
	}
	if !strings.Contains(err.Error(), `job "nightly"`) || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("error missing job name + line: %v", err)
	}
}

func TestParseConfigBadName(t *testing.T) {
	raw := `
jobs:
  - name: Bad Name!
    schedule: "0 8 * * *"
    prompt: "hi"
`
	_, err := ParseConfig([]byte(raw))
	if err == nil {
		t.Fatal("bad name accepted")
	}
	if !strings.Contains(err.Error(), `job "Bad Name!"`) {
		t.Fatalf("error missing job name: %v", err)
	}
}

func TestParseConfigBadDeliver(t *testing.T) {
	raw := `
jobs:
  - name: news
    schedule: "0 8 * * *"
    prompt: "hi"
    deliver: weekly
`
	_, err := ParseConfig([]byte(raw))
	if err == nil {
		t.Fatal("bad deliver accepted")
	}
	if !strings.Contains(err.Error(), `job "news"`) || !strings.Contains(err.Error(), "deliver") {
		t.Fatalf("error missing job name + deliver: %v", err)
	}
}

func TestParseConfigMissingFields(t *testing.T) {
	raw := `
jobs:
  - schedule: "0 8 * * *"
    prompt: "no name here"
`
	_, err := ParseConfig([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "missing a name") {
		t.Fatalf("missing name not rejected: %v", err)
	}
}

func TestParseConfigDuplicateNames(t *testing.T) {
	raw := `
jobs:
  - name: twice
    schedule: "0 8 * * *"
    prompt: "first"
  - name: other
    schedule: "0 9 * * *"
    prompt: "filler"
  - name: twice
    schedule: "0 10 * * *"
    prompt: "second"
`
	_, err := ParseConfig([]byte(raw))
	if err == nil {
		t.Fatal("duplicate names accepted")
	}
	if !strings.Contains(err.Error(), "duplicate job name \"twice\"") ||
		!strings.Contains(err.Error(), "lines 3 and 9") {
		t.Fatalf("duplicate error missing both lines: %v", err)
	}
}

func TestParseConfigUnknownField(t *testing.T) {
	raw := `
jobs:
  - name: typo
    schedule: "0 8 * * *"
    prompt: "hi"
    typoo: surprise
`
	_, err := ParseConfig([]byte(raw))
	if err == nil {
		t.Fatal("unknown field accepted")
	}
	if !strings.Contains(err.Error(), "unknown field \"typoo\"") || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("unknown field error missing context: %v", err)
	}
}

func TestParseConfigUnknownTopLevelField(t *testing.T) {
	_, err := ParseConfig([]byte("nope: 1\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown top-level field not rejected: %v", err)
	}
}

func TestParseConfigDescriptorsAndSecondsRejected(t *testing.T) {
	for _, s := range []string{"@daily", "*/10 * * * * *", "30 4 * * 0 *", "* * *"} {
		raw := "jobs:\n  - name: x\n    schedule: \"" + s + "\"\n    prompt: hi\n"
		if _, err := ParseConfig([]byte(raw)); err == nil {
			t.Fatalf("schedule %q accepted (seconds/descriptors must be rejected)", s)
		}
	}
}

func TestSchedulePrev(t *testing.T) {
	sched, err := scheduleParser.Parse("30 7 * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := mustLocal(2026, 10, 17, 10, 0) // Saturday
	prev := schedulePrev(sched, now)
	if prev.IsZero() {
		t.Fatal("no previous occurrence found")
	}
	want := mustLocal(2026, 10, 17, 7, 30) // same day 07:30
	if !prev.Equal(want) {
		t.Fatalf("prev = %s, want %s", prev, want)
	}
}

func TestSchedulePrevNoOccurrence(t *testing.T) {
	// Feb 29 schedule: no occurrence in the last 7 days of Oct.
	sched, err := scheduleParser.Parse("0 3 29 2 *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := mustLocal(2026, 10, 17, 10, 0)
	if !schedulePrev(sched, now).IsZero() {
		t.Fatal("expected zero prev for a schedule with no recent occurrence")
	}
}

// mustLocal builds a fixed time.Date in the local zone (schedules
// evaluate in server local time).
func mustLocal(y, m, d, hh, mm int) time.Time {
	return time.Date(y, time.Month(m), d, hh, mm, 0, 0, time.Local)
}

// TestHomeFileRootWins verifies the legacy home-root crons.yaml is
// preferred when present (existing installs keep working unchanged).
func TestHomeFileRootWins(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "crons.yaml"), []byte("subdir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "crons.yaml"), []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := homeFile(home); got != filepath.Join(home, "crons.yaml") {
		t.Fatalf("homeFile = %q, want root (legacy wins)", got)
	}
}

// TestHomeFileConfigSubdirFallback verifies crons.yaml in <home>/config/
// (the Endo 2026-10-18 layout) is used when nothing is at the home root.
func TestHomeFileConfigSubdirFallback(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "crons.yaml"), []byte("subdir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := homeFile(home); got != filepath.Join(home, "config", "crons.yaml") {
		t.Fatalf("homeFile = %q, want config/ fallback", got)
	}
}

// TestHomeFileMissingReturnsConfigPath verifies that when crons.yaml is
// absent everywhere, homeFile points at the config/ location (the write
// target for new installs) - never the legacy root.
func TestHomeFileMissingReturnsConfigPath(t *testing.T) {
	home := t.TempDir()
	if got := homeFile(home); got != filepath.Join(home, "config", "crons.yaml") {
		t.Fatalf("homeFile = %q, want config/ target when absent", got)
	}
}