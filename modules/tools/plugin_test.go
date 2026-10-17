package tools

import (
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// ---------------------------------------------------------------------------
// firstLineOfDesc
// ---------------------------------------------------------------------------

func TestFirstLineOfDescShort(t *testing.T) {
	got := firstLineOfDesc("list files")
	if got != "list files" {
		t.Fatalf("got %q, want %q", got, "list files")
	}
}

func TestFirstLineOfDescEmpty(t *testing.T) {
	got := firstLineOfDesc("")
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestFirstLineOfDescMultiLine(t *testing.T) {
	got := firstLineOfDesc("first line\nsecond line\nthird line")
	if got != "first line" {
		t.Fatalf("got %q, want %q", got, "first line")
	}
}

func TestFirstLineOfDescTruncate(t *testing.T) {
	long := "this is a very long description that definitely exceeds sixty chars boundary"
	got := firstLineOfDesc(long)
	// truncated to 57 chars + "..."
	if len(got) != 60 {
		t.Fatalf("len(got) = %d, want 60", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("got %q, want suffix '...'", got)
	}
	if got[:57] != long[:57] {
		t.Fatalf("truncated prefix = %q, want %q", got[:57], long[:57])
	}
}

func TestFirstLineOfDescMultiLineTruncate(t *testing.T) {
	long := "this is a very long first line that definitely exceeds sixty chars\nsecond line"
	got := firstLineOfDesc(long)
	if len(got) != 60 {
		t.Fatalf("len(got) = %d, want 60", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("got %q, want suffix '...'", got)
	}
	if got[:57] != long[:57] {
		t.Fatalf("truncated prefix = %q, want %q", got[:57], long[:57])
	}
}

func TestFirstLineOfDescExactly60(t *testing.T) {
	// 60 chars exactly: should NOT be truncated (len > 60 is false)
	desc := "012345678901234567890123456789012345678901234567890123456789" // 60 chars
	got := firstLineOfDesc(desc)
	if got != desc {
		t.Fatalf("got %q, want %q (boundary case should not truncate)", got, desc)
	}
}

// ---------------------------------------------------------------------------
// listTools
// ---------------------------------------------------------------------------

func TestListToolsEmpty(t *testing.T) {
	ctx := &agent.Context{}
	res := listTools(ctx)
	if !strings.Contains(res.Text, "[no tools available]") {
		t.Fatalf("expected '[no tools available]', got %q", res.Text)
	}
}

func TestListToolsEmptyToolListSkipped(t *testing.T) {
	// An extension with an empty ToolList should be skipped, and since no
	// extension has tools, "[no tools available]" should appear.
	ctx := &agent.Context{
		Infos: []agent.ExtensionInfo{
			{Name: "empty", ToolList: []agent.ToolInfo{}},
		},
	}
	res := listTools(ctx)
	if !strings.Contains(res.Text, "[no tools available]") {
		t.Fatalf("expected '[no tools available]', got %q", res.Text)
	}
}

func TestListToolsSingleExtension(t *testing.T) {
	ctx := &agent.Context{
		Infos: []agent.ExtensionInfo{
			{
				Name: "files",
				ToolList: []agent.ToolInfo{
					{Name: "files.read", Description: "read a file"},
					{Name: "files.write", Description: "write a file"},
				},
			},
		},
	}
	res := listTools(ctx)
	if !strings.Contains(res.Text, "files") {
		t.Fatalf("missing extension header 'files', got %q", res.Text)
	}
	if !strings.Contains(res.Text, "files.read") {
		t.Fatalf("missing tool 'files.read', got %q", res.Text)
	}
	if !strings.Contains(res.Text, "files.write") {
		t.Fatalf("missing tool 'files.write', got %q", res.Text)
	}
	if !strings.Contains(res.Text, "read a file") {
		t.Fatalf("missing description 'read a file', got %q", res.Text)
	}
	if strings.Contains(res.Text, "[no tools available]") {
		t.Fatalf("should not show '[no tools available]' when tools exist, got %q", res.Text)
	}
}

func TestListToolsMultipleExtensions(t *testing.T) {
	ctx := &agent.Context{
		Infos: []agent.ExtensionInfo{
			{
				Name: "files",
				ToolList: []agent.ToolInfo{
					{Name: "files.read", Description: "read a file"},
				},
			},
			{
				Name: "shell",
				ToolList: []agent.ToolInfo{
					{Name: "shell.run", Description: "run a command"},
				},
			},
		},
	}
	res := listTools(ctx)
	if !strings.Contains(res.Text, "files") {
		t.Fatalf("missing extension 'files', got %q", res.Text)
	}
	if !strings.Contains(res.Text, "shell") {
		t.Fatalf("missing extension 'shell', got %q", res.Text)
	}
	if !strings.Contains(res.Text, "files.read") {
		t.Fatalf("missing tool 'files.read', got %q", res.Text)
	}
	if !strings.Contains(res.Text, "shell.run") {
		t.Fatalf("missing tool 'shell.run', got %q", res.Text)
	}
	if !strings.Contains(res.Text, "run a command") {
		t.Fatalf("missing description 'run a command', got %q", res.Text)
	}
	if strings.Contains(res.Text, "[no tools available]") {
		t.Fatalf("should not show '[no tools available]' when tools exist, got %q", res.Text)
	}
}

func TestListToolsMixedEmptyAndNonEmpty(t *testing.T) {
	// Extension with tools followed by an empty one — empty should be skipped.
	ctx := &agent.Context{
		Infos: []agent.ExtensionInfo{
			{
				Name: "files",
				ToolList: []agent.ToolInfo{
					{Name: "files.read", Description: "read a file"},
				},
			},
			{Name: "empty", ToolList: []agent.ToolInfo{}},
		},
	}
	res := listTools(ctx)
	if !strings.Contains(res.Text, "files.read") {
		t.Fatalf("missing tool 'files.read', got %q", res.Text)
	}
	if strings.Contains(res.Text, "[no tools available]") {
		t.Fatalf("should not show '[no tools available]' when tools exist, got %q", res.Text)
	}
}

// ---------------------------------------------------------------------------
// handleToolsCommand
// ---------------------------------------------------------------------------

func TestHandleToolsCommandNoArgs(t *testing.T) {
	ctx := &agent.Context{}
	res, err := handleToolsCommand(ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "[no tools available]") {
		t.Fatalf("no args should call listTools; got %q", res.Text)
	}
	if len(res.Actions) != 0 {
		t.Fatalf("no args should not produce actions; got %v", res.Actions)
	}
}

func TestHandleToolsCommandList(t *testing.T) {
	ctx := &agent.Context{}
	res, err := handleToolsCommand(ctx, "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "[no tools available]") {
		t.Fatalf("'list' should call listTools; got %q", res.Text)
	}
}

func TestHandleToolsCommandListWithTools(t *testing.T) {
	ctx := &agent.Context{
		Infos: []agent.ExtensionInfo{
			{
				Name: "files",
				ToolList: []agent.ToolInfo{
					{Name: "files.read", Description: "read a file"},
				},
			},
		},
	}
	res, err := handleToolsCommand(ctx, "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "files.read") {
		t.Fatalf("'list' should show tools; got %q", res.Text)
	}
}

func TestHandleToolsCommandOn(t *testing.T) {
	ctx := &agent.Context{}
	res, err := handleToolsCommand(ctx, "on")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "[tool results expanded]" {
		t.Fatalf("Text = %q, want '[tool results expanded]'", res.Text)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("Actions len = %d, want 1", len(res.Actions))
	}
	a := res.Actions[0]
	if a.Type != "set_tool_display" || a.Value != "expanded" {
		t.Fatalf("Action = %+v, want {set_tool_display expanded}", a)
	}
}

func TestHandleToolsCommandExpanded(t *testing.T) {
	ctx := &agent.Context{}
	res, err := handleToolsCommand(ctx, "expanded")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "[tool results expanded]" {
		t.Fatalf("Text = %q, want '[tool results expanded]'", res.Text)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("Actions len = %d, want 1", len(res.Actions))
	}
	a := res.Actions[0]
	if a.Type != "set_tool_display" || a.Value != "expanded" {
		t.Fatalf("Action = %+v, want {set_tool_display expanded}", a)
	}
}

func TestHandleToolsCommandOff(t *testing.T) {
	ctx := &agent.Context{}
	res, err := handleToolsCommand(ctx, "off")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "[tool results collapsed]" {
		t.Fatalf("Text = %q, want '[tool results collapsed]'", res.Text)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("Actions len = %d, want 1", len(res.Actions))
	}
	a := res.Actions[0]
	if a.Type != "set_tool_display" || a.Value != "collapsed" {
		t.Fatalf("Action = %+v, want {set_tool_display collapsed}", a)
	}
}

func TestHandleToolsCommandCollapsed(t *testing.T) {
	ctx := &agent.Context{}
	res, err := handleToolsCommand(ctx, "collapsed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "[tool results collapsed]" {
		t.Fatalf("Text = %q, want '[tool results collapsed]'", res.Text)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("Actions len = %d, want 1", len(res.Actions))
	}
	a := res.Actions[0]
	if a.Type != "set_tool_display" || a.Value != "collapsed" {
		t.Fatalf("Action = %+v, want {set_tool_display collapsed}", a)
	}
}

func TestHandleToolsCommandAuto(t *testing.T) {
	ctx := &agent.Context{}
	res, err := handleToolsCommand(ctx, "auto")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "[tool results auto]" {
		t.Fatalf("Text = %q, want '[tool results auto]'", res.Text)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("Actions len = %d, want 1", len(res.Actions))
	}
	a := res.Actions[0]
	if a.Type != "set_tool_display" || a.Value != "auto" {
		t.Fatalf("Action = %+v, want {set_tool_display auto}", a)
	}
}

func TestHandleToolsCommandInvalid(t *testing.T) {
	ctx := &agent.Context{}
	_, err := handleToolsCommand(ctx, "bogus")
	if err == nil {
		t.Fatal("expected error for invalid arg, got nil")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Fatalf("error should mention usage, got %q", err.Error())
	}
}

func TestHandleToolsCommandWhitespaceTrimmed(t *testing.T) {
	// Leading/trailing whitespace should be trimmed, so "  list  " → listTools.
	ctx := &agent.Context{}
	res, err := handleToolsCommand(ctx, "  list  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "[no tools available]") {
		t.Fatalf("trimmed 'list' should call listTools; got %q", res.Text)
	}
}

