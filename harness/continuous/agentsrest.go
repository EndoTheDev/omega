package continuous

// agentsrest.go: the roster-management REST face (the shell-ring
// shrink wave 2, ADR Workshop/omega-shell-ring-shrink.md - the agents
// tab is the continuous capsule's face, "multi-agent IS the continuous
// capsule's design"). Moved whole from agent/api/server_agents.go:
//
// GET /agents profile inventory (the roster list)
// POST /agents scaffold a new agent directory (or a clone)
// GET /agents/{name} one agent's detail
// PUT /agents/{name} the roster's edit affordance
// DELETE /agents/{name} delete the HOME, sessions included
// GET /agents/{name}/sessions sessions bound to this agent
// POST /agents/{name}/sessions create a session AS this agent
//
// THE FLOOR VERDICT (the task's classification rule): no handler here
// serves the chat floor - the chat page never fetches agent profiles
// (the only /agents consumer in the webui is the roster panel), and
// the canonical-chat cross-home reads (GET /continuous/{name}/chat)
// were born on this capsule's own route list. So ALL of the old file
// moved; nothing stayed base. The chat floor the proof checks is a
// single agent running and chatting - which touches none of these
// routes.
//
// The home is the ROOT home the plugin already resolved (NewPluginWithHome
// / datadir fallback) - the roster reads agents/ relative to it, the
// same surface the old env-scan answered. The store resolves per
// request (the store seam's live re-wire law): the session counts and
// the agent-session reads die with a disabled store, the profile
// reads never needed one.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// agentRoutes is the roster REST face, built on the plugin's root home and mount.
type agentRoutes struct {
	home    string
	ctx     *core.Context
	machine *Machine
}

// mountedStore resolves the daemon's store seam per request (the store seam's live. Nil
// = no store capsule mounted: the roster keeps its zero rows and the session routes
// answer 501.
func (ar *agentRoutes) mountedStore() agent.StoreProvider {
	st, _ := ar.ctx.Services().Resolve("store").(agent.StoreProvider)
	return st
}

// agentStoreFor resolves the store that OWNS agentName's sessions plus a closer for it.
// A nil store with a nil error means "nothing to read": no seam mounted, or a home that
// never ran (no store file). The daemon's store resolves agent-home rows by id through
// its router, so a caller that only needs ONE row can pass a name and still read
// through it.
func (ar *agentRoutes) agentStoreFor(name string) (agent.StoreProvider, func(), error) {
	st := ar.mountedStore()
	if st == nil {
		return nil, func() {}, nil
	}
	if name == agent.DefaultAgentName {
		return st, func() {}, nil
	}
	path := filepath.Join(agent.AgentDir(ar.home, name), "data", "omega.db")
	if _, err := os.Stat(path); err != nil {
		return nil, func() {}, nil // never ran: no store, no sessions
	}
	ast, err := store.Open(path)
	if err != nil {
		return nil, func() {}, err
	}
	return ast, func() { _ = ast.Close() }, nil
}

// readAgentSessions reads ONE agent's sessions from the store that OWNS them. A home
// that never ran has no store file: an empty list, never a created database.
func (ar *agentRoutes) readAgentSessions(ctx context.Context, name string) ([]core.Session, error) {
	st, closeFn, err := ar.agentStoreFor(name)
	if err != nil || st == nil {
		return nil, err
	}
	defer closeFn()
	as, _ := st.(agent.AgentSessionStore)
	if as == nil {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	return as.ListAgentSessions(cctx, name)
}

// writeAgentSession creates a session in the store that will OWN it.
func (ar *agentRoutes) writeAgentSession(ctx context.Context, name, id string) error {
	st := ar.mountedStore()
	if st == nil {
		return errNoStoreSeam
	}
	if name == agent.DefaultAgentName {
		as, _ := st.(agent.AgentSessionStore)
		if as == nil {
			return errNoStoreSeam
		}
		return as.CreateAgentSession(ctx, id, "", "", name)
	}
	ast, err := store.Open(filepath.Join(agent.AgentDir(ar.home, name), "data", "omega.db"))
	if err != nil {
		return err
	}
	defer ast.Close()
	cctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	return ast.CreateAgentSession(cctx, id, "", "", name)
}

// errNoStoreSeam is the 501 lane.
var errNoStoreSeam = errors.New("store does not support agent sessions")

// writeJSON renders one 200-family response.
func (ar *agentRoutes) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// httpError renders one failure.
func (ar *agentRoutes) httpError(w http.ResponseWriter, code int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf(format, args...)})
}

