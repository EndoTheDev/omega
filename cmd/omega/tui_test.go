package main

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega-dev/internal/agent"
	"github.com/EndoTheDev/omega-dev/internal/ai"
	"github.com/EndoTheDev/omega-dev/internal/gateway"
	tea "github.com/charmbracelet/bubbletea"
)

// ansiStrips ANSI escape sequences so tests can assert on plain content
// regardless of glamour styling.
var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func ansiStrip(s string) string { return ansi.ReplaceAllString(s, "") }

// TestDrainEventsDeliversStream verifies the channel-drain path: events
// written by the run goroutine are delivered to Update, and the closed
// channel yields streamDoneMsg. This guards the regression where the
// goroutine's Send never reached the program (m.program was always nil).
func TestDrainEventsDeliversStream(t *testing.T) {
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)

	m.handleEvent(agent.StreamEvent{Event: ai.ResponseChunk{Content: "hello"}})
	m.handleEvent(agent.StreamEvent{Event: ai.ResponseChunk{Content: " world"}})
	m.handleEvent(agent.StreamEvent{Event: ai.ToolCallEvent{ToolCall: ai.ToolCall{Name: "shell"}}})
	m.handleEvent(agent.AgentEnd{Type: "agent_end", FinishReason: "stop"})

	plain := ansiStrip(m.transcript)
	if !strings.Contains(plain, "hello world") {
		t.Fatalf("transcript missing streamed content: %q", plain)
	}
	if !strings.Contains(plain, "[tool: shell]") {
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)

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
	if m.err != "usage: /model <name>" {
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

// TestProviderCommand verifies /provider switches the provider type and
// rejects unknown names.
func TestProviderCommand(t *testing.T) {
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)

	updated, _ := m.handleCommand("/provider openai")
	m = updated.(model)
	if m.providerType != "openai" {
		t.Fatalf("provider = %q, want openai", m.providerType)
	}
	if !strings.Contains(m.transcript, "provider set to openai") {
		t.Fatalf("transcript missing provider confirm: %q", m.transcript)
	}

	updated, _ = m.handleCommand("/provider")
	m = updated.(model)
	if m.err != "usage: /provider <ollama|openai|anthropic>" {
		t.Fatalf("expected usage error, got %q", m.err)
	}

	updated, _ = m.handleCommand("/provider grok")
	m = updated.(model)
	if m.providerType != "openai" {
		t.Fatalf("invalid provider changed type to %q, want openai", m.providerType)
	}
	if m.err == "" {
		t.Fatal("expected error for unknown provider type")
	}
}

