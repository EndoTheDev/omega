package loop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// uploads fixture: a fake home whose uploads tree holds one file.
func attachmentFixture(t *testing.T, name string, content []byte) core.Attachment {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "uploads", "s1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	return core.Attachment{Kind: kind, Name: name, Size: int64(len(content)), Path: path}
}

// TestAttachmentContextRendersReference: a non-text attachment surfaces
// as a structured entry (name, type, size, path) with no content, and
// points the agent at its own read tool.
func TestAttachmentContextRendersReference(t *testing.T) {
	att := attachmentFixture(t, "invoice.pdf", []byte("%PDF-1.7\n\x00\x01binary-ish"))
	msgs := []core.Message{core.User{Content: "what is this?", Attachments: []core.Attachment{att}}}

	out := AttachmentContext(msgs)
	u, ok := out[0].(core.User)
	if !ok {
		t.Fatalf("out[0] = %T, want core.User", out[0])
	}
	for _, want := range []string{
		"what is this?",
		"--- attached file ---",
		"name: invoice.pdf",
		"type: pdf",
		"size: 21 bytes",
		"path: " + att.Path,
		"content: not inlined - read it with files.read",
	} {
		if !strings.Contains(u.Content, want) {
			t.Errorf("content missing %q:\n%s", want, u.Content)
		}
	}
	if strings.Contains(u.Content, "%PDF-1.7") {
		t.Errorf("pdf bytes were inlined:\n%s", u.Content)
	}
}

// TestAttachmentContextInlinesSmallText: a small csv is inlined - the
// one exception to reference-only, so the agent answers without a tool
// round trip.
func TestAttachmentContextInlinesSmallText(t *testing.T) {
	att := attachmentFixture(t, "report.csv", []byte("name,qty\nbolt,3\nnut,7\n"))
	msgs := []core.Message{core.User{Content: "what columns?", Attachments: []core.Attachment{att}}}

	u := AttachmentContext(msgs)[0].(core.User)
	for _, want := range []string{
		"name: report.csv",
		"type: csv",
		"content:\nname,qty\nbolt,3\nnut,7\n",
		"--- end attached file content ---",
	} {
		if !strings.Contains(u.Content, want) {
			t.Errorf("content missing %q:\n%s", want, u.Content)
		}
	}
}

// TestAttachmentContextKeepsBigAndBinaryOut: the inline exception is
// narrow - over-threshold text and non-UTF-8 bodies stay
// reference-only, with the reason named.
func TestAttachmentContextKeepsBigAndBinaryOut(t *testing.T) {
	big := strings.Repeat("a,b\n", inlineAttachmentMaxBytes/4+16)
	bigAtt := attachmentFixture(t, "big.csv", []byte(big))
	binAtt := attachmentFixture(t, "notes.txt", []byte("head\x00tail"))

	msgs := []core.Message{core.User{
		Content:     "check these",
		Attachments: []core.Attachment{bigAtt, binAtt},
	}}
	u := AttachmentContext(msgs)[0].(core.User)
	if strings.Contains(u.Content, strings.Repeat("a,b\n", 8)) {
		t.Errorf("oversize csv was inlined (%d bytes):\n%.200s", len(big), u.Content)
	}
	if strings.Contains(u.Content, "head\x00tail") {
		t.Errorf("binary .txt was inlined:\n%s", u.Content)
	}
	if n := strings.Count(u.Content, "content: not inlined (over 8 KB or not plain UTF-8)"); n != 2 {
		t.Errorf("reason lines = %d, want 2:\n%s", n, u.Content)
	}
}

// TestAttachmentContextDoesNotMutate: the persisted message stays
// reference-only; only the provider-facing copy carries the block.
func TestAttachmentContextDoesNotMutate(t *testing.T) {
	att := attachmentFixture(t, "notes.md", []byte("# hi\n"))
	orig := core.User{Content: "read this", Attachments: []core.Attachment{att}}
	msgs := []core.Message{core.NewSystem("sys"), orig}

	out := AttachmentContext(msgs)
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
	if msgs[1].(core.User).Content != "read this" {
		t.Errorf("input message mutated: %q", msgs[1].(core.User).Content)
	}
	if got := out[1].(core.User).Content; !strings.Contains(got, "--- attached file ---") {
		t.Errorf("provider copy missing the block:\n%s", got)
	}
	if msgs[0].(core.System).Content != "sys" || out[0].(core.System).Content != "sys" {
		t.Error("system message disturbed")
	}
}

