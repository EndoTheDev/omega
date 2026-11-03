package cron

// save.go: the cron tab's job editor write face (card t_ecec864e, the
// settings-exposure ruling: the tab shows and edits ALL job fields).
// The write lands in ONE file - the MAIN home's crons.yaml. Agent-home
// (wake) jobs are refused here by name: that file belongs to a
// different home (the LoadAllConfigs ownership law, ruling 12); their
// enable/disable keeps riding the toggle route, full editing keeps
// belonging to the agent's own home.
//
// The write is the fold-write pattern (harness/cron/toggle.go): parse
// at the yaml.Node level, splice the ONE job's mapping, marshal the
// WHOLE document - comments, key order and the file's style survive
// everywhere except inside the replaced entry itself. A new job's
// schema goes through the same ParseConfig path the operator's file
// goes through (one validation law), so an editor-produced crons.yaml
// is byte-lawful by construction.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/EndoTheDev/omega/core"
)

// cronSaver is the write face the job editor rides, separate from the
// reader (the CronLister law) and the toggle (the flag law): a full
// job upsert and a delete are bulkier scalars than `enabled:`.
type cronSaver interface {
	SaveJob(job Job) ([]core.CronJobInfo, error)
	DeleteJob(name string) ([]core.CronJobInfo, error)
}

// jobWire is the POST /cron/save body: the full job definition, the
// CronJobInfo listing's fields plus the prompt. Enabled is explicit -
// the editor always sends the whole job.
type jobWire struct {
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`
	Schedule string `json:"schedule,omitempty"`
	Minutes  int    `json:"minutes,omitempty"`
	Prompt   string `json:"prompt"`
	Deliver  string `json:"deliver,omitempty"`
	File     string `json:"file,omitempty"`
	Agent    string `json:"agent,omitempty"`
	Model    string `json:"model,omitempty"`
	Enabled  bool   `json:"enabled"`
}

func (w jobWire) toJob() Job {
	return Job{
		Name:     w.Name,
		Kind:     w.Kind,
		Schedule: w.Schedule,
		Minutes:  w.Minutes,
		Prompt:   w.Prompt,
		Deliver:  w.Deliver,
		File:     w.File,
		Agent:    w.Agent,
		Model:    w.Model,
		Enabled:  &w.Enabled,
	}
}

// SaveJob upserts one job into the MAIN crons.yaml and hot-reloads the
// runner (the wire contract carries the landed truth back - the toggle
// route's one-response law). The job goes through ParseConfig first:
// the editor's output obeys the exact schema law a hand-written
// crons.yaml obeys. An unknown or reload-failing write leaves the file
// untouched - server truth never fakes a swap.
func (r *Runner) SaveJob(job Job) ([]core.CronJobInfo, error) {
	data, err := yaml.Marshal(Config{Jobs: []Job{job}})
	if err != nil {
		return nil, fmt.Errorf("marshal job: %w", err)
	}
	// The one validation law: the editor-authored yaml re-parses like
	// any operator yaml, rejections name the field (a bad kind, a
	// schedule on an interval job...) and never reach the disk.
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

// DeleteJob removes one job from the MAIN crons.yaml and hot-reloads.
// Wake jobs are refused like SaveJob; an unknown name is an
// UnknownJobError (the 404 lane).
func (r *Runner) DeleteJob(name string) ([]core.CronJobInfo, error) {
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

// reloadAfterWrite lands the write: the mtime watcher would pick the
// file up anyway, the immediate reload makes the response carry the
// landed truth (the toggle route's reasoning, harness/cron/toggle.go).
// A parse failure after the write keeps the previous job set serving.
func (r *Runner) reloadAfterWrite() ([]core.CronJobInfo, error) {
	cfg, err := LoadAllConfigs(r.home)
	if err != nil {
		return nil, fmt.Errorf("reload after write: %w", err)
	}
	r.applyReload(context.Background(), cfg)
	return r.Jobs(), nil
}

// upsertJobNode writes the job into the crons.yaml at path at the node
// level: replace an entry named job.Name in place, or append one.
// data is the single-job yaml document already parsed+validated by the
// caller - the entry node spliced here comes from the very bytes that
// validated.
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

// jobsDocument reads the crons.yaml at path and returns its document
// node plus the jobs: sequence, creating a minimal `jobs:` document
// when the file does not exist yet (the editor is the bootstrap for a
// fresh home). The path's config/ directory is created on demand the
// same way the journal append creates its directory.
func jobsDocument(path string) (*yaml.Node, *yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("read %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, nil, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
		}
		// a fresh document, marshalled once from the shape the
		// loader itself would produce for an empty config
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
	// a file that talks about something else entirely: append the jobs
	// key rather than error - the next write lands it
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

// handleSaveJob serves POST /cron/save: decode the job, validate via
// the loader's own schema law, upsert into the MAIN crons.yaml, answer
// the fresh job set. A job name the editor may not touch (an agent
// home's wake) is a loud 400, never a silent cross-home overwrite; a
// missing saver is the mount posture's 501 (the toggle route's shape).
func (p *Plugin) handleSaveJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	saver, ok := p.runner.(cronSaver)
	if !ok || saver == nil {
		http.Error(w, "cron save: no cron runner mounted", http.StatusNotImplemented)
		return
	}
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
		// header already sent; the connection carries the failure
		return
	}
}

// jobDeleteRequest is the POST /cron/delete body: the job's name as
// the editor knows it.
type jobDeleteRequest struct {
	Name string `json:"name"`
}

// handleDeleteJob serves POST /cron/delete: remove the job from the
// MAIN crons.yaml, answer the fresh job set. Unknown name = 404 (the
// silent-persist law works in reverse: a no-op delete must be visible).
func (p *Plugin) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	saver, ok := p.runner.(cronSaver)
	if !ok || saver == nil {
		http.Error(w, "cron delete: no cron runner mounted", http.StatusNotImplemented)
		return
	}
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