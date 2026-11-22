package loop

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// runForPrompt runs one turn with a recording prompt builder and returns it.
func runForPrompt(t *testing.T, ctx context.Context, infos []core.ExtensionInfo, fold []string) *mockPromptBuilder {
	t.Helper()
	pb := &mockPromptBuilder{prompt: "system", guidelines: []string{"guideline"}}
	provider := agent.NewFakeProvider("test",
		core.ResponseChunk{Type: "response_chunk", Content: "hi"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	events := make(chan core.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(ctx, agent.LoopOptions{
			Provider:       provider,
			Messages:       []core.Message{core.NewUser("hi")},
			ExtensionInfos: infos,
			DisabledTools:  fold,
			PromptBuilder:  pb,
			Events:         events,
		})
	}()
	drainEvents(t, events)
	if !pb.called {
		t.Fatal("the prompt builder was never called")
	}
	return pb
}

// TestPromptCarriesTheToolFold pins the wiring the defect needed: the fold the loop applies
// to the tool registry is the fold the prompt build receives, and the inventory it ships is
// stamped with it - so a capsule's prose section can ask "is this tool on the wire?".
func TestPromptCarriesTheToolFold(t *testing.T) {
	infos := []core.ExtensionInfo{{
		Name: "tools",
		ToolList: []core.ToolInfo{
			{Name: "files.read", Description: "Read a file."},
			{Name: "shell.run", Description: "Run a command."},
		},
	}}
	fold := []string{"shell.run"}

	pb := runForPrompt(t, context.Background(), infos, fold)

	if len(pb.lastOpts.DisabledTools) != 1 || pb.lastOpts.DisabledTools[0] != "shell.run" {
		t.Fatalf("prompt fold = %v, want [shell.run]", pb.lastOpts.DisabledTools)
	}
	if len(pb.guideOpts.DisabledTools) != 1 || pb.guideOpts.DisabledTools[0] != "shell.run" {
		t.Fatalf("guidelines fold = %v, want [shell.run] (the same build options)", pb.guideOpts.DisabledTools)
	}
	stamped := map[string]bool{}
	for _, ext := range pb.lastOpts.Extensions {
		for _, ti := range ext.ToolList {
			stamped[ti.Name] = ti.Disabled
		}
	}
	if !stamped["shell.run"] {
		t.Error("shell.run is not stamped disabled in the prompt inventory")
	}
	if stamped["files.read"] {
		t.Error("files.read is stamped disabled although the fold does not name it")
	}
}

// TestPromptFoldsEveryToolWhenToolless pins the extreme fold: a toolless run empties the
// registry, so its prompt must carry every known tool as disabled rather than describe a
// registry the loop just dropped.
func TestPromptFoldsEveryToolWhenToolless(t *testing.T) {
	infos := []core.ExtensionInfo{{
		Name: "tools",
		ToolList: []core.ToolInfo{
			{Name: "files.read", Description: "Read a file."},
			{Name: "shell.run", Description: "Run a command."},
		},
	}}

	pb := runForPrompt(t, agent.WithToolless(context.Background()), infos, nil)

	got := pb.lastOpts.DisabledTools
	if len(got) != 2 || got[0] != "files.read" || got[1] != "shell.run" {
		t.Fatalf("toolless prompt fold = %v, want [files.read shell.run]", got)
	}
	for _, ext := range pb.lastOpts.Extensions {
		for _, ti := range ext.ToolList {
			if !ti.Disabled {
				t.Errorf("toolless prompt inventory still marks %s enabled", ti.Name)
			}
		}
	}
}
