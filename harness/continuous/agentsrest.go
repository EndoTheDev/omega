package continuous

// agentsrest.go: the roster-management REST face.
//
// No handler here serves the chat floor - the chat page never fetches agent profiles
// (the only /agents consumer in the webui is the roster panel), and the canonical-chat
// cross-home reads (GET /continuous/{name}/chat) were born on this capsule's own route
// list. The store resolves per request (the store seam's live re-wire law): the session
// counts and the agent-session reads die with a disabled store, the profile reads never
// needed one.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

type agentRoutes struct {
	home    string
	ctx     *core.Context
	machine *Machine

	// newSessionID mints the roster session-create ids. Nil = agent.NewSessionID;
	// a test swaps it to force the mint-failure path.
	newSessionID func() (string, error)
}

// mintSessionID is the roster's id source: agent.NewSessionID unless a test pinned one.
func (ar *agentRoutes) mintSessionID() (string, error) {
	if ar.newSessionID != nil {
		return ar.newSessionID()
	}
	return agent.NewSessionID()
}

// mountedStore resolves the daemon's store seam per request (the store seam's live
// re-wire law). Nil = no store capsule mounted: the roster keeps its zero rows and the
// session routes answer 501.
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

// writeAgentSession creates a session in the store that will OWN it:
// the daemon's store for main, the agent home's own data/omega.db for an
// agent (created on first write - a session needs a home). The release
// func closes the agent store; main's is a no-op.
//
// A session created through this door is the caller's OWN conversation,
// never a machinery-owned one: `listed` stays 1 on both branches, and
// nothing here reaches for the canonical (listed = 0) door
// (store.CreateCanonicalChat). Pin: TestAgentSessionCreateKeepsBothListsHonest.
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

type agentFacts struct {
	canonical string
	report    string
	reportAt  string
}

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
		// The canonical chat, NOT the newest row: the click's target must not move when
		// the newest row changes.
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

// A dead read answers "": the row renders without a preview, never with an invented one.
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

