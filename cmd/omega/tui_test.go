package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/extensions/compactor"
	"github.com/EndoTheDev/omega/extensions/provider"
	"github.com/EndoTheDev/omega/gateway"
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

// testContext creates a minimal agent.Context with a real provider plugin
// and command handler, for testing extension-routed commands.
func testContext() *agent.Context {
	ctx := &agent.Context{}
	p := &provider.Plugin{}
	p.Mount(ctx)
	return ctx
}

// ansiStrips ANSI escape sequences so tests can assert on plain content
// regardless of glamour styling.
var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func ansiStrip(s string) string { return ansi.ReplaceAllString(s, "") }

// TestDrainEventsDeliversStream verifies the channel-drain path: events
// written by the run goroutine are delivered to Update, and the closed
// channel yields streamDoneMsg. This guards the regression where the
// goroutine's Send never reached the program (m.program was always nil).
func TestDrainEventsDeliversStream(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	ch := make(chan agent.Event, 64)
	m.events = ch

	// Simulate the run goroutine: one event, then close.
	ch <- agent.StreamEvent{Event: ai.ResponseChunk{Content: "hi"}}
	close(ch)

	// First drain delivers the event.
	msg := m.drainEvents()()
	if _, ok := msg.(agent.StreamEvent); !ok {
		t.Fatalf("expected agent.StreamEvent, got %T", msg)
	}

	// Second drain sees the closed channel and signals done.
	msg = m.drainEvents()()
	if _, ok := msg.(streamDoneMsg); !ok {
		t.Fatalf("expected streamDoneMsg after close, got %T", msg)
	}
}

// TestSubmitCreatesFreshChannel guards the regression where the events
// channel was created once in newChatModel and closed after the first run,
// so a second submit wrote to a closed channel and panicked. submit() must
// allocate a fresh channel per run.
func TestSubmitCreatesFreshChannel(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.textarea.SetValue("hello")
	// Simulate a completed first run: the channel is closed.
	ch := make(chan agent.Event, 64)
	m.events = ch
	close(ch)

	updated, _ := m.submit()
	m = updated.(model)

	if m.events == nil {
		t.Fatal("submit() left events channel nil")
	}
	// A fresh open channel receives the first event (AgentStart, written
	// synchronously before any network I/O) with ok=true. A closed channel
	// from a prior run would return immediately with ok=false.
	if _, ok := <-m.events; !ok {
		t.Fatal("expected fresh open channel from submit(), got a closed one")
	}
}

// TestHandleEventFoldsStream verifies that response chunks, tool calls, and
// the agent end fold into the transcript and history in the right order.
func TestHandleEventFoldsStream(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")

	m.handleEvent(agent.StreamEvent{Event: ai.ResponseChunk{Content: "hello"}})
	m.handleEvent(agent.StreamEvent{Event: ai.ResponseChunk{Content: " world"}})
	m.handleEvent(agent.StreamEvent{Event: ai.ToolCallEvent{ToolCall: ai.ToolCall{Name: "shell.run"}}})
	m.handleEvent(agent.AssistantMessageEvent{Type: "assistant_message", Message: ai.NewAssistant("hello world")})
	m.handleEvent(agent.AgentEnd{Type: "agent_end", FinishReason: "stop"})

	plain := ansiStrip(m.transcript)
	if !strings.Contains(plain, "hello world") {
		t.Fatalf("transcript missing streamed content: %q", plain)
	}
	if !strings.Contains(plain, "[tool: shell.run]") {
		t.Fatalf("transcript missing tool label: %q", plain)
	}
	if len(m.history) != 1 {
		t.Fatalf("expected 1 assistant message in history, got %d", len(m.history))
	}
	if len(m.segments) != 0 {
		t.Fatalf("segments should be cleared after AgentEnd, got %d", len(m.segments))
	}
}

// TestHandleEventError verifies a stream error is surfaced and folded.
func TestHandleEventError(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.handleEvent(agent.StreamEvent{Event: ai.StreamEnd{FinishReason: "error", Error: "boom"}})
	m.handleEvent(agent.AgentEnd{Type: "agent_end", FinishReason: "error", Error: "boom"})

	if m.err != "boom" {
		t.Fatalf("expected err boom, got %q", m.err)
	}
	if !strings.Contains(ansiStrip(m.transcript), "error: boom") {
		t.Fatalf("transcript missing error: %q", m.transcript)
	}
}

// TestSlashCommands verifies /new, /model, /help, and unknown handling.
func TestSlashCommands(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", testContext(), nil, "dark", "", "bell")

	// /model sets the model for the next run. handleCommand returns a new
	// model copy (value receiver); the caller must use the return value.
	updated, _ := m.handleCommand("/model llama3.1")
	m = updated.(model)
	if m.modelName != "llama3.1" {
		t.Fatalf("expected model llama3.1, got %q", m.modelName)
	}

	// /model with no arg reports usage.
	updated, _ = m.handleCommand("/model")
	m = updated.(model)
	if m.err != "usage: /model <#|name>" {
		t.Fatalf("expected usage error, got %q", m.err)
	}

	// /help renders help text.
	updated, _ = m.handleCommand("/help")
	m = updated.(model)
	if !strings.Contains(m.transcript, "/exit") {
		t.Fatalf("help text missing /exit: %q", m.transcript)
	}

	// /new wipes history and transcript.
	m.history = append(m.history, ai.NewUser("hi"))
	m.transcript = "some old text"
	updated, _ = m.handleCommand("/new")
	m = updated.(model)
	if len(m.history) != 0 || m.transcript != "" {
		t.Fatalf("clear failed: history=%d transcript=%q", len(m.history), m.transcript)
	}

	// unknown command reports an error.
	updated, _ = m.handleCommand("/nope")
	m = updated.(model)
	if m.err != "unknown command: /nope" {
		t.Fatalf("expected unknown command error, got %q", m.err)
	}
}

// TestSkillsCommand verifies that /skills is no longer a built-in TUI
// command — it's registered by the skills extension. With no
// extension loaded, /skills falls through to the unknown-command path.
func TestSkillsCommand(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	updated, _ := m.handleCommand("/skills")
	m = updated.(model)
	// /skills is not a built-in command; with no extension manager,
	// it produces an error message, not a skills listing.
	if !strings.Contains(m.err, "unknown command") && !strings.Contains(m.err, "no extensions") {
		// If the error is empty, the command may have been routed to
		// the extension manager which returns "no extensions loaded".
		if m.err == "" {
			t.Fatalf("expected error for /skills with no extension, got empty error and transcript %q", m.transcript)
		}
	}
}

// TestProviderCommand verifies /provider shows the current provider.
// The provider type is set at startup via env var and cannot be changed
// at runtime (requires extension restart with a different API endpoint).
func TestProviderCommand(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", testContext(), nil, "dark", "", "bell")

	// /provider with no args shows current provider.
	updated, _ := m.handleCommand("/provider")
	m = updated.(model)
	if !strings.Contains(m.transcript, "provider: ollama") {
		t.Fatalf("transcript should show current provider, got: %q", m.transcript)
	}
}

// TestModelViaExtension verifies /model <name> routes through the
// extension CommandHandler, sets the model via Provider.SetModel,
// and returns CmdAction actions that update TUI state.
func TestModelViaExtension(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", testContext(), nil, "dark", "", "bell")

	updated, _ := m.handleCommand("/model qwen2.5")
	m = updated.(model)
	if m.modelName != "qwen2.5" {
		t.Fatalf("expected model qwen2.5 via CmdAction, got %q", m.modelName)
	}
	if !strings.Contains(m.transcript, "switched to qwen2.5") {
		t.Fatalf("expected confirmation text, got %q", m.transcript)
	}
}

// TestCompactViaExtension verifies /compact routes through the
// extension CommandHandler and returns a run_compact CmdAction.
func TestCompactViaExtension(t *testing.T) {
	ctx := testContext()
	// Also mount the compactor plugin so /compact is registered.
	comp := &compactor.Plugin{}
	comp.Mount(ctx)

	// Compaction config is needed for the TUI to run the compaction.
	compCfg := &agent.CompactionConfig{
		Threshold:     0.5,
		KeepFirst:     2,
		KeepLast:      10,
		ReserveTokens: 16384,
	}

	m := newChatModel("ollama", "llama3", compCfg, "", nil, "", ctx, nil, "dark", "", "bell")

	// Add enough history for compaction to work (needs >= 3 messages).
	m.history = append(m.history, ai.NewUser("hello"))
	m.history = append(m.history, ai.NewAssistant("hi there"))
	m.history = append(m.history, ai.NewUser("how are you?"))

	// /compact should route to extension command handler.
	// It returns run_compact action — the TUI handles the actual
	// compaction.
	updated, _ := m.handleCommand("/compact")
	m = updated.(model)
	if m.err != "" {
		t.Fatalf("expected no error from /compact, got %q", m.err)
	}
}

// TestDynamicHelpWithExtensions verifies /help includes extension
// commands when extensions are loaded.
func TestDynamicHelpWithExtensions(t *testing.T) {
	ctx := testContext()
	// testContext mounts the provider plugin, which registers
	// /model and /provider commands.
	m := newChatModel("ollama", "llama3", nil, "", nil, "", ctx, nil, "dark", "", "bell")
	help := m.renderHelp()
	plain := ansiStrip(help)

	// Built-in commands should still be present.
	for _, cmd := range []string{"/exit", "/new", "/sessions", "/help"} {
		if !strings.Contains(plain, cmd) {
			t.Fatalf("help text missing built-in %q", cmd)
		}
	}

	// Extension commands should appear dynamically.
	for _, cmd := range []string{"/model", "/provider"} {
		if !strings.Contains(plain, cmd) {
			t.Fatalf("help text missing extension command %q", cmd)
		}
	}
}

