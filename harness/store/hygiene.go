package store

// hygiene.go: the session hygiene pass (card t_329b9312). Division of
// labor is the curator's law (card t_9972dfd0), applied to session
// data:
//
//   - Mechanics are CODE: the prune (updated_at past retention, pinned
//     exempt) and the vacuum (wal_checkpoint TRUNCATE + the
//     auto_vacuum conversion). The LLM never deletes anything.
//   - The LLM only writes PROSE: one call per session drafts the
//     summary; the code truncates, stores, and FTS carries it.
//
// The pass is invoked through the sessions.hygiene tool: the async cron
// fire is an agent turn whose prompt names the tool, exactly the way
// the skills curator rides a jobs: prompt. One pass, one report.
//
// Compactor untouched (card note): in-context compaction is a
// different organ and works.

import (
	"github.com/EndoTheDev/omega/agent"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// HygieneReport is one pass's outcome.
type HygieneReport struct {
	// Summarized is the count of summaries written this pass.
	Summarized int `json:"summarized"`
	// Pending is the sessions that still lack a summary after the
	// pass (over the age floor) - the next run's backlog.
	Pending int `json:"pending"`
	// Pruned is the count of sessions deleted by retention.
	Pruned int `json:"pruned"`
	// Vacuumed reports whether the vacuum step ran to completion.
	Vacuumed bool `json:"vacuumed"`
	// DBSizeBytes / WALSizeBytes are the file sizes read AFTER the
	// vacuum step - the state the pass left the database in.
	DBSizeBytes  int64 `json:"db_size_bytes"`
	WALSizeBytes int64 `json:"wal_size_bytes"`
	// PrunedIDs names what retention deleted. Retention deletes
	// history; a report that cannot say which history it deleted is a
	// silent failure.
	PrunedIDs []string `json:"pruned_ids,omitempty"`
	// Skipped carries per-session summarizer failures (id: cause).
	Skipped []string `json:"skipped,omitempty"`
}

// summarizerMaxChars caps the transcript the summarizer reads per
// consultation - long sessions still summarize from their first 12k
// characters, which is where the conversation establishes itself. The
// tail is detail, the head is intent.
const summarizerMaxChars = 12000

// maxSummariesPerPass bounds one hygiene run: the pass runs on a cron
// schedule, and the backlog clears across runs. An unbounded pass
// could spend an hour streaming summaries on a first run against a
// large existing history.
const maxSummariesPerPass = 5

// RunSummarizer summarizes sessions older than afterHours that lack a
// summary. prov may be nil - the pass reports the skip and writes
// nothing. model routes the consult through prov.WithModel when set
// (the curated ruling: empty = the session provider model). Empty
// transcripts produce no summary: nothing to say.
func (s *Store) RunSummarizer(ctx context.Context, prov agent.Provider, model string, afterHours int) (HygieneReport, error) {
	rep := HygieneReport{}
	if afterHours <= 0 {
		afterHours = DefaultSummarizeAfterHours
	}
	if prov == nil {
		rep.Skipped = append(rep.Skipped, "no provider mounted: summarizer skipped")
		rep.Pending = 0
		return rep, nil
	}
	if model != "" {
		prov = prov.WithModel(model)
	}
	cutoff := time.Now().Add(-time.Duration(afterHours) * time.Hour)
	candidates, err := s.unsummarized(ctx, cutoff)
	if err != nil {
		return rep, err
	}
	rep.Pending = len(candidates)
	for _, sess := range candidates {
		if rep.Summarized >= maxSummariesPerPass {
			break
		}
		summary, err := s.summarizeSession(ctx, prov, sess)
		if err != nil {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s: %v", sess.ID, err))
			continue
		}
		if err := s.SetSessionSummary(ctx, sess.ID, summary); err != nil {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s: %v", sess.ID, err))
			continue
		}
		rep.Summarized++
	}
	rep.Pending -= rep.Summarized
	return rep, nil
}

// unsummarized lists the pass's work queue: sessions past the age
// floor, without a summary, that carry at least one message - an empty
// session has nothing to summarize and must not sit pending forever.
// The identity law shapes the list the same way ListSessions does.
func (s *Store) unsummarized(ctx context.Context, cutoff time.Time) ([]core.Session, error) {
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	var out []core.Session
	for _, sess := range sessions {
		if sess.Summary != "" || sess.Archived {
			continue
		}
		// Archived sessions are the user's hide-from-list call. The
		// summarizer is a LIST surface: a hidden conversation never
		// gains a searchable summary from a background pass.
		t, err := time.Parse(time.RFC3339, sess.UpdatedAt)
		if err != nil {
			continue // unparseable timestamps are not the pass's job
		}
		if t.After(cutoff) {
			continue
		}
		n, err := s.CountMessages(ctx, sess.ID)
		if err != nil || n == 0 {
			continue
		}
		out = append(out, sess)
	}
	return out, nil
}

// summarizeSession runs the one LLM call: transcript in, one-paragraph
// summary out. The answer is trimmed; an empty answer is refused (a
// summary row never stores nothing - that would re-queue forever).
func (s *Store) summarizeSession(ctx context.Context, prov agent.Provider, sess core.Session) (string, error) {
	msgs, err := s.GetMessages(ctx, sess.ID)
	if err != nil {
		return "", fmt.Errorf("messages: %w", err)
	}
	transcript := renderTranscript(msgs, summarizerMaxChars)
	if strings.TrimSpace(transcript) == "" {
		return "", fmt.Errorf("transcript renders empty")
	}
	prompt := "You summarize past AI conversations. Write ONE short paragraph (3-6 sentences) summarizing what happened in the conversation below: what the user wanted, what was done or decided, any durable outcome. Plain text, no headings, no markup, no preamble.\n\n"
	if sess.Label != "" {
		prompt += "Session title: " + sess.Label + "\n\n"
	}
	prompt += "Conversation transcript:\n" + transcript
	out, err := completeText(ctx, prov, prompt)
	if err != nil {
		return "", fmt.Errorf("summarize: %w", err)
	}
	out = strings.TrimSpace(out)
	// Defensive cap: a runaway model answer does not become an
	// unbounded row. 2000 chars is far past any honest paragraph.
	if len(out) > 2000 {
		out = out[:2000]
	}
	if out == "" {
		return "", fmt.Errorf("empty summary answer")
	}
	return out, nil
}

// renderTranscript flattens messages to "role: content" lines, capped
// at maxChars. Tool results render as "(tool output omitted)" stubs:
// their payloads are the bulk of every transcript and carry almost none
// of the narrative.
func renderTranscript(msgs []core.Message, maxChars int) string {
	var sb strings.Builder
	for _, msg := range msgs {
		switch m := msg.(type) {
		case core.User:
			sb.WriteString("user: " + strings.TrimSpace(m.Content) + "\n")
		case core.Assistant:
			text := strings.TrimSpace(m.Content)
			if len(m.ToolCalls) > 0 {
				names := make([]string, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					names = append(names, tc.Name)
				}
				text += " (" + strings.Join(names, ", ") + ")"
			}
			sb.WriteString("assistant: " + text + "\n")
		case core.ToolResult:
			sb.WriteString("(tool output omitted)\n")
		default:
			// Bookkeeping entries (model_change, verdict cards, ...)
			// are not conversation content.
			continue
		}
		if sb.Len() >= maxChars {
			break
		}
	}
	if sb.Len() > maxChars {
		return sb.String()[:maxChars]
	}
	return sb.String()
}

// completeText runs ONE collect-the-text completion over the provider
// (the curator's thin bridge shape, mirrored here so the two LLM passes
// stay identical in posture: no tool loop, no retries beyond the ai
// layer's own, failures surface and the pass moves on).
func completeText(ctx context.Context, prov agent.Provider, prompt string) (string, error) {
	var sb strings.Builder
	for ev := range prov.Stream(ctx, []core.Message{core.NewUser(prompt)}, nil) {
		switch e := ev.(type) {
		case core.ResponseChunk:
			sb.WriteString(e.Content)
		case core.StreamEnd:
			if e.FinishReason == "error" {
				return "", fmt.Errorf("%s", e.Error)
			}
		}
	}
	return sb.String(), nil
}

// PruneSessions deletes past-retention sessions. The window rides
// updated_at - "older" means last active, not created (an old session
// that was actively used yesterday is recent history). Pinned sessions
// are exempt: a pin is the user's keep-forever mark and retention may
// never overrule it. 0 (or less) = never prune. Children cascade via
// the FK - pruning a parent removes its branches with it.
func (s *Store) PruneSessions(ctx context.Context, retentionDays int) ([]string, error) {
	if retentionDays <= 0 {
		return nil, nil
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM sessions WHERE pinned = 0 AND updated_at < ? ORDER BY updated_at`, cutoff)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, id := range ids {
		if err := s.DeleteSession(ctx, id); err != nil {
			return ids, err
		}
	}
	return ids, nil
}

// RunVacuum is the hygiene pass's third step: reclaim the write
// backlog. wal_checkpoint(TRUNCATE) collapses the WAL into the main
// database and truncates the -wal file to zero - without it, any
// long-running WAL database grows without bound. The pragma returns a
// result row (busy, log, checkpointed): a busy=1 failure IS a result,
// not an error, so the pool issue it as a drained query and retry on
// transient readers. The auto_vacuum conversion (incremental, 2) turns
// empty-page reclaim on for the main file once - a pragma copy and a
// VACUUM on the same dedicated connection; every later pass runs
// incremental_vacuum to hand back free pages. Probe-proven against the
// bundled modernc sqlite: a pool-scoped auto_vacuum=2 + VACUUM does
// NOT convert (the pragma lands on one random connection, the VACUUM
// on another) - single connection, in order, is the only shape that
// takes.
func (s *Store) RunVacuum(ctx context.Context) error {
	var busy, logPages, ckptPages int
	for attempt := 0; ; attempt++ {
		if err := s.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).
			Scan(&busy, &logPages, &ckptPages); err != nil {
			return fmt.Errorf("wal checkpoint: %w", err)
		}
		if busy == 0 {
			break
		}
		if attempt >= 2 {
			return fmt.Errorf("wal checkpoint stayed busy after %d attempts", attempt+1)
		}
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("vacuum conn: %w", err)
	}
	defer conn.Close()
	var mode int
	if err := conn.QueryRowContext(ctx, `PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		return fmt.Errorf("read auto_vacuum: %w", err)
	}
	if mode == 2 { // incremental: reclaim free pages in place
		if _, err := conn.ExecContext(ctx, `PRAGMA incremental_vacuum`); err != nil {
			return fmt.Errorf("incremental vacuum: %w", err)
		}
		return nil
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA auto_vacuum = 2`); err != nil {
		return fmt.Errorf("set auto_vacuum: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("vacuum conversion: %w", err)
	}
	if err := conn.QueryRowContext(ctx, `PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		return fmt.Errorf("verify auto_vacuum: %w", err)
	}
	if mode != 2 {
		return fmt.Errorf("auto_vacuum stayed %d after the VACUUM conversion", mode)
	}
	// THE CONVERSION FLOODS THE WAL: the VACUUM rewrites the whole main
	// file through it (probe: a 9MB db left a 8.8MB -wal). Close the
	// pass the way it opened it - one more truncate checkpoint, so the
	// end state on disk is an empty WAL, not a fresh 9MB backlog.
	for attempt := 0; ; attempt++ {
		if err := s.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).
			Scan(&busy, &logPages, &ckptPages); err != nil {
			return fmt.Errorf("post-conversion checkpoint: %w", err)
		}
		if busy == 0 {
			break
		}
		if attempt >= 2 {
			return fmt.Errorf("post-conversion checkpoint stayed busy after %d attempts", attempt+1)
		}
	}
	return nil
}

