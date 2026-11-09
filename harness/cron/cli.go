package cron

// cli.go: the crons subcommand (card t_aca45258) - `omega crons
// list|add|edit|delete`, the terminal face of the cron tab's editor.
// The capsule registers the name at mount (RegisterSubcommand, v0.12
// phase D Q6: the module owns its argv), exactly like kanban and
// curator.
//
// Every write rides Runner.SaveJob / Runner.DeleteJob - the SAME write
// law the POST /cron/save and POST /cron/delete routes use (yaml.Node
// splice, ParseConfig validation, hot reload). The CLI adds no second
// writer: an `omega crons add` and a webui editor save land byte-
// identically in crons.yaml.
//
// Agent-home ownership (ruling 12): SaveJob/DeleteJob already refuse a
// wake job by name (that file belongs to another home); the CLI only
// surfaces the refusal - it never crosses homes.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/EndoTheDev/omega/agent"
)

// cronsSummary is the one-line entry the global `omega --help` module
// section shows for this capsule (the listing is registry introspection).
const cronsSummary = "cron jobs: list, add, edit, delete (see: omega crons --help)"

// cronsHelp is the module's own help: `omega crons --help` prints it.
const cronsHelp = `omega crons - scheduled prompts in crons.yaml

Usage:
  omega crons                       list every job (same as "omega crons list")
  omega crons list                  job table: name, cadence, agent, last fire, next fire
  omega crons add <name> [flags]    create a job
  omega crons edit <name> [flags]   change the named job (only the flags you pass)
  omega crons delete <name>         remove a job

Job flags (add/edit):
  --schedule <expr>    5-field cron expression (minute hour dom month dow), kind cron
  --kind <cron|interval>
  --minutes <n>        interval jobs: fire every n minutes
  --prompt <text>      the user message a fire sends (required)
  --deliver <target>   new-session | continuous-agent | append-to-file (default new-session)
  --file <path>        append-to-file target, relative to the omega home
  --agent <name>       agent the fire runs as (default: the default agent)
  --model <name>       per-job model override (default: the session model)
  --skills <a,b>       skill names pinned into the fire prompt
  --enabled[=false]    whether the scheduler fires the job (add default: true)

Writes land in the main home's crons.yaml at the yaml.Node level (comments
and style survive); an agent home's wake job is owned by that home's own
config/crons.yaml and is refused here.
`

// cmdCrons builds the subcommand handler. The closure keeps the mounted
// plugin (and with it the runner created at mount - the same instance the
// /cron command and the HTTP routes read).
func cmdCrons(p *Plugin) func([]string) error {
	return func(args []string) error {
		if len(args) == 0 {
			return cronsList(p, os.Stdout)
		}
		sub, rest := args[0], args[1:]
		switch sub {
		case "list":
			if len(rest) != 0 {
				return fmt.Errorf("crons list: unexpected argument %q (list takes none, omega crons --help lists them)", rest[0])
			}
			return cronsList(p, os.Stdout)
		case "add":
			return cronsAdd(p, rest)
		case "edit":
			return cronsEdit(p, rest)
		case "delete":
			return cronsDelete(p, rest)
		case "--help", "-h", "help":
			fmt.Print(cronsHelp)
			return nil
		default:
			fmt.Fprint(os.Stderr, cronsHelp)
			return fmt.Errorf("unknown crons subcommand %q", sub)
		}
	}
}

// cliRunner returns the mount-time runner. A missing runner is the same
// posture the wire takes (501): the cron capsule did not mount, so there
// is no job set to edit.
func (p *Plugin) cliRunner() (*Runner, error) {
	if p.runner == nil {
		return nil, fmt.Errorf("crons: no cron runner mounted")
	}
	return p.runner, nil
}

// cronsList renders the job table - the same renderer the /cron command
// uses, so the terminal and the webui Cron tab speak one table.
func cronsList(p *Plugin, w io.Writer) error {
	r, err := p.cliRunner()
	if err != nil {
		return err
	}
	jobs := r.Jobs()
	if len(jobs) == 0 {
		fmt.Fprintln(w, "[cron] no cron jobs configured")
		return nil
	}
	fmt.Fprint(w, renderCronTable(jobs))
	return nil
}

// cronsFlags is the parsed flag set of add/edit. enabled defaults to
// true (a fresh job fires); edit only applies it when the flag was
// passed (flag.Visit).
type cronsFlags struct {
	kind     string
	schedule string
	minutes  int
	prompt   string
	deliver  string
	file     string
	agent    string
	model    string
	skills   string
	enabled  bool
}

// parseCronsArgs parses the job flags and returns the positional args
// (the job name). Unknown flags and bad values error - the operator
// gets the flag name, never a silent default.
func parseCronsArgs(args []string) (*cronsFlags, []string, *flag.FlagSet, error) {
	f := &cronsFlags{enabled: true}
	fs := flag.NewFlagSet("crons", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&f.kind, "kind", "", "cron | interval")
	fs.StringVar(&f.schedule, "schedule", "", "5-field cron expression")
	fs.IntVar(&f.minutes, "minutes", 0, "interval minutes")
	fs.StringVar(&f.prompt, "prompt", "", "the user message a fire sends")
	fs.StringVar(&f.deliver, "deliver", "", "new-session | continuous-agent | append-to-file")
	fs.StringVar(&f.file, "file", "", "append-to-file target, home-relative")
	fs.StringVar(&f.agent, "agent", "", "agent the fire runs as")
	fs.StringVar(&f.model, "model", "", "per-job model override")
	fs.StringVar(&f.skills, "skills", "", "comma-separated skill names")
	fs.BoolVar(&f.enabled, "enabled", true, "whether the scheduler fires the job")
	err := fs.Parse(args)
	if err != nil {
		return nil, nil, nil, err
	}
	return f, fs.Args(), fs, nil
}