// TestHelpWithoutExtensions verifies /help works with no extensions
// and only shows built-in commands.
func TestHelpWithoutExtensions(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	help := m.renderHelp()
	plain := ansiStrip(help)

	// Built-in commands should be present.
	if !strings.Contains(plain, "/exit") {
		t.Fatal("help text missing /exit")
	}

	// Extension commands should NOT appear (no extensions loaded).
	// Check for "/model " (with space) to avoid matching "/models".
	if strings.Contains(plain, "/model ") {
		t.Fatal("help text should not contain /model with no extensions")
	}
	if strings.Contains(plain, "/provider") {
		t.Fatal("help text should not contain /provider with no extensions")
	}
	if strings.Contains(plain, "/compact") {
		t.Fatal("help text should not contain /compact with no extensions")
	}
}

// TestRenderAssistant renders markdown through glamour: bold becomes
// ANSI-styled output (contains escape sequences), code blocks are preserved,
// and plain text still appears verbatim.
func TestRenderAssistant(t *testing.T) {
	out := renderAssistant("**bold** `code`", 80, "dark")
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ANSI escape codes in rendered markdown, got %q", out)
	}
	if !strings.Contains(out, "bold") {
		t.Fatalf("expected bold text preserved, got %q", out)
	}
	if !strings.Contains(out, "code") {
		t.Fatalf("expected inline code preserved, got %q", out)
	}

	// Fallback: a zero width is normalized to 80, not a panic.
	if out := renderAssistant("plain", 0, "dark"); !strings.Contains(out, "plain") {
		t.Fatalf("plain text missing at zero width: %q", out)
	}
}

// TestRenderTranscriptRendersAssistant verifies the resume path routes
// Assistant content through glamour: a markdown message yields ANSI-styled
// output (escape codes present) with the text preserved, while a User
// message stays plain-styled.
func TestRenderTranscriptRendersAssistant(t *testing.T) {
	messages := []ai.Message{
		ai.NewUser("hi"),
		ai.NewAssistant("**bold** `code`"),
	}
	out := renderTranscript(messages, 80, themes["dark"])

	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ANSI escape codes in rendered transcript, got %q", out)
	}
	plain := ansiStrip(out)
	if !strings.Contains(plain, "bold") {
		t.Fatalf("expected bold text preserved, got %q", plain)
	}
	if !strings.Contains(plain, "code") {
		t.Fatalf("expected inline code preserved, got %q", plain)
	}
	if !strings.Contains(plain, "hi") {
		t.Fatalf("expected user content preserved, got %q", plain)
	}
}

// TestNewSessionIDCryptoRand verifies session IDs are generated from
// crypto/rand and are the expected hex length.
func TestNewSessionIDCryptoRand(t *testing.T) {
	a, err := newSessionID()
	if err != nil {
		t.Fatalf("newSessionID: %v", err)
	}
	b, err := newSessionID()
	if err != nil {
		t.Fatalf("newSessionID: %v", err)
	}
	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("expected 32-char hex IDs, got %q and %q", a, b)
	}
	if a == b {
		t.Fatalf("two session IDs collided: %q", a)
	}
}

// TestSubmitPersistsMessages verifies that a submit against a store
// auto-creates a session and persists the user message, and that an
// AgentEnd persists the assistant response.
func TestSubmitPersistsMessages(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	m.textarea.SetValue("hello")

	// Simulate a completed prior run so submit creates a fresh channel and
	// the goroutine's AgentStart lands synchronously.
	ch := make(chan agent.Event, 64)
	m.events = ch
	close(ch)
	updated, _ := m.submit()
	m = updated.(model)

	if m.sessionID == "" {
		t.Fatal("submit() did not create a session ID")
	}
	if m.storeErr != "" {
		t.Fatalf("store error: %s", m.storeErr)
	}

	// Fold an assistant response.
	m.handleEvent(agent.StreamEvent{Event: ai.ResponseChunk{Content: "ok"}})
	m.handleEvent(agent.AssistantMessageEvent{Type: "assistant_message", Message: ai.NewAssistant("ok")})
	m.handleEvent(agent.AgentEnd{Type: "agent_end", FinishReason: "stop"})

	sessions, err := s.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	msgs, err := s.GetMessages(context.Background(), m.sessionID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 persisted messages, got %d", len(msgs))
	}
	u, ok := msgs[0].(ai.User)
	if !ok || u.Content != "hello" {
		t.Fatalf("first message = %#v, want user 'hello'", msgs[0])
	}
	if _, ok := msgs[1].(ai.Assistant); !ok {
		t.Fatalf("second message = %T, want ai.Assistant", msgs[1])
	}
}

// TestClearStartsFreshSession verifies /new wipes in-memory history and
// detaches from the current session so the next message starts a new one.
func TestClearStartsFreshSession(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	m.sessionID = "sess1"
	m.history = append(m.history, ai.NewUser("hi"))
	m.transcript = "old text"

	updated, _ := m.handleCommand("/new")
	m = updated.(model)

	if len(m.history) != 0 || m.transcript != "" {
		t.Fatalf("clear failed: history=%d transcript=%q", len(m.history), m.transcript)
	}
	if m.sessionID != "" {
		t.Fatalf("clear kept session: %q", m.sessionID)
	}
	if m.ephemeral {
		t.Fatal("/new without --ephemeral must not enter ephemeral mode")
	}
}

