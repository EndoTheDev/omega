package tui

// The chat view layer (chat-last 2b): pure composition and rendering
// over the canvas chat vocabulary (core.ChatPayload/ChatRow/ChatBlock,
// card t_ec45b670). The chat pane renders FROM the capsule's panel
// payload - the per-frontend transcript renderer (renderTranscript +
// the streamSegment machinery) is deleted, not parked. Rows map one
// per transcript item (user | assistant thinking | tool call |
// assistant text | tool result), the same row vocabulary the http
// channel's panel carries; only the panel owner differs per frontend
// mode (the 2b design ruling: one chat panel per live frontend).

import (
	"fmt"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// chatRowsOf maps conversation messages into the canvas chat
// vocabulary. One row per transcript item, narrative order preserved
// (thinking -> tool calls -> response for an assistant message; the
// same order renderTranscript kept). Pure. Rows carry no id: the TUI
// composes from in-memory history, the store row ids are the store's
// vocabulary (http-side), and ChatRow.ID is optional by contract.
func chatRowsOf(msgs []core.Message) []core.ChatRow {
	rows := make([]core.ChatRow, 0, len(msgs))
	for _, msg := range msgs {
		switch m := msg.(type) {
		case core.User:
			rows = append(rows, core.ChatRow{
				Role:   core.ChatRoleUser,
				Status: core.ChatStatusComplete,
				Blocks: []core.ChatBlock{{Type: core.ChatBlockText, Content: m.Content, Images: m.Images}},
			})
		case core.Assistant:
			if m.Thinking != nil && *m.Thinking != "" {
				rows = append(rows, core.ChatRow{
					Role:   core.ChatRoleAssistant,
					Status: core.ChatStatusComplete,
					Blocks: []core.ChatBlock{{Type: core.ChatBlockThinking, Content: *m.Thinking}},
				})
			}
			for _, call := range m.ToolCalls {
				call := call // local copy; the row owns its own pointer
				rows = append(rows, core.ChatRow{
					Role:   core.ChatRoleAssistant,
					Status: core.ChatStatusComplete,
					Blocks: []core.ChatBlock{{Type: core.ChatBlockToolCall, ToolCall: &call}},
				})
			}
			if m.Content != "" {
				rows = append(rows, core.ChatRow{
					Role:   core.ChatRoleAssistant,
					Status: core.ChatStatusComplete,
					Blocks: []core.ChatBlock{{Type: core.ChatBlockText, Content: m.Content}},
				})
			}
		case core.ToolResult:
			elapsed := int64(0)
			if m.Timing != nil {
				elapsed = m.Timing.ElapsedMs
			}
			rows = append(rows, core.ChatRow{
				Role:   core.ChatRoleTool,
				Status: core.ChatStatusComplete,
				Blocks: []core.ChatBlock{{Type: core.ChatBlockToolResult, Content: m.Content, ToolCallID: m.ToolCallID, IsError: m.IsError, ElapsedMs: elapsed}},
			})
		}
	}
	return rows
}

// chatRowOffsets returns, for each message index, the index of its
// FIRST row in chatRowsOf(msgs) - or -1 when the message maps to no
// rows (system/meta messages never render). Used to place variant
// markers: the markers are path-index keyed message facts, the rows
// are the render surface, and the alignment is deterministic because
// both sides derive from the same mapper.
func chatRowOffsets(msgs []core.Message) []int {
	offs := make([]int, len(msgs))
	at := 0
	for i, msg := range msgs {
		offs[i] = -1
		switch msg.(type) {
		case core.User, core.ToolResult:
			offs[i] = at
			at++
		case core.Assistant:
			offs[i] = at
			m := msg.(core.Assistant)
			if m.Thinking != nil && *m.Thinking != "" {
				at++
			}
			at += len(m.ToolCalls)
			if m.Content != "" {
				at++
			}
		}
	}
	return offs
}

// turnStartOf returns the row index where the in-flight turn begins:
// just after the newest user row (the run's input row lands at send
// time, so everything after it is the live tail). 0 when no user row
// exists - a whole-payload live tail.
func turnStartOf(rows []core.ChatRow) int {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Role == core.ChatRoleUser {
			return i + 1
		}
	}
	return 0
}

// chatLine is one rendered block of chat body output: the payload row
// it came from and whether that row is a text-block row (the pane
// shell keeps live text in the main pane and the other live blocks in
// the run band).
type chatLine struct {
	at    int
	text  string
	isText bool
}

// renderCache carries the live glamour debounce across render passes.
// The renderer reads it and writes it back through the pointer so the
// model keeps the cache - the debounce stays the model's timing state.
type renderCache struct {
	at       time.Time
	rendered string
}

