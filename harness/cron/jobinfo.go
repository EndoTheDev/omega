package cron

import "encoding/json"

// JobInfo is one read-only row in the job inspector listing
// (GET /cron/jobs, card t_db430fea): the job's definition fields, its
// kind (cron 5-field schedule or interval minutes), whether the
// scheduler fires it, and the next fire time in RFC3339 local time.
// Next is empty when the job is disabled, the schedule does not parse,
// or nothing ever fires - a disabled job must never promise a next
// fire.
//
// Capsule-owned vocabulary (part 3 seam constitution): the row shape
// moved here from core with the seam interfaces. The serving surface's
// consumer interface receives the rows as pre-marshaled JSON, so the
// wire bytes are identical to what the shared type produced.
type JobInfo struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // cron | interval
	// Schedule is the 5-field expression for kind cron; empty for
	// kind interval, whose cadence rides Minutes.
	Schedule string `json:"schedule"`
	Minutes  int    `json:"minutes,omitempty"`
	// Deliver is the resolved output target (new-session,
	// continuous-agent, append-to-file) - the ONE deliver vocabulary
	// the cron Job schema carries, legacy aliases already folded.
	Deliver string `json:"deliver"`
	Enabled bool   `json:"enabled"`
	Next    string `json:"next"`
	// Agent names the agent the job fires under, per the defaulting
	// law (empty authored field = the default agent): the agent home's
	// name for loader-owned wake jobs, the authored `agent:` for
	// main-home continuous-agent jobs, empty once resolved to the
	// default for in-process jobs (the default IS the absence).
	Agent string `json:"agent,omitempty"`
	// Model is the authored per-job model override; empty = the
	// session provider model (the defaulting law).
	Model string `json:"model,omitempty"`
	// File is the append-to-file target, home-relative; empty for the
	// other targets (and for append-to-file, where the empty file
	// means the journal/<date>.md naming).
	File string `json:"file,omitempty"`
	// Prompt is the job's authored user message. The listing carries
	// it so the editor drafts a full job (the whole-job upsert needs
	// it - an empty-prompt draft dies at "prompt is required").
	Prompt string `json:"prompt,omitempty"`
	// Skills are the fire-pinned skill names (empty = the job hunts
	// its own procedures; the bodies ride the fire prompt).
	Skills []string `json:"skills,omitempty"`
	// Last status: the outcome of the most recent fire. Status is ""
	// (never fired), "ok", or "error"; Last is the completion wall
	// clock in RFC3339 local time; StatusNote carries the capped error
	// message when a fire errored. The record survives restarts in
	// cronstate.json - a status is a promise the process can keep: it
	// names something that already happened.
	Status     string `json:"status,omitempty"`
	Last       string `json:"last,omitempty"`
	StatusNote string `json:"status_note,omitempty"`
}

// JobRows is the pre-marshaled wire form of Jobs() - the consumer
// interface the serving surface asserts (std types only; the capsule
// owns the row type, the consumer owns the route).
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
