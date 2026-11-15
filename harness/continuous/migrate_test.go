package continuous

// migrate_test.go: the canonical-id migration and the invariant it
// protects (card t_5df01d0f). Every agent home's chat carries its own
// id, because the daemon resolves a session by id across homes - two
// homes holding one id is the collision that opened the wrong agent's
// chat.

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
	// The default agent's chat keeps the bare id: its sessions live in
	// the home's MAIN store, where nothing competes for it.
	if got := CanonicalSessionIDFor(agent.DefaultAgentName); got != CanonicalSessionID {
		t.Errorf("default agent id = %q, want the bare %q", got, CanonicalSessionID)
	}
}

// legacyCanonicalHome writes an agent home the pre-fix way: a store
// whose one permanent chat still carries the bare id, with history.
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

	// The roster reads the id the home carries: before the migration the
	// two homes answer the same one (the bug), after it each answers its
	// own.
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
		// The preview now answers from the home's own chat.
		if got, _, _ := m.canonicalPreview(dir, name); got != name+" report" {
			t.Errorf("%s preview after migration = %q, want its own report", name, got)
		}
	}

	// Idempotent: a second mount renames nothing and errors nothing.
	if err := m.MigrateCanonicalIDs(); err != nil {
		t.Fatalf("second MigrateCanonicalIDs: %v", err)
	}
}

func TestMigrateCanonicalIDsSkipsHomesWithoutStores(t *testing.T) {
	home := testHome(t)
	// A scaffolded home that never ran, a malformed dir, and a home
	// already carrying the qualified id: none may create, rename or
	// error.
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
	// No agents directory at all is equally quiet.
	empty := NewMachine(t.TempDir(), Default())
	if err := empty.MigrateCanonicalIDs(); err != nil {
		t.Errorf("MigrateCanonicalIDs(no agents dir) = %v", err)
	}
}
