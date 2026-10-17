package mcp

import (
	"encoding/json"
	"errors"
	"testing"
)

// scriptedConn is a test MCPConn whose request() returns a pre-encoded
// JSON payload (or an error), letting mcpListTools/mcpCallTool exercise
// their unmarshalling logic without a real MCP server.
type scriptedConn struct {
	result json.RawMessage
	err    error
}

func (s scriptedConn) request(method string, params any) (json.RawMessage, error) {
	return s.result, s.err
}
func (s scriptedConn) listTools() ([]mcpTool, error) {
	return mcpListTools(s)
}
func (s scriptedConn) callTool(name string, args map[string]any) (string, bool, error) {
	return mcpCallTool(s, name, args)
}
func (s scriptedConn) close() {}

// --- mcpListTools ---

func TestMcpListToolsValid(t *testing.T) {
	payload := json.RawMessage(`{"tools":[
		{"name":"search","description":"Search the web","inputSchema":{"type":"object"}},
		{"name":"fetch","description":"Fetch a URL","inputSchema":{"type":"object"}}
	]}`)
	conn := scriptedConn{result: payload}
	tools, err := mcpListTools(conn)
	if err != nil {
		t.Fatalf("mcpListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "search" || tools[0].Description != "Search the web" {
		t.Fatalf("tool[0]: got %+v", tools[0])
	}
	if tools[1].Name != "fetch" || tools[1].Description != "Fetch a URL" {
		t.Fatalf("tool[1]: got %+v", tools[1])
	}
}

func TestMcpListToolsEmpty(t *testing.T) {
	conn := scriptedConn{result: json.RawMessage(`{"tools":[]}`)}
	tools, err := mcpListTools(conn)
	if err != nil {
		t.Fatalf("mcpListTools: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(tools))
	}
}

func TestMcpListToolsMalformed(t *testing.T) {
	conn := scriptedConn{result: json.RawMessage(`{bad json}`)}
	_, err := mcpListTools(conn)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestMcpListToolsRequestError(t *testing.T) {
	conn := scriptedConn{err: errors.New("connection refused")}
	_, err := mcpListTools(conn)
	if err == nil {
		t.Fatal("expected error from request, got nil")
	}
}

// --- mcpCallTool ---

func TestMcpCallToolTextContent(t *testing.T) {
	payload := json.RawMessage(`{"content":[
		{"type":"text","text":"hello "},
		{"type":"text","text":"world"}
	],"isError":false}`)
	conn := scriptedConn{result: payload}
	text, isErr, err := mcpCallTool(conn, "echo", map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatalf("mcpCallTool: %v", err)
	}
	if text != "hello world" {
		t.Fatalf("text: got %q, want %q", text, "hello world")
	}
	if isErr {
		t.Fatal("isError: got true, want false")
	}
}

func TestMcpCallToolIsError(t *testing.T) {
	payload := json.RawMessage(`{"content":[
		{"type":"text","text":"tool failed"}
	],"isError":true}`)
	conn := scriptedConn{result: payload}
	text, isErr, err := mcpCallTool(conn, "broken", nil)
	if err != nil {
		t.Fatalf("mcpCallTool: %v", err)
	}
	if text != "tool failed" {
		t.Fatalf("text: got %q, want %q", text, "tool failed")
	}
	if !isErr {
		t.Fatal("isError: got false, want true")
	}
}

func TestMcpCallToolSkipsNonText(t *testing.T) {
	payload := json.RawMessage(`{"content":[
		{"type":"image","text":"should-be-skipped"},
		{"type":"text","text":"only-text"}
	],"isError":false}`)
	conn := scriptedConn{result: payload}
	text, _, err := mcpCallTool(conn, "mixed", nil)
	if err != nil {
		t.Fatalf("mcpCallTool: %v", err)
	}
	if text != "only-text" {
		t.Fatalf("text: got %q, want %q (non-text should be skipped)", text, "only-text")
	}
}

func TestMcpCallToolEmptyContent(t *testing.T) {
	conn := scriptedConn{result: json.RawMessage(`{"content":[],"isError":false}`)}
	text, isErr, err := mcpCallTool(conn, "noop", nil)
	if err != nil {
		t.Fatalf("mcpCallTool: %v", err)
	}
	if text != "" {
		t.Fatalf("text: got %q, want empty", text)
	}
	if isErr {
		t.Fatal("isError: got true, want false")
	}
}

func TestMcpCallToolMalformed(t *testing.T) {
	conn := scriptedConn{result: json.RawMessage(`not json`)}
	_, _, err := mcpCallTool(conn, "bad", nil)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestMcpCallToolRequestError(t *testing.T) {
	conn := scriptedConn{err: errors.New("timeout")}
	_, _, err := mcpCallTool(conn, "any", nil)
	if err == nil {
		t.Fatal("expected error from request, got nil")
	}
}