package store

import (
	"database/sql"

	"github.com/EndoTheDev/omega/agent"
)

// The pending-calls ledger (async agent loop phase 1, card t_f3f953d0):
// the durable backing of agent.YieldLedger over the SQLite
// pending_calls table. The store satisfies the consumer's face
// structurally - no agent-side adapter, no import inversion. A
// session's rows survive restarts; the serve-start boot recovery sweep
// (agent/api/wakes.go recoverPendingCalls) reads All() and injects a
// synthetic timeout result per entry.

// Register records one dispatched call (agent.YieldLedger). A
// same-key (session, call id) re-registration replaces - the clean
// idempotent retry.
func (s *Store) Register(pc agent.PendingCall) error {
	_, err := s.db.Exec(`INSERT INTO pending_calls
		(session_id, call_id, tool_name, arguments, dispatched_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(session_id, call_id) DO UPDATE SET
			tool_name = excluded.tool_name,
			arguments = excluded.arguments,
			dispatched_at = excluded.dispatched_at`,
		pc.SessionID, pc.CallID, pc.ToolName, pc.Arguments, pc.DispatchedAt)
	return err
}

// Settle removes a completed call's row (agent.YieldLedger). An
// unknown key is a no-op.
func (s *Store) Settle(sessionID, callID string) error {
	_, err := s.db.Exec(`DELETE FROM pending_calls WHERE session_id = ? AND call_id = ?`, sessionID, callID)
	return err
}

// Pending lists a home's still-settling calls in dispatch order
// (agent.YieldLedger). A query failure is the empty list - the ledger
// contract is an errorless read; the wake path cannot 500 a re-entry.
func (s *Store) Pending(sessionID string) []agent.PendingCall {
	rows, err := s.db.Query(`SELECT session_id, call_id, tool_name, arguments, dispatched_at
		FROM pending_calls WHERE session_id = ? ORDER BY dispatched_at, call_id`, sessionID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanPendingCalls(rows)
}

// All lists every unsettled call in dispatch order (agent.YieldLedger)
// - the boot recovery sweep. Same errorless read law as Pending.
func (s *Store) All() []agent.PendingCall {
	rows, err := s.db.Query(`SELECT session_id, call_id, tool_name, arguments, dispatched_at
		FROM pending_calls ORDER BY dispatched_at, call_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanPendingCalls(rows)
}

// scanPendingCalls drains a pending-calls rowset.
func scanPendingCalls(rows *sql.Rows) []agent.PendingCall {
	var out []agent.PendingCall
	for rows.Next() {
		var pc agent.PendingCall
		if err := rows.Scan(&pc.SessionID, &pc.CallID, &pc.ToolName, &pc.Arguments, &pc.DispatchedAt); err != nil {
			continue
		}
		out = append(out, pc)
	}
	return out
}
