// Package cron schedules omega prompts on cron expressions. Trust gate: omega may only
// ever PROPOSE jobs - it never writes crons.yaml.
package cron

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/EndoTheDev/omega/agent"

	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// scheduleParser is the ONLY accepted schedule grammar.
var scheduleParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// namePattern restricts job names to [a-z0-9-].
var namePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// Deliver targets (ONE enum - the curator,
// hygiene and delegation job definitions author through this list):
//
// new-session fresh in-process session, the fire runs cron:<name>
//	continuous-agent a WAKE into the agent's existing continuous session
// append-to-file the run's final message appends to job.File
//
// Legacy values (deprecated, still accepted - existing installs keep
// working byte-identically):
//
// none -> new-session
//	journal -> append-to-file, journal/YYYY-MM-DD.md naming when File empty
// memory -> the memory extension, unchanged
const (
	deliverNewSession   = "new-session"
	deliverContinuous   = "continuous-agent"
	deliverAppendToFile = "append-to-file"
	deliverNone         = "none"    // legacy alias of new-session
	deliverJournal      = "journal" // legacy alias of append-to-file
	deliverMemory       = "memory"
)

// Job kinds.
const (
	kindCron     = "cron"
	kindInterval = "interval"
)

// Job is one scheduled prompt from crons.yaml.
//
//	yaml:
// name: daily-journal # required, unique, [a-z0-9-]
// kind: cron # cron | interval, default cron
// schedule: "0 8 * * *" # required iff kind cron, robfig 5-field
// minutes: 5 # required iff kind interval, >= 1
// agent: scout # optional; empty = default agent
// model: glm-5.3 # optional; empty = session provider model
// prompt: "." # required, the user message the run sends
// skills: omega-dev # optional; skill names pinned into the fire
// deliver: new-session # see the deliver enum above, default new-session
// file: notes/report.md # required iff append-to-file, home-relative
// enabled: true # default true
//
// The defaulting law: a field naming an
// AGENT defaults to the default agent when empty; a field naming a
// MODEL defaults to the session provider model when empty. Empty =
// inherit, never error. Resolution happens at fire time
// (Runner.fire/agentTurn): agent names are dynamic (agent homes appear
// and vanish), model names belong to the provider - so a bad name is a
// clear error in the job log, never a crash and never a load failure.
//
// Multi-home: this one module reads the
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
	Skills []string `yaml:"skills,omitempty"`
	// Agent is the job's agent reference. For jobs loaded from an agent home's own
	// config/crons.yaml the loader OVERWRITES this with the owning agent's name (an
	// authored name there is a load error - the file belongs to that one home) and sets
	// AgentOwned.
	Agent string `yaml:"agent,omitempty"`
	// AgentOwned is the FILE ownership mark. yaml:"-": never authored.
	AgentOwned bool `yaml:"-"`
}

// KindResolved returns the job kind with its default.
func (j Job) KindResolved() string {
	if j.Kind == "" {
		return kindCron
	}
	return j.Kind
}

// IsAgent reports whether the job's FILE belongs to an agent home. Authored `agent:`
// references on main-home jobs do NOT count: their fire path resolves and validates the
// target at fire time.
func (j Job) IsAgent() bool { return j.AgentOwned }

// ResolveAgent applies the defaulting law to the agent field: empty =
// the default agent, never an error.
func (j Job) ResolveAgent() string {
	if j.Agent == "" {
		return agent.DefaultAgentName
	}
	return j.Agent
}

// StateKey names the job persistently.
func (j Job) StateKey() string {
	if j.IsAgent() {
		return j.Agent + "/" + j.Name
	}
	return j.Name
}

// IsEnabled reports whether the job should run.
func (j Job) IsEnabled() bool {
	return j.Enabled == nil || *j.Enabled
}

// DeliverTarget returns the deliver mode with its default and the legacy aliases.
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

// readCronFile reads one crons.yaml.
func readCronFile(path string) (data []byte, existed bool, err error) {
	data, err = os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read crons.yaml: %w", err)
	}
	return data, true, nil
}

