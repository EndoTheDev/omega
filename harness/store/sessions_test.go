package store

// sessions_test.go: the agent-scoped session surface. The laws asserted here: binding
// happens at creation, the default list never shows other agents' sessions, and
// pre-agent rows ride 'default' (nothing historical drifts).

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
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
	// empty agent creation binds default.
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

// HandleNewCommand is gone (t_146d3133): its only output was a new_session intent no host
// executed, so /new now lives where it can actually open a conversation - the webui's shell
// (a chat tab) and the terminal loop (a session on the first message). This pin keeps the
// name from returning to the capsule registry without a door behind it.
func TestNewCommandIsNotRegistered(t *testing.T) {
	for _, c := range sessionCommands() {
		if c.Name == "/new" {
			t.Fatalf("/new is back in the store capsule with no executor: %+v", c)
		}
	}
	if _, err := (&Plugin{}).handleSessionCommand(nil, context.Background(), "/new", ""); err == nil {
		t.Fatal("/new still resolves in the store capsule - drop it or give it a surface")
	}
}

func TestSessionsCommandAgentBindingSurfaces(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.CreateAgentSession(ctx, "scout-1", "", "probe row", "scout"); err != nil {
		t.Fatal(err)
	}
}
