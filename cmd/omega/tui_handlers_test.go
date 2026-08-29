package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// newTestModel mirrors the existing tui_test.go pattern. The helper
// keeps the newChatModel signature stable so tests can swap themes cleanly.
func newTestModel(themeName string) model {
	return newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, themeName, "", "bell")
}

// ctxWithInfos builds a non-nil agent.Context carrying the given Infos,
// with the tools extension's command handler wired so /tools dispatches
// correctly. We set Infos directly rather than calling MountAll so the
// tests stay focused on the TUI render path and don't depend on plugin
// wiring.
func ctxWithInfos(infos ...agent.ExtensionInfo) *agent.Context {
	pctx := &agent.Context{Infos: infos}
	pctx.Commands = append(pctx.Commands,
		agent.ExtensionCommand{Name: "/tools", Description: "list tools"},
	)
	prev := pctx.CommandHandler
	pctx.CommandHandler = func(c context.Context, name, args string) (agent.CommandResult, error) {
		if name == "/tools" {
			arg := strings.TrimSpace(args)
			if arg == "" || arg == "list" {
				var sb strings.Builder
				sb.WriteString("\n")
				nameWidth := 0
				for _, ext := range pctx.Infos {
					for _, t := range ext.ToolList {
						if len(t.Name) > nameWidth {
							nameWidth = len(t.Name)
						}
					}
				}
				for _, ext := range pctx.Infos {
					if len(ext.ToolList) == 0 {
						continue
					}
					sb.WriteString(ext.Name)
					sb.WriteString("\n")
					for _, t := range ext.ToolList {
						desc := t.Description
						if i := strings.IndexByte(desc, '\n'); i >= 0 {
							desc = desc[:i]
						}
						if len(desc) > 60 {
							desc = desc[:57] + "..."
						}
						fmt.Fprintf(&sb, "  %-*s  %s\n", nameWidth, t.Name, desc)
					}
					sb.WriteString("\n")
				}
				if nameWidth == 0 {
					sb.WriteString("[no tools available]\n")
				}
				return agent.CommandResult{Text: sb.String()}, nil
			}
			return agent.CommandResult{}, fmt.Errorf("usage: /tools [on|off|auto|list]")
		}
		if prev != nil {
			return prev(c, name, args)
		}
		return agent.CommandResult{}, fmt.Errorf("unknown command: %s", name)
	}
	return pctx
}

// --- handleToolsList ---

func TestHandleToolsListNoExtensions(t *testing.T) {
	m := newTestModel("dark")
	// No extensions -> [no tools available].
	m.extensions = ctxWithInfos()
	out, _ := m.handleCommand("/tools")
	mm := out.(model)
	if !strings.Contains(ansiStrip(mm.transcript), "[no tools available]") {
		t.Fatalf("expected [no tools available], got: %q", ansiStrip(mm.transcript))
	}
}

func TestHandleToolsListWithTools(t *testing.T) {
	m := newTestModel("dark")
	m.extensions = ctxWithInfos(agent.ExtensionInfo{
		Name: "shell",
		ToolList: []agent.ToolInfo{
			{Name: "shell.run", Description: "run a shell command"},
			{Name: "files.read", Description: "read a file\nsecond line ignored"},
		},
	})
	out, _ := m.handleCommand("/tools")
	mm := out.(model)
	plain := ansiStrip(mm.transcript)
	if !strings.Contains(plain, "shell") {
		t.Fatalf("expected extension name 'shell' in transcript, got: %q", plain)
	}
	if !strings.Contains(plain, "shell.run") || !strings.Contains(plain, "files.read") {
		t.Fatalf("expected tool names in transcript, got: %q", plain)
	}
	// firstLineOfDesc keeps only the first non-empty line.
	if strings.Contains(plain, "second line ignored") {
		t.Fatalf("expected only first line of description, got: %q", plain)
	}
	if strings.Contains(plain, "run a shell command") {
		// description first line present
	} else {
		t.Fatalf("expected first-line description in transcript, got: %q", plain)
	}
}

