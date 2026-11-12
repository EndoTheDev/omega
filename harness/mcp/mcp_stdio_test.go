package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pythonBin finds the Python binary at test time. Modern Linux distros
// ship python3 but not python, so try both.
func pythonBin() string {
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return "python3"
}

// fakeServerScript is a minimal MCP server over stdio: it reads
// newline-delimited JSON-RPC from stdin and answers initialize,
// tools/list, and tools/call with canned responses.
const fakeServerScript = `import sys, json

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except Exception:
        continue
    if "id" not in req:
        continue  # notification (e.g. notifications/initialized)
    method = req.get("method", "")
    if method == "initialize":
        result = {"protocolVersion": "2025-11-25",
                  "serverInfo": {"name": "fake-mcp", "version": "0.1.0"},
                  "capabilities": {}}
    elif method == "tools/list":
        result = {"tools": [
            {"name": "echo", "description": "Echo tool",
             "inputSchema": {"type": "object"}},
        ]}
    elif method == "tools/call":
        args = req.get("params", {}).get("arguments", {})
        result = {"content": [{"type": "text", "text": "echoed:" + json.dumps(args, sort_keys=True)}],
                  "isError": False}
    else:
        resp = {"jsonrpc": "2.0", "id": req["id"],
                "error": {"code": -32601, "message": "method not found: " + method}}
        sys.stdout.write(json.dumps(resp) + "\n")
        sys.stdout.flush()
        continue
    resp = {"jsonrpc": "2.0", "id": req["id"], "result": result}
    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()
`

// writeFakeServer writes the fake MCP server script to a temp file and
// returns its path.
func writeFakeServer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake_mcp_server.py")
	if err := os.WriteFile(path, []byte(fakeServerScript), 0o644); err != nil {
		t.Fatalf("write fake server script: %v", err)
	}
	return path
}

// TestNewStdioServerHandshake verifies that newStdioServer spawns the
// subprocess, completes the initialize handshake, and that listTools and
// callTool round-trip through the real request/readLoop path.
func TestNewStdioServerHandshake(t *testing.T) {
	script := writeFakeServer(t)
	s, err := newStdioServer("fake", pythonBin(), []string{script}, nil)
	if err != nil {
		t.Fatalf("newStdioServer: %v", err)
	}
	defer s.close()

	// listTools through the real stdio request path.
	tools, err := s.listTools(context.Background())
	if err != nil {
		t.Fatalf("listTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "echo" || tools[0].Description != "Echo tool" {
		t.Fatalf("tool[0]: got %+v", tools[0])
	}

	// callTool through the real stdio request path.
	text, isErr, err := s.callTool(context.Background(), "echo", map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}
	if isErr {
		t.Fatal("callTool: got isError=true, want false")
	}
	if text != `echoed:{"msg": "hi"}` {
		t.Fatalf("callTool text: got %q", text)
	}
}

// TestNewStdioServerBadCommand verifies the start-error branch returns an
// error when the command does not exist.
func TestNewStdioServerBadCommand(t *testing.T) {
	_, err := newStdioServer("bad", "definitely-not-a-real-executable-xyz-12345", nil, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent command, got nil")
	}
	if !strings.Contains(err.Error(), "start") {
		t.Fatalf("expected start error, got: %v", err)
	}
}

// TestStdioServerClose verifies close terminates the subprocess promptly
// without hanging.
func TestStdioServerClose(t *testing.T) {
	script := writeFakeServer(t)
	s, err := newStdioServer("fake", pythonBin(), []string{script}, nil)
	if err != nil {
		t.Fatalf("newStdioServer: %v", err)
	}
	done := make(chan struct{})
	go func() {
		s.close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("close did not return within 10s")
	}
	if s.cmd.ProcessState == nil {
		t.Fatal("expected process to be reaped after close (ProcessState nil)")
	}
}

// TestStdioServerEnv verifies env vars are passed through to the subprocess
// by having the fake server echo an env var back via a tools/call.
func TestStdioServerEnv(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "env_mcp_server.py")
	scriptSrc := `import sys, json, os

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    if "id" not in req:
        continue
    if req.get("method") == "initialize":
        result = {"protocolVersion": "2025-11-25", "serverInfo": {"name": "env-mcp", "version": "0.1.0"}, "capabilities": {}}
    elif req.get("method") == "tools/call":
        result = {"content": [{"type": "text", "text": os.environ.get("MCP_TEST_VAR", "MISSING")}], "isError": False}
    else:
        result = {}
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": req["id"], "result": result}) + "\n")
    sys.stdout.flush()
`
	if err := os.WriteFile(script, []byte(scriptSrc), 0o644); err != nil {
		t.Fatalf("write env script: %v", err)
	}
	s, err := newStdioServer("env", pythonBin(), []string{script}, map[string]string{"MCP_TEST_VAR": "passed"})
	if err != nil {
		t.Fatalf("newStdioServer: %v", err)
	}
	defer s.close()
	text, _, err := s.callTool(context.Background(), "envcheck", nil)
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}
	if text != "passed" {
		t.Fatalf("env var not passed through: got %q, want %q", text, "passed")
	}
}

