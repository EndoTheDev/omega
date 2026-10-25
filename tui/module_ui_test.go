package tui

// Tests for the module-TUI shell plumbing (card t_88bb7dcf): the
// command table breathes with the mounted providers, dispatch reaches
// the provider that registered the command, and the shell applies the
// returned instructions (screen to focus, follow loop to drive).

import (
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	tea "github.com/charmbracelet/bubbletea"
)

// fakeModuleProvider is a TUIProvider whose handler records the calls
// it receives and returns a configurable result.
type fakeModuleProvider struct {
	cmds   []core.TUICommand
	got    []string // recorded (name, args) pairs
	result core.TUIResult
}

func (f *fakeModuleProvider) TUICommands() []core.TUICommand { return f.cmds }

func (f *fakeModuleProvider) HandleTUICommand(name, args string, c core.TUIConsole) core.TUIResult {
	f.got = append(f.got, name+"|"+args)
	c.Labeled("", "ran "+name)
	return f.result
}

// fakeTUICommands is a provider that only declares commands, for
// table-shape tests.
type fakeTUICommands []core.TUICommand

func (f fakeTUICommands) TUICommands() []core.TUICommand { return f }

func (f fakeTUICommands) HandleTUICommand(_ string, _ string, _ core.TUIConsole) core.TUIResult {
	return core.TUIResult{}
}

// fakeModuleScreen is a TUIScreen that closes on demand.
type fakeModuleScreen struct {
	renders int
	closed  bool
}

func (s *fakeModuleScreen) Update(msg any, c core.TUIConsole) {
	if _, isKey := msg.(tea.KeyMsg); isKey && !s.closed {
		s.renders++
	}
}
func (s *fakeModuleScreen) View(width, height int) string { return "SCREEN" }
func (s *fakeModuleScreen) Closed() bool                  { return s.closed }

// moduleTestModel builds a chat model with the given providers mounted.
func moduleTestModel(t *testing.T, providers ...core.TUIProvider) model {
	t.Helper()
	pctx := &core.Context{TUIProviders: providers}
	m := newChatModel("ollama", "llama3", nil, "", nil, "", pctx, nil, "dark", "", "bell", "dev", "")
	m.screenHeight = 40
	return m
}

// TestModuleCommandDispatch routes a provider-owned command to its
// provider and writes the console output into the transcript.
func TestModuleCommandDispatch(t *testing.T) {
	p := &fakeModuleProvider{cmds: []core.TUICommand{{Name: "/feat", Help: "exercise"}}}
	m := moduleTestModel(t, p)

	updated, _ := m.handleCommand("/feat alpha")
	m = updated.(model)
	if len(p.got) != 1 || p.got[0] != "/feat|alpha" {
		t.Fatalf("provider not called: %v", p.got)
	}
	if got := transcriptOf(m); !strings.Contains(got, "ran /feat") {
		t.Fatalf("transcript missing provider output: %q", got)
	}

	// an unknown command with no provider, no extension and no skill
	// still lands on the unknown-command error
	updated, _ = m.handleCommand("/nope")
	m = updated.(model)
	if m.err != "unknown command: /nope" {
		t.Fatalf("err = %q", m.err)
	}
}

// TestModuleCommandTableOrder pins the command list shape: shell
// commands first in their order, then module commands alphabetical,
// then extension commands, then skills.
func TestModuleCommandTableOrder(t *testing.T) {
	p1 := &fakeModuleProvider{cmds: []core.TUICommand{{Name: "/zeta"}, {Name: "/alpha"}}}
	p2 := &fakeModuleProvider{cmds: []core.TUICommand{{Name: "/mid"}}}
	pctx := &core.Context{
		TUIProviders: []core.TUIProvider{p1, p2},
		Commands:     []core.ExtensionCommand{{Name: "/ext"}},
	}
	got := buildCommands(knownCommands, pctx, []core.Skill{{Name: "sk"}})
	want := []string{
		"/copy", "/extensions", "/panels", "/theme", "/variant", "/fork", "/retry", "/exit", "/help",
		"/alpha", "/mid", "/zeta",
		"/ext",
		"/sk",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("command table = %v, want %v", got, want)
	}
}

// TestModuleOverlay focuses the returned full-screen view, routes keys
// to it, and folds it back to the transcript when it closes. Opening
// a screen also disarms a running follow loop (the mode change hands
// the keys back), and a keypress stops the loop in every case.
func TestModuleOverlay(t *testing.T) {
	screen := &fakeModuleScreen{}
	p := &fakeModuleProvider{
		cmds:   []core.TUICommand{{Name: "/feat"}},
		result: core.TUIResult{Overlay: screen},
	}
	m := moduleTestModel(t, p)

	updated, _ := m.handleCommand("/feat")
	m = updated.(model)
	if m.screen == nil {
		t.Fatal("/feat left no screen open")
	}

	if got := ansiStrip(m.View()); !strings.Contains(got, "SCREEN") {
		t.Fatalf("View missing the screen body: %q", got)
	}

	// keys route to the screen; closing it folds back
	screen.closed = true
	mo, _, consumed := m.routeModuleScreen(tea.KeyMsg{Type: tea.KeyEsc})
	if !consumed {
		t.Fatal("screen did not consume the key")
	}
	if folded := mo.(model); folded.screen != nil {
		t.Fatal("closed screen did not fold back")
	}
}

// TestModuleTick: a returned TUITick runs on the shell's tick message,
// appends what the step writes, and stops when the step says so or on
// any keypress.
func TestModuleTick(t *testing.T) {
	steps := 0
	p := &fakeModuleProvider{
		cmds: []core.TUICommand{{Name: "/feat"}},
		result: core.TUIResult{Tick: &core.TUITick{
			Interval: time.Second,
			Step: func(c core.TUIConsole) (string, bool) {
				steps++
				c.Labeled("", "tick")
				return "", steps < 2
			},
		}},
	}
	m := moduleTestModel(t, p)

	updated, _ := m.handleCommand("/feat")
	m = updated.(model)
	if m.tick == nil {
		t.Fatal("/feat left no follow loop")
	}

	updated, _ = m.handleModuleTick()
	m = updated.(model)
	if m.tick == nil || steps != 1 {
		t.Fatalf("tick dropped early: steps=%d tick=%v", steps, m.tick)
	}
	updated, _ = m.handleModuleTick()
	m = updated.(model)
	if m.tick != nil || steps != 2 {
		t.Fatalf("keep=false did not stop the loop: steps=%d tick=%v", steps, m.tick)
	}

	// a keypress stops an armed loop
	m.tick = &core.TUITick{Step: func(core.TUIConsole) (string, bool) { return "", true }}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.tick != nil {
		t.Fatal("keypress did not stop the loop")
	}
}
