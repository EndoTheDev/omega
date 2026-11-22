package cron

// The canvas surface: the cron capsule ships its view-model as canvas panels. The job
// set never changes through the action pipe - the module declares exactly one action,
// refresh: it answers the live payload and pushes the panel, no mutation.

import (
	"fmt"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

const (
	jobsPanelID   = "cron"
	actionRefresh = "cron.refresh_jobs"
)

type jobRow struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Schedule string   `json:"schedule,omitempty"`
	Minutes  int      `json:"minutes,omitempty"`
	Deliver  string   `json:"deliver"`
	Agent    string   `json:"agent,omitempty"`
	Model    string   `json:"model,omitempty"`
	File     string   `json:"file,omitempty"`
	Prompt   string   `json:"prompt,omitempty"`
	Skills   []string `json:"skills,omitempty"`
	Enabled  bool     `json:"enabled"`
	Next     string   `json:"next,omitempty"`
	Status     string `json:"status,omitempty"`
	Last       string `json:"last,omitempty"`
	StatusNote string `json:"status_note,omitempty"`
}

// jobsPayload is the jobs panel's view-model: the job count (the
// badge) and every job, sorted by name (the established render order).
type jobsPayload struct {
	Total int      `json:"total"`
	Jobs  []jobRow `json:"jobs"`
}

type capsule struct {
	runner *Runner

	mu     sync.Mutex
	update func(core.Panel) error
}

var _ core.PanelProvider = (*capsule)(nil)
var _ core.PanelUpdateSink = (*capsule)(nil)

func newCapsule(r *Runner) *capsule { return &capsule{runner: r} }

// SetPanelUpdate installs the canvas closure (core.PanelUpdateSink).
// Nil means no canvas built (or none left after a re-registration) -
// pushes stay silent and frontends catch up on their next snapshot.
func (c *capsule) SetPanelUpdate(update func(core.Panel) error) {
	c.mu.Lock()
	c.update = update
	c.mu.Unlock()
}

// push routes one panel through the installed closure. Nil-safe on the capsule itself -
// a runner without a canvas surface (the unit tests, embeds) simply never pushes.
func (c *capsule) push() {
	if c == nil {
		return
	}
	c.mu.Lock()
	up := c.update
	c.mu.Unlock()
	if up == nil {
		return
	}
	_ = up(c.jobsPanel())
}

func (c *capsule) Panels() []core.Panel {
	return []core.Panel{c.jobsPanel()}
}

func (c *capsule) jobsPanel() core.Panel {
	infos := []JobInfo(nil)
	if c.runner != nil {
		infos = c.runner.Jobs()
	}
	rows := make([]jobRow, 0, len(infos))
	for _, j := range infos {
		rows = append(rows, jobRow{
			Name:       j.Name,
			Kind:       j.Kind,
			Schedule:   j.Schedule,
			Minutes:    j.Minutes,
			Deliver:    j.Deliver,
			Agent:      j.Agent,
			Model:      j.Model,
			File:       j.File,
			Prompt:     j.Prompt,
			Skills:     j.Skills,
			Enabled:    j.Enabled,
			Next:       j.Next,
			Status:     j.Status,
			Last:       j.Last,
			StatusNote: j.StatusNote,
		})
	}
	return core.Panel{
		ID:      jobsPanelID,
		Title:   "Cron",
		Kind:    "list",
		Payload: jobsPayload{Total: len(rows), Jobs: rows},
		Actions: []core.PanelAction{
			{ID: actionRefresh, Label: "refresh", Module: "cron"},
		},
	}
}

func (c *capsule) HandleAction(call core.ActionCall) (any, error) {
	switch call.Action {
	case actionRefresh:
		lp := c.jobsPanel()
		c.push()
		return lp.Payload, nil
	default:
		return nil, fmt.Errorf("%w: %q", core.ErrUnknownAction, call.Action)
	}
}
