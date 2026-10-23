package agent

import "github.com/EndoTheDev/omega/core"

// Re-export shims (arc 1b, t_7bb4790d): the kanban seam family moved
// to core - Context.Kanban is a KanbanProvider, so the family rides
// with the seam contracts. The aliases disappear when modules rewire
// in phase 2. The ADR already parks KanbanTask in core's shared
// inventory.
//
// DO NOT add new kanban seam types here - they belong in core.

type (
	// KanbanTask is one board task.
	KanbanTask = core.KanbanTask
	// KanbanComment is one comment in a task's thread.
	KanbanComment = core.KanbanComment
	// KanbanEvent is one audit-trail entry on a task.
	KanbanEvent = core.KanbanEvent
	// KanbanPatch is a partial task update from HTTP PATCH or task.update.
	KanbanPatch = core.KanbanPatch
	// BoardInfo is one row of the board registry (multi-board listing).
	BoardInfo = core.BoardInfo
	// BoardLister is the optional multi-board capability on the kanban seam.
	BoardLister = core.BoardLister
	// KanbanProvider is the board seam: task CRUD plus comment and event threads.
	KanbanProvider = core.KanbanProvider
)

// ErrTaskNotFound is the shared "no such task" sentinel on the kanban
// seam: the store returns it and the HTTP channel maps it to a 404.
// The alias preserves the sentinel identity (one instance, created in core).
var ErrTaskNotFound = core.ErrTaskNotFound