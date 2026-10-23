package agent

import (
	"fmt"
	"strconv"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// Message tree derivation (t_17365247): the session store stays a
// linear append list; the TREE derives from parent pointers that ride
// the message payloads (ai.User/ai.Assistant/ai.ToolResult.ParentID,
// whose values are store row ids). Everything here is pure - same
// input, same tree, no store.

// BranchInfo describes one branch point on the wire (moved to core,
// arc 1b - ActiveView's wire field), t_17365247.
type BranchInfo = core.BranchInfo

// ActiveView is the tree-derived view of a session (moved to core,
// arc 1b - StoreProvider.GetActiveView returns it and the seam
// contracts must be self-contained). The rest of messagetree follows
// in arc 1c (t_483de955). Path is the active
// path (render + context assembly), Branches the >=2-sibling branch
// points the transcript wire serves. The remaining maps are the
// navigation ground truth for the variants endpoint: SiblingHeads lists
// every branch point's run heads in append order (single runs too),
// ActiveHeads the selected head per branch, HeadByMessage each
// conversation message's run head (itself for heads), BranchByHead each
// head's branch point. Path/Branches are wire fields; the maps are
// internal resolution surfaces and never serialized.
type ActiveView = core.ActiveView

// isConversation reports whether a message takes part in the tree.
// Bookkeeping entries (model change, thinking level, variant selects,
// system entries) are outside the conversation.
func isConversation(msg ai.Message) bool {
	switch msg.(type) {
	case ai.User, ai.Assistant, ai.ToolResult:
		return true
	}
	return false
}

// parentIDOf reads a message's tree pointer (conversation types only;
// the type switch is total for the roles isConversation admits).
func parentIDOf(msg ai.Message) string {
	switch m := msg.(type) {
	case ai.User:
		return m.ParentID
	case ai.Assistant:
		return m.ParentID
	case ai.ToolResult:
		return m.ParentID
	}
	return ""
}

// MsgID renders a row id the way the wire (and the pointers) spell it.
func MsgID(id int64) string {
	return strconv.FormatInt(id, 10)
}

// ParentIDOf reads a message's tree pointer. Conversation message
// types carry the pointer; everything else (bookkeeping entries) has
// none.
func ParentIDOf(msg ai.Message) string {
	return parentIDOf(msg)
}

// WithParentID returns the conversation message with its tree pointer
// set to parent (other types come back unchanged - the /chat fork path
// uses this to chain its incoming message onto the forked point).
func WithParentID(msg ai.Message, parent string) ai.Message {
	return setParentNode(msg, parent)
}

// setParentNode returns the message with its pointer set.
func setParentNode(msg ai.Message, parent string) ai.Message {
	switch m := msg.(type) {
	case ai.User:
		m.ParentID = parent
		return m
	case ai.Assistant:
		m.ParentID = parent
		return m
	case ai.ToolResult:
		m.ParentID = parent
		return m
	}
	return msg
}

// DeriveTree builds the active view from a session's persisted
// messages in append (row) order. Semantics:
//
//   - Implicit chain: a conversation message without a pointer parents
//     to the previous conversation message in row order (legacy rows
//     written before pointers existed therefore stay one linear path -
//     they render exactly as they always did).
//   - A run head is an ASSISTANT message whose parent is a user
//     message. Retry/fork re-runs produce another head with the same
//     parent user - sibling variants.
//   - Selections come from ai.VariantSelect entries; the last entry per
//     branch wins. No entry: the LAST sibling is active (row order) -
//     a retry's fresh response is the active one with no selector
//     state.
//   - The leaf is the newest (row order) conversation message whose
//     ancestor chain carries only active heads; the path is that leaf's
//     ancestor chain, root first.
func DeriveTree(msgs []StoredMessage) ActiveView {
	// Selections first: the last variant_select entry per branch wins.
	sel := map[string]string{}
	for _, sm := range msgs {
		if vs, ok := sm.Message.(ai.VariantSelect); ok {
			sel[vs.Branch] = vs.Child
		}
	}

	// Conversation messages in row order with resolved parents.
	parentOf := map[string]string{} // id -> parent id ("" = root)
	roleOf := map[string]string{}   // id -> role, by id
	idOf := make([]string, 0, len(msgs))
	var prevID string
	for _, sm := range msgs {
		if !isConversation(sm.Message) {
			continue
		}
		id := MsgID(sm.ID)
		parent := parentIDOf(sm.Message)
		if id != "" && parent == "" {
			parent = prevID // implicit row-order adjacency (legacy rows)
		}
		parentOf[id] = parent
		roleOf[id] = ai.MessageRole(sm.Message)
		idOf = append(idOf, id)
		prevID = id
	}
	// Role of each pointed-at parent ("" when dangling).
	parentRole := make(map[string]string, len(parentOf))
	for id, parent := range parentOf {
		parentRole[id] = roleOf[parent]
	}

	// Heads: assistants parented to a user; their siblings per branch.
	headsOf := map[string][]string{} // branch (user id) -> head ids
	branchOf := map[string]string{}  // head id -> branch (user id)
	for _, sm := range msgs {
		if !isConversation(sm.Message) {
			continue
		}
		id := MsgID(sm.ID)
		if roleOf[id] != "assistant" || parentRole[id] != "user" {
			continue
		}
		branch := parentOf[id]
		headsOf[branch] = append(headsOf[branch], id)
		branchOf[id] = branch
	}

	// Active head per branch: persisted selection (last entry wins;
	// stale or dangling children fall back to the default), else the
	// LAST sibling in row order - the newest response is active by
	// default.
	active := map[string]string{}
	for branch, heads := range headsOf {
		active[branch] = heads[len(heads)-1]
	}
	for branch, child := range sel {
		if _, ok := branchOf[child]; ok && branchOf[child] == branch {
			active[branch] = child
		}
	}

	// Wire branch points (>=2) in first-appearance order; every branch
	// point lands in SiblingHeads/ActiveHeads regardless of count.
	branches := []BranchInfo{}
	siblingHeads := map[string][]string{}
	activeHeads := map[string]string{}
	headByMessage := map[string]string{}
	branchByHead := map[string]string{}
	for _, sm := range msgs {
		id := MsgID(sm.ID)
		heads, ok := headsOf[id]
		if !ok {
			continue
		}
		siblingHeads[id] = heads
		activeHeads[id] = active[id]
		for _, h := range heads {
			branchByHead[h] = id
		}
		if len(heads) >= 2 {
			idx := 0
			for i, h := range heads {
				if h == active[id] {
					idx = i
					break
				}
			}
			branches = append(branches, BranchInfo{
				ParentID:    id,
				Siblings:    heads,
				Count:       len(heads),
				ActiveIndex: idx,
			})
		}
	}
	// Each conversation message resolves to its run head: the first
	// head on the ancestor chain (a head resolves to itself).
	for _, id := range idOf {
		walked := map[string]bool{}
		for cur := id; cur != "" && !walked[cur]; {
			walked[cur] = true
			if b := branchOf[cur]; b != "" {
				headByMessage[id] = cur
				break
			}
			cur = parentOf[cur]
		}
	}

	// chain reports whether id's ancestor chain carries only active
	// heads at every branch point. Root nodes (no parent row) are
	// always fine.
	valid := func(id string) bool {
		seen := map[string]bool{}
		for cur := id; cur != ""; {
			if seen[cur] {
				return false // defensive: pointer cycle
			}
			seen[cur] = true
			if want := active[branchOf[cur]]; want != "" && want != cur {
				return false
			}
			cur = parentOf[cur]
		}
		return true
	}

	// Leaf: the newest conversation message matching the selections.
	var leaf string
	for i := len(idOf) - 1; i >= 0; i-- {
		if valid(idOf[i]) {
			leaf = idOf[i]
			break
		}
	}
	if leaf == "" {
		return ActiveView{Branches: branches, SiblingHeads: siblingHeads, ActiveHeads: activeHeads, HeadByMessage: headByMessage, BranchByHead: branchByHead}
	}

	// Path: walk the ancestor chain, then reverse to root-first.
	var rev []StoredMessage
	seen := map[string]bool{}
	for cur := leaf; cur != "" && !seen[cur]; {
		seen[cur] = true
		node, ok := nodeByID(msgs, cur)
		if !ok {
			return ActiveView{Branches: branches, SiblingHeads: siblingHeads, ActiveHeads: activeHeads, HeadByMessage: headByMessage, BranchByHead: branchByHead} // dangling pointer
		}
		rev = append(rev, node)
		cur = parentOf[cur]
	}
	pathOut := make([]StoredMessage, len(rev))
	for i, n := range rev {
		pathOut[len(rev)-1-i] = n
	}
	return ActiveView{
		Path: pathOut, Branches: branches,
		SiblingHeads: siblingHeads, ActiveHeads: activeHeads,
		HeadByMessage: headByMessage, BranchByHead: branchByHead,
	}
}

// nodeByID finds the StoredMessage carrying a row id. Linear scan:
// sessions are hundreds of messages, and DeriveTree is O(n) already.
func nodeByID(msgs []StoredMessage, id string) (StoredMessage, bool) {
	for _, sm := range msgs {
		if MsgID(sm.ID) == id {
			return sm, true
		}
	}
	return StoredMessage{}, false
}

// ParseMsgID inverts MsgID; a malformed id fails loudly so callers can
// 400 instead of silently matching nothing.
func ParseMsgID(id string) (int64, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid message id %q", id)
	}
	return n, nil
}