// TestSessionsListsAndResumeLoads verifies /sessions renders persisted
// sessions with message counts and /resume loads history and continues.
func TestSessionsListsAndResumeLoads(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := s.CreateSession(ctx, "abc123", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.AppendMessage(ctx, "abc123", ai.NewUser("first")); err != nil {
		t.Fatalf("append user: %v", err)
	}
	if err := s.AppendMessage(ctx, "abc123", ai.NewAssistant("reply")); err != nil {
		t.Fatalf("append assistant: %v", err)
	}

	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")

	updated, _ := m.handleCommand("/sessions")
	m = updated.(model)
	if !strings.Contains(m.transcript, "abc123") {
		t.Fatalf("/sessions missing session id: %q", m.transcript)
	}
	if !strings.Contains(m.transcript, "MSGS") {
		t.Fatalf("/sessions missing table header: %q", m.transcript)
	}

	updated, _ = m.handleCommand("/resume abc123")
	m = updated.(model)
	if m.sessionID != "abc123" {
		t.Fatalf("resume session = %q, want abc123", m.sessionID)
	}
	if len(m.history) != 2 {
		t.Fatalf("resume history len = %d, want 2", len(m.history))
	}
	if !strings.Contains(m.transcript, "first") || !strings.Contains(m.transcript, "reply") {
		t.Fatalf("resume transcript missing history: %q", m.transcript)
	}
}

// TestResumeUnknownSession reports an error for a missing session.
func TestResumeUnknownSession(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	updated, _ := m.handleCommand("/resume nope")
	m = updated.(model)
	if m.err == "" {
		t.Fatal("expected error for unknown session")
	}
}

// TestBranchCommand verifies /branch creates a child session, inherits the
// parent's history, and switches the active session to the branch.
func TestBranchCommand(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := s.CreateSession(ctx, "parent", "", ""); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if err := s.AppendMessage(ctx, "parent", ai.NewUser("root msg")); err != nil {
		t.Fatalf("append parent: %v", err)
	}

	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	m.sessionID = "parent"

	updated, _ := m.handleCommand("/branch")
	m = updated.(model)

	// A new session was created and inherited the parent history.
	if m.sessionID == "" || m.sessionID == "parent" {
		t.Fatalf("branch session = %q, want a new id", m.sessionID)
	}
	sess, err := s.GetSession(ctx, m.sessionID)
	if err != nil {
		t.Fatalf("get branch session: %v", err)
	}
	if sess.ParentID != "parent" {
		t.Fatalf("branch parent = %q, want parent", sess.ParentID)
	}
	if len(m.history) != 1 {
		t.Fatalf("branch history len = %d, want 1", len(m.history))
	}
	if !strings.Contains(m.transcript, "root msg") {
		t.Fatalf("branch transcript missing inherited history: %q", m.transcript)
	}

	// Branch from an explicit id.
	updated, _ = m.handleCommand("/branch parent")
	m = updated.(model)
	if m.sessionID == "parent" {
		t.Fatalf("explicit branch left session as parent")
	}
	if sess, _ := s.GetSession(ctx, m.sessionID); sess.ParentID != "parent" {
		t.Fatalf("explicit branch parent = %q, want parent", sess.ParentID)
	}

	// Branch from a missing session reports an error.
	updated, _ = m.handleCommand("/branch nope")
	m = updated.(model)
	if m.err == "" {
		t.Fatal("expected error branching from unknown session")
	}
}

// TestLabelCommand verifies /label sets and clears a session label.
func TestLabelCommand(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	m.sessionID = "s1"

	updated, _ := m.handleCommand("/label my branch")
	m = updated.(model)
	if m.storeErr != "" {
		t.Fatalf("label store error: %s", m.storeErr)
	}
	sess, err := s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Label != "my branch" {
		t.Fatalf("label = %q, want my branch", sess.Label)
	}
	if !strings.Contains(m.transcript, "[label: my branch]") {
		t.Fatalf("transcript missing label confirm: %q", m.transcript)
	}

	// No text clears the label.
	updated, _ = m.handleCommand("/label")
	m = updated.(model)
	sess, _ = s.GetSession(ctx, "s1")
	if sess.Label != "" {
		t.Fatalf("label = %q, want empty after clear", sess.Label)
	}

	// No current session reports an error.
	m2 := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	updated, _ = m2.handleCommand("/label x")
	m2 = updated.(model)
	if m2.err == "" {
		t.Fatal("expected error labeling with no current session")
	}
}

// TestTreeCommand verifies /tree renders the session tree with nesting,
// labels, and message counts.
func TestTreeCommand(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := s.CreateSession(ctx, "root", "", ""); err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := s.CreateSession(ctx, "child", "root", ""); err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := s.UpdateSession(ctx, "root", "main"); err != nil {
		t.Fatalf("set label: %v", err)
	}
	if err := s.AppendMessage(ctx, "root", ai.NewUser("hi")); err != nil {
		t.Fatalf("append root: %v", err)
	}

	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	updated, _ := m.handleCommand("/tree")
	m = updated.(model)
	if m.storeErr != "" {
		t.Fatalf("tree store error: %s", m.storeErr)
	}

	plain := ansiStrip(m.transcript)
	if !strings.Contains(plain, "root") || !strings.Contains(plain, "child") {
		t.Fatalf("tree missing session ids: %q", plain)
	}
	if !strings.Contains(plain, "main") {
		t.Fatalf("tree missing label: %q", plain)
	}
	if !strings.Contains(plain, "1") || !strings.Contains(plain, "MSGS") {
		t.Fatalf("tree missing message count: %q", plain)
	}
	// Child is indented deeper than root.
	rootIdx := strings.Index(plain, "root")
	childIdx := strings.Index(plain, "child")
	if rootIdx < 0 || childIdx <= rootIdx {
		t.Fatalf("tree order wrong: root=%d child=%d", rootIdx, childIdx)
	}
}

// TestTabComplete verifies slash-command completion: a single match
// completes the command and moves the cursor to the end, multiple matches
// highlight the selected one in the status line, and no match or
// non-command input leaves the model unchanged. Matches are computed by
// updateAutocomplete (run after every keystroke), so the test drives that
// path before pressing Tab.
func TestTabComplete(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")

	// Single match: "/exi" -> "/exit". CursorEnd() moves the cursor to the
	// end; the cursor position is private on textarea.Model, so we verify
	// it behaviorally: a char inserted after completion must land at the
	// end, not mid-command.
	m.textarea.SetValue("/exi")
	m.updateAutocomplete()
	updated, _ := m.handleTabComplete()
	m = updated.(model)
	if got := m.textarea.Value(); got != "/exit" {
		t.Fatalf("tab complete = %q, want /exit", got)
	}
	m.textarea.InsertString("X")
	if got := m.textarea.Value(); got != "/exitX" {
		t.Fatalf("cursor not at end after completion: insert gave %q, want /exitX", got)
	}
	m.textarea.SetValue("/exit")
	if m.err != "" {
		t.Fatalf("err not cleared on single match: %q", m.err)
	}

	// Multiple matches: "/" matches every known command, so the
	// autocomplete panel lists the options with the selected one highlighted.
	m.textarea.SetValue("/")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != len(knownCommands) {
		t.Fatalf("expected %d matches for /, got %d", len(knownCommands), len(m.autocompleteMatches))
	}
	panel := ansiStrip(m.autocompletePanel())
	if !strings.Contains(panel, "/new") || !strings.Contains(panel, "/sessions") {
		t.Fatalf("autocomplete panel missing options: %q", panel)
	}
	// Nothing selected initially.
	if m.autocompleteIndex != -1 {
		t.Fatalf("initial index = %d, want -1", m.autocompleteIndex)
	}
	// Down selects the first match.
	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = up.(model)
	if m.autocompleteIndex != 0 {
		t.Fatalf("down index = %d, want 0", m.autocompleteIndex)
	}

	// No match: "/zzz" leaves the input and match state unchanged.
	m.textarea.SetValue("/zzz")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != 0 {
		t.Fatalf("expected 0 matches for /zzz, got %d", len(m.autocompleteMatches))
	}
	updated, _ = m.handleTabComplete()
	m = updated.(model)
	if m.textarea.Value() != "/zzz" {
		t.Fatalf("no-match changed input to %q", m.textarea.Value())
	}
	if len(m.autocompleteMatches) != 0 {
		t.Fatalf("no-match left matches: %v", m.autocompleteMatches)
	}

	// Non-command input: "hello" does nothing.
	m.textarea.SetValue("hello")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != 0 {
		t.Fatalf("non-command produced matches: %v", m.autocompleteMatches)
	}
	updated, _ = m.handleTabComplete()
	m = updated.(model)
	if m.textarea.Value() != "hello" {
		t.Fatalf("non-command changed input to %q", m.textarea.Value())
	}
	if len(m.autocompleteMatches) != 0 {
		t.Fatalf("non-command set matches: %v", m.autocompleteMatches)
	}
	if m.autocompleteIndex != -1 {
		t.Fatalf("non-command left index %d, want -1", m.autocompleteIndex)
	}
}

// TestAutocompleteLiveFilter verifies matches are recomputed from the input
// on every update, clear when the input stops starting with "/", and that a
// single match is auto-selected for immediate Enter/Tab acceptance.
func TestAutocompleteLiveFilter(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", testContext(), nil, "dark", "", "bell")

	// "/" matches every known command + extension commands, nothing selected.
	m.textarea.SetValue("/")
	m.updateAutocomplete()
	// knownCommands (built-in) + extension commands (/model, /provider).
	if len(m.autocompleteMatches) < len(knownCommands) {
		t.Fatalf("/ matches = %d, want >= %d", len(m.autocompleteMatches), len(knownCommands))
	}
	if m.autocompleteIndex != -1 {
		t.Fatalf("/ index = %d, want -1 (no single match)", m.autocompleteIndex)
	}

	// "/p" narrows to a single match and auto-selects it.
	m.textarea.SetValue("/p")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != 1 || m.autocompleteMatches[0] != "/provider" {
		t.Fatalf("/p matches = %v, want [/provider]", m.autocompleteMatches)
	}
	if m.autocompleteIndex != 0 {
		t.Fatalf("/p index = %d, want 0 (auto-selected single match)", m.autocompleteIndex)
	}

	// "/model" narrows to /model and /models, auto-selects the first.
	m.textarea.SetValue("/model")
	m.updateAutocomplete()
	want := []string{"/models", "/model"}
	if len(m.autocompleteMatches) != 2 || m.autocompleteMatches[0] != want[0] || m.autocompleteMatches[1] != want[1] {
		t.Fatalf("/model matches = %v, want %v", m.autocompleteMatches, want)
	}
	if m.autocompleteIndex != 0 {
		t.Fatalf("/model index = %d, want 0 (auto-selected)", m.autocompleteIndex)
	}

	// Typing a non-slash clears matches and resets the highlight.
	m.textarea.SetValue("hello")
	m.updateAutocomplete()
	if m.autocompleteMatches != nil {
		t.Fatalf("non-slash left matches: %v", m.autocompleteMatches)
	}
	if m.autocompleteIndex != -1 {
		t.Fatalf("non-slash left index %d, want -1", m.autocompleteIndex)
	}
}

// TestAutocompleteArrows verifies Up/Down cycle the selection across
// matches, wrapping at both ends.
func TestAutocompleteArrows(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.textarea.SetValue("/")
	m.updateAutocomplete()
	if m.autocompleteIndex != -1 {
		t.Fatalf("start index = %d, want -1", m.autocompleteIndex)
	}
	n := len(m.autocompleteMatches)

	// Down from none selects the first.
	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = up.(model)
	if m.autocompleteIndex != 0 {
		t.Fatalf("down from none index = %d, want 0", m.autocompleteIndex)
	}

	// Down wraps to the first after the last.
	for i := 0; i < n; i++ {
		up, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = up.(model)
	}
	if m.autocompleteIndex != 0 {
		t.Fatalf("down wrap index = %d, want 0", m.autocompleteIndex)
	}

	// Up from the first wraps to the last.
	up, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = up.(model)
	if m.autocompleteIndex != n-1 {
		t.Fatalf("up wrap index = %d, want %d", m.autocompleteIndex, n-1)
	}
}

// TestAutocompleteAccept verifies Enter accepts the selected match and that
// Enter on a fully-typed command falls through to submit.
func TestAutocompleteAccept(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")

	// "/mo" is a single match auto-selected; a Down is not needed. Use "/"
	// (multiple matches) to test arrow-driven selection instead: select
	// /provider via Down, then Enter accepts it.
	m.textarea.SetValue("/")
	m.updateAutocomplete()
	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown}) // select first match
	m = up.(model)
	// Find /theme and advance to it. Bound the loop so a missing
	// command fails fast instead of hanging forever.
	for i := 0; i < len(m.autocompleteMatches); i++ {
		if m.autocompleteMatches[m.autocompleteIndex] == "/theme" {
			break
		}
		up, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = up.(model)
	}
	if m.autocompleteMatches[m.autocompleteIndex] != "/theme" {
		t.Fatalf("did not find /theme in matches: %v", m.autocompleteMatches)
	}
	up, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = up.(model)
	if m.textarea.Value() != "/theme" {
		t.Fatalf("enter accepted %q, want /theme", m.textarea.Value())
	}
	if m.autocompleteMatches != nil || m.autocompleteIndex != -1 {
		t.Fatalf("match state not cleared after accept: %v idx=%d", m.autocompleteMatches, m.autocompleteIndex)
	}

	// Enter on a single-match auto-selected command completes it.
	m.textarea.SetValue("/exi")
	m.updateAutocomplete()
	up, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = up.(model)
	if m.textarea.Value() != "/exit" {
		t.Fatalf("enter single match = %q, want /exit", m.textarea.Value())
	}

	// Enter on a fully-typed command is unchanged, so it falls through to
	// submit. submit() trims, so we can't observe a no-op; instead verify
	// the autocomplete is inactive (no auto-selected match) and the input
	// is submitted as a command.
	m.textarea.SetValue("/exit")
	m.updateAutocomplete()
	if m.autocompleteIndex != 0 {
		t.Fatalf("/exit should be single auto-selected, got index %d", m.autocompleteIndex)
	}
	// Accepting an exact match returns nil (no input change) and clears
	// the match state, so a subsequent Enter submits the command.
	if cmd := m.acceptMatch(); cmd != nil {
		t.Fatalf("acceptMatch on exact match returned a cmd, want nil")
	}
	if m.autocompleteMatches != nil || m.autocompleteIndex != -1 {
		t.Fatalf("acceptMatch on exact match left state: %v idx=%d", m.autocompleteMatches, m.autocompleteIndex)
	}
}

