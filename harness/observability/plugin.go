package observability

import (
	"context"
	"fmt"
	"net/http"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin is the observability capsule: BOTH logging directions under one
// registration (0.17.3 modules fold, card t_ee3a968b). Outbound is the
// file logger (OMEGA_HOME/omega.log, offered on the `logger` seam every
// consumer still resolves by that name); inbound is the bounded
// run-event ring fed from the run event spine plus the omega.log tail -
// one read-only view, two GETs.
//
// One module means one row in /modules and one ledger entry: the tier
// re-law keeps it ALWAYS ON (Endo: "always on: agent-loop, provider and
// logging"), so it carries no Toggleable opt-in - a host cannot disable
// the machinery that records what it did.
//
// State lives here and nowhere else (the card's "no new global state
// beyond the capsule"): the logger, the ring, the pairing tables and the
// routes all hang off this instance.
type Plugin struct {
	logger agent.LoggerProvider
	ring   *Ring
	rec    *Recorder
	routes *Routes
}

var _ agent.Plugin = (*Plugin)(nil)

// NewPlugin returns an observability capsule instance.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "observability" }

// Description implements agent.Describer: the folded one-liner names both
// directions (the /modules row renders this line).
func (p *Plugin) Description() string {
	return "observability: file logging (OMEGA_HOME/omega.log) + the structured run-event ring and the omega.log tail (read-only)"
}

// ConfigKey implements agent.Describer: the `logging:` section (the fold
// moved the code, not the operator's config file). The ring's bound is a
// constant, not a knob - a knob here is a knob nobody can tune correctly
// from a config file.
func (p *Plugin) ConfigKey() string { return "logging" }

// Provides implements agent.Plugin: the logger seam, exactly as the
// pre-fold logging module did - dependents (delegate, skills, verdict)
// keep their `logger` edge and never learn the module changed name.
func (p *Plugin) Provides() []string { return []string{"logger"} }

func (p *Plugin) Requires() []string { return nil }

// MountWithAPI implements agent.APIPlugin: build the logger and offer it,
// register the /logs command, subscribe to the session-attributed spine,
// register the two view routes, and register one lifecycle handle whose
// Stop silences the recorder.
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
	cfg := Default()
	if c, ok := ctx.Configs["logging"].(Config); ok {
		cfg = c
	}
	logger, err := NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("observability: %w", err)
	}
	p.logger = logger
	if err := api.Offer("logger", logger); err != nil {
		return err
	}
	if err := api.RegisterCommand("/logs", "tail omega.log", []string{}, logsHandler(ctx)); err != nil {
		return err
	}

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
