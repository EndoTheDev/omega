package continuous

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// storeTimeout bounds every cross-home read the roster and chat view
// make; a wedged agent store must never wedge the roster route.
const storeTimeout = 5 * time.Second

// canonicalChatWriter is the store surface that creates a canonical chat HIDDEN.
type canonicalChatWriter interface {
	CreateCanonicalChat(ctx context.Context, id, agentName string) error
}

const chatMessagesTail = 100

const previewRuneCap = 160

// Machine is the capsule's home machinery, one per process.
type Machine struct {
	home string
	cfg  Config
	// mu guards the live-wake map: one live wake per agent, and the guard skips rather
	// than queues.
	mu   sync.Mutex
	live map[string]func()
	// disabled is the config.yaml disabled.agents fold, the soft toggle: a name here
	// stays registered (folder presence IS registration - never removed from disk) but
	// vanishes from the roster and every wake trigger.
	disabledMu sync.RWMutex
	disabled   map[string]bool
	// spawnExe resolves the binary a wake child runs. os.Executable is
	// the law; tests swap it so a wake never executes the test binary.
	spawnExe func() (string, error)
	// storeFn resolves the mounted store seam. Nil = no store capsule mounted: main's
	// canonical chat cannot be minted, and the roster row's click answers the error.
	storeFn func() agent.StoreProvider
}

func (m *Machine) SetDisabledAgents(names []string) {
	m.disabledMu.Lock()
	defer m.disabledMu.Unlock()
	if len(names) == 0 {
		m.disabled = nil
		return
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	m.disabled = set
}

func (m *Machine) agentDisabled(name string) bool {
	m.disabledMu.RLock()
	defer m.disabledMu.RUnlock()
	return m.disabled[name]
}

func (m *Machine) setSpawnExe(fn func() (string, error)) {
	m.spawnExe = fn
}

func (m *Machine) SetStoreResolver(fn func() agent.StoreProvider) {
	m.storeFn = fn
}

func (m *Machine) mountStore() agent.StoreProvider {
	if m.storeFn == nil {
		return nil
	}
	return m.storeFn()
}

// sessionRows lists the sessions a home holds. A home that never ran has no store file
// - an empty list, never a created database.
func (m *Machine) sessionRows(name string) ([]core.Session, error) {
	st := m.mountStore()
	if name == agent.DefaultAgentName {
		if st == nil {
			return nil, fmt.Errorf("no store seam mounted - main's canonical chat needs the daemon store")
		}
		as, _ := st.(agent.AgentSessionStore)
		if as == nil {
			return nil, fmt.Errorf("store does not support agent sessions")
		}
		ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
		defer cancel()
		return as.ListAgentSessions(ctx, name)
	}
	path := filepath.Join(agent.AgentDir(m.home, name), "data", "omega.db")
	if _, err := os.Stat(path); err != nil {
		return nil, nil // never ran: no sessions, no database
	}
	ast, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	defer ast.Close()
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	return ast.ListAgentSessions(ctx, name)
}

func CurrentCanonicalSession(sessions []core.Session, agentName string) (core.Session, bool) {
	var best core.Session
	found := false
	for _, sess := range sessions {
		if sess.Archived || !IsCanonicalSessionID(agentName, sess.ID) {
			continue
		}
		if !found || sess.CreatedAt > best.CreatedAt ||
			(sess.CreatedAt == best.CreatedAt &&
				canonicalGeneration(agentName, sess.ID) > canonicalGeneration(agentName, best.ID)) {
			best, found = sess, true
		}
	}
	return best, found
}

// EnsureCanonical answers agentName's canonical chat, MINTING it when the home holds.
// Its one caller is the roster's canonical route (POST /agents/{name}/canonical), so
// there is exactly one mint law.
func (m *Machine) EnsureCanonical(name string) (core.Session, error) {
	if _, err := agent.LoadProfile(m.home, name); err != nil {
		return core.Session{}, err
	}
	sessions, err := m.sessionRows(name)
	if err != nil {
		return core.Session{}, fmt.Errorf("read %s sessions: %w", name, err)
	}
	if cur, ok := CurrentCanonicalSession(sessions, name); ok {
		return cur, nil
	}
	taken := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		taken[sess.ID] = true
	}
	id := NextCanonicalSessionID(name, taken)
	if err := m.createCanonical(name, id); err != nil {
		return core.Session{}, fmt.Errorf("mint %s canonical chat: %w", name, err)
	}
	now := core.NowISO()
	return core.Session{ID: id, Label: store.CanonicalChatLabel, Agent: name, CreatedAt: now, UpdatedAt: now, LastReadAt: now, MachineOwned: true}, nil
}

