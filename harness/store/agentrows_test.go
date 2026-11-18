package store


import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

func TestMigrateAgentRowsMovesHistoryWhole(t *testing.T) {
	ctx := context.Background()
	mainDSN := filepath.Join(t.TempDir(), "omega.db")
	main, err := Open(mainDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := main.CreateSession(ctx, "main-sess", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := main.AppendMessage(ctx, "main-sess", core.NewUser("main note")); err != nil {
		t.Fatal(err)
	}
	if err := main.CreateAgentSession(ctx, "scout-sess", "", "probe run", "scout"); err != nil {
		t.Fatal(err)
	}
	if err := main.AppendMessage(ctx, "scout-sess", core.NewUser("wake prompt")); err != nil {
		t.Fatal(err)
	}
	if err := main.AppendMessage(ctx, "scout-sess", core.NewAssistant("report body")); err != nil {
		t.Fatal(err)
	}
	if err := main.CreateAgentSession(ctx, "scout-sess-2", "scout-sess", "follow-up", "scout"); err != nil {
		t.Fatal(err)
	}
	if err := main.AppendMessage(ctx, "scout-sess-2", core.NewUser("second wake")); err != nil {
		t.Fatal(err)
	}
	if err := main.Close(); err != nil {
		t.Fatal(err)
	}

	agentDSN := filepath.Join(t.TempDir(), "scout", "data", "omega.db")
	if err := MigrateAgentRows(mainDSN, map[string]string{"scout": agentDSN}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	t.Setenv("OMEGA_AGENT", "scout")
	agentStore, err := Open(agentDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()
	sessions, err := agentStore.ListSessions(ctx)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("agent store sessions = %v %v, want both scout sessions", sessions, err)
	}
	msgs, err := agentStore.GetMessages(ctx, "scout-sess")
	if err != nil || len(msgs) != 2 {
		t.Fatalf("scout-sess messages = %d %v, want 2", len(msgs), err)
	}
	gotMain, err := agentStore.GetSession(ctx, "scout-sess")
	if err != nil || gotMain.Label != "probe run" || gotMain.Agent != "scout" {
		t.Errorf("moved session = %+v %v, want label/agent intact", gotMain, err)
	}
	// FTS index rides the move: the agent can search its own history.
	results, err := agentStore.SearchMessages(ctx, "report body")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	found := false
	for _, r := range results {
		if r.SessionID == "scout-sess" {
			found = true
		}
	}
	if !found {
		t.Errorf("FTS lost the moved history: %v", results)
	}

	t.Setenv("OMEGA_AGENT", "")
	reopened, err := Open(mainDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	sessions, err = reopened.ListSessions(ctx)
	if err != nil || len(sessions) != 1 || sessions[0].ID != "main-sess" {
		t.Fatalf("main store sessions after move = %v %v, want main-sess only", sessions, err)
	}
	if msgs, _ := reopened.GetMessages(ctx, "main-sess"); len(msgs) != 1 {
		t.Errorf("main session messages = %d, want 1 (the move must not touch main rows)", len(msgs))
	}

	// idempotent: a second migration is a no-op.
	if err := MigrateAgentRows(mainDSN, map[string]string{"scout": agentDSN}); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if sessions, _ := agentStore.ListSessions(ctx); len(sessions) != 2 {
		t.Errorf("second migration duplicated rows: %d sessions", len(sessions))
	}
}

// TestMigrateAgentRowsKeepsNativeDestRows pins the boot-brick fix: an
// agent store that already holds its own native session (a wake that
// wrote directly to its isolated store) must not fail the verify -
// the moved subset is what counts, not the whole sessions table.
func TestMigrateAgentRowsKeepsNativeDestRows(t *testing.T) {
	ctx := context.Background()
	mainDSN := filepath.Join(t.TempDir(), "omega.db")
	main, err := Open(mainDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := main.CreateAgentSession(ctx, "scout-sess", "", "probe run", "scout"); err != nil {
		t.Fatal(err)
	}
	if err := main.AppendMessage(ctx, "scout-sess", core.NewUser("wake prompt")); err != nil {
		t.Fatal(err)
	}
	if err := main.Close(); err != nil {
		t.Fatal(err)
	}

	agentDSN := filepath.Join(t.TempDir(), "scout", "data", "omega.db")
	if err := os.MkdirAll(filepath.Dir(agentDSN), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMEGA_AGENT", "scout")
	native, err := Open(agentDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := native.CreateAgentSession(ctx, "native-sess", "", "own wake", "scout"); err != nil {
		t.Fatal(err)
	}
	if err := native.Close(); err != nil {
		t.Fatal(err)
	}

	// the move must succeed despite the pre-existing row.
	if err := MigrateAgentRows(mainDSN, map[string]string{"scout": agentDSN}); err != nil {
		t.Fatalf("migrate with native dest rows: %v", err)
	}
	agentStore, err := Open(agentDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()
	sessions, err := agentStore.ListSessions(ctx)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("agent store sessions = %v %v, want native + moved", sessions, err)
	}

	t.Setenv("OMEGA_AGENT", "")
	reopened, err := Open(mainDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if sessions, _ := reopened.ListSessions(ctx); len(sessions) != 0 {
		t.Errorf("main store still holds %d agent rows", len(sessions))
	}
}

// TestMigrateAgentRowsSkipsChildlessAgents pins the no-phantom-store law.
func TestMigrateAgentRowsSkipsChildlessAgents(t *testing.T) {
	mainDSN := filepath.Join(t.TempDir(), "omega.db")
	main, err := Open(mainDSN)
	if err != nil {
		t.Fatal(err)
	}
	main.Close()
	agentDSN := filepath.Join(t.TempDir(), "scout", "data", "omega.db")
	if err := MigrateAgentRows(mainDSN, map[string]string{"scout": agentDSN}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := os.Stat(agentDSN); !os.IsNotExist(err) {
		t.Errorf("scout store file created for an agent that never wrote: %v", err)
	}
}

// TestRetagAgentSessions pins the clone law's last step.
func TestRetagAgentSessions(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "omega.db")
	st, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAgentSession(ctx, "scout-sess", "", "probe", "scout"); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAgentSession(ctx, "legacy-sess", "", "old", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := RetagAgentSessions(dsn, "probe", "scout", "default", ""); err != nil {
		t.Fatalf("retag: %v", err)
	}
	st, err = Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.ListAgentSessions(ctx, "probe")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("probe sessions = %d, want both copied rows", len(rows))
	}
	if rows, err := st.ListAgentSessions(ctx, "scout"); err != nil || len(rows) != 0 {
		t.Errorf("scout still owns %d rows (err %v)", len(rows), err)
	}
	// A missing store is a no-op: a source that never ran has nothing to
	// re-tag, and the clone must not create a database for it.
	if err := RetagAgentSessions(filepath.Join(t.TempDir(), "ghost.db"), "probe", "scout"); err != nil {
		t.Errorf("missing store answered %v, want a no-op", err)
	}
}
