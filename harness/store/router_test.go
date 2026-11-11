package store

// router_test.go: session-addressed routing across agent homes (card
// t_a7fab745, Endo's ruling A). Law: the chat floor holds ONE store
// handle and every conversation the agents roster lists lives in the
// owning agent home's own file - reads answer from it, writes land in
// it, and a main-store session behaves exactly as it did before.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// agentHomeFixture builds <home>/data/omega.db with one main session and
// <home>/agents/scout/data/omega.db with one agent session.
func agentHomeFixture(t *testing.T) (home, mainDSN, scoutDSN string) {
	t.Helper()
	home = t.TempDir()
	ctx := context.Background()
	mainDSN = datadir.DBPath(home, "omega.db")
	main, err := Open(mainDSN)
	if err != nil {
		t.Fatalf("open main store: %v", err)
	}
	if err := main.CreateSession(ctx, "sess-main", "", ""); err != nil {
		t.Fatalf("create main session: %v", err)
	}
	if err := main.AppendMessage(ctx, "sess-main", core.NewUser("main hello")); err != nil {
		t.Fatalf("append main message: %v", err)
	}
	if err := main.Close(); err != nil {
		t.Fatalf("close main store: %v", err)
	}
	scoutDSN = datadir.DBPath(filepath.Join(home, "agents", "scout"), "omega.db")
	scout, err := Open(scoutDSN)
	if err != nil {
		t.Fatalf("open scout store: %v", err)
	}
	if err := scout.CreateSession(ctx, "sess-scout-2", "", ""); err != nil {
		t.Fatalf("create scout session: %v", err)
	}
	if err := scout.AppendMessage(ctx, "sess-scout-2", core.NewUser("probe one")); err != nil {
		t.Fatalf("append scout message: %v", err)
	}
	if err := scout.AppendMessage(ctx, "sess-scout-2", core.NewAssistant("report one")); err != nil {
		t.Fatalf("append scout answer: %v", err)
	}
	if err := scout.Close(); err != nil {
		t.Fatalf("close scout store: %v", err)
	}
	return home, mainDSN, scoutDSN
}

