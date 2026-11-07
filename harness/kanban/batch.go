package kanban

// batch.go is the atomic kanban batch. The tool surface's `operations`
// array runs entirely inside one SQLite transaction (the Batcher
// capability on the store): every operation applies or none does - a
// failed batch cannot orphan the tasks its earlier creates made, and
// the failure report says so. In-batch referencing rides the same
// machinery: an operation's `id` may read LAST (the task the most
// recent create in this batch made) or #N (the task created at
// operations[N-1], 1-based, matching the numbered result lines), so a
// create-then-act sequence fits one atomic call.
//
// Boards whose provider lacks the Batcher capability keep the honest
// fail-forward semantics (applied ops stand, the failure rides the
// result) - see runBatchFailForward.

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"
)

// opIndexRef matches a "#N" positional reference to a create operation.
var opIndexRef = regexp.MustCompile(`^#([0-9]+)$`)

// batchPreflight validates every in-batch reference BEFORE the first
// operation applies: a batch with a malformed or forward-pointing
// reference fails at the door and leaves the board untouched.
func batchPreflight(ops []map[string]any) error {
	hasCreate := false
	for i, op := range ops {
		act, _ := op["action"].(string)
		id, _ := op["id"].(string)
		if act == "create" {
			hasCreate = true
			continue
		}
		if id == "" {
			continue
		}
		switch {
		case id == "LAST":
			if !hasCreate {
				return fmt.Errorf(`operations[%d]: id "LAST" needs an earlier create to reference`, i)
			}
		case strings.HasPrefix(id, "#"):
			n, rerr := parseOpIndex(id, len(ops))
			if rerr != nil {
				return fmt.Errorf("operations[%d]: %v", i, rerr)
			}
			if n-1 >= i {
				return fmt.Errorf("operations[%d]: id %q points forward - references may only name earlier operations", i, id)
			}
			target, _ := ops[n-1]["action"].(string)
			if target != "create" {
				return fmt.Errorf("operations[%d]: id %q names operations[%d], which is %q - references resolve to the task an earlier CREATE made", i, id, n-1, target)
			}
		}
	}
	return nil
}

// parseOpIndex reads a "#N" reference; N is 1-based (matching the
// numbered batch result lines) up to the batch size.
func parseOpIndex(id string, total int) (int, error) {
	m := opIndexRef.FindStringSubmatch(id)
	if m == nil {
		return 0, fmt.Errorf("id %q is not a reference: either a real task id, LAST, or #N", id)
	}
	n, _ := strconv.Atoi(m[1])
	if n < 1 || n > total {
		return 0, fmt.Errorf("id %q is out of range (the batch runs %d operations)", id, total)
	}
	return n, nil
}

// resolveRef rewrites one operation's in-batch id reference to the
// real task id. Empty return = the id is not a reference (a literal
// id passes through untouched). "LAST" resolves to the latest create
// that RAN in this batch; "#N" to the create at operations[N-1].
func resolveRef(op map[string]any, last string, created map[int]string, total int) (map[string]any, string, error) {
	id, _ := op["id"].(string)
	if id == "" {
		return op, "", nil
	}
	var resolved string
	switch {
	case id == "LAST":
		if last == "" {
			return nil, "", fmt.Errorf(`id "LAST": no create has run in this batch yet`)
		}
		resolved = last
	case strings.HasPrefix(id, "#"):
		n, err := parseOpIndex(id, total)
		if err != nil {
			return nil, "", err
		}
		got, ok := created[n]
		if !ok {
			return nil, "", fmt.Errorf(`id %q: operations[%d] has not run yet or did not create a task`, id, n-1)
		}
		resolved = got
	default:
		return op, "", nil
	}
	out := maps.Clone(op)
	out["id"] = resolved
	return out, resolved, nil
}

// runTaskBatch runs the operations array on the tool provider's board
// (the consolidated task tool's batch lane). On a Batcher board the
// whole batch is one transaction: the first failure rolls back the
// operations that already applied and the result says so; on success
// everything commits at once (done-notifications flush after the
// commit, never for a rolled-back batch). In-batch references resolve
// live: LAST / #N read the ids the batch itself created.
func (p *ToolProvider) runTaskBatch(ctx context.Context, ops []map[string]any) (string, error) {
	if err := batchPreflight(ops); err != nil {
		return "", err
	}
	batcher, ok := p.board.(Batcher)
	if !ok {
		return p.runBatchFailForward(ctx, ops)
	}
	b, commit, cancel, err := batcher.BeginBatch(ctx)
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			cancel()
		}
	}()

	created := make(map[int]string, len(ops)) // 1-based op index -> the created task's id
	last := ""
	var sb strings.Builder
	for i, op := range ops {
		resolved, _, rerr := resolveRef(op, last, created, len(ops))
		if rerr != nil {
			cancel()
			committed = true // cancel owns the rollback; stop the defer
			p.refreshCapsule()
			return sb.String() + fmt.Sprintf("failed at operations[%d]: %v (batch rolled back, %d applied operations reverted)", i, rerr, i), nil
		}
		res, createdID, oerr := p.runTaskOp(ctx, b, resolved)
		if oerr != nil {
			cancel()
			committed = true
			p.refreshCapsule()
			return sb.String() + fmt.Sprintf("failed at operations[%d]: %v (batch rolled back, %d applied operations reverted)", i, oerr, i), nil
		}
		if createdID != "" {
			created[i+1] = createdID
			last = createdID
		}
		fmt.Fprintf(&sb, "%d: %s\n", i+1, res)
	}
	if cerr := commit(); cerr != nil {
		// a failed commit leaves nothing applied; cancel is a no-op
		// after the attempt (and harmless either way)
		cancel()
		committed = true
		return "", fmt.Errorf("batch commit failed (rolled back): %w", cerr)
	}
	committed = true
	p.refreshCapsule()
	fmt.Fprintf(&sb, "%d operations applied", len(ops))
	return sb.String(), nil
}

// runBatchFailForward is the no-capability fallback: operations stop at
// the first failure and whatever already applied stays (it is
// impossible to undo without the store's transaction), so the result
// reports the applied prefix and never claims atomicity.
func (p *ToolProvider) runBatchFailForward(ctx context.Context, ops []map[string]any) (string, error) {
	var sb strings.Builder
	for i, op := range ops {
		res, _, err := p.runTaskOp(ctx, p.board, op)
		if err != nil {
			// earlier ops still applied: the panel refresh below
			// is exactly the catch-up the partial batch needs
			p.refreshCapsule()
			return sb.String() + fmt.Sprintf("failed at operations[%d]: %v", i, err), nil
		}
		fmt.Fprintf(&sb, "%d: %s\n", i+1, res)
	}
	p.refreshCapsule()
	fmt.Fprintf(&sb, "%d operations applied", len(ops))
	return sb.String(), nil
}
