package main

// supply_test.go: the three-layer sessions law's ACCEPTANCE test (the
// shell-ring shrink wave 2, card t_490fed88 - ADR
// Workshop/omega-shell-ring-shrink.md). The law:
//
//	layer 1  the store NEED (conversation existence, compaction
//	         continuity)                - substrate, untouched
//	layer 2  the default SUPPLY (sqlite,
//	         omega.db)                  - substrate-class default
//	layer 3  session MANAGEMENT (browse,
//	         resume, export, list)      - capsule, moved
//
// The boot with the store capsule ABSENT (the scratch build's deleted
// capsule: no "store" offer on the mount) must resolve the DEFAULT
// SUPPLY - the same omega.db file the capsule's own mount resolves -
// never :memory: (the pre-wave fallback): a fresh chat boots, persists,
// and SURVIVES A RESTART with the capsule gone. Only the management
// face (the tab + its routes) dies with the capsule. The route-level
// 404s ride the agent/api floor tests (the base set never carried the
// capsule patterns) and the SPA manifest proof rides the webui suite.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// mountedContextSansStore is the removed-capsule mount: the Context a
// boot sees when the store capsule never offered (the capsule-deleted
// scratch build's shape - the mount journal replays nothing for it).
func mountedContextSansStore() *agent.Context {
	return &agent.Context{Configs: map[string]any{}}
}

func TestDefaultSupplyOpensTheCapsuleFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	pctx := mountedContextSansStore()
	st, err := openDefaultSupply(pctx, nil)
	if err != nil {
		t.Fatalf("supply fallback: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if pctx.Services().Owner("store") != "host" {
		t.Fatalf("store seam owner = %q, want host (the capsule owns the offer on a full mount, the host on the supply fallback)", pctx.Services().Owner("store"))
	}
	if got, want := store.DefaultSupplyPath(), filepath.Join(home, "data", "omega.db"); got != want {
		t.Fatalf("DefaultSupplyPath = %q, want %q (the same file the capsule's mount resolves)", got, want)
	}
}

// TestFreshChatPersistsAcrossRestart is the acceptance itself: boot 1
// creates a chat (the POST /sessions parity - the id creation is the
// chat floor) and a user turn; boot 2 (a RESTART: the same home, the
// fresh fallback Open) reads them back from the default supply. The
// old :memory: fallback failed this by construction.
func TestFreshChatPersistsAcrossRestart(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())

	// boot 1
	pctx1 := mountedContextSansStore()
	st1, err := openDefaultSupply(pctx1, nil)
	if err != nil {
		t.Fatalf("boot 1: %v", err)
	}
	s1, ok := st1.(*store.Store)
	if !ok {
		t.Fatalf("boot 1 store = %T, want *store.Store (the default supply is the sqlite supply)", st1)
	}
	if err := s1.CreateSession(context.Background(), "sess-accept", "", "the surviving chat"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s1.AppendMessage(context.Background(), "sess-accept", core.NewUser("persist me")); err != nil {
		t.Fatalf("append boot 1: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close boot 1 supply: %v", err)
	}

	// boot 2: the RESTART - the supply file re-opens, the conversation
	// is there, and the agent's seam read (the chat floor's live read)
	// resolves it like any capsule-opened store.
	pctx2 := mountedContextSansStore()
	st2, err := openDefaultSupply(pctx2, nil)
	if err != nil {
		t.Fatalf("boot 2: %v", err)
	}
	t.Cleanup(func() { st2.Close() })
	sess, err := st2.GetSession(context.Background(), "sess-accept")
	if err != nil {
		t.Fatalf("restart read: %v", err)
	}
	if sess.Label != "the surviving chat" {
		t.Fatalf("restarted session = %+v, want the persisted label", sess)
	}
	msgs, err := st2.GetActiveMessages(context.Background(), "sess-accept")
	if err != nil {
		t.Fatalf("restart messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("restarted messages = %d, want 1 (the persisted user turn)", len(msgs))
	}
	if u, ok := msgs[0].(core.User); !ok || u.Content != "persist me" {
		t.Fatalf("restarted message[0] = %T, want the user turn", msgs[0])
	}
}

// TestCapsuleDeletedRoutesDieWithIt: the MANAGEMENT face mounts and
// dies with the capsule. No store capsule = no management routes on
// the serving mux (the newRoutes face never registers) - the deleted
// capsule's 404 law; nothing to 404-guard in the base set.
func TestCapsuleDeletedRoutesDieWithIt(t *testing.T) {
	pctx := mountedContextSansStore()
	// the boot's mount has no store offer: the management face never
	// mounts, and the base's seam reads answer the supply fallback
	if pctx.Services().Resolve("store") != nil {
		t.Fatal("the removed mount resolved a capsule store offer - the fixture is broken")
	}
	if got := pctx.Services().Owner("store"); got != "" {
		t.Fatalf("no-offer mount owner = %q, want empty (the base set never re-registers a dead face)", got)
	}
}
