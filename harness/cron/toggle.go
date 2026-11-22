package cron

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"gopkg.in/yaml.v3"
)

type cronToggler interface {
	ToggleJob(key string, enabled bool) ([]JobInfo, error)
}

// jobToggleRequest is the POST /cron/toggle body.
type jobToggleRequest struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}

// UnknownJobError marks a toggle error whose key no loaded job knows -
// the 404 lane (a silently persisted unknown key would be invisible
// forever, the registry-toggle refusal law).
type UnknownJobError struct{ Key string }

func (e UnknownJobError) Error() string {
	return fmt.Sprintf("no job %q in the loaded cron set", e.Key)
}
func (e UnknownJobError) Unknown() bool { return true }

// ToggleJob flips the enabled flag of one job and hot-reloads the runner. Returns the
// fresh job set (the wire contract: one response carries the state it landed - no
// second GET). An unknown key errors without writing a byte.
func (r *Runner) ToggleJob(key string, enabled bool) ([]JobInfo, error) {
	job, ok := r.jobByKey(key)
	if !ok {
		return nil, UnknownJobError{Key: key}
	}
	path, err := r.jobFileFor(job)
	if err != nil {
		return nil, err
	}
	if err := setJobEnabled(path, job.Name, enabled); err != nil {
		return nil, err
	}
	// the write landed; reload through the same path the mtime watcher rides, so the
	// scheduler follows the file (a parse failure after the write keeps the previous job
	// set serving - the MCP reload law: server truth never fakes a swap)
	cfg, err := LoadAllConfigs(r.home)
	if err != nil {
		return nil, fmt.Errorf("reload after write: %w", err)
	}
	r.applyReload(context.Background(), cfg)
	return r.Jobs(), nil
}

func (r *Runner) jobByKey(key string) (Job, bool) {
	for _, j := range r.jobsSnapshot() {
		if j.StateKey() == key {
			return j, true
		}
	}
	return Job{}, false
}

func (r *Runner) jobFileFor(job Job) (string, error) {
	for path, owner := range r.cronFiles() {
		if job.IsAgent() && owner == job.Agent {
			return path, nil
		}
		if !job.IsAgent() && owner == "main" {
			return path, nil
		}
	}
	return "", UnknownJobError{Key: job.StateKey()}
}

func setJobEnabled(path, name string, enabled bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) != 1 {
		return fmt.Errorf("parse %s: empty document", path)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("parse %s: top-level document is not a mapping", path)
	}
	var jobs *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		if key.Kind == yaml.ScalarNode && key.Value == "jobs" && val.Kind == yaml.SequenceNode {
			jobs = val
			break
		}
	}
	if jobs == nil {
		return fmt.Errorf("parse %s: no jobs: array", path)
	}
	flag := enabledValue(enabled)
	for _, item := range jobs.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		named := false
		for i := 0; i+1 < len(item.Content); i += 2 {
			key, val := item.Content[i], item.Content[i+1]
			if key.Kind != yaml.ScalarNode || key.Value != "name" {
				continue
			}
			named = val.Kind == yaml.ScalarNode && val.Value == name
			break
		}
		if !named {
			continue
		}
		for i := 0; i+1 < len(item.Content); i += 2 {
			key, val := item.Content[i], item.Content[i+1]
			if key.Kind == yaml.ScalarNode && key.Value == "enabled" {
				val.Kind, val.Tag, val.Value = yaml.ScalarNode, "!!bool", flag
				return writeJobsYAML(path, &doc)
			}
		}
		item.Content = append(item.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "enabled"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: flag},
		)
		return writeJobsYAML(path, &doc)
	}
	return fmt.Errorf("parse %s: no job named %s", path, name)
}

// handleToggle serves POST /cron/toggle: decode the key/target, write the owning
// crons.yaml's `enabled:` flag, hot-reload the runner, and answer the fresh job set
// (the wire contract: one response carries the state it landed, no second GET required
// - CronJobs.vue still re-fetches, the ruled two-step). An unknown key is a loud 404 (a
// silently persisted unknown key would be invisible forever); a write or reload failure
// is 400/500 with the file untouched when the write never happened.
func (p *Plugin) handleToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	toggler := cronToggler(p.runner)
	if toggler == nil {
		http.Error(w, "cron toggle: no cron runner mounted", http.StatusNotImplemented)
		return
	}
	var req jobToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
		http.Error(w, "cron toggle: key and enabled are required", http.StatusBadRequest)
		return
	}
	jobs, err := toggler.ToggleJob(req.Key, req.Enabled)
	if err != nil {
		var unknown UnknownJobError
		if errors.As(err, &unknown) {
			http.Error(w, "cron toggle: "+err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, "cron toggle: "+err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(jobs); err != nil {
		return
	}
}

// enabledValue renders the flag the way yaml node scalars carry bools.
func enabledValue(enabled bool) string {
	if enabled {
		return "true"
	}
	return "false"
}

// writeJobsYAML replaces the whole document node. The write is the caller's
// responsibility to surface: a parse or lookup failure never reaches this point (the
// file stays untouched).
func writeJobsYAML(path string, doc *yaml.Node) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	// the config files are operator-owned. tmp+rename - a crash or power loss mid-write
	// must never truncate the operator's hand-owned file to zero bytes (the config
	// writer's pattern).
	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(path); statErr == nil {
		mode = fi.Mode()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