// chatRenderOpts is the model-side display state the renderer rides:
// the two display toggles, the variant markers keyed by row index, the
// pinned marker of a just-retried response, the live-turn boundary,
// and the run's stream error.
type chatRenderOpts struct {
	showThinking    bool
	showToolResults bool
	toolResultsAuto bool
	markers         map[int]string
	tailMarker      string
	liveFrom        int // payload.Running: the in-flight turn's first row index
	running         bool
	errText         string // the committed run's stream error ("" = none)
}

const chatRenderNoLive = -1

// toolCallsOfRows indexes the payload's tool_call blocks by call id so
// tool_result rows can infer a syntax-highlight language (the call's
// name and arguments decide it) - the payload vocabulary keeps
// tool_result blocks nameless, the pairing is the renderer's read of
// the same panel.
func toolCallsOfRows(rows []core.ChatRow) map[string]core.ToolCall {
	byID := make(map[string]core.ToolCall)
	for _, row := range rows {
		for _, b := range row.Blocks {
			if b.Type == core.ChatBlockToolCall && b.ToolCall != nil {
				byID[b.ToolCall.ID] = *b.ToolCall
			}
		}
	}
	return byID
}

// renderChatLines renders the payload rows as chat body lines, one
// emitted block per row (the text carries its own leading/trailing
// newlines, the same shapes renderTranscript emitted). Pure except
// for the debounce cache pointer.
func renderChatLines(width int, t Theme, rows []core.ChatRow, opts chatRenderOpts, cache *renderCache) []chatLine {
	width = max(1, width)
	byID := toolCallsOfRows(rows)
	lines := make([]chatLine, 0, len(rows))
	for i, row := range rows {
		live := opts.running && i >= opts.liveFrom
		marker := opts.markers[i]
		for _, b := range row.Blocks {
			switch b.Type {
			case core.ChatBlockThinking:
				if !opts.showThinking {
					continue
				}
				text := "\n" + t.Thinking.Render("[thinking]") + "\n" +
					t.Thinking.Render(wordWrap(b.Content, width)) + "\n"
				if marker != "" {
					text = "\n" + t.Info.Render(marker) + " " + text[1:]
				}
				lines = append(lines, chatLine{at: i, text: text})
			case core.ChatBlockToolCall:
				if b.ToolCall == nil {
					continue
				}
				if b.ToolCall.Name == "verdict" {
					// Verdict cards v1: the question banner replaces the
					// raw args dump; the busy-Enter intercept answers it.
					lines = append(lines, chatLine{at: i, text: verdictBanner(b.ToolCall.Arguments, width, t)})
					continue
				}
				var sb strings.Builder
				sb.WriteString("\n")
				sb.WriteString(t.Tool.Render("[tool: " + b.ToolCall.Name + "]"))
				sb.WriteString("\n")
				for k, v := range b.ToolCall.Arguments {
					sb.WriteString(wordWrap(fmt.Sprintf("  %s: %v", k, v), width))
					sb.WriteString("\n")
				}
				lines = append(lines, chatLine{at: i, text: sb.String()})
			case core.ChatBlockToolResult:
				lines = append(lines, chatLine{at: i, text: renderToolResultLine(b, byID, width, t, live, opts)})
			case core.ChatBlockText:
				lines = append(lines, chatLine{at: i, isText: true, text: renderTextRow(row.Role, b, marker, opts, i, rows, cache, width, t)})
			}
		}
	}
	// The committed run's stream error rides one dim line after the
	// rows (the payload vocabulary carries no error row; the same
	// truth renderTranscript folded into the transcript). Cleared by
	// the next run's first commit/stream event.
	if opts.errText != "" {
		lines = append(lines, chatLine{at: len(rows), text: "\n" + t.Error.Render("error: "+opts.errText) + "\n"})
	}
	return lines
}

// renderTextRow renders one user or assistant text block. User rows
// keep the "> " prompt echo; assistant rows render through the
// markdown pipeline - completed rows get the AgentEnd leading/trailing
// newline shape, the live tail keeps the bare shape, and the live
// tail's glamour pass debounces at 80ms (the same law the segment
// renderer kept, now on the last row of the payload).
func renderTextRow(role string, b core.ChatBlock, marker string, opts chatRenderOpts, at int, rows []core.ChatRow, cache *renderCache, width int, t Theme) string {
	if role == core.ChatRoleUser {
		text := t.User.Render("> " + wordWrap(b.Content, width))
		if len(b.Images) > 0 {
			text += "\n" + t.Info.Render(fmt.Sprintf("[loaded %d image(s)]", len(b.Images)))
		}
		if marker != "" {
			return "\n" + t.Info.Render(marker) + " " + text + "\n"
		}
		return "\n" + text + "\n"
	}
	rendered := b.Content
	last := opts.running && at == len(rows)-1
	if last && time.Since(cache.at) < 80*time.Millisecond && cache.rendered != "" {
		// Debounce: replay the last glamour output instead of re-rendering
		// the raw text at token cadence.
		rendered = cache.rendered
	} else {
		rendered = renderMarkdown(b.Content, width, t)
		if last {
			cache.at = time.Now()
			cache.rendered = rendered
		}
	}
	if last {
		// The live tail keeps the bare shape the segment renderer used.
		return rendered
	}
	if marker != "" {
		return "\n" + t.Info.Render(marker) + " " + rendered + "\n"
	}
	if opts.tailMarker != "" && isLastTextRow(rows, at) {
		return "\n" + t.Info.Render(opts.tailMarker) + " " + rendered + "\n"
	}
	return "\n" + rendered + "\n"
}