// TestRenderAssistant renders markdown through glamour: bold becomes
// ANSI-styled output (contains escape sequences), code blocks are preserved,
// and plain text still appears verbatim.
func TestRenderAssistant(t *testing.T) {
	out := renderAssistant("**bold** `code`", 80)
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
	if out := renderAssistant("plain", 0); !strings.Contains(out, "plain") {
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
	out := renderTranscript(messages, 80)

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

	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", s, nil)
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

// TestNewKeepsSession verifies /new wipes in-memory history but keeps
// the session ID so the current conversation stays persisted.
func TestClearKeepsSession(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", s, nil)
	m.sessionID = "sess1"
	m.history = append(m.history, ai.NewUser("hi"))
	m.transcript = "old text"

	updated, _ := m.handleCommand("/new")
	m = updated.(model)

	if len(m.history) != 0 || m.transcript != "" {
		t.Fatalf("clear failed: history=%d transcript=%q", len(m.history), m.transcript)
	}
	if m.sessionID != "sess1" {
		t.Fatalf("clear dropped session: %q", m.sessionID)
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

	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", s, nil)

	updated, _ := m.handleCommand("/sessions")
	m = updated.(model)
	if !strings.Contains(m.transcript, "abc123") {
		t.Fatalf("/sessions missing session id: %q", m.transcript)
	}
	if !strings.Contains(m.transcript, "2 messages") {
		t.Fatalf("/sessions missing message count: %q", m.transcript)
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

	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", s, nil)
	updated, _ := m.handleCommand("/resume nope")
	m = updated.(model)
	if m.storeErr == "" {
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

	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", s, nil)
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

	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", s, nil)
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
	m2 := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", s, nil)
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
	if err := s.SetLabel(ctx, "root", "main"); err != nil {
		t.Fatalf("set label: %v", err)
	}
	if err := s.AppendMessage(ctx, "root", ai.NewUser("hi")); err != nil {
		t.Fatalf("append root: %v", err)
	}

	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", s, nil)
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
	if !strings.Contains(plain, "1 messages") {
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)

	// Single match: "/ex" -> "/exit". CursorEnd() moves the cursor to the
	// end; the cursor position is private on textarea.Model, so we verify
	// it behaviorally: a char inserted after completion must land at the
	// end, not mid-command.
	m.textarea.SetValue("/ex")
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

	// Multiple matches: "/" matches every known command, so the status
	// line lists the options with the selected one highlighted.
	m.textarea.SetValue("/")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != len(knownCommands) {
		t.Fatalf("expected %d matches for /, got %d", len(knownCommands), len(m.autocompleteMatches))
	}
	line := ansiStrip(m.statusLine())
	if !strings.Contains(line, "/exit") || !strings.Contains(line, "/model") {
		t.Fatalf("status line missing options: %q", line)
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)

	// "/" matches every known command, nothing selected.
	m.textarea.SetValue("/")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != len(knownCommands) {
		t.Fatalf("/ matches = %d, want %d", len(m.autocompleteMatches), len(knownCommands))
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

	// "/model" is an exact single match and is auto-selected.
	m.textarea.SetValue("/model")
	m.updateAutocomplete()
	if len(m.autocompleteMatches) != 1 || m.autocompleteMatches[0] != "/model" {
		t.Fatalf("/model matches = %v, want [/model]", m.autocompleteMatches)
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)

	// "/mo" is a single match auto-selected; a Down is not needed. Use "/"
	// (multiple matches) to test arrow-driven selection instead: select
	// /provider via Down, then Enter accepts it.
	m.textarea.SetValue("/")
	m.updateAutocomplete()
	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown}) // select first match
	m = up.(model)
	// Find /provider and advance to it.
	for m.autocompleteMatches[m.autocompleteIndex] != "/provider" {
		up, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = up.(model)
	}
	up, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = up.(model)
	if m.textarea.Value() != "/provider" {
		t.Fatalf("enter accepted %q, want /provider", m.textarea.Value())
	}
	if m.autocompleteMatches != nil || m.autocompleteIndex != -1 {
		t.Fatalf("match state not cleared after accept: %v idx=%d", m.autocompleteMatches, m.autocompleteIndex)
	}

	// Enter on a single-match auto-selected command completes it.
	m.textarea.SetValue("/ex")
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

// TestEscapeCancelsRun verifies Escape during a busy run calls cancel.
func TestEscapeCancelsRun(t *testing.T) {
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)

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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)

	m.handleEvent(agent.StreamEvent{Event: ai.ThinkingChunk{Content: "plan"}})
	m.handleEvent(agent.StreamEvent{Event: ai.ToolCallEvent{ToolCall: ai.ToolCall{Name: "shell"}}})
	m.handleEvent(agent.StreamEvent{Event: ai.ResponseChunk{Content: "done"}})
	m.handleEvent(agent.AgentEnd{Type: "agent_end", FinishReason: "stop"})

	plain := ansiStrip(m.transcript)
	thinkIdx := strings.Index(plain, "[thinking]")
	toolIdx := strings.Index(plain, "[tool: shell]")
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil, nil)
	m.sessionID = "abc123"

	line := ansiStrip(m.statusLine())
	if !strings.Contains(line, "omega") {
		t.Fatalf("status line missing omega: %q", line)
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
	if !strings.Contains(line, "sess: abc123") {
		t.Fatalf("status line missing session: %q", line)
	}
}

// TestHelpText verifies help text contains all commands.
func TestHelpText(t *testing.T) {
	help := renderHelp()
	plain := ansiStrip(help)
	for _, cmd := range []string{"/exit", "/new", "/sessions", "/resume", "/help", "/model", "/provider", "/compact"} {
		if !strings.Contains(plain, cmd) {
			t.Fatalf("help text missing %q", cmd)
		}
	}
}
