package tools

// The session-script verify (card t_db98ebfe): grep and glob run for
// real inside the production agent loop - a FakeProvider scripts two
// turns (one grep call, then one glob call), the loop executes them
// against a seeded workspace and the tool results land as ToolResult
// events on the transcript spine, exactly the path a serve run takes
// (the channel's WithRunWorkspace wrap supplies the workspace root).

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
)

func TestSearchToolsRunInScriptedSession(t *testing.T) {
	root := seedTree(t)

	// Mount the real plugin (the buildPlugins path): one provider with
	// grep + glob.
	mctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{NewPlugin()}, mctx, nil, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if len(mctx.ToolProviders) != 1 {
		t.Fatalf("ToolProviders = %d, want 1", len(mctx.ToolProviders))
	}

	provider := agent.NewFakeProviderScripts("test",
		// turn 1: the model greps the workspace
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c1", Name: "grep",
				Arguments: map[string]any{"pattern": "search me", "output_mode": "content", "context": float64(0)},
			}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// turn 2: then globs for the markdown notes
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c2", Name: "glob",
				Arguments: map[string]any{"pattern": "**/*.md", "type": "files"},
			}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// turn 3: done
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "done"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	events := make(chan core.Event, 256)
	done := make(chan struct{})
	go func() {
		// The caller owns the events channel: the loop writes its spine
		// and returns; only the producer closes (the agent/loop tests
		// carry the same shape).
		defer close(events)
		defer close(done)
		loop.Loop{}.Run(core.WithRunWorkspace(context.Background(), root, ""), agent.LoopOptions{
			Provider:      provider,
			Messages:      []core.Message{core.NewUser("find the notes")},
			ToolProviders: mctx.ToolProviders,
			MaxTurns:      5,
			Events:        events,
		})
	}()

	var toolResults []string
	var finish string
	for {
		e, ok := <-events
		if !ok {
			break
		}
		switch v := e.(type) {
		case core.ToolResultEvent:
			toolResults = append(toolResults, "isError="+boolWord(v.Message.IsError)+": "+v.Message.Content)
		case core.AgentEnd:
			finish = v.FinishReason
		}
	}
	<-done

	if finish != "stop" {
		t.Fatalf("FinishReason = %q, want stop", finish)
	}
	if len(toolResults) != 2 {
		t.Fatalf("tool results = %d, want 2 (grep + glob):\n%s", len(toolResults), strings.Join(toolResults, "\n"))
	}
	// grep's transcript row: the content rows for the seeded files.
	if !strings.Contains(toolResults[0], "isError=no") || !strings.Contains(toolResults[0], "main.go") || !strings.Contains(toolResults[0], "notes.md") {
		t.Errorf("grep result wrong in the transcript:\n%s", toolResults[0])
	}
	// glob's transcript row: the one markdown file, without an error.
	if !strings.Contains(toolResults[1], "notes.md") {
		t.Errorf("glob result wrong in the transcript:\n%s", toolResults[1])
	}
	if !strings.Contains(toolResults[1], "isError=no") {
		t.Errorf("glob errored:\n%s", toolResults[1])
	}
}

func boolWord(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