// TestAttachmentContextNoAttachments: a run with no attachments hands
// the very same slice through - zero allocation, zero behavior change.
func TestAttachmentContextNoAttachments(t *testing.T) {
	msgs := []core.Message{core.NewSystem("sys"), core.NewUser("hi")}
	out := AttachmentContext(msgs)
	if len(out) != len(msgs) {
		t.Fatalf("len(out) = %d, want %d", len(out), len(msgs))
	}
	for i := range msgs {
		if core.MessageRole(out[i]) != core.MessageRole(msgs[i]) {
			t.Errorf("message %d role changed", i)
		}
		switch want := msgs[i].(type) {
		case core.System:
			if got := out[i].(core.System).Content; got != want.Content {
				t.Errorf("message %d changed: %q", i, got)
			}
		case core.User:
			got := out[i].(core.User)
			if got.Content != want.Content || len(got.Attachments) != 0 || len(got.Images) != 0 {
				t.Errorf("message %d changed: %+v", i, got)
			}
		}
	}
}

// TestLoopSurfacesAttachmentsToProvider pins the wiring: the provider
// the loop drives sees the rendered entry, and the messages the caller
// handed in do not grow it.
func TestLoopSurfacesAttachmentsToProvider(t *testing.T) {
	att := attachmentFixture(t, "report.csv", []byte("a,b\n1,2\n"))
	provider := agent.NewFakeProvider("test",
		core.ResponseChunk{Type: "response_chunk", Content: "columns a and b"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	user := core.User{Content: "what columns?", Attachments: []core.Attachment{att}}
	msgs := []core.Message{user}

	events := make(chan core.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: msgs,
			Events:   events,
		})
	}()
	for range events {
	}

	if len(provider.LastMessages) != 1 {
		t.Fatalf("provider saw %d messages, want 1", len(provider.LastMessages))
	}
	seen := provider.LastMessages[0].(core.User).Content
	for _, want := range []string{"name: report.csv", "content:\na,b\n1,2\n"} {
		if !strings.Contains(seen, want) {
			t.Errorf("provider message missing %q:\n%s", want, seen)
		}
	}
	if msgs[0].(core.User).Content != "what columns?" {
		t.Errorf("caller message mutated: %q", msgs[0].(core.User).Content)
	}
}

// TestLoopAttachmentsRenderOnceAcrossTurns: the render is per provider
// call on a copy, so a multi-turn run (tool call, then answer) hands
// the provider the block exactly once per turn - a decoration applied
// once to the loop's own history would double it on turn two.
func TestLoopAttachmentsRenderOnceAcrossTurns(t *testing.T) {
	att := attachmentFixture(t, "notes.md", []byte("# hi\n"))
	provider := agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"msg": "hi"}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "done"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	tools := map[string]core.Tool{"echo": {
		Description: "Echo back",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"msg": map[string]any{"type": "string"}}},
		Run: func(_ context.Context, args map[string]any) (string, error) {
			msg, _ := args["msg"].(string)
			return msg, nil
		},
	}}

	events := make(chan core.Event, 64)
	go func() {
		defer close(events)
		Loop{}.Run(context.Background(), agent.LoopOptions{
			Provider: provider,
			Messages: []core.Message{core.User{Content: "look", Attachments: []core.Attachment{att}}},
			Tools:    tools,
			Events:   events,
		})
	}()
	for range events {
	}

	// LastMessages is the SECOND call's input (one turn later).
	if n := strings.Count(provider.LastMessages[0].(core.User).Content, "--- attached file ---"); n != 1 {
		t.Fatalf("attachment block rendered %d times on turn 2, want 1:\n%s", n, provider.LastMessages[0].(core.User).Content)
	}
}