// registrationRows returns the pattern/handler rows the mount registers - exactly the.
func (ar *agentRoutes) registrationRows() []struct {
	pattern string
	handler func(http.ResponseWriter, *http.Request)
} {
	return []struct {
		pattern string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"/agents", ar.handleAgents},
		{"/agents/{name}", ar.handleAgent},
		{"PUT /agents/{name}", ar.handleAgentUpdate},
		{"DELETE /agents/{name}", ar.handleAgentDelete},
		{"POST /agents/{name}/canonical", ar.handleAgentCanonical},
		{"/agents/{name}/sessions", ar.handleAgentSessions},
	}
}

// agentRow is one roster row on the wire. An agent with no live canonical answers an
// empty id - its click MINTS one (POST /agents/{name}/canonical), so the field being
// absent never makes a row unclickable.
type agentRow struct {
	agent.Profile
	CanonicalSessionID string `json:"canonical_session_id,omitempty"`
	LastReport         string `json:"last_report,omitempty"`
	LastReportAt       string `json:"last_report_at,omitempty"`
}

// agentFacts is what the roster needs from outside the profile.
type agentFacts struct {
	canonical string
	report    string
	reportAt  string
}

// fillAgentSessions writes session counts and last activity into profiles from the.
func (ar *agentRoutes) fillAgentSessions(ctx context.Context, profiles []agent.Profile) map[string]agentFacts {
	if ar.mountedStore() == nil {
		return nil
	}
	facts := make(map[string]agentFacts, len(profiles))
	for i := range profiles {
		sessions, err := ar.readAgentSessions(ctx, profiles[i].Name)
		if err != nil {
			continue
		}
		profiles[i].SessionCount = len(sessions)
		for _, sess := range sessions {
			if sess.UpdatedAt > profiles[i].LastActivity {
				profiles[i].LastActivity = sess.UpdatedAt
			}
		}
		// The canonical chat, NOT the newest row: the click's target must not move when a.
		cur, ok := CurrentCanonicalSession(sessions, profiles[i].Name)
		if !ok {
			continue // never minted, or retired: the click mints anew
		}
		f := agentFacts{canonical: cur.ID, reportAt: cur.UpdatedAt}
		f.report = ar.canonicalPreview(ctx, profiles[i].Name, cur.ID)
		facts[profiles[i].Name] = f
	}
	return facts
}

// canonicalPreview reads one canonical chat's one-line preview off the store that. A
// dead read answers "" - the row renders without a preview, never with an invented one.
func (ar *agentRoutes) canonicalPreview(ctx context.Context, name, id string) string {
	st, closeFn, err := ar.agentStoreFor(name)
	if err != nil || st == nil {
		return ""
	}
	defer closeFn()
	cctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	msgs, err := st.GetMessages(cctx, id)
	if err != nil {
		return ""
	}
	return previewOf(msgs)
}

// rosterRows pairs each profile with its canonical chat facts.
func rosterRows(profiles []agent.Profile, facts map[string]agentFacts) []agentRow {
	rows := make([]agentRow, 0, len(profiles))
	for _, p := range profiles {
		f := facts[p.Name]
		rows = append(rows, agentRow{
			Profile:            p,
			CanonicalSessionID: f.canonical,
			LastReport:         f.report,
			LastReportAt:       f.reportAt,
		})
	}
	return rows
}