// isLastTextRow reports the row at index at is the newest text row of
// the payload - the retried response pins its variant marker there.
func isLastTextRow(rows []core.ChatRow, at int) bool {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Role == core.ChatRoleAssistant && len(rows[i].Blocks) > 0 && rows[i].Blocks[0].Type == core.ChatBlockText {
			return i == at
		}
	}
	return false
}

// renderToolResultLine renders one tool_result block. The display
// toggles apply on every view (a reload re-renders from the payload,
// so a collapsed result re-expands when the toggle lifts) - one
// display law for live and persisted rows alike.
func renderToolResultLine(b core.ChatBlock, byID map[string]core.ToolCall, width int, t Theme, live bool, opts chatRenderOpts) string {
	lines := strings.Count(b.Content, "\n") + 1
	if b.IsError {
		return "\n" + t.Error.Render("[tool result: error]") + "\n" +
			t.Tool.Render(wordWrap(b.Content, width)) + "\n"
	}
	lang := ""
	if call, ok := byID[b.ToolCallID]; ok {
		lang = langForTool(call.Name, call.Arguments)
	}
	full := opts.showToolResults && (!opts.toolResultsAuto || lines <= toolResultAutoThreshold)
	if full {
		content := b.Content
		highlighted := false
		if lang != "" {
			content = highlightCode(content, lang, t.Name)
			highlighted = true
		}
		if !live {
			// Persisted shape: the [tool result] header, highlight wins
			// its own colors.
			if highlighted {
				return "\n" + t.Tool.Render("[tool result]") + "\n" + wordWrap(content, width) + "\n"
			}
			return "\n" + t.Tool.Render("[tool result]") + "\n" + t.Tool.Render(wordWrap(content, width)) + "\n"
		}
		// Live shape: content only (the call row above names the tool).
		if highlighted {
			return "\n" + wordWrap(content, width) + "\n"
		}
		return "\n" + t.Tool.Render(wordWrap(content, width)) + "\n"
	}
	return "\n" + t.Tool.Render(fmt.Sprintf("[tool result: %d lines]", lines)) + "\n"
}

// chatBandLines renders the run band's body from the panel payload:
// the in-flight turn's thinking/tool_call/tool_result rows (everything
// after the newest user row while the panel runs - the split chatBody
// applies), at the band's own width. Empty string: no run, no band.
// Plain styled text - the band carries no glamour markdown.
func chatBandLines(width int, t Theme, payload core.ChatPayload) string {
	if !payload.Running {
		return ""
	}
	liveFrom := turnStartOf(payload.Rows)
	byID := toolCallsOfRows(payload.Rows)
	opts := chatRenderOpts{
		showThinking:    true,
		showToolResults: true,
		toolResultsAuto: false,
		liveFrom:        chatRenderNoLive,
		running:         true,
	}
	var sb strings.Builder
	for i, row := range payload.Rows {
		if i < liveFrom {
			continue
		}
		for _, b := range row.Blocks {
			switch b.Type {
			case core.ChatBlockThinking:
				sb.WriteString(t.Thinking.Render("[thinking]") + "\n")
				sb.WriteString(t.Thinking.Render(wordWrap(b.Content, width)) + "\n")
			case core.ChatBlockToolCall:
				if b.ToolCall == nil {
					continue
				}
				if b.ToolCall.Name == "verdict" {
					sb.WriteString(verdictBanner(b.ToolCall.Arguments, width, t))
					continue
				}
				sb.WriteString("\n" + t.Tool.Render("[tool: "+b.ToolCall.Name+"]") + "\n")
				for k, v := range b.ToolCall.Arguments {
					sb.WriteString(wordWrap(fmt.Sprintf("  %s: %v", k, v), width) + "\n")
				}
			case core.ChatBlockToolResult:
				sb.WriteString(renderToolResultLine(b, byID, width, t, true, opts))
			}
		}
	}
	return sb.String()
}

// chatPayloadOf extracts the TUI chat panel's payload from one canvas
// snapshot: typed in-process (the canvas carries the module's payload;
// the shell holds core types - layer law), the chat panel is found by
// kind. ok=false when no chat panel is mounted.
func chatPayloadOf(panels []core.Panel) (core.ChatPayload, bool) {
	for _, p := range panels {
		if p.Kind != core.PanelKindChat {
			continue
		}
		if payload, ok := p.Payload.(core.ChatPayload); ok {
			return payload, true
		}
	}
	return core.ChatPayload{}, false
}