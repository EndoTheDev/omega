package tui

// Tests for the chat-pane refresh pipeline over the chat capsule
// (chat-last 2b): content renders FROM the panel payload the capsule
// pushes - completed rows via setConversation/commit, live rows via
// the stream events, notices via their row anchors.

import (
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// newRefreshModel returns a model configured the same way the existing
// tui_test.go helpers set one up, ready for refresh() assertions.
func newRefreshModel() *model {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell", "dev", "")
	return &m
}

// TestRefreshFromPayload verifies refresh renders the committed rows:
// the conversation composes from the capsule's payload, not any local
// transcript string.
func TestRefreshFromPayload(t *testing.T) {
	m := newRefreshModel()
	m.chatCanvas.setConversation("sess", []core.Message{
		core.NewUser("hello there"),
		core.NewAssistant("hello world"),
	})
	m.refresh()
	got := ansiStrip(m.viewport.View())
	if !strings.Contains(got, "> hello there") {
		t.Fatalf("viewport missing the user row; got %q", got)
	}
	if !strings.Contains(got, "hello world") {
		t.Fatalf("viewport missing the assistant row; got %q", got)
	}
}

// TestRefreshNoticeAnchor verifies shell notices re-insert at their
// pinned row anchors: a notice appended before the next exchange stays
// between the rows it separates.
func TestRefreshNoticeAnchor(t *testing.T) {
	m := newRefreshModel()
	m.addNotice("[model: llama3]\n")
	m.chatCanvas.setConversation("sess", []core.Message{core.NewUser("hi")})
	m.refresh()
	got := ansiStrip(m.viewport.View())
	modelAt, userAt := strings.Index(got, "[model: llama3]"), strings.Index(got, "> hi")
	if modelAt < 0 || userAt < 0 || modelAt > userAt {
		t.Fatalf("notice order wrong: model@%d user@%d in %q", modelAt, userAt, got)
	}
}

// TestRefreshThinkingRow verifies the [thinking] header renders from a
// thinking row (the display toggle hides it when off).
func TestRefreshThinkingRow(t *testing.T) {
	m := newRefreshModel()
	m.chatCanvas.setRun("sess", func() {})
	m.chatCanvas.streamThinking("pondering the universe")
	m.refresh()
	got := ansiStrip(m.viewport.View())
	if !strings.Contains(got, "[thinking]") {
		t.Fatalf("viewport missing [thinking] header; got %q", got)
	}
	if !strings.Contains(got, "pondering the universe") {
		t.Fatalf("viewport missing thinking content; got %q", got)
	}
	m.showThinking = false
	m.refresh()
	if strings.Contains(ansiStrip(m.viewport.View()), "[thinking]") {
		t.Fatal("showThinking off still rendered the thinking row")
	}
}

// TestRefreshToolCallRow verifies the live tool call row renders its
// label and arguments in the main viewport.
func TestRefreshToolCallRow(t *testing.T) {
	m := newRefreshModel()
	m.chatCanvas.setRun("sess", func() {})
	m.chatCanvas.streamCall(core.ToolCall{ID: "call1", Name: "files.read", Arguments: map[string]any{"path": "/tmp/x"}})
	m.refresh()
	got := ansiStrip(m.viewport.View())
	if !strings.Contains(got, "[tool: files.read]") {
		t.Fatalf("viewport missing tool label; got %q", got)
	}
	if !strings.Contains(got, "path: /tmp/x") {
		t.Fatalf("viewport missing tool args; got %q", got)
	}
}

// TestRefreshToolResultRow verifies the live tool result renders full
// content (a short result under auto).
func TestRefreshToolResultRow(t *testing.T) {
	m := newRefreshModel()
	m.chatCanvas.setRun("sess", func() {})
	m.chatCanvas.streamCall(core.ToolCall{ID: "call1", Name: "files.read"})
	m.chatCanvas.streamResult(core.NewToolResult("file.txt:42:match", "call1", false))
	m.refresh()
	if got := ansiStrip(m.viewport.View()); !strings.Contains(got, "file.txt:42:match") {
		t.Fatalf("viewport missing the tool result; got %q", got)
	}
}

// TestRefreshToolResultCollapsed verifies the auto-collapse law: a
// long result renders as the line count, re-expanded when the toggle
// lifts (one display law, re-rendered from the payload).
func TestRefreshToolResultCollapsed(t *testing.T) {
	m := newRefreshModel()
	m.toolResultsAuto = true
	assistant := core.NewAssistant("")
	assistant.ToolCalls = []core.ToolCall{{ID: "call1", Name: "files.read"}}
	m.chatCanvas.setConversation("", []core.Message{
		assistant,
		core.NewToolResult(strings.Repeat("long line\n", 25), "call1", false),
	})
	m.refresh()
	if got := ansiStrip(m.viewport.View()); !strings.Contains(got, "[tool result: 26 lines]") {
		t.Fatalf("long result not collapsed; got %q", got)
	}
	m.toolResultsAuto = false
	m.refresh()
	if !strings.Contains(ansiStrip(m.viewport.View()), "long line") {
		t.Fatal("toolResultsAuto off did not re-expand the result")
	}
}

// TestRefreshResponseMarkdown verifies assistant text renders through
// the markdown pipeline.
func TestRefreshResponseMarkdown(t *testing.T) {
	m := newRefreshModel()
	m.chatCanvas.setConversation("", []core.Message{core.NewAssistant("# Hello World")})
	m.refresh()
	if !strings.Contains(ansiStrip(m.viewport.View()), "Hello World") {
		t.Fatal("viewport missing rendered response")
	}
}

// TestRefreshResponseDebounce verifies the 80ms live debounce: a frame
// just rendered replays the cached glamour output; a cold frame
// renders raw.
func TestRefreshResponseDebounce(t *testing.T) {
	m := newRefreshModel()
	m.chatCanvas.setRun("sess", func() {})
	m.chatCanvas.streamResponse("fresh raw content")
	m.renderCache.at = time.Now() // just rendered
	m.renderCache.rendered = "cached-output"
	m.refresh()
	if got := ansiStrip(m.viewport.View()); !strings.Contains(got, "cached-output") {
		t.Fatalf("debounced frame did not replay the cache; got %q", got)
	}
	m.renderCache.at = time.Time{}
	m.renderCache.rendered = ""
	m.refresh()
	if got := ansiStrip(m.viewport.View()); !strings.Contains(got, "fresh raw content") {
		t.Fatalf("cold frame did not render raw; got %q", got)
	}
}

// TestRefreshStreamingOrder verifies the live rows render in emit
// order: thinking, tool call, streamed text - the narrative law the
// segment machinery preserved.
func TestRefreshStreamingOrder(t *testing.T) {
	m := newRefreshModel()
	m.chatCanvas.setConversation("sess", []core.Message{core.NewUser("hi")})
	m.chatCanvas.setRun("sess", func() {})
	m.chatCanvas.streamThinking("FIRST_THINK")
	m.chatCanvas.streamCall(core.ToolCall{ID: "c1", Name: "files.read"})
	m.chatCanvas.streamResult(core.NewToolResult("THIRD_RESULT", "c1", false))
	m.chatCanvas.streamResponse("FIFTH_RESP")
	m.refresh()
	got := ansiStrip(m.viewport.View())
	prev := -1
	for _, marker := range []string{"FIRST_THINK", "files.read", "THIRD_RESULT", "FIFTH_RESP"} {
		idx := strings.Index(got, marker)
		if idx < 0 {
			t.Fatalf("marker %q missing from %q", marker, got)
		}
		if idx < prev {
			t.Errorf("marker %q at %d before previous at %d (ordering broken)", marker, idx, prev)
		}
		prev = idx
	}
}

// TestRefreshErrorLine verifies the committed run's stream error rides
// one dim line after the rows until the next run recomposes.
func TestRefreshErrorLine(t *testing.T) {
	m := newRefreshModel()
	m.chatCanvas.setConversation("sess", []core.Message{core.NewUser("hi")})
	m.chatCanvas.commit([]core.Message{core.NewUser("hi")}, "boom")
	m.refresh()
	if got := ansiStrip(m.viewport.View()); !strings.Contains(got, "error: boom") {
		t.Fatalf("viewport missing the error line; got %q", got)
	}
	m.chatCanvas.setRun("sess", func() {})
	m.refresh()
	if strings.Contains(ansiStrip(m.viewport.View()), "error: boom") {
		t.Fatal("error line survived the next run")
	}
}
