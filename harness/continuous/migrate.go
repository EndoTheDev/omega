package continuous

// migrate.go: the canonical-id migration. Boot-time and idempotent: a home already
// carrying the qualified id has nothing to rename, so every mount may run it.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/datadir"
	"github.com/EndoTheDev/omega/harness/store"
)

// sessionListedReader/Writer are the store surface the visibility backfill needs,
// declared by the consumer.
type sessionListedReader interface {
	agent.AgentSessionStore
	SetSessionListed(ctx context.Context, id string, listed bool) error
}

// MigrateCanonicalVisibility hides every canonical chat that an older build seeded as.
// Boot-time and idempotent: a row already machine-owned is skipped, so every mount may
// run it.
func (m *Machine) MigrateCanonicalVisibility() error {
	// main's canonical chat lives in the daemon's own store.
	if st := m.mountStore(); st != nil {
		if w, ok := st.(sessionListedReader); ok {
			if err := hideCanonicalRows(w, agent.DefaultAgentName); err != nil {
				return fmt.Errorf("canonical visibility main: %w", err)
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(m.home, "agents"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no agent homes: nothing to repair
		}
		return fmt.Errorf("canonical visibility: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if agent.ValidAgentName(name) != nil {
			continue // malformed dir: never an owner, never touched
		}
		dsn := datadir.DBPath(filepath.Join(m.home, "agents", name), "omega.db")
		if _, err := os.Stat(dsn); err != nil {
			continue // never ran: no store, no row to repair
		}
		st, err := store.Open(dsn)
		if err != nil {
			return fmt.Errorf("canonical visibility %s: %w", name, err)
		}
		err = hideCanonicalRows(st, name)
		_ = st.Close()
		if err != nil {
			return fmt.Errorf("canonical visibility %s: %w", name, err)
		}
	}
	return nil
}

// hideCanonicalRows flips every LISTED canonical-id row of one agent to machine-owned.
func hideCanonicalRows(w sessionListedReader, agentName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	rows, err := w.ListAgentSessions(ctx, agentName)
	cancel()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.MachineOwned || !IsCanonicalSessionID(agentName, row.ID) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
		err := w.SetSessionListed(ctx, row.ID, false)
		cancel()
		if err != nil {
			return fmt.Errorf("hide %s: %w", row.ID, err)
		}
	}
	return nil
}

// MigrateCanonicalIDs renames each agent home's bare-id canonical chat to its.
func (m *Machine) MigrateCanonicalIDs() error {
	entries, err := os.ReadDir(filepath.Join(m.home, "agents"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no agent homes: nothing to rename
		}
		return fmt.Errorf("canonical ids: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if agent.ValidAgentName(name) != nil {
			continue // malformed dir: never an owner, never renamed
		}
		dsn := datadir.DBPath(filepath.Join(m.home, "agents", name), "omega.db")
		if err := store.RenameSession(dsn, CanonicalSessionID, CanonicalSessionIDFor(name)); err != nil {
			return fmt.Errorf("canonical id %s: %w", name, err)
		}
	}
	return nil
}
