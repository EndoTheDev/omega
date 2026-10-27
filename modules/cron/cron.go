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

// knownDeliver values for the deliver field.
const (
	deliverJournal = "journal"
	deliverMemory  = "memory"
	deliverNone    = "none"
)

// Job is one scheduled prompt from crons.yaml.
//
//	yaml:
//	  - name: daily-journal      # required, unique, [a-z0-9-]
//	    schedule: "0 8 * * *"    # required, robfig 5-field, local time
//	    prompt: "..."            # required, the user message the run sends
//	    deliver: journal         # journal | memory | none, default none
//	    enabled: true            # default true
//
// Multi-home (card t_72ecca67, ruling 12): this one module reads the
// main crons.yaml AND every agent home's config/crons.yaml. Agent jobs
// carry the owning agent in Agent (set by the loader, never authored in
// yaml - that file belongs to a different home).
type Job struct {
	Name     string `yaml:"name"`
	Schedule string `yaml:"schedule"`
	Prompt   string `yaml:"prompt"`
	Deliver  string `yaml:"deliver"`
	Enabled  *bool  `yaml:"enabled"`
	// Agent is the agent home owner, set by LoadAllConfigs. Empty =
	// main's own job (an in-process turn, today's behavior). Non-empty
	// = the fire is a WAKE: one tick through the event spine, the
	// continuous capsule owns what the tick means (cron owns WHEN).
	Agent string `yaml:"-"`
}

// IsAgent reports whether the job belongs to an agent home (a wake).
func (j Job) IsAgent() bool { return j.Agent != "" }

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

// DeliverTarget returns the deliver mode with its default ("none").
func (j Job) DeliverTarget() string {
	if j.Deliver == "" {
		return deliverNone
	}
	return j.Deliver
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
			j.Agent = p.Name
			// A wake's delivery IS its report message in the agent's
			// own canonical chat (card t_72ecca67, ruling 10): the
			// main-home deliver targets have no meaning here, and a
			// journal write would cross the home boundary.
			if d := j.DeliverTarget(); d != deliverNone && d != "" {
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
		lines := make([]int, 0, len(val.Content))
		for _, item := range val.Content {
			job, err := parseJob(item)
			if err != nil {
				return nil, err
			}
			cfg.Jobs = append(cfg.Jobs, *job)
			lines = append(lines, item.Line)
		}
		if err := checkDuplicates(cfg.Jobs, lines); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// jobFields are the only allowed keys in a job entry. Unknown fields
// are rejected (KnownFields semantics, with a job name + line in the
// error message).
var jobFields = map[string]bool{
	"name": true, "schedule": true, "prompt": true,
	"deliver": true, "enabled": true,
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

// validateJob enforces the schema: name, schedule, prompt required;
// deliver enum; schedule parses under the 5-field grammar.
func validateJob(job Job, line int) error {
	if job.Name == "" {
		return fmt.Errorf("line %d: job at line %d is missing a name", line, line)
	}
	if !namePattern.MatchString(job.Name) {
		return fmt.Errorf("job %q (line %d): name must match [a-z0-9-]", job.Name, line)
	}
	if job.Schedule == "" {
		return fmt.Errorf("job %q (line %d): missing schedule", job.Name, line)
	}
	if _, err := scheduleParser.Parse(job.Schedule); err != nil {
		return fmt.Errorf("job %q (line %d): bad schedule %q: %v", job.Name, line, job.Schedule, err)
	}
	if job.Prompt == "" {
		return fmt.Errorf("job %q (line %d): missing prompt", job.Name, line)
	}
	switch job.DeliverTarget() {
	case deliverJournal, deliverMemory, deliverNone:
	default:
		return fmt.Errorf("job %q (line %d): deliver must be journal, memory, or none (got %q)", job.Name, line, job.Deliver)
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
