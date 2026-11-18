package store

// router_test.go: session-addressed routing across agent homes. Law: the chat floor
// holds ONE store handle and every conversation the agents roster lists lives in the
// owning agent home's own file - reads answer from it, writes land in it, and a
// main-store session behaves exactly as it did before.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// agentHomeFixture builds <home>/data/omega.db with one main session and.
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

	if got := router.SessionOwner("sess-scout-2"); got != "scout" {
		t.Fatalf("SessionOwner(scout) = %q, want %q", got, "scout")
	}
	if got := router.SessionOwner("sess-main"); got != "" {
		t.Fatalf("SessionOwner(main) = %q, want empty", got)
	}
	if got := router.SessionOwner("sess-ghost"); got != "" {
		t.Fatalf("SessionOwner(unknown) = %q, want empty", got)
	}

	// WRITE routing: a turn's writes land in the owning file, never the daemon's.
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

	// The cross-store negative: a daemon-owned session works exactly as today.
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

func TestRouterSeesAgentHomeCreatedAfterStart(t *testing.T) {
	ctx := context.Background()
	home, mainDSN, _ := agentHomeFixture(t)
	// Start with NO agents directory: the roster has minted no home yet.
	if err := os.RemoveAll(filepath.Join(home, "agents")); err != nil {
		t.Fatalf("clear agents dir: %v", err)
	}
	router, err := Open(mainDSN)
	if err != nil {
		t.Fatalf("open router: %v", err)
	}
	router.SetHome(home)
	defer router.Close()

	if _, err := router.GetSession(ctx, "sess-late"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSession(late) before the home exists = %v, want ErrNotFound", err)
	}

	lateDSN := datadir.DBPath(filepath.Join(home, "agents", "scout"), "omega.db")
	late, err := Open(lateDSN)
	if err != nil {
		t.Fatalf("open late store: %v", err)
	}
	if err := late.CreateSession(ctx, "sess-late", "", ""); err != nil {
		t.Fatalf("create late session: %v", err)
	}
	if err := late.AppendMessage(ctx, "sess-late", core.NewUser("late hello")); err != nil {
		t.Fatalf("append late message: %v", err)
	}
	if err := late.Close(); err != nil {
		t.Fatalf("close late store: %v", err)
	}

	// No restart: the router must see the new home and route to it.
	if got := router.SessionOwner("sess-late"); got != "scout" {
		t.Fatalf("SessionOwner(late) = %q, want scout (a cached miss hid a home created after start)", got)
	}
	if sess, err := router.GetSession(ctx, "sess-late"); err != nil || sess.ID != "sess-late" {
		t.Fatalf("GetSession(late) = %+v, %v", sess, err)
	}
}

// TestRouterTwoHomesChatsAreTwoIDs pins the invariant the per-agent canonical id exists
// for: routing answers by ID, so two homes' permanent chats must be two ids.
func TestRouterTwoHomesChatsAreTwoIDs(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	mainDSN := datadir.DBPath(home, "omega.db")
	main, err := Open(mainDSN)
	if err != nil {
		t.Fatalf("open main: %v", err)
	}
	if err := main.CreateSession(ctx, "canonical", "", "canonical"); err != nil {
		t.Fatalf("main canonical: %v", err)
	}
	if err := main.AppendMessage(ctx, "canonical", core.NewUser("daemon chat")); err != nil {
		t.Fatalf("main message: %v", err)
	}
	if err := main.Close(); err != nil {
		t.Fatalf("close main: %v", err)
	}
	for _, name := range []string{"scout", "cora"} {
		dsn := datadir.DBPath(filepath.Join(home, "agents", name), "omega.db")
		st, err := Open(dsn)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		if err := st.CreateSession(ctx, name+"-canonical", "", "canonical"); err != nil {
			t.Fatalf("%s canonical: %v", name, err)
		}
		if err := st.AppendMessage(ctx, name+"-canonical", core.NewUser(name+" chat")); err != nil {
			t.Fatalf("%s message: %v", name, err)
		}
		if err := st.Close(); err != nil {
			t.Fatalf("close %s: %v", name, err)
		}
	}
	router, err := Open(mainDSN)
	if err != nil {
		t.Fatalf("open router: %v", err)
	}
	router.SetHome(home)
	defer router.Close()

	// Each id answers its OWN home.
	for _, name := range []string{"scout", "cora"} {
		id := name + "-canonical"
		if got := router.SessionOwner(id); got != name {
			t.Errorf("SessionOwner(%s) = %q, want %q", id, got, name)
		}
		msgs, err := router.GetMessages(ctx, id)
		if err != nil || len(msgs) != 1 {
			t.Fatalf("GetMessages(%s) = %d msgs, %v; want 1", id, len(msgs), err)
		}
		if got := agent.MessageText(msgs[0]); got != name+" chat" {
			t.Errorf("GetMessages(%s) answered %q - another home's chat", id, got)
		}
	}
	if got := router.SessionOwner("canonical"); got != "" {
		t.Errorf("SessionOwner(canonical) = %q, want empty (the daemon's own store)", got)
	}
}

// TestForgetHomeReleasesTheHandle pins the DELETE verb's release face.
func TestForgetHomeReleasesTheHandle(t *testing.T) {
	ctx := context.Background()
	home, _, scoutDSN := agentHomeFixture(t)
	router, err := Open(datadir.DBPath(home, "omega.db"))
	if err != nil {
		t.Fatalf("open router: %v", err)
	}
	router.SetHome(home)
	defer router.Close()

	if _, err := router.GetSession(ctx, "sess-scout-2"); err != nil {
		t.Fatalf("GetSession(scout): %v", err)
	}
	if _, ok := router.agents["scout"]; !ok {
		t.Fatal("the routed read left no cached handle - the fixture no longer exercises the lock")
	}
	if router.ForgetHome("") || router.ForgetHome("cora") {
		t.Error("ForgetHome answered true with no handle to drop")
	}
	if !router.ForgetHome("scout") {
		t.Fatal("ForgetHome(scout) = false, want true (a handle was cached)")
	}
	if router.ForgetHome("scout") {
		t.Error("the second ForgetHome still reported a handle")
	}
	// Released: the home's directory can be removed, and the router no longer answers.
	if err := os.RemoveAll(filepath.Join(home, "agents", "scout")); err != nil {
		t.Fatalf("remove released home: %v", err)
	}
	if _, err := router.GetSession(ctx, "sess-scout-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSession after the home is gone = %v, want ErrNotFound", err)
	}
	if got := router.SessionOwner("sess-scout-2"); got != "" {
		t.Fatalf("SessionOwner after the home is gone = %q, want empty", got)
	}
	if _, err := os.Stat(scoutDSN); !os.IsNotExist(err) {
		t.Fatalf("the released store file survived removal (%v)", err)
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

	// An agent's OWN store routes nothing even when it knows the home.
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
