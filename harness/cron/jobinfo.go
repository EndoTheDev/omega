package cron

import "encoding/json"

// JobInfo is one read-only row in the job inspector listing. Next is empty when the job
// is disabled, the schedule does not parse, or nothing ever fires - a disabled job must
// never promise a next fire.
type JobInfo struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // cron | interval
	Schedule string `json:"schedule"`
	Minutes  int    `json:"minutes,omitempty"`
	Deliver string `json:"deliver"`
	Enabled bool   `json:"enabled"`
	Next    string `json:"next"`
	Agent string `json:"agent,omitempty"`
	// AgentOwned is the FILE ownership mark. A non-empty Agent carries two different
	// meanings - the owning home's name on a wake row, the AUTHORED `agent:` on a
	// main-home continuous-agent job - and Agent alone cannot tell them apart. Consumers
	// that must refuse a cross-home write (the editor's read-only rows, the toggle's state
	// key) read THIS mark.
	AgentOwned bool `json:"agent_owned,omitempty"`
	Model string `json:"model,omitempty"`
	File string `json:"file,omitempty"`
	Prompt string `json:"prompt,omitempty"`
	Skills []string `json:"skills,omitempty"`
	// Last status: the outcome of the most recent fire. Status is "" (never fired), "ok",
	// or "error"; Last is the completion wall clock in RFC3339 local time; StatusNote
	// carries the capped error message when a fire errored.
	Status     string `json:"status,omitempty"`
	Last       string `json:"last,omitempty"`
	StatusNote string `json:"status_note,omitempty"`
}

// JobRows is the pre-marshaled wire form of Jobs.
func (r *Runner) JobRows() []json.RawMessage {
	jobs := r.Jobs()
	if jobs == nil {
		return nil
	}
	rows := make([]json.RawMessage, 0, len(jobs))
	for _, j := range jobs {
		b, err := json.Marshal(j)
		if err != nil {
			continue
		}
		rows = append(rows, b)
	}
	return rows
}