// TestAutocompleteMidSentence verifies that typing / after a space
// triggers autocomplete mid-sentence, and accepting a match splices
// the completion while preserving the text before the slash.
func TestAutocompleteMidSentence(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")

	// "go ahead and /exi" should trigger autocomplete on /exi.
	m.textarea.SetValue("go ahead and /exi")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != 1 {
		t.Fatalf("expected 1 match for /exi, got %v", m.autocompleteMatches)
	}
	if m.autocompleteMatches[0] != "/exit" {
		t.Fatalf("match = %q, want /exit", m.autocompleteMatches[0])
	}
	if m.autocompleteSlashPos != 13 {
		t.Fatalf("slashPos = %d, want 13", m.autocompleteSlashPos)
	}

	// Accept the match: should splice /exit after "go ahead and ".
	m.acceptMatch()
	if m.textarea.Value() != "go ahead and /exit" {
		t.Fatalf("value = %q, want 'go ahead and /exit'", m.textarea.Value())
	}
}

// TestAutocompleteMidSentenceNoSpaceBeforeSlash verifies that / not
// preceded by a space (e.g. in a URL) does not trigger autocomplete.
func TestAutocompleteMidSentenceNoSpaceBeforeSlash(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")

	m.textarea.SetValue("check http://example.com")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != 0 {
		t.Fatalf("expected 0 matches for URL, got %v", m.autocompleteMatches)
	}
	if m.autocompleteSlashPos != -1 {
		t.Fatalf("slashPos = %d, want -1", m.autocompleteSlashPos)
	}
}

// TestEscapeCancelsRun verifies Escape during a busy run calls cancel.
func TestEscapeCancelsRun(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.busy = true
	called := false
	m.cancel = func() { called = true }

	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = up.(model)
	if !called {
		t.Fatal("Escape did not call cancel during busy run")
	}
}

// TestEnterSubmits verifies Enter on non-empty input triggers submit.
func TestEnterSubmits(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.textarea.SetValue("hello")

	up, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = up.(model)
	if cmd == nil {
		t.Fatal("Enter on non-empty input did not return a command")
	}
	if !m.busy {
		t.Fatal("Enter did not set busy")
	}
}

// TestPgUpPgDnScrolls verifies PgUp/PgDn are forwarded to the viewport.
func TestPgUpPgDnScrolls(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")

	// PgUp should not panic and should return a model.
	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if _, ok := up.(model); !ok {
		t.Fatal("PgUp did not return a model")
	}

	// PgDown should not panic and should return a model.
	up, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if _, ok := up.(model); !ok {
		t.Fatal("PgDown did not return a model")
	}
}

// TestUpDownHistory verifies Up/Down recall prompt history.
func TestUpDownHistory(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.promptHistory = []string{"first", "second"}

	// Up from empty input recalls the most recent prompt.
	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = up.(model)
	if m.textarea.Value() != "second" {
		t.Fatalf("Up recall = %q, want second", m.textarea.Value())
	}

	// Down returns to empty.
	up, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = up.(model)
	if m.textarea.Value() != "" {
		t.Fatalf("Down to empty = %q, want empty", m.textarea.Value())
	}
}

// TestSegmentOrder verifies segments render in the order they were emitted.
func TestSegmentOrder(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.thinkingLevel = "on"
	m.showThinking = true

	m.handleEvent(agent.StreamEvent{Event: ai.ThinkingChunk{Content: "plan"}})
	m.handleEvent(agent.StreamEvent{Event: ai.ToolCallEvent{ToolCall: ai.ToolCall{Name: "shell.run"}}})
	m.handleEvent(agent.StreamEvent{Event: ai.ResponseChunk{Content: "done"}})
	m.handleEvent(agent.AgentEnd{Type: "agent_end", FinishReason: "stop"})

	plain := ansiStrip(m.transcript)
	thinkIdx := strings.Index(plain, "[thinking]")
	toolIdx := strings.Index(plain, "[tool: shell.run]")
	respIdx := strings.Index(plain, "done")

	if thinkIdx < 0 || toolIdx < 0 || respIdx < 0 {
		t.Fatalf("missing segments: think=%v tool=%v resp=%v", thinkIdx >= 0, toolIdx >= 0, respIdx >= 0)
	}
	if thinkIdx > toolIdx || toolIdx > respIdx {
		t.Fatalf("segment order wrong: think=%d tool=%d resp=%d", thinkIdx, toolIdx, respIdx)
	}
}

// TestStatusLineFormat verifies the status line contains expected fields.
func TestStatusLineFormat(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.sessionID = "abc123"

	line := ansiStrip(m.statusLine())
	if !strings.Contains(line, "Ω") {
		t.Fatalf("status line missing Ω: %q", line)
	}
	if !strings.Contains(line, "idle") {
		t.Fatalf("status line missing idle: %q", line)
	}
	if !strings.Contains(line, "ollama/llama3") {
		t.Fatalf("status line missing provider/model: %q", line)
	}
	if !strings.Contains(line, "tokens:") {
		t.Fatalf("status line missing tokens: %q", line)
	}
	if !strings.Contains(line, "abc123") {
		t.Fatalf("status line missing session: %q", line)
	}
}

// TestHelpText verifies help text contains all commands.
func TestHelpText(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	help := m.renderHelp()
	plain := ansiStrip(help)
	for _, cmd := range []string{"/exit", "/new", "/sessions", "/resume", "/help"} {
		if !strings.Contains(plain, cmd) {
			t.Fatalf("help text missing %q", cmd)
		}
	}
}

func TestEphemeralNewClearsSession(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.sessionID = "abc123"
	m.ephemeral = false

	updated, _ := m.handleCommand("/new --ephemeral")
	m = updated.(model)
	if !m.ephemeral {
		t.Fatal("expected ephemeral mode after /new --ephemeral")
	}
	if m.sessionID != "" {
		t.Fatalf("session id = %q, want empty in ephemeral mode", m.sessionID)
	}
}

func TestEphemeralSkipsStoreOnSubmit(t *testing.T) {
	// A store is present, but ephemeral mode must not create or persist.
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	m.ephemeral = true
	m.textarea.SetValue("hello ephemeral")
	updated, _ := m.submit()
	m = updated.(model)

	if m.sessionID != "" {
		t.Fatalf("session id = %q, want empty (ephemeral must not create sessions)", m.sessionID)
	}
	sessions, _ := s.ListSessions(context.Background())
	if len(sessions) != 0 {
		t.Fatalf("store has %d sessions, want 0", len(sessions))
	}
}

func TestEphemeralBlocksStoreCommands(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.ephemeral = true

	for _, cmd := range []string{"/sessions", "/resume abc", "/branch", "/label x", "/tree"} {
		updated, _ := m.handleCommand(cmd)
		m = updated.(model)
		if m.err != "no sessions in ephemeral mode" {
			t.Fatalf("%s in ephemeral mode: err = %q, want %q", cmd, m.err, "no sessions in ephemeral mode")
		}
		m.err = ""
	}
}

func TestEphemeralStatusLine(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.ephemeral = true
	line := ansiStrip(m.statusLine())
	if !strings.Contains(line, "ephemeral") {
		t.Fatalf("status line = %q, want ephemeral", line)
	}
}

