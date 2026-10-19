// Package kanban provides the omega task board: a SQLite-backed kanban
// store with the 9-status hermes grammar (triage todo scheduled ready
// running blocked review done archived), six agent tools that manage
// the board natively, and the seam the HTTP channel serves /tasks from.
//
// omega has ONE agent - no claim locks, no dispatch races, no tenants.
// The board is exactly hermes-proven core: task = title + BODY-AS-SPEC,
// a comment thread, an append-only event log, assignee and session
// links. Anything the Improver or cron would need (the nullable source
// field) is already there.
package kanban