func (m *Machine) createCanonical(name, id string) error {
	if name == agent.DefaultAgentName {
		st := m.mountStore()
		if st == nil {
			return fmt.Errorf("no store seam mounted")
		}
		w, ok := st.(canonicalChatWriter)
		if !ok {
			return fmt.Errorf("store cannot create a canonical chat")
		}
		// The main store is remote from this capsule (the daemon's own handle), so it gets.
		ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
		defer cancel()
		return w.CreateCanonicalChat(ctx, id, name)
	}
	path := filepath.Join(agent.AgentDir(m.home, name), "data", "omega.db")
	// A home scaffolded but never run has no data dir.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	ast, err := store.Open(path)
	if err != nil {
		return err
	}
	defer ast.Close()
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	w, ok := agent.StoreProvider(ast).(canonicalChatWriter)
	if !ok {
		return fmt.Errorf("store cannot create a canonical chat")
	}
	return w.CreateCanonicalChat(ctx, id, name)
}

// previewOf renders a canonical chat's one-line preview. "" when the conversation has
// no reply yet - never a fabricated placeholder.
func previewOf(msgs []core.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if core.MessageRole(msgs[i]) == "assistant" {
			return truncateRunes(strings.Join(strings.Fields(agent.MessageText(msgs[i])), " "), previewRuneCap)
		}
	}
	return ""
}

func NewMachine(home string, cfg Config) *Machine {
	m := &Machine{home: home, cfg: cfg, live: map[string]func(){}}
	m.SetDisabledAgents(cfg.Disabled) // the boot-time disabled.agents fold
	return m
}

