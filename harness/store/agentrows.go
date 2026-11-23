package store

// agentrows.go: the boot migrations that move agent-tagged sessions between stores. Under
// the JSONL layout a session's store is a FILE, so these move files and re-index the
// destination - copy-then-verify, and the source row goes only after the copy lands.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/EndoTheDev/omega/core"
)

// MigrateAgentRows moves agent-tagged sessions out of mainDSN into their per-agent
// stores. An agent with no sessions in the main store is skipped entirely - no store
// appears for an agent that never ran. Idempotent: an already-migrated home has nothing
// left to move, so the second boot is a no-op.
func MigrateAgentRows(mainDSN string, agentDSNs map[string]string) error {
	if len(agentDSNs) == 0 || !homeHasStore(mainDSN) {
		return nil
	}
	ctx := context.Background()
	main, err := Open(mainDSN)
	if err != nil {
		return fmt.Errorf("agent rows migration: open main store: %w", err)
	}
	defer main.Close()
	for name, agentDSN := range agentDSNs {
		sessions, err := main.ListAgentSessions(ctx, name)
		if err != nil {
			return fmt.Errorf("agent rows migration: list %s: %w", name, err)
		}
		if len(sessions) == 0 {
			continue // never wrote as this agent: no store for it
		}
		if err := moveAgentFiles(ctx, main, agentDSN, name, sessions); err != nil {
			return err
		}
	}
	return nil
}

// moveAgentFiles relocates one agent's session files and re-indexes them in the
// destination. The count check is the verify step: every source file must land before the
// source rows are dropped.
func moveAgentFiles(ctx context.Context, main *Store, agentDSN, name string, sessions []core.Session) error {
	dest, err := Open(agentDSN)
	if err != nil {
		return fmt.Errorf("agent rows migration: open %s store: %w", name, err)
	}
	defer dest.Close()
	if err := os.MkdirAll(dest.sessionsDir, 0o755); err != nil {
		return fmt.Errorf("agent rows migration: %s: sessions dir: %w", name, err)
	}
	moved := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		src := filepath.Join(main.sessionsDir, sess.ID+".jsonl")
		dst := filepath.Join(dest.sessionsDir, sess.ID+".jsonl")
		if err := moveFile(src, dst); err != nil {
			return fmt.Errorf("agent rows migration: %s: %s: %w", name, sess.ID, err)
		}
		moved = append(moved, sess.ID)
	}
	for _, id := range moved {
		path := filepath.Join(dest.sessionsDir, id+".jsonl")
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("agent rows migration: %s: %s did not land: %w", name, id, err)
		}
		if err := dest.indexFile(ctx, id, path, 0); err != nil {
			return fmt.Errorf("agent rows migration: %s: index %s: %w", name, id, err)
		}
	}
	if err := main.catchUp(ctx); err != nil {
		return fmt.Errorf("agent rows migration: main catch-up: %w", err)
	}
	return nil
}

// moveFile renames across directories, falling back to copy-and-remove when the two live
// on different volumes.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// RetagAgentSessions rewrites the agent tag of every session in ONE store. Sessions bind
// to an agent at creation, so a clone's copy carries the source's tag until this runs. A
// store that never ran is a no-op: the clone must not create one.
func RetagAgentSessions(dsn, to string, from ...string) error {
	if !homeHasStore(dsn) {
		return nil
	}
	if to == "" {
		to = "default"
	}
	ctx := context.Background()
	st, err := Open(dsn)
	if err != nil {
		return fmt.Errorf("retag agent sessions: %w", err)
	}
	defer st.Close()
	for _, src := range from {
		if src == "" {
			src = "default"
		}
		if src == to {
			continue
		}
		sessions, err := st.ListAgentSessions(ctx, src)
		if err != nil {
			return fmt.Errorf("retag agent sessions %q -> %q: %w", src, to, err)
		}
		for _, sess := range sessions {
			tag := to
			if err := st.appendMeta(ctx, sess.ID, &changes{Agent: &tag}); err != nil {
				return fmt.Errorf("retag agent sessions %q -> %q: %s: %w", src, to, sess.ID, err)
			}
		}
	}
	return nil
}
