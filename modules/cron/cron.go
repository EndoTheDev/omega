// Package cron schedules omega prompts on cron expressions. Jobs live
// in crons.yaml in OMEGA_HOME; each fire is one full agent turn in a
// session labeled cron:<name>.
//
// Trust gate: omega may only ever PROPOSE jobs - it never writes
// crons.yaml. The only automatic write to the home directory is the
// bootstrap seed of crons.yaml.example (cmd/omega/home.go); a live
// crons.yaml is always human-authored.
package cron

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/EndoTheDev/omega/engine/agent"

	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// scheduleParser is the ONLY accepted schedule grammar: standard
// 5-field cron (minute hour dom month dow), local time. No seconds,
// no descriptors - those are rejected at parse time.
var scheduleParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// namePattern restricts job names to [a-z0-9-].
var namePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// Deliver targets (Endo ruling 2026-11-01, ONE enum - the curator,
// hygiene and delegation job definitions author through this list):
//
//	new-session      fresh in-process session, the fire runs cron:<name>
//	continuous-agent a WAKE into the agent's existing continuous session
//	append-to-file   the run's final message appends to job.File
//
// Legacy values (deprecated, still accepted - existing installs keep
// working byte-identically):
//
//	none    -> new-session
//	journal -> append-to-file, journal/YYYY-MM-DD.md naming when File empty
//	memory  -> the memory extension, unchanged
const (
	deliverNewSession   = "new-session"
	deliverContinuous   = "continuous-agent"
	deliverAppendToFile = "append-to-file"
	deliverNone         = "none"    // legacy alias of new-session
	deliverJournal      = "journal" // legacy alias of append-to-file
	deliverMemory       = "memory"
)

// Job kinds. kind: interval fires every N minutes; kind: cron (the
// default when absent) keeps the 5-field grammar. No seconds, no
// descriptors - the existing law, both kinds.
const (
	kindCron     = "cron"
	kindInterval = "interval"
)

// Job is one scheduled prompt from crons.yaml.
//
//	yaml:
//	  - name: daily-journal      # required, unique, [a-z0-9-]
//	    kind: cron               # cron | interval, default cron
//	    schedule: "0 8 * * *"    # required iff kind cron, robfig 5-field
//	    minutes: 5               # required iff kind interval, >= 1
//	    agent: scout             # optional; empty = default agent
//	    model: glm-5.3           # optional; empty = session provider model
//	    prompt: "..."            # required, the user message the run sends
//	    deliver: new-session     # see the deliver enum above, default new-session
//	    file: notes/report.md    # required iff append-to-file, home-relative
//	    enabled: true            # default true
//
// The defaulting law (Endo, 2026-11-01, omega-wide): a field naming an
// AGENT defaults to the default agent when empty; a field naming a
// MODEL defaults to the session provider model when empty. Empty =
// inherit, never error. Resolution happens at fire time
// (Runner.fire/agentTurn): agent names are dynamic (agent homes appear
// and vanish), model names belong to the provider - so a bad name is a
// clear error in the job log, never a crash and never a load failure.
//
// Multi-home (card t_72ecca67, ruling 12): this one module reads the
// main crons.yaml AND every agent home's config/crons.yaml. Agent jobs
// carry the owning agent in Agent (set by the loader, never authored in
// yaml - that file belongs to a different home).
type Job struct {
	Name     string `yaml:"name"`
	Kind     string `yaml:"kind,omitempty"`
	Schedule string `yaml:"schedule,omitempty"`
	Minutes  int    `yaml:"minutes,omitempty"`
	Prompt   string `yaml:"prompt"`
	Deliver  string `yaml:"deliver,omitempty"`
	File     string `yaml:"file,omitempty"`
	Model    string `yaml:"model,omitempty"`
	Enabled  *bool  `yaml:"enabled"`
	// Agent is the job's agent reference. Authored in the yaml as
	// `agent: <name>`, the DEFAULTING LAW resolves empty to the default
	// agent (agent.DefaultAgentName). For jobs loaded from an agent
	// home's own config/crons.yaml the loader OVERWRITES this with the
	// owning agent's name (an authored name there is a load error - the
	// file belongs to that one home) and sets AgentOwned. Empty =
	// main's own job, an in-process turn under the default agent
	// today's behavior. Non-empty with an in-process deliver target is
	// a fire-time error (in-process fires run as the default agent); a
	// continuous-agent deliver turns the fire into a WAKE: one tick
	// through the event spine, the continuous capsule owns what the
	// tick means (cron owns WHEN).
	Agent string `yaml:"agent,omitempty"`
	// AgentOwned is the FILE ownership mark: set by LoadAllConfigs when
	// the job's yaml lives in an agent home's config/crons.yaml. A file
	// - owned job wakes by construction (the loader IS the ownership
	// proof, no validation needed); a main-home job reaching a wake
	// goes through the fire-time agent resolution instead. yaml:"-":
	// never authored.
	AgentOwned bool `yaml:"-"`
}

// KindResolved returns the job kind with its default: absent kind is a
// 5-field cron schedule (existing installs keep parsing unchanged).
func (j Job) KindResolved() string {
	if j.Kind == "" {
		return kindCron
	}
	return j.Kind
}

