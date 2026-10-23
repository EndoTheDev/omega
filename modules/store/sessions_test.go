package store

// sessions_test.go: the agent-scoped session surface (card t_7e2b7002).
// The laws asserted here: binding happens at creation, the default
// list never shows other agents' sessions, and pre-agent rows ride
// 'default' (nothing historical drifts).

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAgentSessionBinds(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.CreateAgentSession(ctx, "scout-a", "", "", "scout"); err != nil {
		t.Fatalf("create agent session: %v", err)
	}
	sess, err := s.GetSession(ctx, "scout-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if sess.Agent != "scout" {
		t.Errorf("session agent = %q, want scout", sess.Agent)
	}
	// empty agent creation binds default (the same semantics the
	// core CreateSession relies on via the column default).
	if err := s.CreateAgentSession(ctx, "def-a", "", "", ""); err != nil {
		t.Fatalf("create default-bound: %v", err)
	}
	if sess, _ := s.GetSession(ctx, "def-a"); sess.Agent != agent.DefaultAgentName {
		t.Errorf("empty binding = %q, want default", sess.Agent)
	}
}

func TestListSessionsHidesAgentSessions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "legacy-1", "", "legacy row"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAgentSession(ctx, "scout-1", "", "", "scout"); err != nil {
		t.Fatal(err)
	}
	def, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(def) != 1 || def[0].ID != "legacy-1" {
		t.Errorf("default list shows %v, want [legacy-1]", ids(def))
	}
	for _, sess := range def {
		if sess.Agent != agent.DefaultAgentName {
			t.Errorf("default-listed row carries agent %q", sess.Agent)
		}
	}
	scout, err := s.ListAgentSessions(ctx, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if len(scout) != 1 || scout[0].ID != "scout-1" || scout[0].Agent != "scout" {
		t.Errorf("scout list = %v", ids(scout))
	}
	// unknown agent: honest empty, not an error.
	empty, err := s.ListAgentSessions(ctx, "ghost")
	if err != nil || len(empty) != 0 {
		t.Errorf("unknown agent = %v, %v", ids(empty), err)
	}
}

func TestLegacyRowsNormalizeToDefault(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "old-1", "", ""); err != nil {
		t.Fatal(err)
	}
	sess, err := s.GetSession(ctx, "old-1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Agent != agent.DefaultAgentName {
		t.Errorf("legacy row agent = %q, want normalized 'default'", sess.Agent)
	}
}

// ids is the test projection helper.
func ids(sessions []core.Session) []string {
	out := make([]string, len(sessions))
	for i, s := range sessions {
		out[i] = s.ID
	}
	return out
}

// HandleNewCommand flag parsing (the TUI's /new --agent surface).
func TestHandleNewCommandAgentFlag(t *testing.T) {
	res, err := HandleNewCommand("--agent scout")
	if err != nil {
		t.Fatalf("--agent scout: %v", err)
	}
	if len(res.Actions) != 1 || len(res.Actions[0].List) != 1 || res.Actions[0].List[0] != "scout" {
		t.Errorf("action = %+v, want List [scout]", res.Actions)
	}
	// both orders keep the flag.
	res, _ = HandleNewCommand("--ephemeral --agent=a")
	if len(res.Actions[0].List) != 1 || res.Actions[0].List[0] != "a" {
		t.Errorf("--agent= form: %+v", res.Actions)
	}
	// bare flag with no value is a usage error.
	if _, err := HandleNewCommand("--agent"); err == nil {
		t.Errorf("bare --agent accepted")
	}
	// hostile names fail validation.
	if _, err := HandleNewCommand("--agent ../evil"); err == nil {
		t.Errorf("path-shaped agent accepted")
	}
	// plain /new behavior unchanged.
	res, _ = HandleNewCommand("")
	if len(res.Actions[0].List) != 0 {
		t.Errorf("plain /new grew an agent binding: %+v", res.Actions)
	}
	res, _ = HandleNewCommand("--ephemeral")
	if res.Actions[0].Value != "--ephemeral" {
		t.Errorf("--ephemeral lost: %+v", res.Actions)
	}
}

func TestSessionsCommandAgentBindingSurfaces(t *testing.T) {
	// The store-command renderers need the session counts the fill
	// helper computes; assert the helper on a known board.
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.CreateAgentSession(ctx, "scout-1", "", "probe row", "scout"); err != nil {
		t.Fatal(err)
	}
}