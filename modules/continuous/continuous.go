package continuous

// continuous.go: the home machinery and the cross-home reads (card
// t_72ecca67). Ruling 5 is the wall: the COMPLETE v1 read list is the
// roster and the canonical chat view; nothing else crosses a home
// boundary. Ruling 2: folder presence is registration - a directory
// under agents/ IS an agent, no registry file anywhere.

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
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/modules/store"
	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// storeTimeout bounds every cross-home read the roster and chat view
// make; a wedged agent store must never wedge the roster route.
const storeTimeout = 5 * time.Second

// chatMessagesTail is the chat view's message cap: the roster pane and
// the detail strip render tails, not archives. v1 ships the last 100
// messages per read; nothing else about the store crosses.
const chatMessagesTail = 100

// previewRuneCap truncates the report preview for the pane.
const previewRuneCap = 160

// Machine is the capsule's home machinery, one per process. home is
// OMEGA_HOME (the ROOT home - agents/ lives under it even when this
// process itself runs as an agent).
type Machine struct {
	home string
	cfg  Config
	// mu guards the live-wake map (rulings 7+9: one live wake per
	// agent; the guard skips, never queues). The value is the running
	// child's cancel function - the roster's sleep button handle.
	mu   sync.Mutex
	live map[string]func()
	// disabled is the config.yaml disabled.agents fold (v0.14.1), the
	// soft toggle: a name here stays registered (folder presence IS
	// registration - never removed from disk) but vanishes from the
	// roster and every wake trigger. The host swaps the list live
	// through SetDisabledAgents.
	disabledMu sync.RWMutex
	disabled   map[string]bool
	// spawnExe resolves the binary a wake child runs. os.Executable is
	// the law; tests swap it so a wake never executes the test binary.
	spawnExe func() (string, error)
}

// SetDisabledAgents swaps the soft-disable list live (the host's
// registry-toggle seam and hot config reload both take this path).
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

// agentDisabled reports one agent's soft-disable state.
func (m *Machine) agentDisabled(name string) bool {
	m.disabledMu.RLock()
	defer m.disabledMu.RUnlock()
	return m.disabled[name]
}

// setSpawnExe swaps the child-binary resolver (tests only).
func (m *Machine) setSpawnExe(fn func() (string, error)) {
	m.spawnExe = fn
}

// NewMachine builds the machinery for the omega home.
func NewMachine(home string, cfg Config) *Machine {
	m := &Machine{home: home, cfg: cfg, live: map[string]func(){}}
	m.SetDisabledAgents(cfg.Disabled) // the boot-time disabled.agents fold
	return m
}

// CreateHome runs the create flow (ruling 18): name + directive +
// schedule -> agents/<name>/ scaffolded, the directive seeded as the
// agent layer's system_prompt, the schedule seeded as the agent's own
// config/crons.yaml, and the canonical chat created in the agent's
// own store (ruling 6). A conflict is a loud error - an overwriting
// create would fork two owners of one home.
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
	// The one permanent chat, in the agent's OWN store (rulings 4+6):
	// full data isolation starts on creation day.
	st, err := store.Open(filepath.Join(dir, "data", "omega.db"))
	if err != nil {
		return fmt.Errorf("open %s store: %w", name, err)
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	if err := st.CreateAgentSession(ctx, CanonicalSessionID, "", CanonicalSessionID, name); err != nil {
		return fmt.Errorf("create canonical chat: %w", err)
	}
	return nil
}

// seedSystemPrompt appends the directive as the agent layer's
// system_prompt. The layer is the file the wake child loads (the agent
// layer law).
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

// seedWakeSchedule writes the agent's own crons.yaml (ruling 12: ONE
// cron module reads it; ruling 15: a human owns it - the agent has no
// tool that touches this file). One wake job; its prompt is the
// directive so a scheduled wake carries the same standing intent a
// human wake does.
func seedWakeSchedule(dir, schedule, directive string) error {
	prompt := directive
	if prompt == "" {
		prompt = "wake up and check your duties"
	}
	jobs := fmt.Sprintf("# wake schedules for the %s agent home - human-owned config\n# (the agent has no tool that touches this file).\njobs:\n  - name: wake\n    schedule: %s\n    prompt: %s\n", filepath.Base(dir), yamlScalar(schedule), yamlScalar(prompt))
	return os.WriteFile(filepath.Join(dir, "config", "crons.yaml"), []byte(jobs), 0o644)
}