// IsAgent reports whether the job's FILE belongs to an agent home - a
// wake by construction (card t_72ecca67: the loader tags the owner).
// Authored `agent:` references on main-home jobs do NOT count: their
// fire path resolves and validates the target at fire time.
func (j Job) IsAgent() bool { return j.AgentOwned }

// ResolveAgent applies the defaulting law to the agent field: empty =
// the default agent, never an error.
func (j Job) ResolveAgent() string {
	if j.Agent == "" {
		return agent.DefaultAgentName
	}
	return j.Agent
}

// StateKey names the job persistently: agent jobs share the name space
// with main's jobs and with each other (two agents can both carry a job
// literally named wake), so the state, the fire guard and the
// schedule map key on <agent>/<name> while the yaml name stays local.
func (j Job) StateKey() string {
	if j.IsAgent() {
		return j.Agent + "/" + j.Name
	}
	return j.Name
}

// IsEnabled reports whether the job should run. The enabled field
// defaults to true when absent.
func (j Job) IsEnabled() bool {
	return j.Enabled == nil || *j.Enabled
}

// DeliverTarget returns the deliver mode with its default and the
// legacy aliases resolved (the ONE enum above): empty or `none` is
// new-session, `journal` is append-to-file (the journal naming is the
// File-empty case). The raw yaml value stays on job.Deliver for
// validation round-tripping.
func (j Job) DeliverTarget() string {
	switch j.Deliver {
	case "", deliverNone:
		return deliverNewSession
	case deliverJournal:
		return deliverAppendToFile
	default:
		return j.Deliver
	}
}

// Config is the parsed crons.yaml content.
type Config struct {
	Jobs []Job `yaml:"jobs"`
}

// LoadConfig reads and parses the crons.yaml at path. A missing file
// is an empty config (no jobs), matching the mcp.yaml load behavior.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("read crons.yaml: %w", err)
	}
	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// LoadAllConfigs reads the main crons.yaml AND every agent home's
// config/crons.yaml (card t_72ecca67, ruling 12: ONE cron module for
// all). Agent jobs are tagged with their owner; names stay local to
// their file (two agents may both carry wake). A malformed agent file
// is a load error like a malformed main file - the operator sees the
// offending home, not a silent skip. A malformed home directory skips
// silently (agent.LoadProfiles' scan law).
func LoadAllConfigs(home string) (*Config, error) {
	cfg, err := LoadConfig(homeFile(home))
	if err != nil {
		return nil, fmt.Errorf("main crons: %w", err)
	}
	for _, p := range agent.LoadProfiles(home) {
		if !p.Continuous {
			continue // main is the root file, already read
		}
		path := filepath.Join(p.Dir, "config", stateFileName)
		agentCfg, err := LoadConfig(path)
		if err != nil {
			return nil, fmt.Errorf("agent %s crons: %w", p.Name, err)
		}
		for _, j := range agentCfg.Jobs {
			// The owner wins (ruling 12: the file belongs to that one
			// home): an authored `agent:` in an agent home's jobs is a
			// load error, same posture as an authored deliver — the
			// operator wrote a cross-home reference where only the
			// owner's identity may stand.
			if j.Agent != "" {
				return nil, fmt.Errorf("agent %s job %q: jobs in an agent home may not author agent (the owning agent is the loader's)", p.Name, j.Name)
			}
			j.Agent = p.Name
			j.AgentOwned = true
			// A wake's delivery IS its report message in the agent's
			// own canonical chat (card t_72ecca67, ruling 10): the
			// main-home deliver targets have no meaning here, and a
			// journal write would cross the home boundary. Empty and
			// the legacy `none` mean the wake itself.
			switch j.Deliver {
			case "", deliverNone:
			default:
				return nil, fmt.Errorf("agent %s job %q: deliver targets are a main-home feature (wakes deliver into the agent's canonical chat)", p.Name, j.Name)
			}
			cfg.Jobs = append(cfg.Jobs, j)
		}
	}
	return cfg, nil
}

// ParseConfig parses crons.yaml content. Every rejection names the
// offending job and its line number.
func ParseConfig(data []byte) (*Config, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if root.Kind == 0 {
		return &Config{}, nil // empty file
	}
	if root.Kind != yaml.DocumentNode {
		return nil, fmt.Errorf("parse: expected a yaml document")
	}
	body := root.Content[0]
	if body.Tag == "!!null" {
		return &Config{}, nil
	}
	if body.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: top level must be a mapping with a 'jobs' key", body.Line)
	}

	cfg := &Config{}
	// the line numbers travel WITH the accumulated job list: a file
	// that repeats the `jobs` key keeps every entry's line aligned, so
	// a duplicate carrying across the two keys errors with both its
	// line numbers - never an index panic (the checkDuplicates law:
	// one rejection, both lines, serve keeps standing).
	lines := make([]int, 0, len(body.Content)/2)
	for i := 0; i+1 < len(body.Content); i += 2 {
		key := body.Content[i]
		val := body.Content[i+1]
		if key.Value != "jobs" {
			return nil, fmt.Errorf("line %d: unknown field %q (want 'jobs')", key.Line, key.Value)
		}
		if val.Tag == "!!null" {
			continue
		}
		if val.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("line %d: 'jobs' must be a list", val.Line)
		}
		for _, item := range val.Content {
			job, err := parseJob(item)
			if err != nil {
				return nil, err
			}
			cfg.Jobs = append(cfg.Jobs, *job)
			lines = append(lines, item.Line)
		}
	}
	if err := checkDuplicates(cfg.Jobs, lines); err != nil {
		return nil, err
	}

	return cfg, nil
}

