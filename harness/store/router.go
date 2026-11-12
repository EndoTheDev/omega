package store

// router.go: session-addressed routing across agent homes (card
// t_a7fab745, Endo's ruling A, 2026-11-10). Per-agent data isolation
// moved every agent's sessions into agents/<name>/data/omega.db
// (cmd/omega/agents.go migrateAgentStores) and left no reverse map, so
// the chat floor's ONE store handle 404'd every conversation the agents
// roster lists. This file makes that handle resolve the store that OWNS
// a session id: reads answer from the owning file, and the run's writes
// land in it - a session forked across two databases would be worse
// than the 404 it replaces.
//
// Only the home's MAIN store routes. An agent's own store (opened by its
// wake child, or by the per-agent run Context) and every non-data dsn
// keep the single-file behavior they always had.

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// SetHome names the omega home this store belongs to. The store capsule
// passes the host-resolved home at mount; without it (a bare Open in a
// CLI or a test) routing is off and the store serves its own file only.
func (s *Store) SetHome(home string) {
	s.home = filepath.ToSlash(home)
}

// Compile-time assertion that Store carries the optional home-routing
// face the chat floor asks for before a run.
var _ agent.SessionOwnerStore = (*Store)(nil)

// isRouter reports whether this store is its home's MAIN store - the one
// session-addressed routing starts from. A process running AS an agent
// (identity set) and any dsn outside <home>/data/omega.db never route:
// their one file already is the whole truth for them.
func (s *Store) isRouter() bool {
	if s.home == "" || s.identity != "" {
		return false
	}
	return filepath.ToSlash(s.dbPath) == filepath.ToSlash(datadir.DBPath(s.home, "omega.db"))
}

// route returns the store that owns sessionID. The local row wins (the
// common case): a local hit is never cached, so it re-probes with one
// indexed PK query per call. A positive owner found in an agent home is
// cached by id; a miss is not cached. An id no home holds answers this
// store, so creating a session through it keeps today's main-store
// default.
//
// ponytail: the ownership cache is an unbounded map of session id ->
// store, bounded by the sessions one process touches in its lifetime. A
// home count in the hundreds would want a session_owners index table
// instead of the linear probe.
func (s *Store) route(ctx context.Context, sessionID string) *Store {
	if sessionID == "" || !s.isRouter() {
		return s
	}
	s.routeMu.Lock()
	if t, ok := s.routes[sessionID]; ok {
		s.routeMu.Unlock()
		return t
	}
	s.routeMu.Unlock()
	target := s
	if !s.hasSession(ctx, sessionID) {
		if owner := s.probeHomes(ctx, sessionID); owner != nil {
			target = owner
		}
	}
	// Cache POSITIVE owners only. Caching "this store owns it" would pin a
	// miss forever: an id first seen before its agent home existed (or
	// before the roster minted the home at all) would keep answering
	// not-found until a restart. A local hit re-probes with one indexed
	// PK query, which is what the common path paid before anyway.
	if target != s {
		s.routeMu.Lock()
		if s.routes == nil {
			s.routes = map[string]*Store{}
		}
		s.routes[sessionID] = target
		s.routeMu.Unlock()
	}
	return target
}

// owner delegates to route and answers the owning store only when it is
// NOT this one - the routing methods' one-line guard.
func (s *Store) owner(ctx context.Context, sessionID string) *Store {
	if t := s.route(ctx, sessionID); t != s {
		return t
	}
	return nil
}

// SessionOwner implements the agent.SessionOwnerStore need face: the
// agent home that owns sessionID, "" when this store's own file holds
// it. The chat floor reads it before a run to serve the session on the
// owning home's config instead of the daemon's.
func (s *Store) SessionOwner(sessionID string) string {
	o := s.owner(context.Background(), sessionID)
	if o == nil {
		return ""
	}
	return o.agentName
}

// hasSession reports whether this store's file holds the session row.
func (s *Store) hasSession(ctx context.Context, id string) bool {
	var one int
	return s.db.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, id).Scan(&one) == nil
}

// probeHomes walks the home's agent directories and reports the first
// store holding the row.
func (s *Store) probeHomes(ctx context.Context, id string) *Store {
	entries, err := os.ReadDir(filepath.Join(s.home, "agents"))
	if err != nil {
		return nil // no agents directory: this home has one store
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if agent.ValidAgentName(name) != nil {
			continue // malformed dir: never a session owner
		}
		st := s.homeStore(name)
		if st != nil && st.hasSession(ctx, id) {
			return st
		}
	}
	return nil
}

// homeStore opens (once per process) the store of one agent home. A home
// with no data/omega.db answers nil: opening would create an empty file
// for an agent that never ran.
func (s *Store) homeStore(name string) *Store {
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	if st, ok := s.agents[name]; ok {
		return st
	}
	dsn := datadir.DBPath(filepath.Join(s.home, "agents", name), "omega.db")
	var st *Store
	if _, err := os.Stat(dsn); err == nil {
		opened, oerr := Open(dsn)
		if oerr == nil {
			opened.home, opened.agentName = s.home, name
			st = opened
		}
	}
	if st == nil {
		// Do NOT cache the miss: an agent home created while the daemon
		// runs (the roster mints homes at runtime) must start routing
		// without a restart. The cost is one os.Stat per home per
		// unresolved id, not per request.
		return nil
	}
	if s.agents == nil {
		s.agents = map[string]*Store{}
	}
	s.agents[name] = st
	return st
}

// closeHomes releases every agent store this router opened. Called from
// Close: a handle left open keeps the agent's data file locked on
// windows.
func (s *Store) closeHomes() error {
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	var first error
	for name, st := range s.agents {
		if st != nil && st != s {
			if err := st.Close(); err != nil && first == nil {
				first = err
			}
		}
		delete(s.agents, name)
	}
	s.routes = nil
	return first
}
