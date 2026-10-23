package agent

import (
	"github.com/EndoTheDev/omega/core"
)

// Message tree derivation (t_17365247): the session store stays a
// linear append list; the TREE derives from parent pointers that ride
// the message payloads. Everything is pure - same input, same tree,
// no store.
//
// The vocabulary, role switch, and derivation now live in
// core/messagetree.go, core/message_types.go, and core/messages.go
// (arc 1c, t_483de955 - moved under the stdlib-only law test that
// gates core). These wrappers are the agent re-export shim; they die
// in phase 2 when modules rewire to core.

// BranchInfo describes one branch point on the wire (moved to core,
// arc 1b).
type BranchInfo = core.BranchInfo

// ActiveView is the tree-derived view of a session (moved to core,
// arc 1b). Path is the active path (render + context assembly),
// Branches the >=2-sibling branch points the transcript wire serves.
// The remaining maps are the navigation ground truth for the variants
// endpoint: SiblingHeads lists every branch point's run heads in
// append order (single runs too), ActiveHeads the selected head per
// branch, HeadByMessage each conversation message's run head (itself
// for heads), BranchByHead each head's branch point. Path/Branches are
// wire fields; the maps are internal resolution surfaces and never
// serialized.
type ActiveView = core.ActiveView

// MsgID renders a row id the way the wire (and the pointers) spell it.
func MsgID(id int64) string {
	return core.MsgID(id)
}

// ParentIDOf reads a message's tree pointer. Conversation message
// types carry the pointer; everything else (bookkeeping entries) has
// none.
func ParentIDOf(msg core.Message) string {
	return core.ParentIDOf(msg)
}

// WithParentID returns the conversation message with its tree pointer
// set to parent (other types come back unchanged - the /chat fork path
// uses this to chain its incoming message onto the forked point).
func WithParentID(msg core.Message, parent string) core.Message {
	return core.WithParentID(msg, parent)
}

// DeriveTree builds the active view from a session's persisted
// messages in append (row) order.
func DeriveTree(msgs []StoredMessage) ActiveView {
	return core.DeriveTree(msgs)
}

// ParseMsgID inverts MsgID; a malformed id fails loudly so callers can
// 400 instead of silently matching nothing.
func ParseMsgID(id string) (int64, error) {
	return core.ParseMsgID(id)
}