func TestHandleToolsListEmptyToolList(t *testing.T) {
	// Extension present but with zero tools -> [no tools available].
	m := newTestModel("dark")
	m.extensions = ctxWithInfos(agent.ExtensionInfo{Name: "empty"})
	out, _ := m.handleCommand("/tools")
	mm := out.(model)
	if !strings.Contains(ansiStrip(mm.transcript), "[no tools available]") {
		t.Fatalf("expected [no tools available] for tool-less extension, got: %q",
			ansiStrip(mm.transcript))
	}
}

// --- handleExtensions ---

func TestHandleExtensionsNone(t *testing.T) {
	m := newTestModel("dark")
	// Non-nil context with empty Infos -> [no extensions loaded].
	// (handleExtensions dereferences m.extensions.Infos directly, so a
	// truly nil context would panic; production always has a context.)
	m.extensions = &agent.Context{}
	out, _ := m.handleExtensions()
	mm := out.(model)
	if !strings.Contains(ansiStrip(mm.transcript), "[no extensions loaded]") {
		t.Fatalf("expected [no extensions loaded], got: %q", ansiStrip(mm.transcript))
	}
}

func TestHandleExtensionsWithInfos(t *testing.T) {
	m := newTestModel("dark")
	m.extensions = ctxWithInfos(
		agent.ExtensionInfo{Name: "shell", Tools: 3, Commands: 1, Seams: []string{"tools"}},
		agent.ExtensionInfo{Name: "provider", Tools: 0, Commands: 2, Seams: []string{"provider"}},
	)
	out, _ := m.handleExtensions()
	mm := out.(model)
	plain := ansiStrip(mm.transcript)
	for _, want := range []string{"NAME", "TOOLS", "COMMANDS", "SEAMS", "shell", "provider"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected %q in transcript, got: %q", want, plain)
		}
	}
	// Tool/command counts appear as formatted integers.
	if !strings.Contains(plain, "3") || !strings.Contains(plain, "2") {
		t.Fatalf("expected tool/command counts in transcript, got: %q", plain)
	}
}

// --- handleTheme ---

func TestHandleThemeListNoArgs(t *testing.T) {
	m := newTestModel("dark")
	out, _ := m.handleTheme(nil)
	mm := out.(model)
	plain := ansiStrip(mm.transcript)
	if !strings.Contains(plain, "dark") || !strings.Contains(plain, "light") {
		t.Fatalf("expected theme names dark+light, got: %q", plain)
	}
	if !strings.Contains(plain, "auto") {
		t.Fatalf("expected 'auto' option, got: %q", plain)
	}
	// Current theme (dark) marked with '*'.
	lines := strings.Split(plain, "\n")
	marked := false
	for _, l := range lines {
		if strings.Contains(l, "* dark") {
			marked = true
			break
		}
	}
	if !marked {
		t.Fatalf("expected current theme 'dark' marked with '*', got: %q", plain)
	}
}

func TestHandleThemeSwitchLight(t *testing.T) {
	m := newTestModel("dark")
	out, _ := m.handleTheme([]string{"light"})
	mm := out.(model)
	if mm.theme.Name != "light" {
		t.Fatalf("expected theme.Name=light, got %q", mm.theme.Name)
	}
	if !strings.Contains(ansiStrip(mm.transcript), "[theme: light]") {
		t.Fatalf("expected [theme: light] in transcript, got: %q", ansiStrip(mm.transcript))
	}
}

func TestHandleThemeUnknown(t *testing.T) {
	m := newTestModel("dark")
	out, _ := m.handleTheme([]string{"badtheme"})
	mm := out.(model)
	if !strings.Contains(mm.err, "unknown theme") {
		t.Fatalf("expected err containing 'unknown theme', got %q", mm.err)
	}
	if !strings.Contains(mm.err, "badtheme") {
		t.Fatalf("expected err to name the bad theme, got %q", mm.err)
	}
}

func TestHandleThemeAuto(t *testing.T) {
	m := newTestModel("dark")
	out, _ := m.handleTheme([]string{"auto"})
	mm := out.(model)
	if mm.theme.Name != "auto" {
		t.Fatalf("expected theme.Name=auto, got %q", mm.theme.Name)
	}
	plain := ansiStrip(mm.transcript)
	if !strings.Contains(plain, "[theme: auto") {
		t.Fatalf("expected [theme: auto ...] in transcript, got: %q", plain)
	}
}