// loadConfig reads and parses the crons.yaml at path. The reload law is
// switchable: strict turns
// a zero-byte or whitespace-only EXISTING file into a read error instead
// of an empty job set (loadAllConfigsForReload below,.
// A missing file stays an empty config in both modes.
func loadConfig(path string, strict bool) (*Config, error) {
	data, existed, err := readCronFile(path)
	if err != nil {
		return nil, err
	}
	if strict && existed && strings.TrimSpace(string(data)) == "" {
		return nil, fmt.Errorf("%s: empty content (a write may be in progress; write \"jobs: []\" to remove every job)", path)
	}
	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// LoadAllConfigs reads the main crons.yaml AND every agent home's config/crons.yaml. A
// malformed home directory skips silently (agent.LoadProfiles' scan law).
func LoadAllConfigs(home string) (*Config, error) {
	return loadAllConfigs(home, false)
}

// loadAllConfigsForReload is LoadAllConfigs for the hot-reload watcher, with the
// truncated-write law: a zero-byte or whitespace-only crons.yaml is a READ ERROR, never
// an empty job set. The watcher cannot tell a file mid-rewrite from a file the operator
// emptied, and applying the empty set would drop every live job - a non-atomic save
// (os.WriteFile shortens first, most editors write in place) opens exactly that window.
func loadAllConfigsForReload(home string) (*Config, error) {
	return loadAllConfigs(home, true)
}

// loadAllConfigs is the one home walk.
func loadAllConfigs(home string, strictEmpty bool) (*Config, error) {
	cfg, err := loadConfig(homeFile(home), strictEmpty)
	if err != nil {
		return nil, fmt.Errorf("main crons: %w", err)
	}
	for _, p := range agent.LoadProfiles(home) {
		if !p.Continuous {
			continue // main is the root file, already read
		}
		path := filepath.Join(p.Dir, "config", stateFileName)
		agentCfg, err := loadConfig(path, strictEmpty)
		if err != nil {
			return nil, fmt.Errorf("agent %s crons: %w", p.Name, err)
		}
		for _, j := range agentCfg.Jobs {
			// The owner wins: an authored `agent:` in an agent home's jobs is a load error.
			if j.Agent != "" {
				return nil, fmt.Errorf("agent %s job %q: jobs in an agent home may not author agent (the owning agent is the loader's)", p.Name, j.Name)
			}
			j.Agent = p.Name
			j.AgentOwned = true
			// A wake's delivery IS its report message in the agent's own canonical chat.
			switch j.Deliver {
			case "", deliverNone:
			default:
				return nil, fmt.Errorf("agent %s job %q: deliver targets are a main-home feature (wakes deliver into the agent's canonical chat)", p.Name, j.Name)
			}
			// Skills ride the fire prompt. An authored list would parse fine and never take
			// effect: the same silent-persist hole the deliver law closes, so reject.
			if len(j.Skills) > 0 {
				return nil, fmt.Errorf("agent %s job %q: skills pinning is a main-home feature (wakes follow the agent's own skills)", p.Name, j.Name)
			}
			cfg.Jobs = append(cfg.Jobs, j)
		}
	}
	return cfg, nil
}

// ParseConfig parses crons.yaml content.
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
	// the line numbers travel WITH the accumulated job list: a file that repeats the
	// `jobs` key keeps every entry's line aligned, so a duplicate carrying across the two
	// keys errors with both its line numbers - never an index panic (the checkDuplicates
	// law: one rejection, both lines, serve keeps standing).
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

// jobFields are the only allowed keys in a job entry.
var jobFields = map[string]bool{
	"name": true, "kind": true, "schedule": true, "minutes": true,
	"prompt": true, "deliver": true, "file": true, "model": true,
	"agent": true, "enabled": true, "skills": true,
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
	// The skills list normalizes here, the one place a yaml-authored value takes its. The
	// name grammar and the duplicates reject in validateJob with the line in the error
	// (the schema keeps its one rejection voice).
	job.Skills = normalizeSkills(job.Skills)

	if err := validateJob(*job, node.Line); err != nil {
		return nil, err
	}
	return job, nil
}

// normalizeSkills trims whitespace, drops empty entries, keeps order.
func normalizeSkills(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// validateJob enforces the schema.
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
	// The skills law: entries exist (the parser normalized them), names obey the folder
	// grammar, duplicates are the same honesty the duplicate job names get - a rejection
	// naming the line, never a silent dedupe of what the operator wrote.
	seenSkills := make(map[string]bool, len(job.Skills))
	for _, s := range job.Skills {
		if !namePattern.MatchString(s) {
			return fmt.Errorf("job %q (line %d): skill names must match [a-z0-9-] (got %q)", job.Name, line, s)
		}
		if seenSkills[s] {
			return fmt.Errorf("job %q (line %d): duplicate skill %q", job.Name, line, s)
		}
		seenSkills[s] = true
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
		clean := filepath.ToSlash(filepath.Clean(job.File))
		if filepath.IsAbs(job.File) || strings.HasPrefix(clean, "..") || strings.HasPrefix(clean, "/") {
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

// cronHome resolves OMEGA_HOME for crons.yaml, cronstate.json and the journal.
func cronHome() string {
	home := os.Getenv("OMEGA_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".omega")
	}
	return home
}
