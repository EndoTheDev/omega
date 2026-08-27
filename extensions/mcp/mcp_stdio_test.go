package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	s, err := newStdioServer("fake", "python", []string{script}, nil)
	if err != nil {
		t.Fatalf("newStdioServer: %v", err)
	}
	defer s.close()

	// listTools through the real stdio request path.
	tools, err := s.listTools()
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
	text, isErr, err := s.callTool("echo", map[string]any{"msg": "hi"})
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
	s, err := newStdioServer("fake", "python", []string{script}, nil)
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
	s, err := newStdioServer("env", "python", []string{script}, map[string]string{"MCP_TEST_VAR": "passed"})
	if err != nil {
		t.Fatalf("newStdioServer: %v", err)
	}
	defer s.close()
	text, _, err := s.callTool("envcheck", nil)
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
	s, err := newStdioServer("fake", "python", []string{script}, nil)
	if err != nil {
		t.Fatalf("newStdioServer: %v", err)
	}
	defer s.close()

	result, err := s.request("tools/list", map[string]any{})
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
	result2, err := s.request("unknown/method", nil)
	if err == nil {
		t.Fatal("expected error for unknown method, got nil")
	}
	if result2 != nil {
		t.Fatalf("expected nil result on error, got %s", result2)
	}
}