// yamlScalar renders one safe yaml scalar.
func yamlScalar(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", "\\n").Replace(v) + `"`
}

// RosterRow is the webui roster pane's data (ruling 18): the
// last-report preview, the next wake, unread state.
type RosterRow struct {
	agent.Profile
	LastReport   string `json:"last_report,omitempty"`
	LastReportAt string `json:"last_report_at,omitempty"`
	NextWake     string `json:"next_wake,omitempty"`
	Unread       bool   `json:"unread"`
}

// Roster scans the agent homes into roster rows. Main is not on the
// roster: main is the daemon (ruling 1) and never wakes. A
// disabled.agents name stays registered - folder presence is
// registration - and just has NO roster row (v0.14.1: the soft
// toggle lives on the agents tab, the roster only shows live agents).
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
		row.LastReport, row.LastReportAt, row.Unread = m.canonicalPreview(p.Dir)
		row.NextWake = nextWakeFor(filepath.Join(p.Dir, "config", "crons.yaml"))
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

// canonicalView opens an agent's own store for the canonical chat -
// the allow-listed cross-home read (ruling 5).
func canonicalView(agentDir string) (core.StoreProvider, error) {
	return store.Open(filepath.Join(agentDir, "data", "omega.db"))
}

// canonicalPreview returns the roster's preview facts from the agent's
// canonical chat: the closing message's text (ruling 10 - the run's
// closing message IS the report), when it landed, and the unread flag
// (updated_at newer than last_read_at - output the human has not seen).
func (m *Machine) canonicalPreview(agentDir string) (preview, reportAt string, unread bool) {
	st, err := canonicalView(agentDir)
	if err != nil {
		return "", "", false // empty row beats a broken pane
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	sess, err := st.GetSession(ctx, CanonicalSessionID)
	if err != nil {
		return "", "", false // never created: nothing to preview
	}
	msgs, err := st.GetMessages(ctx, CanonicalSessionID)
	if err != nil {
		return "", sess.UpdatedAt, false
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msg := msgs[i]; core.MessageRole(msg) == "assistant" {
			preview = truncateRunes(agent.MessageText(msg), previewRuneCap)
			break
		}
	}
	return preview, sess.UpdatedAt, sess.LastReadAt == "" || sess.UpdatedAt > sess.LastReadAt
}

// ChatView is the canonical chat surface (ruling 5, the second
// allow-listed cross-home read): the session plus its tail.
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
	sess, err := st.GetSession(ctx, CanonicalSessionID)
	if err != nil {
		return ChatView{}, fmt.Errorf("%s canonical chat: %w", name, err)
	}
	msgs, err := st.GetMessages(ctx, CanonicalSessionID)
	if err != nil {
		return ChatView{}, fmt.Errorf("%s canonical history: %w", name, err)
	}
	if len(msgs) > chatMessagesTail {
		msgs = msgs[len(msgs)-chatMessagesTail:]
	}
	if err := st.SetSessionRead(ctx, CanonicalSessionID); err != nil {
		return ChatView{}, err
	}
	return ChatView{Name: name, Session: sess, Messages: msgs}, nil
}

// truncateRunes caps a preview at n runes.
func truncateRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// agentCrons is the minimal read shape of an agent home's
// config/crons.yaml (ruling 12: cron reads the SAME file for WHEN; this
// read renders only - WHEN ownership stays with the cron module).
type agentCrons struct {
	Jobs []struct {
		Name     string `yaml:"name"`
		Schedule string `yaml:"schedule"`
		Prompt   string `yaml:"prompt"`
		Enabled  *bool  `yaml:"enabled"`
	} `yaml:"jobs"`
}

// nextWakeFor parses one agent's crons.yaml and returns the earliest
// upcoming fire across its jobs, ISO-formatted. Missing file, no
// jobs, or a malformed schedule each render empty - the roster pane
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

// parseSchedule mirrors the cron module's parser shape (cron.Minute |
// Hour | Dom | Month | Dow): the same expression grammar cron fires
// on, so the roster's next-wake display and cron's actual wakes agree.
func parseSchedule(expr string) *schedule {
	return &schedule{parser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow), expr: expr}
}

type schedule struct {
	parser cron.Parser
	expr   string
}

// Next resolves the next fire time after the given instant.
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
