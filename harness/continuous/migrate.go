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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/datadir"
	"github.com/EndoTheDev/omega/harness/store"
)

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
