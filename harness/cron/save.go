package cron

// save.go: the cron tab's job editor write face. A new job's schema goes through the
// same ParseConfig path the operator's file goes through (one validation law), so an
// editor-produced crons.yaml is byte-lawful by construction.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"

	"gopkg.in/yaml.v3"
)

// cronSaver is the write face the job editor rides, separate from the reader (the.
type cronSaver interface {
	SaveJob(job Job) ([]JobInfo, error)
	DeleteJob(name string) ([]JobInfo, error)
}

// jobWire is the POST /cron/save body.
type jobWire struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind,omitempty"`
	Schedule string   `json:"schedule,omitempty"`
	Minutes  int      `json:"minutes,omitempty"`
	Prompt   string   `json:"prompt"`
	Skills   []string `json:"skills,omitempty"`
	Deliver  string   `json:"deliver,omitempty"`
	File     string   `json:"file,omitempty"`
	Agent    string   `json:"agent,omitempty"`
	Model    string   `json:"model,omitempty"`
	Enabled  bool     `json:"enabled"`
}

func (w jobWire) toJob() Job {
	return Job{
		Name:     w.Name,
		Kind:     w.Kind,
		Schedule: w.Schedule,
		Minutes:  w.Minutes,
		Prompt:   w.Prompt,
		Skills:   w.Skills,
		Deliver:  w.Deliver,
		File:     w.File,
		Agent:    w.Agent,
		Model:    w.Model,
		Enabled:  &w.Enabled,
	}
}

// SaveJob upserts one job into the MAIN crons.yaml and hot-reloads the runner (the wire
// contract carries the landed truth back - the toggle route's one-response law). An
// unknown or reload-failing write leaves the file untouched - server truth never fakes
// a swap.
func (r *Runner) SaveJob(job Job) ([]JobInfo, error) {
	// The inheritance chain is applied to the editor's job BEFORE it is marshalled, so the
	// file the operator reads carries the chain's own shape: an absent field where the
	// value is inherited, never a resolved copy pinned as authored.
	job, err := chainJob(job)
	if err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(Config{Jobs: []Job{job}})
	if err != nil {
		return nil, fmt.Errorf("marshal job: %w", err)
	}
	// The one validation law: the editor-authored yaml re-parses like any operator yaml,
	// rejections name the field (a bad kind, a schedule on an interval job..) and never
	// reach the disk.
	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, err
	}
	job = cfg.Jobs[0]
	// Agent-home ownership: a wake job's name is claimed by ITS file.
	// The editor writes the main home - a cross-home overwrite would
	// be invisible forever (the silent-persist law, registry toggles).
	for _, e := range r.jobsSnapshot() {
		if e.Name == job.Name && e.IsAgent() {
			return nil, fmt.Errorf("job %q belongs to agent home %s; edit its config/crons.yaml", job.Name, e.Agent)
		}
	}
	path := homeFile(r.home)
	if err := upsertJobNode(path, job, data); err != nil {
		return nil, err
	}
	return r.reloadAfterWrite()
}

// chainJob applies the inheritance chain to a job
// the editor is about to write.
//
// Two links, one law:
//
// agent empty -> inherits the main agent (agent.DefaultAgentName)
// model empty -> inherits the model of the agent the fire runs under
//
// An empty field IS the inherit - the write never materializes a
// resolved value, so the chain keeps inheriting when the main agent's
// own model moves. Two consequences fall out of that:
//
// an AUTHORED reference to the main agent is the chain's absence
//     written out ("the default IS the absence", jobinfo.go). The agent
//     combobox lists the main row first, so picking it would otherwise
//     pin a phantom override that means exactly what an absent field
//     means - the same anti-phantom law the agents panel's unset
//     vocabulary carries (agent/configedit.go). It is normalized away
//     instead of refused: it asks for the right thing.
//
// a WAKE may not lean on that absence. deliver: continuous-agent
//     into the main agent is a chain the fire can never satisfy - the
//     main agent is the daemon and never wakes as a subprocess
//     (runner.go logs "main never wakes", continuous/wake.go refuses the
//     child) - so the job would persist and silently never run. The
//     loader's own voice for that class of hole (an authored `agent:` in
//     an agent home, `skills:` on a wake) is a loud rejection where the
//     operator can fix it, and the editor's write face is that place.
func chainJob(job Job) (Job, error) {
	if job.Agent == agent.DefaultAgentName {
		job.Agent = ""
	}
	if job.DeliverTarget() == deliverContinuous && job.Agent == "" {
		return Job{}, fmt.Errorf("job %q: deliver continuous-agent needs its own agent - an empty agent inherits the main agent %q, which never wakes", job.Name, agent.DefaultAgentName)
	}
	return job, nil
}

// DeleteJob removes one job from the MAIN crons.yaml and hot-reloads.
func (r *Runner) DeleteJob(name string) ([]JobInfo, error) {
	for _, e := range r.jobsSnapshot() {
		if e.Name == name && e.IsAgent() {
			return nil, fmt.Errorf("job %q belongs to agent home %s; edit its config/crons.yaml", name, e.Agent)
		}
	}
	path := homeFile(r.home)
	if _, err := os.Stat(path); err != nil {
		return nil, UnknownJobError{Key: name}
	}
	if err := removeJobNode(path, name); err != nil {
		return nil, err
	}
	return r.reloadAfterWrite()
}

