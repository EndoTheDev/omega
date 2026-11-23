package store

// blocks.go: the context's tier structure - the compact lines and the carrier rows they
// name. A compact is ONE append: the compress tool call, its result, and the block line
// that ties them to the span they replaced, so a reader can never see half a compact.

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

var _ agent.SessionBlockStore = (*Store)(nil)

// ContextBlocks reads a session's blocks: the compact's tier structure, in the order the
// compacts landed. A store that cannot hold them (an owner without the surface) answers
// none, and its sessions keep the pre-block shape.
func (s *Store) ContextBlocks(ctx context.Context, sessionID string) ([]core.ContextBlock, error) {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.ContextBlocks(ctx, sessionID)
	}
	f, err := s.readSessionFile(sessionID)
	if err != nil {
		return nil, err
	}
	return f.blocks, nil
}

// AppendCompact appends one compact: the carrier pair at the TAIL of the session and the
// block line that names it. Nothing already stored is rewritten, which is what lets a
// provider replaying the history see the same prefix it saw before the compact landed.
func (s *Store) AppendCompact(ctx context.Context, sessionID string, req core.CompactWrite) (core.ContextBlock, error) {
	block := core.ContextBlock{
		Tier:    req.Tier,
		From:    req.From,
		To:      req.To,
		Keep:    req.Keep,
		Sources: req.Sources,
	}
	if block.Tier <= 0 {
		block.Tier = 1
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if !s.sessionFileExists(sessionID) {
		return core.ContextBlock{}, fmt.Errorf("store: session %q does not exist", sessionID)
	}
	// The carrier rides the active leaf: the tree must place it after the row the compact
	// left live, never beside an abandoned variant.
	leaf, err := s.leafID(ctx, sessionID)
	if err != nil {
		return block, err
	}
	callID := s.nextMessageSeq()
	s.maxMessageID = callID
	carrierID := s.nextMessageSeq()
	s.maxMessageID = carrierID

	call := core.NewAssistant("")
	call.ParentID = leaf
	call.ToolCalls = []core.ToolCall{{
		ID:   fmt.Sprintf("compress-%d", callID),
		Name: core.CompressToolName,
		Arguments: map[string]any{
			"from":     core.MsgID(req.From),
			"to":       core.MsgID(req.To),
			"messages": req.To - req.From + 1,
		},
	}}
	result := core.NewToolResult(req.Summary, call.ToolCalls[0].ID, false)
	result.ParentID = core.MsgID(callID)

	callRole, callPayload, err := core.EncodeMessage(call)
	if err != nil {
		return block, err
	}
	resultRole, resultPayload, err := core.EncodeMessage(result)
	if err != nil {
		return block, err
	}
	now := core.NowISO()
	block.ID = fmt.Sprintf("blk-%d", callID)
	block.Call = callID
	block.Carrier = carrierID
	block.At = now
	lines := []fileLine{
		{V: jsonlVersion, Kind: kindMessage, Session: sessionID, ID: callID, Role: callRole, Payload: callPayload, At: now},
		{V: jsonlVersion, Kind: kindMessage, Session: sessionID, ID: carrierID, Role: resultRole, Payload: resultPayload, At: now},
		{V: jsonlVersion, Kind: kindBlock, Session: sessionID, At: now, Block: &block},
	}
	if err := s.writeLines(ctx, sessionID, lines); err != nil {
		return core.ContextBlock{}, err
	}
	return block, nil
}

// leafID is the active path's last row - the parent the next appended row belongs under.
func (s *Store) leafID(ctx context.Context, sessionID string) (string, error) {
	rows, err := s.loadViewRows(ctx, sessionID)
	if err != nil {
		return "", err
	}
	path := core.DeriveTree(rows).Path
	if len(path) == 0 {
		return "", nil
	}
	return core.MsgID(path[len(path)-1].ID), nil
}