// TestStatusLineTrust verifies the trust indicator appears only when a
// trust state is set, and uses the right label.
func TestStatusLineTrust(t *testing.T) {
	// No trust state: no indicator.
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	if line := ansiStrip(m.statusLine()); strings.Contains(line, "trusted") || strings.Contains(line, "untrusted") {
		t.Fatalf("status line = %q, want no trust indicator", line)
	}

	// Trusted.
	m = newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "trusted", "bell")
	if line := ansiStrip(m.statusLine()); !strings.Contains(line, "trusted") {
		t.Fatalf("status line = %q, want trusted", line)
	}

	// Untrusted.
	m = newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "untrusted", "bell")
	if line := ansiStrip(m.statusLine()); !strings.Contains(line, "untrusted") {
		t.Fatalf("status line = %q, want untrusted", line)
	}
}

// TestAutocompleteArgLevel verifies the second-level autocomplete: when
// the input equals (or starts) a command with enum options, the matches
// are the full command+option strings, sorted.
func TestAutocompleteArgLevel(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")

	// Bare /thinking offers its options as full strings, in map order.
	m.textarea.SetValue("/thinking")
	m.updateAutocomplete()
	want := []string{"/thinking none", "/thinking off", "/thinking on", "/thinking minimal", "/thinking low", "/thinking medium", "/thinking high", "/thinking extra high", "/thinking max", "/thinking ultra"}
	if len(m.autocompleteMatches) != len(want) {
		t.Fatalf("/thinking matches = %v, want %v", m.autocompleteMatches, want)
	}
	for i, w := range want {
		if m.autocompleteMatches[i] != w {
			t.Fatalf("/thinking match[%d] = %q, want %q", i, m.autocompleteMatches[i], w)
		}
	}

	// /tools offers on/off/auto.
	m.textarea.SetValue("/tools")
	m.updateAutocomplete()
	want = []string{"/tools on", "/tools off", "/tools auto", "/tools list"}
	if len(m.autocompleteMatches) != 4 {
		t.Fatalf("/tools matches = %v, want %v", m.autocompleteMatches, want)
	}
	for i, w := range want {
		if m.autocompleteMatches[i] != w {
			t.Fatalf("/tools match[%d] = %q, want %q", i, m.autocompleteMatches[i], w)
		}
	}

	// Partial option filters: "/tools a" -> "/tools auto" only.
	m.textarea.SetValue("/tools a")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != 1 || m.autocompleteMatches[0] != "/tools auto" {
		t.Fatalf("/tools a matches = %v, want [/tools auto]", m.autocompleteMatches)
	}

	// /new offers --ephemeral.
	m.textarea.SetValue("/new")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != 1 || m.autocompleteMatches[0] != "/new --ephemeral" {
		t.Fatalf("/new matches = %v, want [/new --ephemeral]", m.autocompleteMatches)
	}

	// Unknown option matches nothing.
	m.textarea.SetValue("/thinking x")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != 0 {
		t.Fatalf("/thinking x matches = %v, want none", m.autocompleteMatches)
	}

	// A command without options keeps first-level matching.
	// /copy is a built-in with no entry in commandOptions.
	m.textarea.SetValue("/co")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != 1 || m.autocompleteMatches[0] != "/copy" {
		t.Fatalf("/co matches = %v, want [/copy]", m.autocompleteMatches)
	}
}

// TestAutocompleteSemanticOrder verifies matches follow the semantic
// command order: /new first, /help last.
func TestAutocompleteSemanticOrder(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.textarea.SetValue("/")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != len(knownCommands) {
		t.Fatalf("expected %d matches for /, got %d", len(knownCommands), len(m.autocompleteMatches))
	}
	if m.autocompleteMatches[0] != "/new" {
		t.Fatalf("first match = %q, want /new", m.autocompleteMatches[0])
	}
	if m.autocompleteMatches[len(m.autocompleteMatches)-1] != "/help" {
		t.Fatalf("last match = %q, want /help", m.autocompleteMatches[len(m.autocompleteMatches)-1])
	}
}

// TestAutocompletePanelVertical verifies the panel renders matches as
// newline-separated rows (vertical, not horizontal).
func TestAutocompletePanelVertical(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.textarea.SetValue("/")
	m.updateAutocomplete()
	panel := ansiStrip(m.autocompletePanel())
	if panel == "" {
		t.Fatal("expected non-empty panel for /")
	}
	// Border adds a top and bottom row; the rows between contain matches.
	lines := strings.Split(panel, "\n")
	if len(lines) < 4 {
		t.Fatalf("panel has %d lines, want a bordered vertical list", len(lines))
	}
	if !strings.Contains(lines[1], "/new") {
		t.Fatalf("first match row = %q, want /new", lines[1])
	}
}

// TestAutocompleteHeightMatchesPanel verifies height accounting: 0 when
// empty, rows+2 when matches exist.
func TestAutocompleteHeightMatchesPanel(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.textarea.SetValue("hello")
	m.updateAutocomplete()
	if h := m.autocompleteHeight(); h != 0 {
		t.Fatalf("height with no matches = %d, want 0", h)
	}
	m.textarea.SetValue("/")
	m.updateAutocomplete()
	h := m.autocompleteHeight()
	if h != maxAutocompleteRows+3 {
		t.Fatalf("height with matches = %d, want %d (capped + ... row)", h, maxAutocompleteRows+3)
	}
}

// TestAutocompleteWindowScrolls verifies the dropup window follows the
// selection: pressing Down past the last visible row advances the offset
// so the selected command stays on screen.
func TestAutocompleteWindowScrolls(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.textarea.SetValue("/")
	m.updateAutocomplete()

	// Press Down repeatedly to walk through the matches.
	for i := 0; i < len(knownCommands); i++ {
		up, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = up.(model)
	}
	// After cycling through all matches, the last one (/help) is selected.
	if m.autocompleteIndex != len(knownCommands)-1 {
		t.Fatalf("index after %d downs = %d, want %d", len(knownCommands), m.autocompleteIndex, len(knownCommands)-1)
	}
	// The window must have scrolled: the selected row is visible.
	panel := ansiStrip(m.autocompletePanel())
	if !strings.Contains(panel, "/help") {
		t.Fatalf("panel does not show selected /help after scroll: %q", panel)
	}
	// And the window start moved past the first command.
	if m.autocompleteOffset <= 0 {
		t.Fatalf("offset = %d, want > 0 after scrolling to the end", m.autocompleteOffset)
	}
}

// TestAutocompleteWhileBusy verifies the autocomplete recomputes while a
// run is in flight (the busy guard previously swallowed typing without
// recomputing matches).
func TestAutocompleteWhileBusy(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.busy = true
	m.textarea.SetValue("/")
	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = up.(model)
	m.updateAutocomplete()
	if len(m.autocompleteMatches) == 0 {
		t.Fatal("no autocomplete matches while busy")
	}
}

// TestInlineSkillInvocation verifies a "/name" token inside a normal
// message injects the matching skill's content as a system message while
// leaving the user text unchanged.
func TestInlineSkillInvocation(t *testing.T) {
	skill := agent.Skill{Name: "learn-skill", Content: "skill body here", Description: "test"}
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, []agent.Skill{skill}, "dark", "", "bell")
	m.textarea.SetValue("go ahead and /learn-skill from my notes")
	updated, _ := m.submit()
	m = updated.(model)

	// The user message is persisted intact.
	foundUser := false
	foundSystem := false
	for _, msg := range m.history {
		if u, ok := msg.(ai.User); ok && u.Content == "go ahead and /learn-skill from my notes" {
			foundUser = true
		}
		if s, ok := msg.(ai.System); ok && s.Content == "skill body here" {
			foundSystem = true
		}
	}
	if !foundUser {
		t.Fatal("user message not preserved intact")
	}
	if !foundSystem {
		t.Fatal("skill content not injected as system message")
	}
	if !strings.Contains(m.transcript, "[skill: learn-skill]") {
		t.Fatalf("transcript missing skill marker: %q", m.transcript)
	}
}

// TestInlineSkillUnknownTokenIgnored verifies non-skill "/tokens" (URLs,
// paths) pass through without injection.
func TestInlineSkillUnknownTokenIgnored(t *testing.T) {
	skill := agent.Skill{Name: "learn-skill", Content: "skill body here", Description: "test"}
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, []agent.Skill{skill}, "dark", "", "bell")
	m.textarea.SetValue("check /path/to/x please")
	updated, _ := m.submit()
	m = updated.(model)

	for _, msg := range m.history {
		if s, ok := msg.(ai.System); ok && s.Content == "skill body here" {
			t.Fatal("unknown token injected a skill")
		}
	}
	if len(m.history) != 1 {
		t.Fatalf("history = %d messages, want 1 (user only)", len(m.history))
	}
}

