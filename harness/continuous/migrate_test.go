package continuous

// migrate_test.go: the canonical-id migration and the invariant it protects.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

func TestCanonicalIDsAreUniquePerHome(t *testing.T) {
	home := testHome(t)
	m := NewMachine(home, Default())
	for _, name := range []string{"scout", "cora"} {
		if err := m.CreateHome(name, "", ""); err != nil {
			t.Fatalf("CreateHome(%s): %v", name, err)
		}
	}
	seen := map[string]string{}
	for _, name := range []string{"scout", "cora"} {
		id := CanonicalSessionIDFor(name)
		if prev, dup := seen[id]; dup {
			t.Fatalf("%s and %s share the canonical id %q", prev, name, id)
		}
		seen[id] = name
		if id == CanonicalSessionID {
			t.Fatalf("%s took the bare id - the daemon's own store holds that one", name)
		}
	}
	// The default agent's chat keeps the bare id.
	if got := CanonicalSessionIDFor(agent.DefaultAgentName); got != CanonicalSessionID {
		t.Errorf("default agent id = %q, want the bare %q", got, CanonicalSessionID)
	}
}

// legacyCanonicalHome writes an agent home the pre-fix way.
func legacyCanonicalHome(t *testing.T, home, name string) string {
	t.Helper()
	if err := agent.ScaffoldAgent(home, name, ""); err != nil {
		t.Fatalf("scaffold %s: %v", name, err)
	}
	dir := agent.AgentDir(home, name)
	dsn := filepath.Join(dir, "data", "omega.db")
	st, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("open %s store: %v", name, err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.CreateAgentSession(ctx, CanonicalSessionID, "", CanonicalSessionID, name); err != nil {
		t.Fatalf("seed bare canonical for %s: %v", name, err)
	}
	if err := st.AppendMessage(ctx, CanonicalSessionID, core.NewAssistant(name+" report")); err != nil {
		t.Fatalf("seed message for %s: %v", name, err)
	}
	return dir
}

func TestMigrateCanonicalIDsRenamesLegacyChats(t *testing.T) {
	home := testHome(t)
	legacyCanonicalHome(t, home, "scout")
	legacyCanonicalHome(t, home, "cora")
	m := NewMachine(home, Default())

	// The roster reads the id the home carries.
	preview, _, _ := m.canonicalPreview(agent.AgentDir(home, "cora"), "cora")
	if preview != "" {
		t.Fatalf("pre-migration preview reads the bare id (%q) - the fixture is not legacy", preview)
	}

	if err := m.MigrateCanonicalIDs(); err != nil {
		t.Fatalf("MigrateCanonicalIDs: %v", err)
	}
	for _, name := range []string{"scout", "cora"} {
		dir := agent.AgentDir(home, name)
		st, err := store.Open(filepath.Join(dir, "data", "omega.db"))
		if err != nil {
			t.Fatalf("open %s store: %v", name, err)
		}
		sess, err := st.GetSession(context.Background(), CanonicalSessionIDFor(name))
		if err != nil {
			st.Close()
			t.Fatalf("%s canonical chat after migration: %v", name, err)
		}
		if sess.Agent != name {
			t.Errorf("%s chat agent = %q", name, sess.Agent)
		}
		msgs, err := st.GetMessages(context.Background(), CanonicalSessionIDFor(name))
		st.Close()
		if err != nil || len(msgs) != 1 {
			t.Fatalf("%s history after migration = %d msgs, %v; want 1 (the rename must not strand it)", name, len(msgs), err)
		}
		if got, _, _ := m.canonicalPreview(dir, name); got != name+" report" {
			t.Errorf("%s preview after migration = %q, want its own report", name, got)
		}
	}

	// Idempotent: a second mount renames nothing and errors nothing.
	if err := m.MigrateCanonicalIDs(); err != nil {
		t.Fatalf("second MigrateCanonicalIDs: %v", err)
	}
}

// listedCanonicalHome writes an agent home the way the pre-fix build created it.
func listedCanonicalHome(t *testing.T, home, name string) {
	t.Helper()
	if err := agent.ScaffoldAgent(home, name, ""); err != nil {
		t.Fatalf("scaffold %s: %v", name, err)
	}
	st, err := store.Open(filepath.Join(agent.AgentDir(home, name), "data", "omega.db"))
	if err != nil {
		t.Fatalf("open %s store: %v", name, err)
	}
	defer st.Close()
	if err := st.CreateAgentSession(context.Background(), CanonicalSessionIDFor(name), "", CanonicalSessionID, name); err != nil {
		t.Fatalf("seed listed canonical for %s: %v", name, err)
	}
}

// TestMigrateCanonicalVisibilityRepairsListedCanonicals is the round-1 regression pin.
func TestMigrateCanonicalVisibilityRepairsListedCanonicals(t *testing.T) {
	home := testHome(t)
	listedCanonicalHome(t, home, "scout")
	m := NewMachine(home, Default())

	// The daemon's MAIN store: one legacy canonical row plus the
	// maintainer's own conversation, which the backfill must not touch.
	mainStore, err := store.Open(filepath.Join(home, "data", "omega.db"))
	if err != nil {
		t.Fatalf("main store: %v", err)
	}
	defer mainStore.Close()
	ctx := context.Background()
	if err := mainStore.CreateAgentSession(ctx, CanonicalSessionID, "", CanonicalSessionID, agent.DefaultAgentName); err != nil {
		t.Fatalf("seed main canonical: %v", err)
	}
	if err := mainStore.CreateSession(ctx, "mine", "", "my conversation"); err != nil {
		t.Fatalf("seed human session: %v", err)
	}
	m.SetStoreResolver(func() agent.StoreProvider { return mainStore })

	for _, probe := range []struct {
		label string
		read  func() (core.Session, error)
	}{
		{"agent home", func() (core.Session, error) {
			st, err := store.Open(filepath.Join(agent.AgentDir(home, "scout"), "data", "omega.db"))
			if err != nil {
				return core.Session{}, err
			}
			defer st.Close()
			return st.GetSession(ctx, CanonicalSessionIDFor("scout"))
		}},
		{"main store", func() (core.Session, error) { return mainStore.GetSession(ctx, CanonicalSessionID) }},
	} {
		row, err := probe.read()
		if err != nil {
			t.Fatalf("%s canonical: %v", probe.label, err)
		}
		if row.MachineOwned {
			t.Fatalf("%s canonical is already machine-owned - the fixture is not the legacy shape", probe.label)
		}
	}

	if err := m.MigrateCanonicalVisibility(); err != nil {
		t.Fatalf("MigrateCanonicalVisibility: %v", err)
	}

	scoutStore, err := store.Open(filepath.Join(agent.AgentDir(home, "scout"), "data", "omega.db"))
	if err != nil {
		t.Fatalf("reopen scout store: %v", err)
	}
	scoutRow, err := scoutStore.GetSession(ctx, CanonicalSessionIDFor("scout"))
	if err != nil || !scoutRow.MachineOwned {
		scoutStore.Close()
		t.Fatalf("agent home canonical after backfill: machine-owned=%v err=%v", scoutRow.MachineOwned, err)
	}
	if sessions, err := scoutStore.ListSessions(ctx); err != nil || len(sessions) != 0 {
		scoutStore.Close()
		t.Fatalf("agent home lists %d sessions, %v - a machinery-owned chat is not the human's list", len(sessions), err)
	}
	scoutStore.Close()

	mainRow, err := mainStore.GetSession(ctx, CanonicalSessionID)
	if err != nil || !mainRow.MachineOwned {
		t.Fatalf("main canonical after backfill: machine-owned=%v err=%v", mainRow.MachineOwned, err)
	}
	sessions, err := mainStore.ListSessions(ctx)
	if err != nil {
		t.Fatalf("main list: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "mine" {
		t.Fatalf("main list = %+v, want exactly the maintainer's own conversation", sessions)
	}

	// Idempotent: a second mount writes nothing and errors nothing.
	if err := m.MigrateCanonicalVisibility(); err != nil {
		t.Fatalf("second MigrateCanonicalVisibility: %v", err)
	}
}

func TestMigrateCanonicalIDsSkipsHomesWithoutStores(t *testing.T) {
	home := testHome(t)
	// A scaffolded home that never ran, a malformed dir, and a home already carrying the
	// qualified id.
	if err := agent.ScaffoldAgent(home, "quiet", ""); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, "agents", ".staging"), 0o755); err != nil {
		t.Fatalf("dot dir: %v", err)
	}
	m := NewMachine(home, Default())
	if err := m.MigrateCanonicalIDs(); err != nil {
		t.Fatalf("MigrateCanonicalIDs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agent.AgentDir(home, "quiet"), "data", "omega.db")); !os.IsNotExist(err) {
		t.Error("the migration created a store for a home that never ran")
	}
	empty := NewMachine(t.TempDir(), Default())
	if err := empty.MigrateCanonicalIDs(); err != nil {
		t.Errorf("MigrateCanonicalIDs(no agents dir) = %v", err)
	}
}