// CreateHome runs the create flow. A conflict is a loud error - an overwriting create
// would fork two owners of one home.
func (m *Machine) CreateHome(name, directive, schedule string) error {
	if err := agent.ValidAgentName(name); err != nil {
		return err
	}
	if err := agent.ScaffoldAgent(m.home, name, directive); err != nil {
		return err
	}
	dir := agent.AgentDir(m.home, name)
	if directive != "" {
		if err := seedSystemPrompt(dir, directive); err != nil {
			return fmt.Errorf("seed directive: %w", err)
		}
	}
	if schedule != "" {
		if err := seedWakeSchedule(dir, schedule, directive); err != nil {
			return fmt.Errorf("seed schedule: %w", err)
		}
	}
	// The one permanent chat, in the agent's OWN store. It is seeded through the
	// MACHINERY-OWNED door, not the ordinary agent-session one: the `listed = 0` bit is
	// what makes the row machine-owned, and the /new compact keys on exactly that bit.
	st, err := store.Open(filepath.Join(dir, "data", "omega.db"))
	if err != nil {
		return fmt.Errorf("open %s store: %w", name, err)
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	if err := st.CreateCanonicalChat(ctx, CanonicalSessionIDFor(name), name); err != nil {
		return fmt.Errorf("create canonical chat: %w", err)
	}
	return nil
}

func seedSystemPrompt(dir, directive string) error {
	configPath := filepath.Join(dir, "config", "config.yaml")
	existing, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	text := strings.TrimRight(string(existing), "\n")
	if !strings.Contains(text, "system_prompt:") {
		text += fmt.Sprintf("\nsystem_prompt: %s\n", yamlScalar(directive))
	}
	return os.WriteFile(configPath, []byte(text), 0o644)
}

func seedWakeSchedule(dir, schedule, directive string) error {
	prompt := directive
	if prompt == "" {
		prompt = "wake up and check your duties"
	}
	jobs := fmt.Sprintf("# wake schedules for the %s agent home - human-owned config\n# (the agent has no tool that touches this file).\njobs:\n  - name: wake\n    schedule: %s\n    prompt: %s\n", filepath.Base(dir), yamlScalar(schedule), yamlScalar(prompt))
	return os.WriteFile(filepath.Join(dir, "config", "crons.yaml"), []byte(jobs), 0o644)
}

func yamlScalar(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", "\\n").Replace(v) + `"`
}

// RosterRow is the webui roster pane's data.
type RosterRow struct {
	agent.Profile
	LastReport   string `json:"last_report,omitempty"`
	LastReportAt string `json:"last_report_at,omitempty"`
	NextWake     string `json:"next_wake,omitempty"`
	Unread       bool   `json:"unread"`
}

// Roster scans the agent homes into roster rows. Main is not on the roster: main is the
// daemon and never wakes.
func (m *Machine) Roster() []RosterRow {
	profiles := agent.LoadProfiles(m.home)
	rows := make([]RosterRow, 0, len(profiles))
	for _, p := range profiles {
		if !p.Continuous {
			continue // main: the home root, no roster row
		}
		if m.agentDisabled(p.Name) {
			continue // soft-disabled: excluded from the roster
		}
		row := RosterRow{Profile: p}
		row.LastReport, row.LastReportAt, row.Unread = m.canonicalPreview(p.Dir, p.Name)
		row.NextWake = nextWakeFor(filepath.Join(p.Dir, "config", "crons.yaml"))
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

func canonicalView(agentDir string) (agent.StoreProvider, error) {
	return store.Open(filepath.Join(agentDir, "data", "omega.db"))
}

func agentSessions(ctx context.Context, st agent.StoreProvider, agentName string) ([]core.Session, error) {
	as, ok := st.(agent.AgentSessionStore)
	if !ok {
		return nil, nil
	}
	return as.ListAgentSessions(ctx, agentName)
}

func (m *Machine) canonicalPreview(agentDir, agentName string) (preview, reportAt string, unread bool) {
	st, err := canonicalView(agentDir)
	if err != nil {
		return "", "", false // empty row beats a broken pane
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	sessions, err := agentSessions(ctx, st, agentName)
	if err != nil {
		return "", "", false
	}
	sess, ok := CurrentCanonicalSession(sessions, agentName)
	if !ok {
		return "", "", false // never created, or retired: nothing to preview
	}
	msgs, err := st.GetMessages(ctx, sess.ID)
	if err != nil {
		return "", sess.UpdatedAt, false
	}
	return previewOf(msgs), sess.UpdatedAt, sess.LastReadAt == "" || sess.UpdatedAt > sess.LastReadAt
}

// ChatView is the canonical chat surface: the session plus its tail.
type ChatView struct {
	Name     string         `json:"name"`
	Session  core.Session   `json:"session"`
	Messages []core.Message `json:"messages"`
}

// CanonicalChat reads the canonical chat. Unknown agents and missing
// sessions error - the webui renders them, never guesses.
func (m *Machine) CanonicalChat(name string) (ChatView, error) {
	if _, err := agent.LoadProfile(m.home, name); err != nil {
		return ChatView{}, err
	}
	dir := agent.AgentDir(m.home, name)
	st, err := canonicalView(dir)
	if err != nil {
		return ChatView{}, fmt.Errorf("open %s store: %w", name, err)
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	// A read never mints: the roster's click and a wake do (EnsureCanonical).
	sessions, err := agentSessions(ctx, st, name)
	if err != nil {
		return ChatView{}, fmt.Errorf("%s sessions: %w", name, err)
	}
	sess, ok := CurrentCanonicalSession(sessions, name)
	if !ok {
		return ChatView{}, fmt.Errorf("%s has no canonical chat yet", name)
	}
	id := sess.ID
	msgs, err := st.GetMessages(ctx, id)
	if err != nil {
		return ChatView{}, fmt.Errorf("%s canonical history: %w", name, err)
	}
	if len(msgs) > chatMessagesTail {
		msgs = msgs[len(msgs)-chatMessagesTail:]
	}
	if err := st.SetSessionRead(ctx, id); err != nil {
		return ChatView{}, err
	}
	return ChatView{Name: name, Session: sess, Messages: msgs}, nil
}

func truncateRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// agentCrons is the minimal read shape of an agent home's config/crons.yaml.
type agentCrons struct {
	Jobs []struct {
		Name     string `yaml:"name"`
		Schedule string `yaml:"schedule"`
		Prompt   string `yaml:"prompt"`
		Enabled  *bool  `yaml:"enabled"`
	} `yaml:"jobs"`
}

// nextWakeFor parses one agent's crons.yaml and returns the earliest upcoming fire.
// Missing file, no jobs, or a malformed schedule each render empty - the roster pane
// shows a quiet sleeper, never an invented next wake.
func nextWakeFor(cronsPath string) string {
	data, err := os.ReadFile(cronsPath)
	if err != nil {
		return ""
	}
	var crons agentCrons
	if err := yaml.Unmarshal(data, &crons); err != nil {
		return ""
	}
	next := ""
	now := time.Now()
	for _, j := range crons.Jobs {
		if j.Enabled != nil && !*j.Enabled {
			continue
		}
		t, err := parseSchedule(j.Schedule).Next(now)
		if err != nil {
			continue
		}
		iso := t.Format(time.RFC3339)
		if next == "" || iso < next {
			next = iso
		}
	}
	return next
}

// parseSchedule mirrors the cron module's parser shape.
func parseSchedule(expr string) *schedule {
	return &schedule{parser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow), expr: expr}
}

type schedule struct {
	parser cron.Parser
	expr   string
}

func (s *schedule) Next(after time.Time) (time.Time, error) {
	id, err := s.parser.Parse(s.expr)
	if err != nil {
		return time.Time{}, err
	}
	next := id.Next(after)
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("schedule %q never fires again", s.expr)
	}
	return next, nil
}