// reloadAfterWrite lands the write.
func (r *Runner) reloadAfterWrite() ([]JobInfo, error) {
	cfg, err := LoadAllConfigs(r.home)
	if err != nil {
		return nil, fmt.Errorf("reload after write: %w", err)
	}
	r.applyReload(context.Background(), cfg)
	return r.Jobs(), nil
}

// upsertJobNode writes the job into the crons.yaml at path at the node level. data is
// the single-job yaml document already parsed+validated by the caller - the entry node
// spliced here comes from the very bytes that validated.
func upsertJobNode(path string, job Job, data []byte) error {
	entry, err := jobEntryNode(data)
	if err != nil {
		return err
	}
	doc, seq, err := jobsDocument(path)
	if err != nil {
		return err
	}
	replaced := false
	for _, item := range seq.Content {
		if item.Kind == yaml.MappingNode && namedEntry(item, job.Name) {
			item.Content = entry.Content
			replaced = true
			break
		}
	}
	if !replaced {
		seq.Content = append(seq.Content, entry)
	}
	return writeJobsYAML(path, doc)
}

// removeJobNode removes the entry named name from the crons.yaml at
// path. Missing file or missing entry error here, never silently.
func removeJobNode(path, name string) error {
	doc, seq, err := jobsDocument(path)
	if err != nil {
		return err
	}
	for i, item := range seq.Content {
		if item.Kind == yaml.MappingNode && namedEntry(item, name) {
			seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
			return writeJobsYAML(path, doc)
		}
	}
	return UnknownJobError{Key: name}
}

// jobsDocument reads the crons.yaml at path and returns its document node plus the
// jobs.
func jobsDocument(path string) (*yaml.Node, *yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("read %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, nil, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
		}
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "jobs"},
			&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"},
		)
		return root, root.Content[1], nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) != 1 {
		return nil, nil, fmt.Errorf("parse %s: empty document", path)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("parse %s: top-level document is not a mapping", path)
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		if key.Kind == yaml.ScalarNode && key.Value == "jobs" {
			val := root.Content[i+1]
			if val.Kind != yaml.SequenceNode {
				return nil, nil, fmt.Errorf("parse %s: 'jobs' must be a list", path)
			}
			return &doc, val, nil
		}
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "jobs"},
		seq,
	)
	return &doc, seq, nil
}

// jobEntryNode builds one job's mapping node from the single-job yaml
// in data (the caller's already-validated bytes).
func jobEntryNode(data []byte) (*yaml.Node, error) {
	var mini yaml.Node
	if err := yaml.Unmarshal(data, &mini); err != nil {
		return nil, fmt.Errorf("marshal job: %w", err)
	}
	if len(mini.Content) != 1 {
		return nil, fmt.Errorf("marshal job: empty document")
	}
	var entry *yaml.Node
	seq := findJobsSeq(mini.Content[0])
	if seq != nil && len(seq.Content) > 0 {
		entry = seq.Content[0]
	}
	if entry == nil || entry.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("marshal job: no job entry produced")
	}
	return entry, nil
}

// findJobsSeq returns the jobs: sequence under a mapping root, or nil.
func findJobsSeq(root *yaml.Node) *yaml.Node {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		if key.Kind == yaml.ScalarNode && key.Value == "jobs" && root.Content[i+1].Kind == yaml.SequenceNode {
			return root.Content[i+1]
		}
	}
	return nil
}

// namedEntry matches a job mapping by its name: scalar, the value.
func namedEntry(item *yaml.Node, name string) bool {
	for i := 0; i+1 < len(item.Content); i += 2 {
		k := item.Content[i]
		if k.Kind == yaml.ScalarNode && k.Value == "name" {
			return item.Content[i+1].Kind == yaml.ScalarNode && item.Content[i+1].Value == name
		}
	}
	return false
}

// handleSaveJob serves POST /cron/save. A job name the editor may not touch (an agent
// home's wake) is a loud 400, never a silent cross-home overwrite; a missing saver is
// the mount posture's 501 (the toggle route's shape).
func (p *Plugin) handleSaveJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// A nil *Runner converts to a non-nil interface value (the classic
	// Go nil trap) - the guard must test the pointer, not the wrapper.
	if p.runner == nil {
		http.Error(w, "cron save: no cron runner mounted", http.StatusNotImplemented)
		return
	}
	saver := cronSaver(p.runner)
	var req jobWire
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "cron save: bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "cron save: name is required", http.StatusBadRequest)
		return
	}
	jobs, err := saver.SaveJob(req.toJob())
	if err != nil {
		http.Error(w, "cron save: "+err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(jobs); err != nil {
		return
	}
}

// jobDeleteRequest is the POST /cron/delete body.
type jobDeleteRequest struct {
	Name string `json:"name"`
}

// handleDeleteJob serves POST /cron/delete. Unknown name = 404 (the silent-persist law
// works in reverse: a no-op delete must be visible).
func (p *Plugin) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Same nil-pointer guard the save handler carries (a nil *Runner
	// converts to a non-nil interface).
	if p.runner == nil {
		http.Error(w, "cron delete: no cron runner mounted", http.StatusNotImplemented)
		return
	}
	saver := cronSaver(p.runner)
	var req jobDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, "cron delete: name is required", http.StatusBadRequest)
		return
	}
	jobs, err := saver.DeleteJob(req.Name)
	if err != nil {
		var unknown UnknownJobError
		if errors.As(err, &unknown) {
			http.Error(w, "cron delete: "+err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, "cron delete: "+err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(jobs); err != nil {
		return
	}
}