// DBFileSizes reads the main database and WAL file sizes off disk. The
// dsn the store opened with is the path (":memory:" reads zero). The
// WAL file is read as <path>-wal: absent or zero both mean an empty
// WAL, which is the healthy end-state after a TRUNCATE checkpoint.
func (s *Store) DBFileSizes() (db, wal int64) {
	if s.dbPath == ":memory:" || s.dbPath == "" {
		return 0, 0
	}
	db = fileSize(s.dbPath)
	wal = fileSize(s.dbPath + "-wal")
	return db, wal
}

// fileSize stats path, reporting 0 for any miss (an absent WAL is an
// empty WAL, not an error).
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// RunHygiene is the whole pass: summarize, prune, vacuum, report. The
// steps run in that order on purpose - the summarizer consults the
// LLM BEFORE anything is deleted, and the vacuum happens last so the
// report's file sizes state the post-maintenance shape.
func (s *Store) RunHygiene(ctx context.Context, prov agent.Provider, cfg HygieneConfig) (HygieneReport, error) {
	rep := HygieneReport{}
	sum, err := s.RunSummarizer(ctx, prov, cfg.SummarizerModel, cfg.SummarizeAfterHours)
	if err != nil {
		return rep, err
	}
	rep.Summarized, rep.Pending, rep.Skipped = sum.Summarized, sum.Pending, sum.Skipped
	ids, err := s.PruneSessions(ctx, cfg.RetentionDays)
	rep.PrunedIDs = ids
	rep.Pruned = len(ids)
	if err != nil {
		return rep, err
	}
	if err := s.RunVacuum(ctx); err != nil {
		return rep, err
	}
	rep.Vacuumed = true
	rep.DBSizeBytes, rep.WALSizeBytes = s.DBFileSizes()
	return rep, nil
}
