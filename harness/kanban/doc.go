// Package kanban provides the omega task board: a SQLite-backed kanban store with the
// 9-status hermes grammar (triage todo scheduled ready running blocked review done
// archived), ONE kanban.db owning every board, the board verbs as seven dotted kanban.*
// wire tools, the /board and /boards commands, the canvas panels (the task list and the
// board switcher - panel.go, arc 5), and the seam the HTTP channel serves /tasks from.
// The board is exactly hermes-proven core: task = title + BODY-AS-SPEC, a comment
// thread, an append-only event log, assignee and session links.
package kanban