func TestRouterServesAgentOwnedSession(t *testing.T) {
	ctx := context.Background()
	home, _, scoutDSN := agentHomeFixture(t)
	router, err := Open(datadir.DBPath(home, "omega.db"))
	if err != nil {
		t.Fatalf("open router: %v", err)
	}
	router.SetHome(home)
	t.Cleanup(func() { router.Close() })

	// READ routing: the three chat-floor receipts (session row,
	// transcript view, message history) answer from the agent file.
	sess, err := router.GetSession(ctx, "sess-scout-2")
	if err != nil {
		t.Fatalf("GetSession(scout) = %v, want the agent-owned row", err)
	}
	if sess.ID != "sess-scout-2" {
		t.Fatalf("GetSession id = %q", sess.ID)
	}
	view, err := router.GetActiveView(ctx, "sess-scout-2")
	if err != nil {
		t.Fatalf("GetActiveView(scout) = %v", err)
	}
	if len(view.Path) != 2 {
		t.Fatalf("active view has %d rows, want 2", len(view.Path))
	}
	msgs, err := router.GetMessages(ctx, "sess-scout-2")
	if err != nil || len(msgs) != 2 {
		t.Fatalf("GetMessages(scout) = %d msgs, %v; want 2", len(msgs), err)
	}

	// The owner face the chat floor reads before a run.
	if got := router.SessionOwner("sess-scout-2"); got != "scout" {
		t.Fatalf("SessionOwner(scout) = %q, want %q", got, "scout")
	}
	if got := router.SessionOwner("sess-main"); got != "" {
		t.Fatalf("SessionOwner(main) = %q, want empty", got)
	}
	if got := router.SessionOwner("sess-ghost"); got != "" {
		t.Fatalf("SessionOwner(unknown) = %q, want empty", got)
	}

	// WRITE routing: a turn's writes land in the owning file, never the
	// daemon's - a session forked across two databases is worse than the
	// 404 this replaces.
	if err := router.AppendMessage(ctx, "sess-scout-2", core.NewUser("from the chat floor")); err != nil {
		t.Fatalf("AppendMessage(scout) = %v", err)
	}
	if err := router.SetSessionTitle(ctx, "sess-scout-2", "Scout session", true); err != nil {
		t.Fatalf("SetSessionTitle(scout) = %v", err)
	}
	if err := router.AddSessionTokens(ctx, "sess-scout-2", 10, 20); err != nil {
		t.Fatalf("AddSessionTokens(scout) = %v", err)
	}
	if got := router.SessionOwner("sess-scout-2"); got != "scout" {
		t.Fatalf("owner after write = %q, want %q", got, "scout")
	}

	// Cold reopen: the writes are on disk in the agent's file.
	cold, err := Open(scoutDSN)
	if err != nil {
		t.Fatalf("reopen scout store: %v", err)
	}
	defer cold.Close()
	coldSession, err := cold.GetSession(ctx, "sess-scout-2")
	if err != nil {
		t.Fatalf("cold GetSession: %v", err)
	}
	if coldSession.Label != "Scout session" || coldSession.PromptTokens != 10 || coldSession.CompletionTokens != 20 {
		t.Fatalf("cold row = label %q, tokens %d/%d; want the routed writes",
			coldSession.Label, coldSession.PromptTokens, coldSession.CompletionTokens)
	}
	if n, err := cold.CountMessages(ctx, "sess-scout-2"); err != nil || n != 3 {
		t.Fatalf("cold message count = %d, %v; want 3", n, err)
	}
	// The daemon's own file holds nothing for the agent's session.
	if _, err := router.db.Exec(`SELECT 1`); err != nil {
		t.Fatalf("router handle: %v", err)
	}
	var n int
	if err := router.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE session_id = 'sess-scout-2'`).Scan(&n); err != nil {
		t.Fatalf("count in daemon store: %v", err)
	}
	if n != 0 {
		t.Fatalf("daemon store holds %d rows of the agent session, want 0", n)
	}
}

func TestRouterKeepsMainSessionLocal(t *testing.T) {
	ctx := context.Background()
	home, _, _ := agentHomeFixture(t)
	router, err := Open(datadir.DBPath(home, "omega.db"))
	if err != nil {
		t.Fatalf("open router: %v", err)
	}
	router.SetHome(home)
	defer router.Close()

	// The cross-store negative: a daemon-owned session works exactly as
	// today - reads, writes, and an unknown id still answering not-found.
	if sess, err := router.GetSession(ctx, "sess-main"); err != nil || sess.ID != "sess-main" {
		t.Fatalf("GetSession(main) = %+v, %v", sess, err)
	}
	if err := router.AppendMessage(ctx, "sess-main", core.NewAssistant("main answer")); err != nil {
		t.Fatalf("AppendMessage(main) = %v", err)
	}
	if n, err := router.CountMessages(ctx, "sess-main"); err != nil || n != 2 {
		t.Fatalf("main message count = %d, %v; want 2", n, err)
	}
	if _, err := router.GetSession(ctx, "sess-ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSession(unknown) = %v, want ErrNotFound", err)
	}
}

func TestRouterOffWithoutHome(t *testing.T) {
	ctx := context.Background()
	home, mainDSN, _ := agentHomeFixture(t)

	// No SetHome (CLI tools, bare Open): one file, today's behavior.
	plain, err := Open(mainDSN)
	if err != nil {
		t.Fatalf("open plain store: %v", err)
	}
	defer plain.Close()
	if _, err := plain.GetSession(ctx, "sess-scout-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("plain store answered an agent session: %v, want ErrNotFound", err)
	}

	// An agent's OWN store routes nothing even when it knows the home:
	// its one file is the whole truth for that agent's process.
	scout, err := Open(datadir.DBPath(filepath.Join(home, "agents", "scout"), "omega.db"))
	if err != nil {
		t.Fatalf("open scout store: %v", err)
	}
	defer scout.Close()
	scout.SetHome(home)
	if _, err := os.Stat(filepath.Join(home, "agents", "scout", "data", "omega.db")); err != nil {
		t.Fatalf("scout store file: %v", err)
	}
	if _, err := scout.GetSession(ctx, "sess-main"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("agent store routed to main: %v, want ErrNotFound", err)
	}
}
