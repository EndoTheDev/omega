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
//	         resume, export, list)      - capsule, moves
//
// The boot with the store capsule disabled must NOT fall to :memory:
// (the pre-wave fallback): it opens the DEFAULT SUPPLY - the same
// omega.db file the capsule's own mount resolves - so a fresh chat
// boots, persists, and SURVIVES A RESTART with the capsule gone. Only
// the management face (the tab + its routes) dies with the capsule.
//
// The removal recipe (the Requires edges ARE it): verdicts requires
// store, approval requires verdicts, continuous requires store - the
// disabled list carries the strand. The single-agent boot (layer 1 +
// 2 only) is the shape this test mounts.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// storelessCfg is the wave-2 no-capsule boot: the store capsule, its
// dependent verdict strand (approval) and the machine capsule disabled.
func storelessCfg(t *testing.T) Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Disabled.Modules = []string{"store", "continuous", "verdict", "approval"}
	return cfg
}

func TestStorelessBootOpensDefaultSupply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	cfg := storelessCfg(t)

	ag, _, _, pctx, _, _, _, err := newAgent(cfg, nil, trustFlags{})
	if err != nil {
		t.Fatalf("no-capsule boot: %v", err)
	}
	if ag == nil {
		t.Fatal("the agent did not build on the no-capsule boot (layer 1 - the agent IS substrate)")
	}
	st := pctx.Services().Resolve("store")
	if st == nil {
		t.Fatal("REMOVED MOUNT: the store seam is nil - the default supply did not resolve (the law: the SUPPLY is substrate-class)")
	}
	if owner := pctx.Services().Owner("store"); owner != "host" {
		t.Fatalf("store seam owner = %q, want host (the capsule owns the offer on a full mount, the host on the supply fallback)", owner)
	}
	if got, want := store.DefaultSupplyPath(), filepath.Join(home, "data", "omega.db"); got != want {
		t.Fatalf("DefaultSupplyPath = %q, want %q (the same file the capsule's mount resolves)", got, want)
	}
}

// TestStorelessBootFreshChatPersistsAcrossRestart is the acceptance
// itself: boot 1 creates a chat and a message; boot 2 (a RESTART, the
// same OMEGA_HOME, the store capsule still absent) reads them back
// from the default supply. The old :memory: fallback failed this test
// by construction - the sessions would not survive the restart.
func TestStorelessBootFreshChatPersistsAcrossRestart(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	cfg := storelessCfg(t)

	// boot 1: a fresh chat lives (POST /sessions parity at the store
	// level - the id creation is the chat floor, base set)
	_, st1, _, pctx1, _, _, _, err := newAgent(cfg, nil, trustFlags{})
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
	if pctx1.Services().Owner("store") != "host" {
		t.Fatalf("boot 1 store owner = %q, want host", pctx1.Services().Owner("store"))
	}
	if err := s1.AppendMessage(context.Background(), "sess-accept", core.NewUser("persist me")); err != nil {
		t.Fatalf("append boot 1: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close boot 1 supply: %v", err)
	}

	// boot 2: the restart. The same home, no store capsule, the supply
	// file still opens and the conversation is there.
	_, st2, _, pctx2, _, _, _, err := newAgent(cfg, nil, trustFlags{})
	if err != nil {
		t.Fatalf("boot 2: %v", err)
	}
	s2, ok := st2.(*store.Store)
	if !ok {
		t.Fatalf("boot 2 store = %T, want *store.Store", st2)
	}
	t.Cleanup(func() { s2.Close() })
	if pctx2.Services().Owner("store") != "host" {
		t.Fatalf("boot 2 store owner = %q, want host", pctx2.Services().Owner("store"))
	}
	// the agent's own seam read (the chat page's live read of the open
	// session - the layer-1 NEED) resolves the persisted session
	sess, err := s2.GetSession(context.Background(), "sess-accept")
	if err != nil {
		t.Fatalf("restart read: %v", err)
	}
	if sess.Label != "the surviving chat" {
		t.Fatalf("restarted session = %+v, want the label persisted", sess)
	}
	msgs, err := s2.GetActiveMessages(context.Background(), "sess-accept")
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

// TestNoStoreCapsuleRoutes: the management face is the CAPSULE's - on
// the no-capsule mount the store/continuous plugin seams are gone
// (their registrationRows never mount) while the substrate still
// serves. The seam-level negative is the boot truth; the route-level
// 404 rides the agent/api floor tests (the base set never carries the
// capsule patterns to 404-guard them).
func TestNoStoreCapsuleSurfaces(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	cfg := storelessCfg(t)
	_, _, _, pctx, _, _, _, err := newAgent(cfg, nil, trustFlags{})
	if err != nil {
		t.Fatalf("no-capsule boot: %v", err)
	}
	if svc := pctx.Services().Resolve("store"); svc == nil {
		t.Fatal("the supply fallback never resolved - the chat NEED is unmet")
	}
	// the capsules' OWN seams are gone - nothing answers for them
	for _, seam := range []string{"continuous", "verdicts"} {
		if pctx.Services().Resolve(seam) != nil {
			t.Fatalf("REMOVED MOUNT: the %q seam survived the disabled list", seam)
		}
	}
	// the agent itself builds on the no-capsule boot - nothing more.
	if pctx.Services().Resolve("continuous") != nil {
		t.Fatalf("REMOVED MOUNT: the continuous seam survived the disabled list")
	}
}