// handleAgents serves GET /agents (the inventory) and POST /agents (create an agent.
// POST carries TWO doors, one wire: a bare create scaffolds an empty always-off home; a
// create with inherit_from MATERIALIZES a clone of that source (the CLONE law - a full
// copy at birth, never a live link). Clone-then-delete is the rename path, so there is
// no separate duplicate endpoint.
func (ar *agentRoutes) handleAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profiles := agent.LoadProfiles(ar.home)
		last := ar.fillAgentSessions(r.Context(), profiles)
		ar.writeJSON(w, http.StatusOK, rosterRows(profiles, last))
	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InheritFrom string `json:"inherit_from"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ar.httpError(w, http.StatusBadRequest, "invalid request: %v", err)
			return
		}
		name := strings.TrimSpace(req.Name)
		from := strings.TrimSpace(req.InheritFrom)
		if from != "" {
			if err := ar.cloneAgent(from, name, req.Description); err != nil {
				status := http.StatusConflict
				if errors.Is(err, errCloneStoreTag) {
					status = http.StatusInternalServerError
				}
				ar.httpError(w, status, "clone agent: %v", err)
				return
			}
		} else if err := agent.ScaffoldAgent(ar.home, name, req.Description); err != nil {
			ar.httpError(w, http.StatusConflict, "scaffold agent: %v", err)
			return
		}
		p, err := agent.LoadProfile(ar.home, name)
		if err != nil {
			ar.httpError(w, http.StatusInternalServerError, "load agent: %v", err)
			return
		}
		ar.writeJSON(w, http.StatusCreated, p)
	default:
		ar.httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// errCloneStoreTag marks a clone whose copy landed but whose store re-tag failed. The
// destination has been removed by then (the request is retryable and nothing was
// registered), so it is a server fault, never the caller's conflict.
var errCloneStoreTag = errors.New("re-tag cloned sessions")

// retagAgentSessions is the clone's last SQLite step, swappable in tests (a real.
var retagAgentSessions = store.RetagAgentSessions

// wireStoreSnapshot installs the SQLite-backed store copier the CLONE law needs.
func init() {
	agent.StoreSnapshot = store.SnapshotStore
}

// cloneAgent runs the clone door's two halves as ONE step. A re-tag failure leaves
// exactly the phantom a mid-copy failure does - a registered folder with the source's
// rows still tagged to the source - so the destination is removed here too.
func (ar *agentRoutes) cloneAgent(from, to, description string) error {
	if err := agent.CloneAgent(ar.home, from, to, description); err != nil {
		return err
	}
	if err := ar.retagClonedSessions(from, to); err != nil {
		dir := agent.AgentDir(ar.home, to)
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			return fmt.Errorf("%w: %v (and the half-built %s could not be removed: %v - delete it by hand, folder presence registers an agent)", errCloneStoreTag, err, dir, rmErr)
		}
		return fmt.Errorf("%w: %v", errCloneStoreTag, err)
	}
	return nil
}

// retagClonedSessions rewrites the copied store's agent tags to the clone.
func (ar *agentRoutes) retagClonedSessions(from, to string) error {
	dsn := filepath.Join(agent.AgentDir(ar.home, to), "data", "omega.db")
	if from == agent.DefaultAgentName {
		return retagAgentSessions(dsn, to, agent.DefaultAgentName, "")
	}
	return retagAgentSessions(dsn, to, from)
}

// handleAgent serves GET /agents/{name}: one agent, detail payload.
func (ar *agentRoutes) handleAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ar.httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := r.PathValue("name")
	p, err := agent.LoadProfile(ar.home, name)
	if err != nil {
		ar.httpError(w, http.StatusNotFound, "unknown agent")
		return
	}
	profiles := []agent.Profile{p}
	last := ar.fillAgentSessions(r.Context(), profiles)
	ar.writeJSON(w, http.StatusOK, rosterRows(profiles, last)[0])
}

// handleAgentUpdate serves PUT /agents/{name}. A nil field is untouched.
func (ar *agentRoutes) handleAgentUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		ar.httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := r.PathValue("name")
	if name == agent.DefaultAgentName {
		ar.httpError(w, http.StatusConflict, "%s", agent.ErrAgentConfigLocked.Error())
		return
	}
	if _, err := agent.LoadProfile(ar.home, name); err != nil {
		ar.httpError(w, http.StatusNotFound, "unknown agent")
		return
	}
	var req struct {
		Description      *string             `json:"description"`
		ProviderInstance *string             `json:"provider_instance"`
		ModelName        *string             `json:"model_name"`
		SystemPrompt     *string             `json:"system_prompt"`
		MaxTurns         *int                `json:"max_turns"`
		Toggles          *[]string           `json:"toggles"`
		SkillsAllow      *[]string           `json:"skills_allow"`
		Inherit          *agent.AgentInherit `json:"inherit"`
		Unset            []string            `json:"unset"`
		// Retired secret knobs: decoded only so the refusal is LOUD. A client written before
		// the secret-free law gets the law spelled out (400), never a silent drop.
		ProviderType *string `json:"provider_type"`
		Host         *string `json:"host"`
		APIKey       *string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ar.httpError(w, http.StatusBadRequest, "invalid request: %v", err)
		return
	}
	updates := agent.AgentConfigUpdates{
		Description:      req.Description,
		ProviderInstance: req.ProviderInstance,
		ModelName:        req.ModelName,
		SystemPrompt:     req.SystemPrompt,
		MaxTurns:         req.MaxTurns,
		Toggles:          req.Toggles,
		SkillsAllow:      req.SkillsAllow,
		Inherit:          req.Inherit,
		Unset:            req.Unset,
		ProviderType:     req.ProviderType,
		Host:             req.Host,
		APIKey:           req.APIKey,
	}
	if err := agent.UpdateAgentConfigLayer(ar.home, name, updates); err != nil {
		// A scaffold-law refusal (unknown provider type, the locked default) is the caller's
		// mistake.
		var scaffold *agent.AgentConfigScaffoldError
		switch {
		case errors.Is(agent.ErrAgentConfigLocked, err):
			ar.httpError(w, http.StatusConflict, "%s", err.Error())
			return
		case errors.As(err, &scaffold):
			ar.httpError(w, http.StatusBadRequest, "invalid request: %v", err)
			return
		}
		ar.httpError(w, http.StatusConflict, "update agent config: %v", err)
		return
	}
	p, err := agent.LoadProfile(ar.home, name)
	if err != nil {
		ar.httpError(w, http.StatusInternalServerError, "load agent: %v", err)
		return
	}
	ar.writeJSON(w, http.StatusOK, p)
}

// handleAgentDelete serves DELETE /agents/{name}.
func (ar *agentRoutes) handleAgentDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		ar.httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := r.PathValue("name")
	if _, err := agent.LoadProfile(ar.home, name); err != nil {
		ar.httpError(w, http.StatusNotFound, "unknown agent")
		return
	}
	sessions := ar.sessionCount(r.Context(), name)
	// release the home's cached store handle BEFORE the removal.
	if forget, ok := ar.mountedStore().(agent.AgentHomeForgetter); ok {
		forget.ForgetHome(name)
	}
	if err := agent.DeleteAgentHome(ar.home, name); err != nil {
		var scaffold *agent.AgentConfigScaffoldError
		switch {
		case errors.Is(err, agent.ErrAgentHomeLocked):
			ar.httpError(w, http.StatusConflict, "%s", err.Error())
		case errors.Is(err, agent.ErrAgentNotExist):
			ar.httpError(w, http.StatusNotFound, "unknown agent")
		case errors.As(err, &scaffold):
			ar.httpError(w, http.StatusBadRequest, "invalid request: %v", err)
		default:
			ar.httpError(w, http.StatusInternalServerError, "delete agent: %v", err)
		}
		return
	}
	ar.writeJSON(w, http.StatusOK, map[string]any{"deleted": name, "sessions": sessions})
}

// sessionCount is the delete receipt's number. A store that will not open answers 0 -
// the delete still happens, the receipt just cannot count.
func (ar *agentRoutes) sessionCount(ctx context.Context, name string) int {
	rows, err := ar.readAgentSessions(ctx, name)
	if err != nil {
		return 0
	}
	return len(rows)
}

// handleAgentCanonical serves POST /agents/{name}/canonical: the roster row's click.
// The answer is the session row the chat tab binds to, so the panel never guesses an
// id.
func (ar *agentRoutes) handleAgentCanonical(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ar.httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := r.PathValue("name")
	if _, err := agent.LoadProfile(ar.home, name); err != nil {
		ar.httpError(w, http.StatusNotFound, "unknown agent")
		return
	}
	if ar.machine == nil {
		ar.httpError(w, http.StatusNotImplemented, "canonical chats need the continuous machinery")
		return
	}
	sess, err := ar.machine.EnsureCanonical(name)
	if err != nil {
		ar.httpError(w, http.StatusInternalServerError, "open canonical chat: %v", err)
		return
	}
	ar.writeJSON(w, http.StatusOK, sess)
}

// handleAgentSessions serves GET /agents/{name}/sessions (the agent's own session list
// - the default list never shows these rows) and POST /agents/{name}/sessions: create a
// session bound to this agent. Sessions are bound at creation and never switch agents,
// so there is no PATCH - a re-binding is a new session.
func (ar *agentRoutes) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, err := agent.LoadProfile(ar.home, name); err != nil {
		ar.httpError(w, http.StatusNotFound, "unknown agent")
		return
	}
	// The 501 lane: no store capsule mounted at all.
	if ar.mountedStore() == nil {
		ar.httpError(w, http.StatusNotImplemented, "%s", errNoStoreSeam.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		sessions, err := ar.readAgentSessions(r.Context(), name)
		if err != nil {
			ar.httpError(w, http.StatusInternalServerError, "list sessions: %v", err)
			return
		}
		// List contract: list endpoints emit arrays, never null. A zero- session agent must
		// render 200 [] or the webui's session read crashes on a null.length.
		if sessions == nil {
			sessions = []core.Session{}
		}
		ar.writeJSON(w, http.StatusOK, sessions)
	case http.MethodPost:
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ar.httpError(w, http.StatusBadRequest, "invalid request: %v", err)
			return
		}
		if req.ID == "" {
			id, err := agent.NewSessionID()
			if err != nil {
				// same fallback POST /sessions uses - crypto/rand never fails on supported.
				id = fmt.Sprintf("sess-%d", time.Now().UnixNano())
			}
			req.ID = id
		}
		// Same reserved-namespace guard as POST /sessions: an `eph-` id would create a
		// durable row the store-read endpoints can never serve (the eph- lane is in-memory
		// only).
		if agent.IsEphemeralID(req.ID) {
			ar.httpError(w, http.StatusBadRequest, "invalid request: reserved session id prefix "+agent.EphemeralIDPrefix)
			return
		}
		// the row is written to the store that will OWN it - the agent home's own
		// data/omega.db, never the daemon's (the boot migration would move it there later,
		// and the roster would read zero sessions in the meantime).
		if err := ar.writeAgentSession(r.Context(), name, req.ID); err != nil {
			if errors.Is(err, errNoStoreSeam) {
				ar.httpError(w, http.StatusNotImplemented, "%s", errNoStoreSeam.Error())
				return
			}
			ar.httpError(w, http.StatusConflict, "create session: %v", err)
			return
		}
		sess := core.Session{ID: req.ID, Agent: name}
		if rows, err := ar.readAgentSessions(r.Context(), name); err == nil {
			for _, row := range rows {
				if row.ID == req.ID {
					sess = row
					break
				}
			}
		}
		ar.writeJSON(w, http.StatusCreated, sess)
	default:
		ar.httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