// TestRenderTranscriptToolResults verifies the resume path renders the
// full conversation: thinking, tool calls, tool results, and final
// content, in order, with no blank blocks.
func TestRenderTranscriptToolResults(t *testing.T) {
	thinking := "let me check the time"
	assistant := ai.NewAssistant("")
	assistant.Thinking = &thinking
	assistant.ToolCalls = []ai.ToolCall{{ID: "c1", Name: "shell.run", Arguments: map[string]any{"command": "date"}}}
	messages := []ai.Message{
		ai.NewUser("whats the time rn?"),
		assistant,
		ai.NewToolResult("11:12 AM", "c1", false),
		ai.NewAssistant("It's 11:12 AM."),
	}
	out := ansiStrip(renderTranscript(messages, 80, themes["dark"]))

	if !strings.Contains(out, "whats the time rn?") {
		t.Fatalf("missing user message: %q", out)
	}
	if !strings.Contains(out, "[thinking]") || !strings.Contains(out, "let me check the time") {
		t.Fatalf("missing thinking block: %q", out)
	}
	if !strings.Contains(out, "[tool: shell.run]") || !strings.Contains(out, "command: date") {
		t.Fatalf("missing tool call: %q", out)
	}
	if !strings.Contains(out, "[tool result]") || !strings.Contains(out, "11:12 AM") {
		t.Fatalf("missing tool result: %q", out)
	}
	if !strings.Contains(out, "It's 11:12 AM.") {
		t.Fatalf("missing final content: %q", out)
	}
	// Order check: thinking before tool before result before content.
	idx := func(s string) int { return strings.Index(out, s) }
	if !(idx("[thinking]") < idx("[tool: shell.run]") && idx("[tool: shell.run]") < idx("[tool result]") && idx("[tool result]") < idx("It's 11:12 AM.")) {
		t.Fatalf("blocks out of order: %q", out)
	}
}

// TestRenderTranscriptCompacted verifies compaction summaries render
// while other system messages are skipped.
func TestRenderTranscriptCompacted(t *testing.T) {
	messages := []ai.Message{
		ai.NewSystem("[compacted: user asked about the time]"),
		ai.NewSystem("injected prompt - never persisted, skip"),
		ai.NewUser("continue"),
	}
	out := ansiStrip(renderTranscript(messages, 80, themes["dark"]))
	if !strings.Contains(out, "[compacted:") {
		t.Fatalf("missing compaction summary: %q", out)
	}
	if strings.Contains(out, "injected prompt") {
		t.Fatalf("plain system message rendered: %q", out)
	}
}

// TestWindowTitle verifies the terminal title format.
func TestWindowTitle(t *testing.T) {
	if got := windowTitle("idle", "glm-5.2"); got != "Ω | idle | glm-5.2" {
		t.Fatalf("idle title = %q, want %q", got, "Ω | idle | glm-5.2")
	}
	if got := windowTitle("running", "glm-5.2"); got != "Ω | running | glm-5.2" {
		t.Fatalf("running title = %q, want %q", got, "Ω | running | glm-5.2")
	}
}

