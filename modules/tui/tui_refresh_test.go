package tui

import (
	"strings"
	"testing"
	"time"
)

// newRefreshModel returns a model configured the same way the existing
// tui_test.go helpers set one up, ready for refresh() assertions.
func newRefreshModel() *model {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell", "dev", "")
	return &m
}

// TestRefreshEmptySegments verifies that refresh with no segments writes
// only the transcript into the viewport.
func TestRefreshEmptySegments(t *testing.T) {
	m := newRefreshModel()
	m.transcript = "transcript-only"
	m.segments = nil
	m.refresh()
	got := ansiStrip(m.viewport.View())
	if !strings.Contains(got, "transcript-only") {
		t.Fatalf("viewport missing transcript; got %q", got)
	}
}

// TestRefreshThinkingSegment verifies the [thinking] header is rendered.
func TestRefreshThinkingSegment(t *testing.T) {
	m := newRefreshModel()
	m.segments = []streamSegment{{kind: "thinking", content: "pondering the universe"}}
	m.refresh()
	got := ansiStrip(m.viewport.View())
	if !strings.Contains(got, "[thinking]") {
		t.Fatalf("viewport missing [thinking] header; got %q", got)
	}
	if !strings.Contains(got, "pondering the universe") {
		t.Fatalf("viewport missing thinking content; got %q", got)
	}
}

// TestRefreshToolSegment verifies tool content is written directly.
func TestRefreshToolSegment(t *testing.T) {
	m := newRefreshModel()
	m.segments = []streamSegment{{kind: "tool", content: "running grep"}}
	m.refresh()
	got := ansiStrip(m.viewport.View())
	if !strings.Contains(got, "running grep") {
		t.Fatalf("viewport missing tool content; got %q", got)
	}
}

// TestRefreshToolResultSegment verifies tool_result content is rendered.
func TestRefreshToolResultSegment(t *testing.T) {
	m := newRefreshModel()
	m.segments = []streamSegment{{kind: "tool_result", content: "file.txt:42:match"}}
	m.refresh()
	got := ansiStrip(m.viewport.View())
	if !strings.Contains(got, "file.txt:42:match") {
		t.Fatalf("viewport missing tool_result content; got %q", got)
	}
}

// TestRefreshToolResultHighlightedSegment verifies highlighted tool result
// content is written directly (no theme overlay needed for assertion).
func TestRefreshToolResultHighlightedSegment(t *testing.T) {
	m := newRefreshModel()
	m.segments = []streamSegment{{kind: "tool_result_highlighted", content: "HIGHLIGHTED_CODE"}}
	m.refresh()
	got := ansiStrip(m.viewport.View())
	if !strings.Contains(got, "HIGHLIGHTED_CODE") {
		t.Fatalf("viewport missing highlighted content; got %q", got)
	}
}

// TestRefreshResponseSegmentNotBusy verifies that a response segment when
// not busy is rendered through renderMarkdown (full glamour pass).
func TestRefreshResponseSegmentNotBusy(t *testing.T) {
	m := newRefreshModel()
	m.busy = false
	m.segments = []streamSegment{{kind: "response", content: "# Hello World"}}
	m.refresh()
	got := ansiStrip(m.viewport.View())
	if !strings.Contains(got, "Hello World") {
		t.Fatalf("viewport missing rendered response; got %q", got)
	}
	// renderMarkdown should have been invoked and the cache populated.
	if m.lastRenderedResponse == "" {
		t.Fatalf("lastRenderedResponse not cached after non-busy render")
	}
}

// TestRefreshResponseSegmentBusyDebounce verifies the debounce path: when
// busy and the last render was <80ms ago, the cached lastRenderedResponse
// is used instead of re-rendering.
func TestRefreshResponseSegmentBusyDebounce(t *testing.T) {
	m := newRefreshModel()
	m.busy = true
	m.lastRender = time.Now()
	m.lastRenderedResponse = "cached-output"
	m.segments = []streamSegment{{kind: "response", content: "fresh raw content"}}
	m.refresh()
	got := ansiStrip(m.viewport.View())
	if !strings.Contains(got, "cached-output") {
		t.Fatalf("debounce path did not use cached output; got %q", got)
	}
}

// TestRefreshResponseSegmentBusyNoCache verifies that when busy and within
// the debounce window but no cached output exists, raw content is shown.
func TestRefreshResponseSegmentBusyNoCache(t *testing.T) {
	m := newRefreshModel()
	m.busy = true
	m.lastRender = time.Now()
	m.lastRenderedResponse = ""
	m.segments = []streamSegment{{kind: "response", content: "raw-fallback"}}
	m.refresh()
	got := ansiStrip(m.viewport.View())
	if !strings.Contains(got, "raw-fallback") {
		t.Fatalf("debounce no-cache path did not show raw content; got %q", got)
	}
}

// TestRefreshMultipleSegments verifies segments render in order.
func TestRefreshMultipleSegments(t *testing.T) {
	m := newRefreshModel()
	m.segments = []streamSegment{
		{kind: "thinking", content: "FIRST_THINK"},
		{kind: "tool", content: "SECOND_TOOL"},
		{kind: "tool_result", content: "THIRD_RESULT"},
		{kind: "tool_result_highlighted", content: "FOURTH_HL"},
		{kind: "response", content: "FIFTH_RESP"},
	}
	m.refresh()
	got := ansiStrip(m.viewport.View())

	for _, want := range []string{"FIRST_THINK", "SECOND_TOOL", "THIRD_RESULT", "FOURTH_HL", "FIFTH_RESP"} {
		if !strings.Contains(got, want) {
			t.Errorf("viewport missing %q; got %q", want, got)
		}
	}

	// Verify ordering: each marker should appear after the previous one.
	prev := -1
	for _, marker := range []string{"FIRST_THINK", "SECOND_TOOL", "THIRD_RESULT", "FOURTH_HL", "FIFTH_RESP"} {
		idx := strings.Index(got, marker)
		if idx < 0 {
			t.Fatalf("marker %q not found (shouldn't happen, checked above)", marker)
		}
		if idx < prev {
			t.Errorf("marker %q at %d before previous at %d (ordering broken)", marker, idx, prev)
		}
		prev = idx
	}
}