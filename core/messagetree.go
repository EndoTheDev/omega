package core

// BranchInfo describes one branch point on the wire: the parent
// message id ("message_id"), the sibling RUN HEAD ids in append order
// ("siblings"), how many variants exist ("count"), and which sibling
// index is the active path ("active").
type BranchInfo struct {
	ParentID    string   `json:"message_id"`
	Siblings    []string `json:"siblings,omitempty"`
	Count       int      `json:"count"`
	ActiveIndex int      `json:"active"`
}

// ActiveView is the tree-derived view of a session: Path is the active
// path (render + context assembly), Branches the >=2-sibling branch
// points the transcript wire serves. The remaining maps are the
// navigation ground truth for the variants endpoint: SiblingHeads lists
// every branch point's run heads in append order (single runs too),
// ActiveHeads the selected head per branch, HeadByMessage each
// conversation message's run head (itself for heads), BranchByHead each
// head's branch point. Path/Branches are wire fields; the maps are
// internal resolution surfaces and never serialized.
//
// The two structs moved into core ahead of the rest of messagetree
// (arc 1b: StoreProvider.GetActiveView returns ActiveView and the seam
// contracts must be self-contained; arc 1c, t_483de955, moves the
// remaining tree code).
type ActiveView struct {
	Path     []StoredMessage
	Branches []BranchInfo

	SiblingHeads  map[string][]string
	ActiveHeads   map[string]string
	HeadByMessage map[string]string
	BranchByHead  map[string]string
}
