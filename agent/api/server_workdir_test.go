package api

// The server hand-off pin (card t_74428a10): the live lane saw
// shell.run answer the service cwd (C:\Windows\System32) while the
// workspace chip showed the configured default_workspace. The knob
// wiring exists (SetDefaultWorkspace -> prepareRun fallback ->
// WithRunWorkspace); these tests pin the whole chain with the REAL
// loop and the REAL shell tool, so a broken hand-off between the
// server's resolved req.workspace and the tool's ctx is red here
// instead of silent live:
//
//   - no-store run: the default_workspace knob roots the tools
//     (prepareRun's sibling fallback, card t_ccf74d88)
//   - stored session with no pinned workspace: same fallback
//   - stored session with a pinned workspace: the pin wins
//     (card t_9de5b009)
//
// The probe is `pwd` on unix and bare `cd` on windows (pwd.exe on the
// inherited PATH answers with a translated path, not the workspace -
// the same shape TestShellRoot documents in harness/terminal).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/EndoTheDev/omega/harness/terminal"
)

// newShellServer builds a server with the real loop and the real shell
// tool over a provider scripted to call shell.run once (turn 1 calls
// the cwd probe, turn 2 stops). Returns the server, the provider and
// the OS-shaped cwd probe.
func newShellServer(t *testing.T) (*Server, *agent.FakeProvider, string) {
	t.Helper()
	probe := "pwd"
	if runtime.GOOS == "windows" {
		probe = "cd"
	}
	prov := agent.NewFakeProviderScripts("mock", shellTurn(probe)...)
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	s := NewServer(a, (&terminal.ShellProvider{}).Tools(), nil, nil, nil, nil)
	return s, prov, probe
}

// shellTurn returns the two-script shape: turn 1 calls the probe, turn 2
// stops. One tool call per probe (the provider script counter walks).
func shellTurn(probe string) [][]core.ProviderStreamEvent {
	return [][]core.ProviderStreamEvent{
		{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "shell.run", Arguments: map[string]any{"command": probe}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		{
			core.ResponseChunk{Type: "response_chunk", Content: "done"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	}
}

// chatToolResults posts /chat with the given body and returns every
// tool_result frame the run streamed (content + error flag).
func chatToolResults(t *testing.T, s *Server, body []byte) []core.ToolResult {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var results []core.ToolResult
	for _, e := range parseSSE(t, rec.Body.String()) {
		if e.event != "tool_result" {
			continue
		}
		var tre core.ToolResultEvent
		if err := json.Unmarshal(e.data, &tre); err != nil {
			t.Fatalf("decode tool_result: %v", err)
		}
		results = append(results, tre.Message)
	}
	if len(results) == 0 {
		t.Fatalf("no tool_result frames streamed")
	}
	return results
}

func assertRanIn(t *testing.T, res core.ToolResult, dir string, label string) {
	t.Helper()
	if res.IsError {
		t.Fatalf("%s: shell.run errored: %q", label, res.Content)
	}
	if !strings.Contains(strings.ToLower(res.Content), strings.ToLower(dir)) {
		t.Fatalf("%s: shell.run ran outside %q: %q", label, dir, res.Content)
	}
}

// TestChatNoStoreRootsToolsAtDefaultWorkspace: the legacy no-store path
// applies the default_workspace fallback (card t_ccf74d88) - the tools
// cannot fall back to the service/process cwd when the knob is defined.
func TestChatNoStoreRootsToolsAtDefaultWorkspace(t *testing.T) {
	s, _, _ := newShellServer(t)
	ws := t.TempDir()
	s.SetDefaultWorkspace(ws)

	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "probe the cwd"}},
	})
	res := chatToolResults(t, s, body)[0]
	assertRanIn(t, res, ws, "no-store")
}

// TestChatStoredSessionRootsToolsAtDefaultWorkspace: a session row with
// no pinned workspace takes the knob fallback through prepareRun
// (server.go's sess.Workspace -> defaultWorkspace chain).
func TestChatStoredSessionRootsToolsAtDefaultWorkspace(t *testing.T) {
	s, _, _ := newShellServer(t)
	memStore, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { memStore.Close() })
	s.store = memStore
	ws := t.TempDir()
	s.SetDefaultWorkspace(ws)
	if err := memStore.CreateSession(context.Background(), "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// Pre-title the row (final=true bumps TitleState past 0): the
	// detached title kick streams through the SAME scripted provider
	// and would eat the tool-call script before the main run claims it.
	if err := memStore.SetSessionTitle(context.Background(), "s1", "probe", true); err != nil {
		t.Fatalf("pre-title session: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"session_id": "s1",
		"messages":   []map[string]any{{"role": "user", "content": "probe the cwd"}},
	})
	res := chatToolResults(t, s, body)[0]
	assertRanIn(t, res, ws, "stored session")
}

// TestChatPinnedWorkspaceWinsOverDefault: a session pinned to a folder
// roots THIS run there (card t_9de5b009) even with a different knob.
func TestChatPinnedWorkspaceWinsOverDefault(t *testing.T) {
	s, _, _ := newShellServer(t)
	memStore, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { memStore.Close() })
	s.store = memStore
	ws := t.TempDir()
	pin := t.TempDir()
	s.SetDefaultWorkspace(ws)
	if err := memStore.CreateSession(context.Background(), "s2", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := memStore.SetSessionWorkspace(context.Background(), "s2", pin); err != nil {
		t.Fatalf("pin workspace: %v", err)
	}
	// Pre-title the row (see TestChatStoredSessionRootsToolsAtDefaultWorkspace):
	// the detached title kick would eat the tool-call script.
	if err := memStore.SetSessionTitle(context.Background(), "s2", "probe", true); err != nil {
		t.Fatalf("pre-title session: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"session_id": "s2",
		"messages":   []map[string]any{{"role": "user", "content": "probe the cwd"}},
	})
	res := chatToolResults(t, s, body)[0]
	assertRanIn(t, res, pin, "pinned session")
}
