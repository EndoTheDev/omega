package cron

// cli_test.go: the crons subcommand (card t_aca45258). The CLI rides
// Runner.SaveJob/DeleteJob - the very write law POST /cron/save uses -
// so these tests pin the CLI's own contract (verbs, flag application,
// refusals) and one end-to-end round trip against a real home.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cliTestCrons = `# the human-authored file
jobs:
  - name: daily-journal   # the owner's note rides the entry
    schedule: "0 8 * * *"
    prompt: journal
`

// newCLIPlugin mounts the job set a CLI dispatch would get: a runner
// created from a real crons.yaml in a temp home.
func newCLIPlugin(t *testing.T, seed string) (*Plugin, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "crons.yaml")
	if seed != "" {
		if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := LoadAllConfigs(home)
	if err != nil {
		t.Fatal(err)
	}
	return &Plugin{home: home, cfg: cfg, runner: NewRunner(cfg, home)}, home
}

func readCrons(t *testing.T, home string) string {
	t.Helper()
	data, err := os.ReadFile(homeFile(home))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func jobByName(jobs []JobInfo, name string) *JobInfo {
	for i := range jobs {
		if jobs[i].Name == name {
			return &jobs[i]
		}
	}
	return nil
}

// TestCronsCLIRoundTrip drives add -> list -> edit -> delete through the
// handler the capsule registers, asserting the file each step (the
// landed truth), not the printed text.
func TestCronsCLIRoundTrip(t *testing.T) {
	p, home := newCLIPlugin(t, cliTestCrons)
	cmd := cmdCrons(p)

	// add: an interval job and a cron job with skills.
	if err := cmd([]string{"add", "pulse", "--kind", "interval", "--minutes", "15", "--prompt", "tick"}); err != nil {
		t.Fatalf("add interval: %v", err)
	}
	if err := cmd([]string{"add", "nightly", "--schedule", "0 3 * * *", "--prompt", "ship it", "--skills", "omega-dev, ponytail"}); err != nil {
		t.Fatalf("add cron: %v", err)
	}
	text := readCrons(t, home)
	for _, want := range []string{"name: pulse", "minutes: 15", "name: nightly", "schedule: 0 3 * * *", "- omega-dev", "- ponytail"} {
		if !strings.Contains(text, want) {
			t.Fatalf("after add, crons.yaml missing %q:\n%s", want, text)
		}
	}
	// The fold-write law survives the CLI: the operator's comment stays.
	if !strings.Contains(text, "# the owner's note rides the entry") {
		t.Fatalf("add normalized the file, lost the entry comment:\n%s", text)
	}

	jobs := p.runner.Jobs()
	for _, name := range []string{"daily-journal", "pulse", "nightly"} {
		if jobByName(jobs, name) == nil {
			t.Fatalf("after add, %q missing from the job set: %+v", name, jobs)
		}
	}
	if got := jobByName(jobs, "pulse"); got.Kind != kindInterval || got.Minutes != 15 || got.Prompt != "tick" || !got.Enabled {
		t.Fatalf("pulse row = %+v", got)
	}
	if got := jobByName(jobs, "nightly"); got.Schedule != "0 3 * * *" || strings.Join(got.Skills, ",") != "omega-dev,ponytail" {
		t.Fatalf("nightly row = %+v", got)
	}

	// list renders the same table the /cron command does.
	var buf bytes.Buffer
	if err := cronsList(p, &buf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "pulse", "15m", "nightly", "0 3 * * *"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("list output missing %q:\n%s", want, buf.String())
		}
	}

	// edit applies ONLY the passed flag - prompt and kind stay.
	if err := cmd([]string{"edit", "pulse", "--minutes", "30"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	got := jobByName(p.runner.Jobs(), "pulse")
	if got == nil || got.Minutes != 30 || got.Kind != kindInterval || got.Prompt != "tick" {
		t.Fatalf("edit clobbered untouched fields: %+v", got)
	}
	if text = readCrons(t, home); strings.Contains(text, "minutes: 15") {
		t.Fatalf("minutes 15 survived the edit:\n%s", text)
	}

	// delete removes one job and leaves the rest.
	if err := cmd([]string{"delete", "pulse"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := jobByName(p.runner.Jobs(), "pulse"); got != nil {
		t.Fatalf("pulse survived delete: %+v", got)
	}
	if text = readCrons(t, home); strings.Contains(text, "name: pulse") {
		t.Fatalf("pulse survived delete in the file:\n%s", text)
	}
	if !strings.Contains(text, "name: nightly") {
		t.Fatalf("delete removed a sibling:\n%s", text)
	}
}

// TestCronsCLIEnabledFlip pins the one scalar an operator flips most:
// --enabled=false writes the flag without touching the definition.
func TestCronsCLIEnabledFlip(t *testing.T) {
	p, home := newCLIPlugin(t, cliTestCrons)
	if err := cmdCrons(p)([]string{"edit", "daily-journal", "--enabled=false"}); err != nil {
		t.Fatal(err)
	}
	text := readCrons(t, home)
	if !strings.Contains(text, "enabled: false") {
		t.Fatalf("enabled flag not written:\n%s", text)
	}
	if !strings.Contains(text, "prompt: journal") {
		t.Fatalf("edit rewrote an untouched field:\n%s", text)
	}
	if !strings.Contains(text, "# the human-authored file") {
		t.Fatalf("edit lost the FILE-level comment:\n%s", text)
	}
	// The replaced entry re-marshals clean - its own inline comment dies
	// with the mapping it annotated (save_test.go's documented cost).
	if strings.Contains(text, "# the owner's note rides the entry") {
		t.Fatalf("edit kept a stale inline comment on the replaced entry:\n%s", text)
	}
	if got := jobByName(p.runner.Jobs(), "daily-journal"); got == nil || got.Enabled {
		t.Fatalf("row still enabled: %+v", got)
	}
	if got := jobByName(p.runner.Jobs(), "daily-journal"); got.Next != "" {
		t.Fatalf("disabled job promises a next fire: %+v", got)
	}
}

// TestCronsCLIRefusals: every misuse fails loud and writes nothing.
func TestCronsCLIRefusals(t *testing.T) {
	p, home := newCLIPlugin(t, cliTestCrons)
	cmd := cmdCrons(p)
	before := readCrons(t, home)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"add without prompt", []string{"add", "noprompt", "--schedule", "0 1 * * *"}, "--prompt is required"},
		{"add without name", []string{"add"}, "usage: omega crons add"},
		// the name is positional FIRST: a trailing name would be swallowed
		// by flag parsing along with every flag after it
		{"add name last", []string{"add", "--prompt", "x", "late"}, "the job name comes first"},
		{"bad schedule", []string{"add", "badsched", "--schedule", "every tuesday", "--prompt", "p"}, "bad schedule"},
		{"edit unknown", []string{"edit", "ghost", "--prompt", "p"}, "no job \"ghost\""},
		{"edit with no flags", []string{"edit", "daily-journal"}, "nothing to change"},
		{"delete unknown", []string{"delete", "ghost"}, "no job \"ghost\""},
		// delete refuses a leading-dash name through the same jobName
		// guard add/edit use (review round 1 nit fix)
		{"delete leading dash", []string{"delete", "--help"}, "the job name comes first"},
		{"delete trailing junk", []string{"delete", "daily-journal", "junk"}, "only the name is positional"},
		{"list trailing junk", []string{"list", "junk"}, "list takes none"},
		{"unknown verb", []string{"frobnicate"}, "unknown crons subcommand"},
	}
	for _, tc := range cases {
		err := cmd(tc.args)
		if err == nil {
			t.Fatalf("%s: expected an error", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: error %q does not name %q", tc.name, err, tc.want)
		}
	}
	if after := readCrons(t, home); after != before {
		t.Fatalf("a refused command wrote the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestCronsCLIAgentJobRefused: a wake job's file belongs to its agent
// home - the CLI names that file instead of crossing homes.
func TestCronsCLIAgentJobRefused(t *testing.T) {
	home := t.TempDir()
	owned := true
	cfg := &Config{Jobs: []Job{{
		Name: "wake", Kind: kindInterval, Minutes: 5, Prompt: "tick",
		Agent: "scout", AgentOwned: true, Enabled: &owned,
	}}}
	p := &Plugin{home: home, cfg: cfg, runner: NewRunner(cfg, home)}
	handler := cmdCrons(p)
	for _, args := range [][]string{
		{"edit", "wake", "--minutes", "9"},
		{"delete", "wake"},
	} {
		err := handler(args)
		if err == nil {
			t.Fatalf("%v: expected a refusal", args)
		}
		if !strings.Contains(err.Error(), "agent home scout") {
			t.Fatalf("%v: error does not name the owning home: %v", args, err)
		}
	}
}

// TestCronsCLIHelp: the module's own help answers, and `omega crons`
// with no argv lists.
func TestCronsCLIHelp(t *testing.T) {
	p, _ := newCLIPlugin(t, cliTestCrons)
	if err := cmdCrons(p)([]string{"--help"}); err != nil {
		t.Fatalf("help: %v", err)
	}
	var buf bytes.Buffer
	if err := cronsList(p, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "daily-journal") {
		t.Fatalf("list lost the seeded job:\n%s", buf.String())
	}
}
