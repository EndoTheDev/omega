package store

// sessions.go: agent-scoped session storage. Sessions carry their owning agent on the
// row ("agent" column, default "default" - the migration backfills every existing row),
// created bound and never re-bound. The default surface stays inside the core
// StoreProvider: ListSessions filters to agent='default', so the pre-agent session
// list, tree and search see exactly what they always saw.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

var _ agent.AgentSessionStore = (*Store)(nil)

// CreateAgentSession creates a session bound to agentName - the binding is written on
// the row itself, never inferred later. The id-must-be-new semantics are the same as
// CreateSession.
func (s *Store) CreateAgentSession(ctx context.Context, id, parentID, label, agentName string) error {
	if agentName == "" {
		agentName = agent.DefaultAgentName
	}
	now := core.NowISO()
	var parent any
	if parentID != "" {
		parent = parentID
	}
	// last_read_at = created time.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, parent_id, label, created_at, updated_at, last_read_at, agent)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, parent, label, now, now, now, agentName)
	return err
}

// ListAgentSessions returns the sessions bound to the given agent, ordered.
func (s *Store) ListAgentSessions(ctx context.Context, agentName string) ([]core.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE agent = ? ORDER BY created_at`, agentName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// CreateCanonicalChat creates a session that is an agent's canonical chat: a permanent
// conversation the AGENTS ROSTER opens, never a row in the maintainer's own session
// list. The `listed = 0` bit is the store's own visibility law - ListSessions surfaces
// the user's conversations, and a machinery-owned chat is not one of them (so main's
// canonical chat cannot appear in the sessions sidebar as if the maintainer had started
// it).
func (s *Store) CreateCanonicalChat(ctx context.Context, id, agentName string) error {
	if agentName == "" {
		agentName = agent.DefaultAgentName
	}
	now := core.NowISO()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, parent_id, label, created_at, updated_at, last_read_at, agent, listed)
		 VALUES (?, NULL, ?, ?, ?, ?, ?, 0)`,
		id, CanonicalChatLabel, now, now, now, agentName)
	return err
}

// SetSessionListed sets a session's list-visibility bit. `listed = 1` is the human's
// own session list; `listed = 0` marks a machinery-owned conversation (an agent's
// canonical chat), which opens from the agents roster and never from the sidebar. The
// CREATE path never needs this (CreateCanonicalChat writes the bit itself).
func (s *Store) SetSessionListed(ctx context.Context, id string, listed bool) error {
	v := 0
	if listed {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET listed = ? WHERE id = ?`, v, id)
	return err
}

// CanonicalChatLabel is the label a canonical chat's row carries.
const CanonicalChatLabel = "canonical"

// aiNowISO removed: core.NowISO is the store-wide timestamp helper.

// HandleAgentsCommand renders the /agents table. Needs OMEGA_HOME - the same env main
// sets for every process - and an AgentSessionStore (a store without the optional
// interface renders zero counts, which is exactly how many sessions such a store can
// attribute).
func HandleAgentsCommand(ctx context.Context, st agent.StoreProvider) (core.CommandResult, error) {
	home := os.Getenv("OMEGA_HOME")
	profiles := agent.LoadProfiles(home)
	fillProfileSessions(ctx, st, profiles)
	var sb strings.Builder
	width := longestName(profiles)
	fmt.Fprintf(&sb, "\n  %-*s  %-*s  %6s  %6s  %s\n", width, "AGENT", 24, "DESCRIPTION", "SKILLS", "SESS", "MODEL")
	for _, p := range profiles {
		desc := p.Description
		if len(desc) > 24 {
			desc = desc[:23] + "…"
		}
		model := p.ModelName
		if model == "" && p.Name == agent.DefaultAgentName {
			model = "(home config)"
		}
		fmt.Fprintf(&sb, "  %-*s  %-*s  %6d  %6d  %s\n", width, p.Name, 24, desc,
			p.SkillCount, p.SessionCount, model)
	}
	return core.CommandResult{Text: sb.String()}, nil
}

// HandleAgentDetailCommand renders the /agent <name>.
func HandleAgentDetailCommand(ctx context.Context, st agent.StoreProvider, arg string) (core.CommandResult, error) {
	home := os.Getenv("OMEGA_HOME")
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return core.CommandResult{}, fmt.Errorf("usage: /agent <name>")
	}
	p, err := agent.LoadProfile(home, arg)
	if err != nil {
		return core.CommandResult{}, err
	}
	fillProfileSessions(ctx, st, []agent.Profile{p})
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n  agent:      %s\n", p.Name)
	if p.Description != "" {
		fmt.Fprintf(&sb, "  description: %s\n", p.Description)
	}
	fmt.Fprintf(&sb, "  dir:        %s\n", p.Dir)
	fmt.Fprintf(&sb, "  provider:   %s\n", providerLabel(p.ProviderInstance))
	fmt.Fprintf(&sb, "  model:      %s\n", p.ModelName)
	fmt.Fprintf(&sb, "  skills:     %s\n", strings.Join(p.Skills, ", "))
	fmt.Fprintf(&sb, "  memory:     %d bytes\n", p.MemorySize)
	fmt.Fprintf(&sb, "  sessions:   %d\n", p.SessionCount)
	if p.LastActivity != "" {
		fmt.Fprintf(&sb, "  last act:   %s\n", p.LastActivity)
	}
	return core.CommandResult{Text: sb.String()}, nil
}

// providerLabel renders the agent's provider reference for the CLI detail view. The
// agent file never carries a type, host or key any more (the secret-free law).
func providerLabel(instance string) string {
	if instance == "" {
		return agent.DefaultProviderInstance
	}
	return instance
}

// fillProfileSessions writes per-agent session counts and the most recent updated_at.
// Store-free callers (nil or a store without the optional surface) keep the zero
// values.
func fillProfileSessions(ctx context.Context, st agent.StoreProvider, profiles []agent.Profile) {
	as, ok := st.(agent.AgentSessionStore)
	if !ok {
		return
	}
	for i := range profiles {
		sessions, err := as.ListAgentSessions(ctx, profiles[i].Name)
		if err != nil {
			continue // counts degrade to zero, never fail the listing
		}
		profiles[i].SessionCount = len(sessions)
		for _, s := range sessions {
			if s.UpdatedAt > profiles[i].LastActivity {
				profiles[i].LastActivity = s.UpdatedAt
			}
		}
	}
}

func longestName(profiles []agent.Profile) int {
	width := 5
	for _, p := range profiles {
		if len(p.Name) > width {
			width = len(p.Name)
		}
	}
	return width
}