// TestSessionsDeleteCommand verifies /sessions delete removes a session
// by #, id, or label, and resets state when the active session is
// deleted.
func TestSessionsDeleteCommand(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := s.CreateSession(ctx, "abc123", "", "old name"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.AppendMessage(ctx, "abc123", ai.NewUser("hi")); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Delete by label (the /sessions list populates the resolve cache).
	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	m.sessionID = "abc123"
	listed, _ := m.handleCommand("/sessions")
	m = listed.(model)
	updated, _ := m.handleCommand("/sessions delete old name")
	m = updated.(model)
	if m.storeErr != "" {
		t.Fatalf("delete store error: %s", m.storeErr)
	}
	if _, err := s.GetSession(ctx, "abc123"); err == nil {
		t.Fatal("session still exists after delete")
	}
	// Active session deleted: state reset like /new.
	if m.sessionID != "" || m.history != nil {
		t.Fatalf("active session not reset: id=%q history=%v", m.sessionID, m.history)
	}
	if !strings.Contains(m.transcript, "deleted") {
		t.Fatalf("transcript missing deleted marker: %q", m.transcript)
	}
}

// TestSessionsDeleteUsage verifies missing or unknown targets error.
func TestSessionsDeleteUsage(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")

	updated, _ := m.handleCommand("/sessions delete")
	m = updated.(model)
	if m.err == "" {
		t.Fatal("expected usage error for /sessions delete without arg")
	}
	updated, _ = m.handleCommand("/sessions delete nope")
	m = updated.(model)
	if m.err == "" {
		t.Fatal("expected not-found error for unknown session")
	}
}

// TestAutoNameAppliedWhenSessionMatches verifies the auto-name result
// updates the status bar label when the session still matches.
func TestAutoNameAppliedWhenSessionMatches(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.sessionID = "abc123"
	up, _ := m.Update(autoNameMsg{sessionID: "abc123", gen: 0, label: "Checking the time"})
	m = up.(model)
	if m.sessionLabel != "Checking the time" {
		t.Fatalf("sessionLabel = %q, want %q", m.sessionLabel, "Checking the time")
	}
	if !m.autoNamed {
		t.Fatal("autoNamed not set")
	}
}

// TestAutoNameIgnoredOnSessionMismatch verifies a stale auto-name result
// (session switched mid-flight) does not overwrite the current label.
func TestAutoNameIgnoredOnSessionMismatch(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.sessionID = "abc123"
	m.sessionLabel = "Keep me"
	up, _ := m.Update(autoNameMsg{sessionID: "other", gen: 0, label: "Stale title"})
	m = up.(model)
	if m.sessionLabel != "Keep me" {
		t.Fatalf("sessionLabel = %q, want %q (stale result must be ignored)", m.sessionLabel, "Keep me")
	}
}

// TestAutoNameIgnoredAfterNew verifies a stale auto-name result from
// before /new does not re-apply the old title or block re-naming.
func TestAutoNameIgnoredAfterNew(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.sessionID = "abc123"
	m.sessionLabel = "Old title"
	m.autoNamed = true
	// /new bumps the generation and detaches the session.
	up, _ := m.handleCommand("/new")
	m = up.(model)
	if m.sessionLabel != "" {
		t.Fatalf("sessionLabel = %q after /new, want empty", m.sessionLabel)
	}
	// A stale result (old gen, old session) must be dropped entirely.
	up, _ = m.Update(autoNameMsg{sessionID: "abc123", gen: 0, label: "Old title"})
	m = up.(model)
	if m.sessionLabel != "" {
		t.Fatalf("stale auto-name re-applied label: %q", m.sessionLabel)
	}
	if m.autoNamed {
		t.Fatal("stale auto-name blocked re-naming (autoNamed set)")
	}
}

// TestSplashView verifies the startup splash contains the logo,
// version, model, tool count, and help hint.
func TestSplashView(t *testing.T) {
	m := newChatModel("ollama", "glm-5.2", nil, "", nil, "", nil, nil, "dark", "", "bell")
	splash := ansiStrip(m.splashView())
	for _, want := range []string{`#"""#`, "omega", "dev", "ollama/glm-5.2", "tools", "/help"} {
		if !strings.Contains(splash, want) {
			t.Fatalf("splash missing %q: %q", want, splash)
		}
	}
}

// TestSplashDisappearsAfterSubmit verifies the splash is replaced by
// the viewport once a message is submitted.
func TestSplashDisappearsAfterSubmit(t *testing.T) {
	m := newChatModel("ollama", "glm-5.2", nil, "", nil, "", nil, nil, "dark", "", "bell")
	// Fresh model shows splash.
	view := ansiStrip(m.View())
	if !strings.Contains(view, `#"""#`) {
		t.Fatal("fresh model should show splash")
	}
	// After submit, transcript is non-empty; View no longer shows splash.
	m.textarea.SetValue("hello")
	up, _ := m.submit()
	m = up.(model)
	view = ansiStrip(m.View())
	if strings.Contains(view, `#"""#`) {
		t.Fatal("splash should disappear after submit")
	}
}

// TestSplashReappearsAfterNew verifies the splash returns after /new
// clears the conversation.
func TestSplashReappearsAfterNew(t *testing.T) {
	m := newChatModel("ollama", "glm-5.2", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.transcript = "some content"
	m.history = append(m.history, ai.NewUser("hi"))
	// Not showing splash (has content).
	view := ansiStrip(m.View())
	if strings.Contains(view, `#"""#`) {
		t.Fatal("should not show splash with content")
	}
	// /new clears everything; splash returns.
	up, _ := m.handleCommand("/new")
	m = up.(model)
	view = ansiStrip(m.View())
	if !strings.Contains(view, `#"""#`) {
		t.Fatal("splash should reappear after /new")
	}
}

func TestThinkingLevelSet(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	if m.thinkingLevel != "medium" {
		t.Fatalf("default thinkingLevel = %q, want medium", m.thinkingLevel)
	}
	// Set a specific level.
	up, _ := m.handleCommand("/thinking high")
	m = up.(model)
	if m.thinkingLevel != "high" {
		t.Fatalf("thinkingLevel = %q, want high", m.thinkingLevel)
	}
	if !m.showThinking {
		t.Error("showThinking should be true for high")
	}
	// Set to none — display off.
	up, _ = m.handleCommand("/thinking none")
	m = up.(model)
	if m.thinkingLevel != "none" {
		t.Fatalf("thinkingLevel = %q, want none", m.thinkingLevel)
	}
	if m.showThinking {
		t.Error("showThinking should be false for none")
	}
	// Set to off — display off.
	up, _ = m.handleCommand("/thinking off")
	m = up.(model)
	if m.thinkingLevel != "off" {
		t.Fatalf("thinkingLevel = %q, want off", m.thinkingLevel)
	}
	if m.showThinking {
		t.Error("showThinking should be false for off")
	}
}

func TestThinkingLevelCycle(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	if m.thinkingLevel != "medium" {
		t.Fatalf("default thinkingLevel = %q, want medium", m.thinkingLevel)
	}
	// Cycle: medium -> high -> extra high -> max -> ultra -> none -> off -> on -> minimal -> low -> medium
	expected := []string{"high", "extra high", "max", "ultra", "none", "off", "on", "minimal", "low", "medium"}
	for _, want := range expected {
		up, _ := m.handleCommand("/thinking")
		m = up.(model)
		if m.thinkingLevel != want {
			t.Fatalf("cycled to %q, want %q", m.thinkingLevel, want)
		}
	}
}

func TestThinkingLevelInvalid(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	up, _ := m.handleCommand("/thinking bogus")
	m = up.(model)
	if m.thinkingLevel != "medium" {
		t.Fatalf("thinkingLevel = %q, want medium (unchanged)", m.thinkingLevel)
	}
	if m.err == "" {
		t.Error("expected error for invalid thinking level")
	}
}

func TestModelsCommand(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	up, _ := m.handleCommand("/models")
	m = up.(model)
	// Should show a table with model names. The fake provider isn't used here
	// (handleModels creates its own provider), so the result depends on
	// whether Ollama is running. Just check it doesn't crash and produces
	// output or an error.
	if m.err != "" && m.transcript == "" {
		// Error with no transcript is fine — Ollama might not be running.
		return
	}
	if !strings.Contains(m.transcript, "NAME") {
		t.Errorf("transcript should contain table header, got: %q", m.transcript)
	}
}

func TestModelSelectByNumber(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", testContext(), nil, "dark", "", "bell")
	m.modelList = []string{"alpha", "beta", "gamma"}
	up, _ := m.handleCommand("/model 2")
	m = up.(model)
	if m.modelName != "beta" {
		t.Fatalf("modelName = %q, want beta", m.modelName)
	}
	if !strings.Contains(m.transcript, "switched to beta") {
		t.Errorf("transcript should confirm model switch: %q", m.transcript)
	}
}

func TestModelSelectOutOfRange(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.modelList = []string{"alpha", "beta"}
	up, _ := m.handleCommand("/model 99")
	m = up.(model)
	if m.modelName != "llama3" {
		t.Fatalf("modelName should be unchanged, got %q", m.modelName)
	}
	if m.err == "" {
		t.Error("expected out-of-range error")
	}
}

func TestModelValidationWithCache(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", testContext(), nil, "dark", "", "bell")
	m.modelList = []string{"alpha", "beta", "gamma"}

	// Valid model accepted.
	up, _ := m.handleCommand("/model beta")
	m = up.(model)
	if m.modelName != "beta" {
		t.Fatalf("modelName = %q, want beta", m.modelName)
	}

	// Invalid model rejected.
	up, _ = m.handleCommand("/model bogus")
	m = up.(model)
	if m.modelName != "beta" {
		t.Fatalf("modelName should be unchanged, got %q", m.modelName)
	}
	if m.err == "" {
		t.Error("expected not-found error for bogus model")
	}
}

func TestModelValidationNoCache(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", testContext(), nil, "dark", "", "bell")
	// No cache — any model name accepted.
	up, _ := m.handleCommand("/model anything-goes")
	m = up.(model)
	if m.modelName != "anything-goes" {
		t.Fatalf("modelName = %q, want anything-goes", m.modelName)
	}
	if m.err != "" {
		t.Errorf("expected no error without cache, got %q", m.err)
	}
}

func TestCtrlPCyclesModels(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.modelList = []string{"alpha", "beta", "gamma"}
	m.modelName = "alpha"

	// Ctrl+P advances to the next model.
	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = up.(model)
	if m.modelName != "beta" {
		t.Fatalf("after Ctrl+P: modelName = %q, want beta", m.modelName)
	}

	// Again -> gamma.
	up, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = up.(model)
	if m.modelName != "gamma" {
		t.Fatalf("after 2nd Ctrl+P: modelName = %q, want gamma", m.modelName)
	}

	// Wrap around to alpha.
	up, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = up.(model)
	if m.modelName != "alpha" {
		t.Fatalf("after 3rd Ctrl+P: modelName = %q, want alpha (wrap)", m.modelName)
	}
}

func TestCtrlPEmptyListFetches(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	// modelList is empty; Ctrl+P should trigger a fetch (returns a cmd).
	up, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = up.(model)
	if cmd == nil {
		t.Fatal("Ctrl+P with empty modelList should return a fetch cmd")
	}
	if !strings.Contains(m.transcript, "fetching models") {
		t.Fatalf("transcript = %q, want 'fetching models'", m.transcript)
	}
}

func TestPasteInsertsText(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	// Focus the textarea so it accepts key input.
	m.textarea.Focus()
	// Simulate a bracketed paste: KeyMsg with Paste=true and some runes.
	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/some/file/path"), Paste: true})
	m = up.(model)
	if m.textarea.Value() != "/some/file/path" {
		t.Fatalf("textarea = %q, want /some/file/path", m.textarea.Value())
	}
}

// ---------------------------------------------------------------------------
// handleSearch tests
// ---------------------------------------------------------------------------

// TestHandleSearchNoArgs verifies that /search with no query sets a usage error.
func TestHandleSearchNoArgs(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	up, _ := m.handleSearch(nil)
	m = up.(model)
	if m.err == "" {
		t.Fatal("expected usage error for /search with no args")
	}
	if !strings.Contains(m.err, "usage") {
		t.Fatalf("err = %q, want it to contain 'usage'", m.err)
	}
}

// TestHandleSearchNoStore verifies that /search with a nil store sets an error.
func TestHandleSearchNoStore(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	up, _ := m.handleSearch([]string{"hello"})
	m = up.(model)
	if m.err == "" {
		t.Fatal("expected error for /search with nil store")
	}
	if !strings.Contains(m.err, "no session store") {
		t.Fatalf("err = %q, want it to contain 'no session store'", m.err)
	}
}

// TestHandleSearchResults verifies that /search finds messages in the store
// and renders results in the transcript.
func TestHandleSearchResults(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateSession(ctx, "sess1", "", "Test Session"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.AppendMessage(ctx, "sess1", ai.NewUser("golang concurrency patterns")); err != nil {
		t.Fatalf("append message: %v", err)
	}
	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	up, _ := m.handleSearch([]string{"golang"})
	m = up.(model)
	if m.storeErr != "" {
		t.Fatalf("search store error: %s", m.storeErr)
	}
	plain := ansiStrip(m.transcript)
	if !strings.Contains(plain, "golang") {
		t.Fatalf("transcript missing search result: %q", plain)
	}
}

// TestHandleSearchNoResults verifies that /search with no matches renders
// "[no results]" in the transcript.
func TestHandleSearchNoResults(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateSession(ctx, "sess1", "", "Test"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.AppendMessage(ctx, "sess1", ai.NewUser("hello world")); err != nil {
		t.Fatalf("append message: %v", err)
	}
	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	up, _ := m.handleSearch([]string{"nonexistent_term_xyz"})
	m = up.(model)
	if m.storeErr != "" {
		t.Fatalf("search store error: %s", m.storeErr)
	}
	plain := ansiStrip(m.transcript)
	if !strings.Contains(plain, "[no results]") {
		t.Fatalf("transcript missing [no results]: %q", plain)
	}
}

// ---------------------------------------------------------------------------
// handleInsights tests
// ---------------------------------------------------------------------------

// TestHandleInsightsNoStore verifies that /insights with a nil store sets an error.
func TestHandleInsightsNoStore(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	up, _ := m.handleInsights(nil)
	m = up.(model)
	if m.err == "" {
		t.Fatal("expected error for /insights with nil store")
	}
	if !strings.Contains(m.err, "no session store") {
		t.Fatalf("err = %q, want it to contain 'no session store'", m.err)
	}
}

// TestHandleInsightsNoSessions verifies that /insights with an empty store
// renders "[no sessions ...]" in the transcript.
func TestHandleInsightsNoSessions(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	up, _ := m.handleInsights(nil)
	m = up.(model)
	if m.err != "" {
		t.Fatalf("unexpected error: %s", m.err)
	}
	plain := ansiStrip(m.transcript)
	if !strings.Contains(plain, "[no sessions") {
		t.Fatalf("transcript missing [no sessions]: %q", plain)
	}
}

// TestHandleInsightsWithSessions verifies that /insights with sessions in the
// store renders formatted insights (containing "omega insights") in the transcript.
func TestHandleInsightsWithSessions(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateSession(ctx, "sess1", "", "My Session"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.AppendMessage(ctx, "sess1", ai.NewUser("hello there")); err != nil {
		t.Fatalf("append message: %v", err)
	}
	if err := s.AppendMessage(ctx, "sess1", ai.NewAssistant("hi back")); err != nil {
		t.Fatalf("append assistant: %v", err)
	}
	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	// Use days=0 to include all sessions regardless of creation time.
	up, _ := m.handleInsights([]string{"0"})
	m = up.(model)
	if m.err != "" {
		t.Fatalf("unexpected error: %s", m.err)
	}
	plain := ansiStrip(m.transcript)
	if !strings.Contains(plain, "omega insights") {
		t.Fatalf("transcript missing insights report: %q", plain)
	}
	if !strings.Contains(plain, "Sessions:") {
		t.Fatalf("transcript missing Sessions line: %q", plain)
	}
}

// ---------------------------------------------------------------------------
// handleCopy tests
// ---------------------------------------------------------------------------

// TestHandleCopyNothing verifies that /copy with empty history sets an error.
func TestHandleCopyNothing(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	up, _ := m.handleCopy()
	m = up.(model)
	if m.err == "" {
		t.Fatal("expected 'nothing to copy' error for empty history")
	}
	if !strings.Contains(m.err, "nothing to copy") {
		t.Fatalf("err = %q, want it to contain 'nothing to copy'", m.err)
	}
}

// TestHandleCopyEmptyText verifies that /copy with a message whose text is
// empty sets an error (the message type is recognized but content is blank).
func TestHandleCopyEmptyText(t *testing.T) {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.history = []ai.Message{ai.NewAssistant("")}
	up, _ := m.handleCopy()
	m = up.(model)
	if m.err == "" {
		t.Fatal("expected 'nothing to copy' error for empty assistant content")
	}
	if !strings.Contains(m.err, "nothing to copy") {
		t.Fatalf("err = %q, want it to contain 'nothing to copy'", m.err)
	}
}

// TestHandleCopyUserMessage verifies that /copy copies a user message's text
// to the clipboard. Skips if the clipboard is unavailable in the test env.
func TestHandleCopyUserMessage(t *testing.T) {
	// Probe clipboard availability first.
	if err := clipboard.WriteAll("probe"); err != nil {
		t.Skipf("clipboard not available in test environment: %v", err)
	}
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell")
	m.history = []ai.Message{ai.NewUser("copy me please")}
	up, _ := m.handleCopy()
	m = up.(model)
	if m.err != "" {
		t.Fatalf("unexpected error: %s", m.err)
	}
	got, err := clipboard.ReadAll()
	if err != nil {
		t.Skipf("clipboard read failed: %v", err)
	}
	if got != "copy me please" {
		t.Fatalf("clipboard = %q, want %q", got, "copy me please")
	}
}

// ---------------------------------------------------------------------------
// handleExport tests
// ---------------------------------------------------------------------------

// TestHandleExportNoSession verifies that /export with no active session sets
// an error.
func TestHandleExportNoSession(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	// sessionID is empty → should error
	up, _ := m.handleExport(nil)
	m = up.(model)
	if m.err == "" {
		t.Fatal("expected error for /export with no active session")
	}
	if !strings.Contains(m.err, "no active session") {
		t.Fatalf("err = %q, want it to contain 'no active session'", m.err)
	}
}

// TestHandleExportWrites verifies that /export writes a valid JSONL file
// containing the session's messages.
func TestHandleExportWrites(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateSession(ctx, "exp1", "", "Export Test"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.AppendMessage(ctx, "exp1", ai.NewUser("export this")); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if err := s.AppendMessage(ctx, "exp1", ai.NewAssistant("ok exported")); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "export.jsonl")
	m := newChatModel("ollama", "llama3", nil, "", nil, "", &agent.Context{Store: s}, nil, "dark", "", "bell")
	m.sessionID = "exp1"
	up, _ := m.handleExport([]string{outPath})
	m = up.(model)
	if m.err != "" {
		t.Fatalf("export error: %s", m.err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d", len(lines))
	}
	for i, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d is not valid JSON: %v (line=%q)", i, err, line)
		}
		if _, ok := entry["role"]; !ok {
			t.Fatalf("line %d missing 'role' field: %v", i, entry)
		}
		if _, ok := entry["content"]; !ok {
			t.Fatalf("line %d missing 'content' field: %v", i, entry)
		}
	}
	plain := ansiStrip(m.transcript)
	if !strings.Contains(plain, "[exported") {
		t.Fatalf("transcript missing export confirmation: %q", plain)
	}
	if !strings.Contains(plain, "2 messages") {
		t.Fatalf("transcript missing message count: %q", plain)
	}
}

// TestRenderCodeBlockBasic verifies renderCodeBlock produces a bordered
// box with the language header and the code text preserved.
func TestRenderCodeBlockBasic(t *testing.T) {
	code := "package main\nfunc main() {}"
	out := renderCodeBlock(code, "go", 60, themes["dark"])
	plain := ansiStrip(out)
	if !strings.Contains(plain, "go") {
		t.Fatalf("expected language header 'go' in output, got %q", plain)
	}
	if !strings.Contains(plain, "package main") {
		t.Fatalf("expected code text preserved, got %q", plain)
	}
	if !strings.Contains(plain, "func main()") {
		t.Fatalf("expected function text preserved, got %q", plain)
	}
	// Rounded border uses these box-drawing chars.
	if !strings.Contains(out, "╭") && !strings.Contains(out, "╯") &&
		!strings.Contains(out, "╰") && !strings.Contains(out, "╮") {
		t.Fatalf("expected rounded border characters in output, got %q", out)
	}
}

// TestRenderCodeBlockEmptyLang verifies an empty language string renders
// the header as "text" and does not panic.
func TestRenderCodeBlockEmptyLang(t *testing.T) {
	out := renderCodeBlock("echo hello", "", 60, themes["dark"])
	plain := ansiStrip(out)
	if !strings.Contains(plain, "text") {
		t.Fatalf("expected header 'text' for empty lang, got %q", plain)
	}
	if !strings.Contains(plain, "echo hello") {
		t.Fatalf("expected code text preserved, got %q", plain)
	}
}

// TestRenderCodeBlockSmallWidth verifies width < 12 clamps boxWidth to
// 10 and still renders without panicking.
func TestRenderCodeBlockSmallWidth(t *testing.T) {
	out := renderCodeBlock("x := 1", "go", 5, themes["dark"])
	plain := ansiStrip(out)
	if !strings.Contains(plain, "go") {
		t.Fatalf("expected header 'go' at small width, got %q", plain)
	}
	if !strings.Contains(plain, "x := 1") {
		t.Fatalf("expected code text preserved at small width, got %q", plain)
	}
}

// TestRenderCodeBlockHighlightFallback verifies an unknown language does
// not panic; chroma falls back and the content is still present.
func TestRenderCodeBlockHighlightFallback(t *testing.T) {
	out := renderCodeBlock("looks weird", "totally-not-a-real-lang", 60, themes["light"])
	plain := ansiStrip(out)
	if !strings.Contains(plain, "totally-not-a-real-lang") {
		t.Fatalf("expected header for unknown lang preserved, got %q", plain)
	}
	if !strings.Contains(plain, "looks weird") {
		t.Fatalf("expected code text preserved for unknown lang, got %q", plain)
	}
}

// TestRenderMarkdownNoCodeBlocks verifies markdown without fenced code
// blocks takes the glamour fast path and preserves prose.
func TestRenderMarkdownNoCodeBlocks(t *testing.T) {
	out := renderMarkdown("**bold** here", 80, themes["dark"])
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ANSI escape codes from glamour, got %q", out)
	}
	plain := ansiStrip(out)
	if !strings.Contains(plain, "bold") {
		t.Fatalf("expected bold text preserved, got %q", plain)
	}
	if !strings.Contains(plain, "here") {
		t.Fatalf("expected prose preserved, got %q", plain)
	}
}

// TestRenderMarkdownWithCodeBlock verifies a fenced code block is split
// out: prose goes through glamour and code goes through renderCodeBlock
// (border present, code text preserved).
func TestRenderMarkdownWithCodeBlock(t *testing.T) {
	content := "Here is code:\n\n```go\npackage main\n```\n\nDone."
	out := renderMarkdown(content, 80, themes["dark"])
	plain := ansiStrip(out)
	if !strings.Contains(plain, "Here is code:") {
		t.Fatalf("expected prose before code block preserved, got %q", plain)
	}
	if !strings.Contains(plain, "package main") {
		t.Fatalf("expected code block text preserved, got %q", plain)
	}
	if !strings.Contains(plain, "Done.") {
		t.Fatalf("expected trailing prose preserved, got %q", plain)
	}
	if !strings.Contains(out, "╭") && !strings.Contains(out, "╯") &&
		!strings.Contains(out, "╰") && !strings.Contains(out, "╮") {
		t.Fatalf("expected rounded border around code block, got %q", out)
	}
}

// TestRenderMarkdownMultipleCodeBlocks verifies two fenced code blocks
// in one markdown string are both rendered with their code preserved.
func TestRenderMarkdownMultipleCodeBlocks(t *testing.T) {
	content := "First:\n\n```go\nfunc a() {}\n```\n\nSecond:\n\n```python\nprint('hi')\n```\n\nEnd."
	out := renderMarkdown(content, 80, themes["dark"])
	plain := ansiStrip(out)
	if !strings.Contains(plain, "func a()") {
		t.Fatalf("expected first code block preserved, got %q", plain)
	}
	if !strings.Contains(plain, "print('hi')") {
		t.Fatalf("expected second code block preserved, got %q", plain)
	}
	if !strings.Contains(plain, "First:") {
		t.Fatalf("expected prose before first block preserved, got %q", plain)
	}
	if !strings.Contains(plain, "Second:") {
		t.Fatalf("expected prose between blocks preserved, got %q", plain)
	}
	if !strings.Contains(plain, "End.") {
		t.Fatalf("expected trailing prose preserved, got %q", plain)
	}
}

// TestRenderMarkdownZeroWidth verifies width 0 normalizes to 80 without
// panicking and still renders content.
func TestRenderMarkdownZeroWidth(t *testing.T) {
	out := renderMarkdown("hello world", 0, themes["dark"])
	plain := ansiStrip(out)
	if !strings.Contains(plain, "hello world") {
		t.Fatalf("expected text preserved at zero width, got %q", plain)
	}
}
