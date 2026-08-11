package main

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega-dev/internal/agent"
	"github.com/EndoTheDev/omega-dev/internal/ai"
	"github.com/EndoTheDev/omega-dev/internal/gateway"
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil)
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil)
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil)

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
	if m.buffer != "" {
		t.Fatalf("buffer should be cleared after AgentEnd, got %q", m.buffer)
	}
}

// TestHandleEventError verifies a stream error is surfaced and folded.
func TestHandleEventError(t *testing.T) {
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil)
	m.handleEvent(agent.StreamEvent{Event: ai.StreamEnd{FinishReason: "error", Error: "boom"}})
	m.handleEvent(agent.AgentEnd{Type: "agent_end", FinishReason: "error", Error: "boom"})

	if m.err != "boom" {
		t.Fatalf("expected err boom, got %q", m.err)
	}
	if !strings.Contains(ansiStrip(m.transcript), "error: boom") {
		t.Fatalf("transcript missing error: %q", m.transcript)
	}
}

// TestSlashCommands verifies /clear, /model, /help, and unknown handling.
func TestSlashCommands(t *testing.T) {
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil)

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

	// /clear wipes history and transcript.
	m.history = append(m.history, ai.NewUser("hi"))
	m.transcript = "some old text"
	updated, _ = m.handleCommand("/clear")
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
	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", nil)

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

	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", s)
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

// TestClearKeepsSession verifies /clear wipes in-memory history but keeps
// the session ID so the current conversation stays persisted.
func TestClearKeepsSession(t *testing.T) {
	s, err := gateway.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", s)
	m.sessionID = "sess1"
	m.history = append(m.history, ai.NewUser("hi"))
	m.transcript = "old text"

	updated, _ := m.handleCommand("/clear")
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

	if err := s.CreateSession(ctx, "abc123"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.AppendMessage(ctx, "abc123", ai.NewUser("first")); err != nil {
		t.Fatalf("append user: %v", err)
	}
	if err := s.AppendMessage(ctx, "abc123", ai.NewAssistant("reply")); err != nil {
		t.Fatalf("append assistant: %v", err)
	}

	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", s)

	updated, _ := m.handleCommand("/sessions")
	m = updated.(model)
	if !strings.Contains(m.transcript, "abc123") {
		t.Fatalf("/sessions missing session id: %q", m.transcript)
	}
	if !strings.Contains(m.transcript, "2 msgs") {
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

	m := newChatModel("ollama", "llama3", "http://localhost:11434", "", nil, "", s)
	updated, _ := m.handleCommand("/resume nope")
	m = updated.(model)
	if m.err == "" {
		t.Fatal("expected error for unknown session")
	}
}