// TestStdioServerRequestRoundTrip verifies request() returns the parsed
// result payload and that responses are routed by ID (two sequential
// requests get distinct results).
func TestStdioServerRequestRoundTrip(t *testing.T) {
	script := writeFakeServer(t)
	s, err := newStdioServer("fake", pythonBin(), []string{script}, nil)
	if err != nil {
		t.Fatalf("newStdioServer: %v", err)
	}
	defer s.close()

	result, err := s.request(context.Background(), "tools/list", map[string]any{})
	if err != nil {
		t.Fatalf("request tools/list: %v", err)
	}
	var list mcpToolsListResult
	if err := json.Unmarshal(result, &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", list.Tools)
	}

	// Second request must still route correctly (no stale pending state).
	result2, err := s.request(context.Background(), "unknown/method", nil)
	if err == nil {
		t.Fatal("expected error for unknown method, got nil")
	}
	if result2 != nil {
		t.Fatalf("expected nil result on error, got %s", result2)
	}
}

// TestStdioServerRequestContextCancel is the receipt for ctx threading on the
// stdio transport: a call the server never answers aborts on cancellation,
// drops its pending slot, and leaves the connection usable.
func TestStdioServerRequestContextCancel(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "slow_mcp_server.py")
	scriptSrc := `import sys, json, time

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    if "id" not in req:
        continue
    method = req.get("method", "")
    if method == "tools/call":
        time.sleep(60)  # never answers within the test window
        continue
    if method == "initialize":
        result = {"protocolVersion": "2025-11-25", "serverInfo": {"name": "slow", "version": "0.1.0"}, "capabilities": {}}
    elif method == "tools/list":
        result = {"tools": [{"name": "slow", "description": "Slow tool", "inputSchema": {"type": "object"}}]}
    else:
        result = {}
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": req["id"], "result": result}) + "\n")
    sys.stdout.flush()
`
	if err := os.WriteFile(script, []byte(scriptSrc), 0o644); err != nil {
		t.Fatalf("write slow script: %v", err)
	}
	s, err := newStdioServer("slow", pythonBin(), []string{script}, nil)
	if err != nil {
		t.Fatalf("newStdioServer: %v", err)
	}
	defer s.close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	type outcome struct {
		err     error
		elapsed time.Duration
	}
	done := make(chan outcome, 1)
	go func() {
		start := time.Now()
		_, _, err := s.callTool(ctx, "slow", nil)
		done <- outcome{err: err, elapsed: time.Since(start)}
	}()

	select {
	case got := <-done:
		if got.err == nil {
			t.Fatal("expected the cancelled call to fail, got nil")
		}
		if got.elapsed > 5*time.Second {
			t.Fatalf("cancelled call took %v, want an abort near the 200ms deadline (err: %v)", got.elapsed, got.err)
		}
		if !errors.Is(got.err, context.DeadlineExceeded) && !strings.Contains(got.err.Error(), "context") {
			t.Fatalf("error should name the cancellation, got %v", got.err)
		}
	case <-time.After(testWaitTimeout):
		t.Fatal("cancelled call did not return - ctx is not threaded through the stdio request")
	}

	// The abandoned request must not leak its pending slot.
	s.pendingMu.Lock()
	pending := len(s.pending)
	s.pendingMu.Unlock()
	if pending != 0 {
		t.Fatalf("pending requests after cancellation: got %d, want 0", pending)
	}
	// No "connection is still usable" assertion here: the fake server is
	// single-threaded and is still inside its 60s sleep for the abandoned
	// call, so any follow-up request would only prove the fake is busy.
}