// handleAgents serves GET /agents (the inventory) and POST /agents.
// POST carries TWO doors, one wire: a bare create scaffolds an empty always-off home; a
// create with inherit_from MATERIALIZES a clone of that source (the CLONE law - a full
// copy at birth, never a live link). Clone-then-delete is the rename path, so there is
// no separate duplicate endpoint.
func (ar *agentRoutes) handleAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profiles := agent.LoadProfiles(ar.home)
		last := ar.fillAgentSessions(r.Context(), profiles)
		writeJSON(w, http.StatusOK, rosterRows(profiles, last))
	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InheritFrom string `json:"inherit_from"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid request: %v", err)
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
				httpError(w, status, "clone agent: %v", err)
				return
			}
		} else if err := agent.ScaffoldAgent(ar.home, name, req.Description); err != nil {
			httpError(w, http.StatusConflict, "scaffold agent: %v", err)
			return
		}
		p, err := agent.LoadProfile(ar.home, name)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "load agent: %v", err)
			return
		}
		writeJSON(w, http.StatusCreated, p)
	default:
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// errCloneStoreTag marks a clone whose copy landed but whose store re-tag failed. The
// destination has been removed by then (the request is retryable and nothing was
// registered), so it is a server fault, never the caller's conflict.
var errCloneStoreTag = errors.New("re-tag cloned sessions")

// retagAgentSessions is the clone's last SQLite step, swappable in tests.
var retagAgentSessions = store.RetagAgentSessions

// The CLONE law's store copier (agent.StoreSnapshot has no default).
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
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := r.PathValue("name")
	p, err := agent.LoadProfile(ar.home, name)
	if err != nil {
		httpError(w, http.StatusNotFound, "unknown agent")
		return
	}
	profiles := []agent.Profile{p}
	last := ar.fillAgentSessions(r.Context(), profiles)
	writeJSON(w, http.StatusOK, rosterRows(profiles, last)[0])
}

// handleAgentUpdate serves PUT /agents/{name}. A nil field is untouched.
func (ar *agentRoutes) handleAgentUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := r.PathValue("name")
	if name == agent.DefaultAgentName {
		httpError(w, http.StatusConflict, "%s", agent.ErrAgentConfigLocked.Error())
		return
	}
	if _, err := agent.LoadProfile(ar.home, name); err != nil {
		httpError(w, http.StatusNotFound, "unknown agent")
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
		httpError(w, http.StatusBadRequest, "invalid request: %v", err)
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
			httpError(w, http.StatusConflict, "%s", err.Error())
			return
		case errors.As(err, &scaffold):
			httpError(w, http.StatusBadRequest, "invalid request: %v", err)
			return
		}
		httpError(w, http.StatusConflict, "update agent config: %v", err)
		return
	}
	p, err := agent.LoadProfile(ar.home, name)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "load agent: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleAgentDelete serves DELETE /agents/{name}.
func (ar *agentRoutes) handleAgentDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := r.PathValue("name")
	if _, err := agent.LoadProfile(ar.home, name); err != nil {
		httpError(w, http.StatusNotFound, "unknown agent")
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
			httpError(w, http.StatusConflict, "%s", err.Error())
		case errors.Is(err, agent.ErrAgentNotExist):
			httpError(w, http.StatusNotFound, "unknown agent")
		case errors.As(err, &scaffold):
			httpError(w, http.StatusBadRequest, "invalid request: %v", err)
		default:
			httpError(w, http.StatusInternalServerError, "delete agent: %v", err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": name, "sessions": sessions})
}

// sessionCount is the delete record's number. A store that will not open answers 0 -
// the delete still happens, the record just cannot count.
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
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := r.PathValue("name")
	if _, err := agent.LoadProfile(ar.home, name); err != nil {
		httpError(w, http.StatusNotFound, "unknown agent")
		return
	}
	if ar.machine == nil {
		httpError(w, http.StatusNotImplemented, "canonical chats need the continuous machinery")
		return
	}
	sess, err := ar.machine.EnsureCanonical(name)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "open canonical chat: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

// handleAgentSessions serves GET /agents/{name}/sessions (the agent's own session list)
// and POST /agents/{name}/sessions: create a session bound to this agent. A NAMED
// agent's rows stay in that agent home's own store and never appear in the human's
// list; a POST as the DEFAULT agent instead creates the caller's own sidebar-listed
// conversation on the main store. Sessions are bound at creation and never switch
// agents, so there is no PATCH - a re-binding is a new session.
func (ar *agentRoutes) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, err := agent.LoadProfile(ar.home, name); err != nil {
		httpError(w, http.StatusNotFound, "unknown agent")
		return
	}
	if ar.mountedStore() == nil {
		httpError(w, http.StatusNotImplemented, "%s", errNoStoreSeam.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		sessions, err := ar.readAgentSessions(r.Context(), name)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "list sessions: %v", err)
			return
		}
		// List contract: list endpoints emit arrays, never null. A zero- session agent must
		// render 200 [] or the webui's session read crashes on a null.length.
		if sessions == nil {
			sessions = []core.Session{}
		}
		writeJSON(w, http.StatusOK, sessions)
	case http.MethodPost:
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid request: %v", err)
			return
		}
		if req.ID == "" {
			id, err := ar.mintSessionID()
			if err != nil {
				httpError(w, http.StatusInternalServerError, "mint session id: %v", err)
				return
			}
			req.ID = id
		}
		// Same reserved-namespace guard as POST /sessions: an `eph-` id would create a
		// durable row the store-read endpoints can never serve (the eph- lane is in-memory
		// only).
		if agent.IsEphemeralID(req.ID) {
			httpError(w, http.StatusBadRequest, "invalid request: reserved session id prefix "+agent.EphemeralIDPrefix)
			return
		}
		// the row is written to the store that will OWN it - the agent home's own
		// data/omega.db, never the daemon's (the boot migration would move it there later,
		// and the roster would read zero sessions in the meantime).
		if err := ar.writeAgentSession(r.Context(), name, req.ID); err != nil {
			if errors.Is(err, errNoStoreSeam) {
				httpError(w, http.StatusNotImplemented, "%s", errNoStoreSeam.Error())
				return
			}
			httpError(w, http.StatusConflict, "create session: %v", err)
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
		writeJSON(w, http.StatusCreated, sess)
	default:
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
