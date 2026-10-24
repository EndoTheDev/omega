package cron

// The canvas surface (arc 6, t_31174f80): the cron capsule ships its
// view-model as canvas panels - the jobs panel, the module's read-only
// job listing the /cron command and the webui Cron tab already show.
// Both frontends render from the canvas; nothing outside the module
// holds a reference. The REST surface (GET /cron/jobs) stays where it
// is (settled Q6a, the same law kanban rode) and the panel surface is
// additive.
//
// The push law is the notifications capsule's law (arc 4b): mutations
// that run through the module's own faces refresh the panels through
// the sink the canvas runtime installed at registration. Cron's one
// module-internal mutation is the crons.yaml hot reload (the
// watcher in runner.go) - a reload pushes the refreshed jobs panel. The
// job set never changes through the action pipe: cron is read-only v1
// (crons.yaml is human-authored and omega may only ever propose jobs,
// the CronJobs.vue settled decision). The module declares exactly one
// action, refresh: it answers the live payload and pushes the panel -
// no mutation. The action id carries the capsule's module name on the
// wire, which is also how the canvas runtime learns it (the runtime
// registers a provider through its declared actions; the toast rides
// its sibling list panel the same way).

import (
	"fmt"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// jobsPanelID is the jobs panel's id. The panel ids the canvas routes
// on stay the module's own namespace; the capsule's module name rides
// its declared actions (refresh carries Module: "cron").
const (
	jobsPanelID   = "cron"
	actionRefresh = "refresh_jobs"
)

// jobRow is one jobs-panel line: the CronJobInfo fields verbatim (the
// same view GET /cron/jobs and the /cron table render). No action
// binding - the module is read-only.
type jobRow struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Enabled  bool   `json:"enabled"`
	Next     string `json:"next,omitempty"`
}

// jobsPayload is the jobs panel's view-model: the job count (the
// badge) and every job, sorted by name (the established render order).
type jobsPayload struct {
	Total int      `json:"total"`
	Jobs  []jobRow `json:"jobs"`
}

// capsule is the cron module's canvas surface. It is the
// PanelProvider the plugin appends to Context.PanelProviders during
// Mount - the registration IS the plugin. It also implements
// core.PanelUpdateSink: the canvas runtime installs the update closure
// at registration, and a hot reload pushes the refreshed panel
// through it.
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

// push routes one panel through the installed closure. A missing sink
// or a failed push is not a reload failure: the job set is already
// rescheduled, the snapshot is the catch-up (the same law the delta
// fan-out obeys). Nil-safe on the capsule itself - a runner without a
// canvas surface (the unit tests, embeds) simply never pushes.
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

// Panels declares the snapshot: the jobs panel, seeded from the live
// runner at registration.
func (c *capsule) Panels() []core.Panel {
	return []core.Panel{c.jobsPanel()}
}

// jobsPanel builds the panel descriptor from the live runner.
func (c *capsule) jobsPanel() core.Panel {
	infos := []core.CronJobInfo(nil)
	if c.runner != nil {
		infos = c.runner.Jobs()
	}
	rows := make([]jobRow, 0, len(infos))
	for _, j := range infos {
		rows = append(rows, jobRow{
			Name:     j.Name,
			Schedule: j.Schedule,
			Enabled:  j.Enabled,
			Next:     j.Next,
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

// HandleAction routes one action-pipe call (core.PanelProvider). The
// refresh action answers the live payload and pushes the panel - the
// module's read-only v1 posture in one arm: crons.yaml is
// human-authored, omega may only ever propose jobs, and nothing here
// mutates. Any other id is the same refusal the runtime's route
// checks encode.
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