package continuous

// migrate.go: the canonical-id migration (card t_5df01d0f). Every agent
// home created before the id became agent-qualified carries its one
// permanent chat under the BARE id "canonical" - and a bare id is what
// made two agents' roster rows open the same chat. Renaming the row (and
// its history) is the only shape that fixes a live instance: minting a
// fresh qualified chat beside it would leave the conversation stranded
// under the colliding id.
//
// Boot-time and idempotent: a home already carrying the qualified id has
// nothing to rename, so every mount may run it. Mount runs before any
// wake child can open a home (a wake spawns from the running daemon), so
// no chat is ever minted under the old id on the way.

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

// sessionListedReader/Writer are the store surface the visibility
// backfill needs, declared by the consumer: list one agent's rows and
// flip one row's list bit. The store knows the `listed` column, not what
// a canonical chat is.
type sessionListedReader interface {
	agent.AgentSessionStore
	SetSessionListed(ctx context.Context, id string, listed bool) error
}

// MigrateCanonicalVisibility hides every canonical chat that an older
// build seeded as a LISTED row (card t_10ac04f0, round-1 finding).
//
// A canonical chat's identity is its `listed = 0` bit: `MachineOwned`
// is derived from it (store_impl's scanSession) and /new's fold keys on
// it. Homes scaffolded before the seed went through the machinery-owned
// door carry `listed = 1`, so their chat answered `{"kind":"fork"}` on
// /new and dropped the human into a fresh main-store conversation -
// ruling 2 broken on the create path. The seed is fixed for NEW homes;
// this repairs the ones already on disk.
//
// Boot-time and idempotent: a row already machine-owned is skipped, so
// every mount may run it. It only ever HIDES a canonical-id row -
// nothing else is touched, and no row is created.
func (m *Machine) MigrateCanonicalVisibility() error {
	// main's canonical chat lives in the daemon's own store; a run
	// started against the bare id ("omega run --session canonical")
	// created it as an ordinary listed row before this card.
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

// hideCanonicalRows flips every LISTED canonical-id row of one agent to
// machine-owned. One row is expected; a retire history may hold several.
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

// MigrateCanonicalIDs renames each agent home's bare-id canonical chat
// to its agent-qualified id. A home with no store file, no canonical row
// or a row already at the qualified id is skipped - nothing is created.
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
