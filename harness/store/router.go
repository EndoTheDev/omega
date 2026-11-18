package store

// router.go: session-addressed routing across agent homes.

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// SetHome names the omega home this store belongs.
func (s *Store) SetHome(home string) {
	s.home = filepath.ToSlash(home)
}

// Compile-time assertion that Store carries the optional home-routing face the chat.
var _ agent.SessionOwnerStore = (*Store)(nil)

// isRouter reports whether this store is its home's MAIN store. A process running AS an
// agent (identity set) and any dsn outside <home>/data/omega.db never route: their one
// file already is the whole truth for them.
func (s *Store) isRouter() bool {
	if s.home == "" || s.identity != "" {
		return false
	}
	return filepath.ToSlash(s.dbPath) == filepath.ToSlash(datadir.DBPath(s.home, "omega.db"))
}

// route returns the store that owns sessionID. The local row wins (the common case): a
// local hit is never cached, so it re-probes with one indexed PK query per call.
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
	// Cache POSITIVE owners only.
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

// owner delegates to route and answers the owning store only when it is NOT this one.
func (s *Store) owner(ctx context.Context, sessionID string) *Store {
	if t := s.route(ctx, sessionID); t != s {
		return t
	}
	return nil
}

// SessionOwner implements the agent.SessionOwnerStore need face.
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

// probeHomes walks the home's agent directories and reports the first store holding.
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
		// Do NOT cache the miss: an agent home created while the daemon runs (the roster
		// mints homes at runtime) must start routing without a restart.
		return nil
	}
	if s.agents == nil {
		s.agents = map[string]*Store{}
	}
	s.agents[name] = st
	return st
}

// closeHomes releases every agent store this router opened.
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

// Compile-time assertion for the delete verb's release face.
var _ agent.AgentHomeForgetter = (*Store)(nil)

// ForgetHome implements the agent.AgentHomeForgetter need face the roster's DELETE.
func (s *Store) ForgetHome(agentName string) bool {
	if agentName == "" {
		return false
	}
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	st, ok := s.agents[agentName]
	if !ok {
		return false
	}
	delete(s.agents, agentName)
	for id, target := range s.routes {
		if target == st {
			delete(s.routes, id)
		}
	}
	if st != nil && st != s {
		// The error is the operating system's complaint about closing a handle; the caller.
		_ = st.Close()
	}
	return true
}
