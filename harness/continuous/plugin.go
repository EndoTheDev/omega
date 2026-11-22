package continuous

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/datadir"
)

type Plugin struct {
	home    string   // the ROOT omega home; "" = datadir.Home() fallback
	cfg     Config
	machine *Machine // nil until MountWithAPI
	ctx     *core.Context
	ledger  *Ledger // nil until MountWithAPI; Close releases it
}

var _ agent.Plugin = (*Plugin)(nil)

// NewPluginWithHome creates the plugin against an explicit root home
// (tests, embedders, and the cmd/omega host that owns exe-dir
// resolution).
func NewPluginWithHome(home string) *Plugin { return &Plugin{home: home} }

func (p *Plugin) Name() string { return "continuous" }

func (p *Plugin) Description() string {
	return "continuous: agent homes, wake subprocesses, canonical chats, the evidence ledger"
}

func (p *Plugin) ConfigKey() string { return "continuous" }

func (p *Plugin) Provides() []string { return nil }

func (p *Plugin) Requires() []string { return []string{"store"} }

func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["continuous"].(Config); ok {
		cfg = c
	}
	p.cfg = cfg
	// The ROOT home, never the agent layer's: even a wake child receives the root via
	// OMEGA_HOME (the spawn contract), and the roster reads agents/ relative to it.
	home := p.home
	if home == "" {
		home = datadir.Home()
	}
	if home == "" {
		return fmt.Errorf("continuous: no home resolvable (set OMEGA_HOME)")
	}
	// Pin the RESOLVED home: the route builders run later and read it, and an agent.
	p.home = home
	machine := NewMachine(home, cfg)
	// The store seam, resolved PER CALL (the live re-wire law). An agent home's chat opens
	// its own file and never touches.
	machine.SetStoreResolver(func() agent.StoreProvider {
		st, _ := ctx.Services().Resolve("store").(agent.StoreProvider)
		return st
	})
	p.machine = machine
	p.ctx = ctx
	// The canonical-id migration. Idempotent, so every mount may run it; it touches only
	// files this capsule owns (agents/<name>/data/omega.db).
	if err := machine.MigrateCanonicalIDs(); err != nil {
		return err
	}
	// The visibility repair: homes created before the canonical seed went through.
	// Boot- time, idempotent, hides canonical-id rows only.
	if err := machine.MigrateCanonicalVisibility(); err != nil {
		return err
	}
	// The evidence ledger: runtime-owned, spine-fed, and NEVER tool-writable.
	ledgerData, err := datadir.AgentHome()
	if err != nil {
		return fmt.Errorf("continuous: ledger home: %w", err)
	}
	ledger, err := OpenLedger(datadir.Dir(ledgerData))
	if err != nil {
		return fmt.Errorf("continuous: ledger: %w", err)
	}
	if p.ledger != nil {
		_ = p.ledger.Close()
	}
	p.ledger = ledger
	ctx.AddEventObserver(ledger.observer())
	ctx.AddEventObserver(machine.tickObserver())

	for _, row := range p.routes() {
		if err := api.RegisterRoute(row.pattern, row.handler); err != nil {
			ledger.Close()
			return err
		}
	}
	return nil
}

// Toggleable marks this capsule as a module capsule (the tier law).
func (p *Plugin) Toggleable() bool { return true }

// SetDisabledAgents swaps the disabled.agents soft-toggle list live. Before Mount it is
// a no-op: the mount reads the config first.
func (p *Plugin) SetDisabledAgents(names []string) {
	if p.machine != nil {
		p.machine.SetDisabledAgents(names)
	}
}

// Dismount releases the mounted evidence ledger. The mount keeps the ledger's database
// open for the process's lifetime - beyond-Context state the journal cannot see.
// Nil-safe: Close before a mount (or twice) closes nothing.
func (p *Plugin) Dismount(ctx *core.Context) error {
	if p.ledger == nil {
		return nil
	}
	err := p.ledger.Close()
	p.ledger = nil
	return err
}

type routeRow struct {
	pattern string
	handler func(http.ResponseWriter, *http.Request)
}

func (p *Plugin) routes() []routeRow {
	m := p.machine
	rows := []routeRow{
		{"GET /continuous/roster", rosterHandler(m)},
		{"POST /continuous", createHandler(m)},
		{"GET /continuous/{name}/chat", chatHandler(m)},
		{"POST /continuous/{name}/chat", wakeHandler(m)},
		{"GET /continuous/{name}/evidence", evidenceHandler(m)},
		{"POST /continuous/{name}/sleep", sleepHandler(m)},
	}
	for _, r := range (&agentRoutes{home: p.home, ctx: p.ctx, machine: m}).registrationRows() {
		rows = append(rows, routeRow{pattern: r.pattern, handler: r.handler})
	}
	return rows
}

// rosterHandler answers the webui roster pane.
func rosterHandler(m *Machine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, m.Roster())
	}
}

// createHandler runs the create flow.
func createHandler(m *Machine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name      string `json:"name"`
			Directive string `json:"directive"`
			Schedule  string `json:"schedule"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, http.StatusBadRequest, "create: decode: %v", err)
			return
		}
		if err := m.CreateHome(body.Name, body.Directive, body.Schedule); err != nil {
			httpError(w, http.StatusBadRequest, "create: %v", err)
			return
		}
		writeJSON(w, map[string]string{"created": body.Name})
	}
}

// chatHandler answers the canonical chat view, marking the session read.
func chatHandler(m *Machine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		view, err := m.CanonicalChat(r.PathValue("name"))
		if err != nil {
			httpError(w, http.StatusNotFound, "%v", err)
			return
		}
		writeJSON(w, view)
	}
}

// wakeHandler is the human typing into the canonical chat.
func wakeHandler(m *Machine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		var body struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, http.StatusBadRequest, "wake: decode: %v", err)
			return
		}
		if strings.TrimSpace(body.Message) == "" {
			body.Message = "" // a bare wake rides the standing prompt
		}
		if err := m.Wake(name, body.Message, ""); err != nil {
			if err == ErrAwake {
				httpError(w, http.StatusConflict, "%v", err)
				return
			}
			httpError(w, http.StatusBadRequest, "%v", err)
			return
		}
		writeJSON(w, map[string]string{"status": "awake"})
	}
}

// evidenceHandler answers the detail strip's evidence rows.
func evidenceHandler(m *Machine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := m.Evidence(r.PathValue("name"))
		if err != nil {
			httpError(w, http.StatusBadRequest, "%v", err)
			return
		}
		writeJSON(w, rows)
	}
}

// sleepHandler is the roster's sleep button: nothing to sleep when no wake runs.
func sleepHandler(m *Machine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.Sleep(r.PathValue("name"))
		writeJSON(w, map[string]string{"status": "asleep"})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

func httpError(w http.ResponseWriter, code int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf(format, args...)})
}
