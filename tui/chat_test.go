package tui

// Test helpers over the chat canvas surface (chat-last 2b): the chat
// pane renders FROM the panel payload, so assertions read the payload
// side - transcriptOf is the flattened chat body (rows + notices, the
// read surface the old transcript string was), chatRowsText renders a
// message list through the row vocabulary the renderer consumes.

import (
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// transcriptOf flattens the chat pane body for assertions: payload
// rows plus pinned notices, ANSI stripped, rendered at the viewport
// width (80 when the model never sized).
func transcriptOf(m model) string {
	w := m.viewport.Width
	if w <= 0 {
		w = 80
	}
	return ansiStrip(m.chatBody(w))
}

// chatRowsText renders a message list through the pure chat view the
// way the committed pane shows it (the surface renderTranscript used
// to be) - no notices, defaults on, ANSI stripped for plain Contains.
func chatRowsText(t *testing.T, msgs []core.Message) string {
	t.Helper()
	rows := chatRowsOf(msgs)
	lines := renderChatLines(80, themes["dark"], rows, chatRenderOpts{
		showThinking:    true,
		showToolResults: true,
		toolResultsAuto: true,
		liveFrom:        chatRenderNoLive,
	}, nil)
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(l.text)
	}
	return ansiStrip(sb.String())
}

// chatRowsRawText is chatRowsText with ANSI intact - the styling
// assertion surface.
func chatRowsRawText(t *testing.T, msgs []core.Message) string {
	t.Helper()
	rows := chatRowsOf(msgs)
	lines := renderChatLines(80, themes["dark"], rows, chatRenderOpts{
		showThinking:    true,
		showToolResults: true,
		toolResultsAuto: true,
		liveFrom:        chatRenderNoLive,
	}, nil)
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(l.text)
	}
	return sb.String()
}

// payloadRows flattens the chat pane body (the same read as
// transcriptOf under its assertion name).
func payloadRows(m model) string {
	return transcriptOf(m)
}

// payloadRowTypes lists the block types of the current payload rows -
// the streaming order assertion surface.
func payloadRowTypes(m model) []string {
	payload := m.chatPayload()
	types := make([]string, 0, len(payload.Rows))
	for _, row := range payload.Rows {
		for _, b := range row.Blocks {
			types = append(types, b.Type)
		}
	}
	return types
}
