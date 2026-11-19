package continuous

// continuous_test.go: the home machinery self-checks.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/store"
)

// testHome writes a minimal omega home.
func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, f := range []string{"user.md", "memory.md"} {
		if err := os.WriteFile(filepath.Join(home, f), []byte("# test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestCreateHomeScaffoldsAndSeeds(t *testing.T) {
	home := testHome(t)
	m := NewMachine(home, Default())
	if err := m.CreateHome("scout", "patrol the journals", "17 * * * *"); err != nil {
		t.Fatalf("CreateHome: %v", err)
	}
	dir := agent.AgentDir(home, "scout")
	for _, sub := range []string{"config", "data", "skills"} {
		if fi, err := os.Stat(filepath.Join(dir, sub)); err != nil || !fi.IsDir() {
			t.Errorf("agent home missing %s/", sub)
		}
	}
	// The directive rides the config layer's system_prompt.
	data, err := os.ReadFile(filepath.Join(dir, "config", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "system_prompt") || !strings.Contains(string(data), "patrol the journals") {
		t.Errorf("directive not seeded as system_prompt:\n%s", data)
	}
	crons, err := os.ReadFile(filepath.Join(dir, "config", "crons.yaml"))
	if err != nil {
		t.Fatalf("wake schedule: %v", err)
	}
	if !strings.Contains(string(crons), "17 * * * *") {
		t.Errorf("schedule not seeded:\n%s", crons)
	}
	// The canonical chat exists in the agent's OWN store, bound to the agent,.
	st, err := store.Open(filepath.Join(dir, "data", "omega.db"))
	if err != nil {
		t.Fatalf("agent store: %v", err)
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	sess, err := st.GetSession(ctx, CanonicalSessionIDFor("scout"))
	if err != nil {
		t.Fatalf("canonical chat: %v", err)
	}
	if sess.ID != "scout-canonical" {
		t.Errorf("canonical chat id = %q, want scout-canonical", sess.ID)
	}
	if sess.Agent != "scout" {
		t.Errorf("canonical chat agent = %q, want scout", sess.Agent)
	}
	// The row /new reads must be MACHINE-OWNED: handleSessionNew (agent/api) folds exactly
	// when it is, and answers "fork" otherwise - which forks the agent relationship on the
	// create path every scaffold-created agent takes.
	if !sess.MachineOwned {
		t.Error("the seeded canonical chat is not machine-owned - /new would fork the relationship instead of compacting it")
	}
	// The bare id never exists in an agent home.
	if _, err := st.GetSession(ctx, CanonicalSessionID); err == nil {
		t.Error("agent home carries the BARE canonical id - two homes would collide again")
	}
}

func TestCreateHomeTwiceIsAnError(t *testing.T) {
	home := testHome(t)
	m := NewMachine(home, Default())
	if err := m.CreateHome("scout", "", ""); err != nil {
		t.Fatal(err)
	}
	// An overwriting create would fork two owners of one home.
	if err := m.CreateHome("scout", "", ""); err == nil {
		t.Fatal("second create accepted")
	}
}

func TestRosterSkipsMainAndShowsWakes(t *testing.T) {
	home := testHome(t)
	// Pre-migration residue: agents/default dies in the move and must never come back.
	if err := os.MkdirAll(filepath.Join(home, "agents", "default"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewMachine(home, Default())
	if err := m.CreateHome("scout", "", ""); err != nil {
		t.Fatal(err)
	}
	rows := m.Roster()
	if len(rows) != 1 {
		t.Fatalf("roster = %d rows for one agent home (main and default excluded), want 1", len(rows))
	}
	if rows[0].Name != "scout" || !rows[0].Continuous {
		t.Errorf("row = %+v", rows[0])
	}
	m2 := NewMachine(home, Default())
	if rows := m2.Roster(); len(rows) != 1 || rows[0].Name != "scout" {
		t.Errorf("after default residue, roster rows = %v", rows)
	}
	_ = m2
}

// TestCurrentCanonicalPicksTheHighestGenerationOnASecondTie pins the same-second
// tie-break: the store writes ISO stamps with second resolution, so two generations
// minted inside one second resolve by the generation NUMBER, never by the id string
// ("-10" sorts before "-2"). Archived generations never win, and a non-canonical id
// that merely starts with the base never competes.
func TestCurrentCanonicalPicksTheHighestGenerationOnASecondTie(t *testing.T) {
	at := "2026-11-17T10:00:00Z"
	rows := []core.Session{
		{ID: "scout-canonical", CreatedAt: at},
		{ID: "scout-canonical-2", CreatedAt: at},
		{ID: "scout-canonical-10", CreatedAt: at},
		{ID: "scout-canonicalish", CreatedAt: at},
	}
	if got, ok := CurrentCanonicalSession(rows, "scout"); !ok || got.ID != "scout-canonical-10" {
		t.Fatalf("current canonical = %q (found=%v), want scout-canonical-10", got.ID, ok)
	}
	rows[2].Archived = true
	if got, ok := CurrentCanonicalSession(rows, "scout"); !ok || got.ID != "scout-canonical-2" {
		t.Fatalf("current canonical after retiring -10 = %q (found=%v), want scout-canonical-2", got.ID, ok)
	}
	if _, ok := CurrentCanonicalSession([]core.Session{{ID: "scout-canonical-1"}}, "scout"); ok {
		t.Error("scout-canonical-1 resolved as canonical - generation 1 is the base id only")
	}
}

func TestCanonicalPreviewUnreadFlow(t *testing.T) {
	home := testHome(t)
	m := NewMachine(home, Default())
	if err := m.CreateHome("scout", "", ""); err != nil {
		t.Fatal(err)
	}
	dir := agent.AgentDir(home, "scout")
	st, err := store.Open(filepath.Join(dir, "data", "omega.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	id := CanonicalSessionIDFor("scout")
	if err := st.AppendMessage(ctx, id, core.Assistant{Content: "patrol complete, 0 anomalies"}); err != nil {
		t.Fatalf("append report: %v", err)
	}
	st.Close()
	preview, at, unread := m.canonicalPreview(dir, "scout")
	if !unread {
		t.Error("report without a read is marked read")
	}
	if preview != "patrol complete, 0 anomalies" {
		t.Errorf("preview = %q", preview)
	}
	if at == "" {
		t.Error("empty report timestamp")
	}
	if _, err := m.CanonicalChat("scout"); err != nil {
		t.Fatalf("CanonicalChat: %v", err)
	}
	if _, _, unread := m.canonicalPreview(dir, "scout"); unread {
		t.Error("viewed chat still unread")
	}
}

func TestCanonicalChatUnknownAgentErrors(t *testing.T) {
	home := testHome(t)
	m := NewMachine(home, Default())
	if _, err := m.CanonicalChat("nobody"); err == nil {
		t.Fatal("unknown agent answered with a view")
	}
}

func TestNextWakeParse(t *testing.T) {
	dir := t.TempDir()
	crons := "jobs:\n  - name: wake\n    schedule: \"5 * * * *\"\n"
	if err := os.WriteFile(filepath.Join(dir, "crons.yaml"), []byte(crons), 0o644); err != nil {
		t.Fatal(err)
	}
	next := nextWakeFor(filepath.Join(dir, "crons.yaml"))
	if next == "" {
		t.Fatal("no next wake parsed")
	}
	ts, err := time.Parse(time.RFC3339, next)
	if err != nil {
		t.Fatalf("next wake %q: %v", next, err)
	}
	if ts.Minute() != 5 {
		t.Errorf("next wake minute = %d, want 5", ts.Minute())
	}
	// No file, no wake - never an invented schedule.
	if got := nextWakeFor(filepath.Join(t.TempDir(), "crons.yaml")); got != "" {
		t.Errorf("missing file next wake = %q", got)
	}
}