// jobName peels the one positional a job verb needs. The name comes
// FIRST (`crons add <name> [flags]`): the Go flag package stops parsing
// at the first non-flag argument, so a trailing name would silently
// swallow every flag after it. Leftover positionals are a usage error,
// never a guess.
func jobName(verb string, args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("usage: omega crons %s <name> [flags] (omega crons --help lists them)", verb)
	}
	if strings.HasPrefix(args[0], "-") {
		return "", nil, fmt.Errorf("usage: omega crons %s <name> [flags] - the job name comes first", verb)
	}
	return args[0], args[1:], nil
}

// splitSkills turns the --skills value into names. Empty stays nil (the
// job hunts its own procedures).
func splitSkills(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// cronsAdd creates one job. The schema law is ParseConfig's (called by
// SaveJob): a missing prompt, a bad schedule, a schedule on an interval
// job - every rejection names the field and nothing reaches the disk.
func cronsAdd(p *Plugin, args []string) error {
	r, err := p.cliRunner()
	if err != nil {
		return err
	}
	name, flagArgs, err := jobName("add", args)
	if err != nil {
		return err
	}
	f, pos, _, err := parseCronsArgs(flagArgs)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fmt.Errorf("crons add: unexpected argument %q (only the name is positional)", pos[0])
	}
	if strings.TrimSpace(f.prompt) == "" {
		return fmt.Errorf("crons add %s: --prompt is required (the message a fire sends)", name)
	}
	enabled := f.enabled
	job := Job{
		Name:     name,
		Kind:     f.kind,
		Schedule: f.schedule,
		Minutes:  f.minutes,
		Prompt:   f.prompt,
		Deliver:  f.deliver,
		File:     f.file,
		Agent:    f.agent,
		Model:    f.model,
		Skills:   splitSkills(f.skills),
		Enabled:  &enabled,
	}
	jobs, err := r.SaveJob(job)
	if err != nil {
		return fmt.Errorf("crons add %s: %w", name, err)
	}
	printCronsSaved(jobs, name)
	return nil
}

// cronsEdit changes the named job. The current definition is the base -
// only flags the operator actually passed are applied, so `omega crons
// edit daily --enabled=false` never rewrites the prompt or the schedule.
func cronsEdit(p *Plugin, args []string) error {
	r, err := p.cliRunner()
	if err != nil {
		return err
	}
	name, flagArgs, err := jobName("edit", args)
	if err != nil {
		return err
	}
	f, pos, fs, err := parseCronsArgs(flagArgs)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fmt.Errorf("crons edit: unexpected argument %q (only the name is positional)", pos[0])
	}
	var existing *Job
	for _, j := range r.jobsSnapshot() {
		if j.Name == name {
			job := j
			existing = &job
			break
		}
	}
	if existing == nil {
		return fmt.Errorf("crons edit: no job %q in the loaded cron set", name)
	}
	if existing.IsAgent() {
		return fmt.Errorf("crons edit: job %q belongs to agent home %s; edit %s/config/crons.yaml",
			name, existing.Agent, agent.AgentDir(r.home, existing.Agent))
	}
	job := *existing
	changed := false
	fs.Visit(func(fl *flag.Flag) {
		changed = true
		switch fl.Name {
		case "kind":
			job.Kind = f.kind
		case "schedule":
			job.Schedule = f.schedule
		case "minutes":
			job.Minutes = f.minutes
		case "prompt":
			job.Prompt = f.prompt
		case "deliver":
			job.Deliver = f.deliver
		case "file":
			job.File = f.file
		case "agent":
			job.Agent = f.agent
		case "model":
			job.Model = f.model
		case "skills":
			job.Skills = splitSkills(f.skills)
		case "enabled":
			enabled := f.enabled
			job.Enabled = &enabled
		}
	})
	if !changed {
		return fmt.Errorf("crons edit %s: nothing to change - pass at least one job flag (--help lists them)", name)
	}
	jobs, err := r.SaveJob(job)
	if err != nil {
		return fmt.Errorf("crons edit %s: %w", name, err)
	}
	printCronsSaved(jobs, name)
	return nil
}

// cronsDelete removes one main-home job. The returned job set is the
// landed truth; the confirmation counts what remains.
func cronsDelete(p *Plugin, args []string) error {
	r, err := p.cliRunner()
	if err != nil {
		return err
	}
	name, rest, err := jobName("delete", args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("crons delete: unexpected argument %q (only the name is positional)", rest[0])
	}
	jobs, err := r.DeleteJob(name)
	if err != nil {
		return fmt.Errorf("crons delete %s: %w", name, err)
	}
	fmt.Printf("deleted job %q; %d job(s) remain\n", name, len(jobs))
	return nil
}

// printCronsSaved reports the landed row (the same truth the HTTP route
// answers): the job's cadence, enablement and next fire.
func printCronsSaved(jobs []JobInfo, name string) {
	for _, j := range jobs {
		if j.Name != name {
			continue
		}
		fmt.Printf("saved job %q: %s, deliver=%s, enabled=%v\n", j.Name, cadenceText(j), j.Deliver, j.Enabled)
		if j.Next != "" {
			fmt.Printf("next fire: %s\n", j.Next)
		}
		return
	}
	fmt.Printf("saved job %q\n", name)
}
