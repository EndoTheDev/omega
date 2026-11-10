package terminal

// The loop hand-off pin (card t_74428a10): the live lane saw shell.run
// answer the service cwd (System32) even though the workspace chip showed
// the configured default. The tool half is pinned by TestShellRoot; this
// pins the LOOP half: a RunWorkspace ctx carried into Agent.RunWithProvider
// must survive to every t.Run call the real loop makes, so the shell's
// cmd.Dir is the workspace - never the process cwd. Drives the real
// agent/loop with a scripted provider, no server involved.

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
)

func TestLoopShellsRunInTheWorkspace(t *testing.T) {
	ws := t.TempDir()
	probe := "pwd"
	if runtime.GOOS == "windows" {
		probe = "cd" // bare `cd` is the cmd builtin that prints the cwd
	}
	prov := agent.NewFakeProviderScripts("test",
		// Turn 1: the model calls shell.run with no dir argument.
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "shell.run", Arguments: map[string]any{"command": probe}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// Turn 2: respond and stop.
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "done"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})

	ctx := core.WithRunWorkspace(context.Background(), ws, "")
	tools := testShellProvider(t).Tools()
	events := a.RunWithProvider(ctx, prov, []core.Message{core.NewUser("probe the cwd")}, tools)

	var result string
	isErr := false
	for e := range events {
		if tre, ok := e.(core.ToolResultEvent); ok {
			result = tre.Message.Content
			isErr = tre.Message.IsError
		}
	}
	if isErr {
		t.Fatalf("shell.run %s errored inside the loop: %q", probe, result)
	}
	if !strings.Contains(strings.ToLower(result), strings.ToLower(ws)) {
		t.Fatalf("shell.run %s ran outside the workspace: %q (want it to contain %q)", probe, result, ws)
	}
}
