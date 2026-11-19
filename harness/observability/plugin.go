package observability

import (
	"context"
	"fmt"
	"net/http"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin is the observability capsule: BOTH logging directions under one registration.
// One module means one row in /modules and one ledger entry: the tier re-law keeps it
// ALWAYS ON (Endo: "always on: agent-loop, provider and logging"), so it carries no
// Toggleable opt-in - a host cannot disable the machinery that records what it did.
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

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "observability: file logging (OMEGA_HOME/omega.log) + the structured run-event ring and the omega.log tail (read-only)"
}

// ConfigKey implements agent.Describer.
func (p *Plugin) ConfigKey() string { return "logging" }

// Provides implements agent.Plugin: the logger seam, exactly as the
// pre-fold logging module did - dependents (delegate, skills, verdict)
// keep their `logger` edge and never learn the module changed name.
func (p *Plugin) Provides() []string { return []string{"logger"} }

func (p *Plugin) Requires() []string { return nil }

// MountWithAPI implements agent.APIPlugin. The observer set on the Context is not part
// of the mount journal (AddSessionEventObserver is a Context method, not an
// ExtensionAPI registration), so a dismount cannot literally remove this closure; Stop
// flips the recorder's active flag instead, which costs a dead observer one atomic read
// per event and writes nothing. Recording starts ON at construction so a host that
// never arms lifecycle handles still gets a populated ring.
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
