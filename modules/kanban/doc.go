// Package kanban provides the omega task board: a SQLite-backed kanban
// store with the 9-status hermes grammar (triage todo scheduled ready
// running blocked review done archived), the six board verbs folded
// into one kanban.task domain tool, the /board and /boards commands,
// the canvas panels
// (the task list and the board switcher - panel.go, arc 5), and the
// seam the HTTP channel serves /tasks from.
//
// omega has ONE agent - no claim locks, no dispatch races, no tenants.
// The board is exactly hermes-proven core: task = title + BODY-AS-SPEC,
// a comment thread, an append-only event log, assignee and session
// links. Anything the Improver or cron would need (the nullable source
// field) is already there.
package kanban
