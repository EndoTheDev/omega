package continuous

// continuous_test.go: the home machinery self-checks (card t_72ecca67).
// Folder presence is registration (ruling 2); the canonical chat is one
// permanent session in the agent's OWN store (rulings 4+6); the roster
// reads are the only cross-home reads (ruling 5).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/store"
)

// testHome writes a minimal omega home: the main persona files and one
// scaffolded agent, nothing else. Every test starts here.
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
	// The scaffold: config/, data/, skills/ (ruling 1).
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
	// The schedule rides the agent's own crons.yaml (rulings 12+15).
	crons, err := os.ReadFile(filepath.Join(dir, "config", "crons.yaml"))
	if err != nil {
		t.Fatalf("wake schedule: %v", err)
	}
	if !strings.Contains(string(crons), "17 * * * *") {
		t.Errorf("schedule not seeded:\n%s", crons)
	}
	// The canonical chat exists in the agent's OWN store (ruling 6),
	// bound to the agent.
	st, err := store.Open(filepath.Join(dir, "data", "omega.db"))
	if err != nil {
		t.Fatalf("agent store: %v", err)
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	sess, err := st.GetSession(ctx, CanonicalSessionID)
	if err != nil {
		t.Fatalf("canonical chat: %v", err)
	}
	if sess.Agent != "scout" {
		t.Errorf("canonical chat agent = %q, want scout", sess.Agent)
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
	// Pre-migration residue: agents/default dies in the move (ruling 3)
	// and must never come back as a roster row.
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
	// Empty home: no rows at all under an agents/ dir holding nothing
	// but the dead default.
	m2 := NewMachine(home, Default())
	if rows := m2.Roster(); len(rows) != 1 || rows[0].Name != "scout" {
		t.Errorf("after default residue, roster rows = %v", rows)
	}
	_ = m2
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
	// First the report (assistant) lands - the wake ran while the human
	// slept. Unread because last_read_at is empty.
	if err := st.AppendMessage(ctx, CanonicalSessionID, core.Assistant{Content: "patrol complete, 0 anomalies"}); err != nil {
		t.Fatalf("append report: %v", err)
	}
	st.Close()
	preview, at, unread := m.canonicalPreview(dir)
	if !unread {
		t.Error("report without a read is marked read")
	}
	if preview != "patrol complete, 0 anomalies" {
		t.Errorf("preview = %q", preview)
	}
	if at == "" {
		t.Error("empty report timestamp")
	}
	// The webui chat view marks it read; the roster clears the flag.
	if _, err := m.CanonicalChat("scout"); err != nil {
		t.Fatalf("CanonicalChat: %v", err)
	}
	if _, _, unread := m.canonicalPreview(dir); unread {
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