// jobFields are the only allowed keys in a job entry. Unknown fields
// are rejected (KnownFields semantics, with a job name + line in the
// error message).
var jobFields = map[string]bool{
	"name": true, "kind": true, "schedule": true, "minutes": true,
	"prompt": true, "deliver": true, "file": true, "model": true,
	"agent": true, "enabled": true,
}

// parseJob decodes one jobs entry with line-aware validation.
func parseJob(node *yaml.Node) (*Job, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: each job must be a mapping", node.Line)
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i]
		if !jobFields[k.Value] {
			return nil, fmt.Errorf("line %d: unknown field %q in job (line %d of crons.yaml)", k.Line, k.Value, node.Line)
		}
	}
	job := &Job{}
	if err := node.Decode(job); err != nil {
		return nil, fmt.Errorf("line %d: bad job: %w", node.Line, err)
	}

	if err := validateJob(*job, node.Line); err != nil {
		return nil, err
	}
	return job, nil
}

// validateJob enforces the schema: name and prompt always required;
// kind cron (default) requires the 5-field schedule, kind interval
// requires minutes >= 1 and no schedule; deliver enum (the ONE enum
// with its legacy aliases); agent/model/file presence rules.
func validateJob(job Job, line int) error {
	if job.Name == "" {
		return fmt.Errorf("line %d: job at line %d is missing a name", line, line)
	}
	if !namePattern.MatchString(job.Name) {
		return fmt.Errorf("job %q (line %d): name must match [a-z0-9-]", job.Name, line)
	}
	switch strings.TrimSpace(job.Kind) {
	case "", kindCron, kindInterval: // ok
	default:
		return fmt.Errorf("job %q (line %d): kind must be cron or interval (got %q)", job.Name, line, job.Kind)
	}
	switch job.KindResolved() {
	case kindInterval:
		if job.Schedule != "" {
			return fmt.Errorf("job %q (line %d): interval jobs take minutes, not schedule", job.Name, line)
		}
		if job.Minutes < 1 {
			return fmt.Errorf("job %q (line %d): interval jobs need minutes >= 1", job.Name, line)
		}
	default: // cron
		if job.Minutes > 0 {
			return fmt.Errorf("job %q (line %d): cron jobs take schedule, not minutes", job.Name, line)
		}
		if job.Schedule == "" {
			return fmt.Errorf("job %q (line %d): missing schedule", job.Name, line)
		}
		if _, err := scheduleParser.Parse(job.Schedule); err != nil {
			return fmt.Errorf("job %q (line %d): bad schedule %q: %v", job.Name, line, job.Schedule, err)
		}
	}
	if job.Prompt == "" {
		return fmt.Errorf("job %q (line %d): missing prompt", job.Name, line)
	}
	switch job.DeliverTarget() {
	case deliverNewSession, deliverContinuous, deliverAppendToFile, deliverMemory:
	default:
		return fmt.Errorf("job %q (line %d): deliver must be new-session, continuous-agent, or append-to-file (legacy: none, journal, memory) (got %q)", job.Name, line, job.Deliver)
	}
	if job.Agent != "" {
		if err := agent.ValidAgentName(job.Agent); err != nil {
			return fmt.Errorf("job %q (line %d): bad agent: %v", job.Name, line, err)
		}
	}
	if job.File != "" {
		if job.DeliverTarget() != deliverAppendToFile {
			return fmt.Errorf("job %q (line %d): file only applies to deliver append-to-file", job.Name, line)
		}
		if filepath.IsAbs(job.File) || strings.HasPrefix(filepath.ToSlash(filepath.Clean(job.File)), "..") {
			return fmt.Errorf("job %q (line %d): file must be a path inside the omega home (relative, no ..)", job.Name, line)
		}
	}
	return nil
}

// checkDuplicates rejects two jobs sharing a name, naming both lines.
func checkDuplicates(jobs []Job, lines []int) error {
	seen := make(map[string]int, len(jobs)) // name -> index into jobs
	for i, j := range jobs {
		if first, exists := seen[j.Name]; exists {
			return fmt.Errorf("duplicate job name %q (lines %d and %d)", j.Name, lines[first], lines[i])
		}
		seen[j.Name] = i
	}
	return nil
}

// cronHome resolves OMEGA_HOME for crons.yaml, cronstate.json and the
// journal, mirroring the mcp.yaml load logic.
func cronHome() string {
	home := os.Getenv("OMEGA_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".omega")
	}
	return home
}
