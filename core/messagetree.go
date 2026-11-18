package core

import (
	"fmt"
	"strconv"
)

// BranchInfo describes one branch point on the wire.
type BranchInfo struct {
	ParentID    string   `json:"message_id"`
	Siblings    []string `json:"siblings,omitempty"`
	Count       int      `json:"count"`
	ActiveIndex int      `json:"active"`
}

// ActiveView is the tree-derived view of a session. Path/Branches are wire fields; the
// maps are internal resolution surfaces and never serialized.
type ActiveView struct {
	Path     []StoredMessage
	Branches []BranchInfo

	SiblingHeads  map[string][]string
	ActiveHeads   map[string]string
	HeadByMessage map[string]string
	BranchByHead  map[string]string
}

// isConversation reports whether a message takes part in the tree.
func isConversation(msg Message) bool {
	switch msg.(type) {
	case User, Assistant, ToolResult, YieldResult:
		return true
	}
	return false
}

// parentIDOf reads a message's tree pointer.
func parentIDOf(msg Message) string {
	switch m := msg.(type) {
	case User:
		return m.ParentID
	case Assistant:
		return m.ParentID
	case ToolResult:
		return m.ParentID
	case YieldResult:
		return m.ParentID
	}
	return ""
}

// MsgID renders a row id the way the wire (and the pointers) spell it.
func MsgID(id int64) string {
	return strconv.FormatInt(id, 10)
}

// ParentIDOf reads a message's tree pointer.
func ParentIDOf(msg Message) string {
	return parentIDOf(msg)
}

// WithParentID returns the conversation message with its tree pointer set to parent.
func WithParentID(msg Message, parent string) Message {
	return setParentNode(msg, parent)
}

// setParentNode returns the message with its pointer set.
func setParentNode(msg Message, parent string) Message {
	switch m := msg.(type) {
	case User:
		m.ParentID = parent
		return m
	case Assistant:
		m.ParentID = parent
		return m
	case ToolResult:
		m.ParentID = parent
		return m
	case YieldResult:
		m.ParentID = parent
		return m
	}
	return msg
}

// DeriveTree builds the active view from a session's persisted
// messages in append (row) order. Semantics:
//
// Implicit chain: a conversation message without a pointer parents
//     to the previous conversation message in row order (legacy rows
// written before pointers existed therefore stay one linear path
//     they render exactly as they always did).
// A run head is an ASSISTANT message whose parent is a user
//     message. Retry/fork re-runs produce another head with the same
//     parent user - sibling variants.
// Selections come from VariantSelect entries; the last entry per
// branch wins. No entry: the LAST sibling is active (row order)
//     a retry's fresh response is the active one with no selector
//     state.
// The leaf is the newest (row order) conversation message whose
//     ancestor chain carries only active heads; the path is that leaf's
//     ancestor chain, root first.
func DeriveTree(msgs []StoredMessage) ActiveView {
	sel := map[string]string{}
	for _, sm := range msgs {
		if vs, ok := sm.Message.(VariantSelect); ok {
			sel[vs.Branch] = vs.Child
		}
	}

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
		roleOf[id] = MessageRole(sm.Message)
		idOf = append(idOf, id)
		prevID = id
	}
	parentRole := make(map[string]string, len(parentOf))
	for id, parent := range parentOf {
		parentRole[id] = roleOf[parent]
	}

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

	active := map[string]string{}
	for branch, heads := range headsOf {
		active[branch] = heads[len(heads)-1]
	}
	for branch, child := range sel {
		if _, ok := branchOf[child]; ok && branchOf[child] == branch {
			active[branch] = child
		}
	}

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

// nodeByID finds the StoredMessage carrying a row id.
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
