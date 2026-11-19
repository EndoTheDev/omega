package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestPrepareRunDefaultWorkspace pins the default_workspace fallback: a session WITHOUT
// a pinned workspace roots its tool runs at the configured server.default_workspace - a
// defined default, never the process working directory. A pinned session keeps its own
// root; a stat refusal keeps the previous root - a vanished folder never uproots live
// sessions.
func TestPrepareRunDefaultWorkspace(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ws := t.TempDir()

	ctx := context.Background()
	if err := st.CreateSession(ctx, "w1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	s.wsMu.Lock()
	s.defaultWorkspace = ""
	s.wsMu.Unlock()
	req := &chatRequest{SessionID: "w1"}
	cr, err := s.prepareRun(ctx, req, nil)
	if err != nil {
		t.Fatalf("prepare (unset): %v", err)
	}
	s.runs.finish(cr.run)
	if req.workspace != "" {
		t.Errorf("unset default: workspace = %q, want empty", req.workspace)
	}

	s.wsMu.Lock()
	s.defaultWorkspace = ws
	s.wsMu.Unlock()
	req = &chatRequest{SessionID: "w1"}
	cr, err = s.prepareRun(ctx, req, nil)
	if err != nil {
		t.Fatalf("prepare (default): %v", err)
	}
	s.runs.finish(cr.run)
	if filepath.Clean(req.workspace) != filepath.Clean(ws) {
		t.Errorf("default root: workspace = %q, want %q", req.workspace, ws)
	}

	pinned := t.TempDir()
	if err := st.SetSessionWorkspace(ctx, "w1", pinned); err != nil {
		t.Fatalf("pin workspace: %v", err)
	}
	req = &chatRequest{SessionID: "w1"}
	cr, err = s.prepareRun(ctx, req, nil)
	if err != nil {
		t.Fatalf("prepare (pinned): %v", err)
	}
	s.runs.finish(cr.run)
	if filepath.Clean(req.workspace) != filepath.Clean(pinned) {
		t.Errorf("pinned root: workspace = %q, want %q", req.workspace, pinned)
	}

	// The impossible-path rule: a stat failure is a REFUSAL, the previous root survives -
	// it never silently clears to the process cwd.
	s.SetDefaultWorkspace("/omega-definitely-not-here")
	if s.defaultWorkspace != ws {
		t.Fatalf("vanished default: knob = %q, want the previous root %q kept", s.defaultWorkspace, ws)
	}
	req = &chatRequest{SessionID: "w1"}
	cr, err = s.prepareRun(ctx, req, nil)
	if err != nil {
		t.Fatalf("prepare (vanished): %v", err)
	}
	s.runs.finish(cr.run)
	if filepath.Clean(req.workspace) != filepath.Clean(pinned) {
		t.Errorf("vanished default: workspace = %q, want the pin %q", req.workspace, pinned)
	}
}

// TestSetDefaultWorkspaceGuards pins the setter's validation half.
func TestSetDefaultWorkspaceGuards(t *testing.T) {
	s := newTestServer()
	ws := t.TempDir()
	s.SetDefaultWorkspace(ws)
	if s.defaultWorkspace != ws {
		t.Fatalf("setter: defaultWorkspace = %q, want %q", s.defaultWorkspace, ws)
	}
	s.SetDefaultWorkspace("/omega-definitely-not-here")
	if s.defaultWorkspace != ws {
		t.Errorf("missing dir: defaultWorkspace = %q, want kept %q", s.defaultWorkspace, ws)
	}
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.SetDefaultWorkspace(file)
	if s.defaultWorkspace != ws {
		t.Errorf("file path: defaultWorkspace = %q, want kept %q", s.defaultWorkspace, ws)
	}

	s.SetDefaultWorkspace("")
	if s.defaultWorkspace != "" {
		t.Errorf("clear: defaultWorkspace = %q, want unset", s.defaultWorkspace)
	}
}
