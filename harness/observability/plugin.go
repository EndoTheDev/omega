package observability

import (
	"context"
	"fmt"
	"net/http"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin is the observability capsule: one bounded ring fed from the run
// event spine, one logger-path tail, two read-only routes. It owns no
// seam, offers no service and registers no tool - the whole surface is
// the two GETs, so disabling the capsule removes the observability view
// (and its recording) whole.
//
// State lives here and nowhere else (the card's "no new global state
// beyond the capsule"): the ring, the pairing tables and the routes all
// hang off this instance.
type Plugin struct {
	ring   *Ring
	rec    *Recorder
	routes *Routes
}

var _ agent.Plugin = (*Plugin)(nil)

// NewPlugin returns an observability capsule instance.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "observability" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "observability: the structured run-event ring + the omega.log tail (read-only)"
}

// ConfigKey implements agent.Describer: no config section. The ring's
// bound is a constant, not a knob - a knob here is a knob nobody can
// tune correctly from a config file.
func (p *Plugin) ConfigKey() string { return "" }

// Provides implements agent.Plugin: no seam is claimed and none is
// consumed (the logger is read through the need key at request time,
// nil-safe).
func (p *Plugin) Provides() []string { return nil }

func (p *Plugin) Requires() []string { return nil }

// Toggleable implements agent.Toggleable: the observability view is an
// opinion, not machine-critical - a disabled capsule stops recording and
// its two routes answer 404 at the module mux (the whole view, and its
// cost, leaves together).
func (p *Plugin) Toggleable() bool { return true }

// MountWithAPI implements agent.APIPlugin: subscribe to the
// session-attributed spine, register the two routes, and register one
// lifecycle handle whose Stop silences the recorder.
//
// The handle is the honest teardown story. The observer set on the
// Context is not part of the mount journal (AddSessionEventObserver is a
// Context method, not an ExtensionAPI registration), so a dismount
// cannot literally remove this closure; Stop flips the recorder's active
// flag instead, which costs a dead observer one atomic read per event and
// writes nothing. A remount mounts a fresh recorder - the silenced one
// stays inert. Recording starts ON at construction so a host that never
// arms lifecycle handles still gets a populated ring.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	p.ring = NewRing(RingCap)
	p.rec = NewRecorder(p.ring)
	p.routes = newRoutes(ctx, p.ring)
	ctx.AddSessionEventObserver(p.rec.Observe)
	for _, route := range []struct {
		pattern string
		h       func(http.ResponseWriter, *http.Request)
	}{
		{"GET /observability/events", p.routes.handleEvents},
		{"GET /observability/logs", p.routes.handleLogs},
	} {
		if err := api.RegisterRoute(route.pattern, route.h); err != nil {
			return fmt.Errorf("observability: register %s: %w", route.pattern, err)
		}
	}
	return api.RegisterLifecycle("ring",
		func(ctx context.Context, deps core.StartDeps) error {
			p.rec.Start()
			return nil
		},
		func() error {
			p.rec.Stop()
			return nil
		})
}
