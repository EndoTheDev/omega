package provider

import (
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// TestAttachmentsStayOffTheProviderWire pins the card t_1ffd07de rule:
// a non-image attachment is a local disk reference. Strict wire
// validators reject unknown fields and a path means nothing to a remote
// model, so the serialization drops it - both argument encodings, so no
// provider lane can leak it.
func TestAttachmentsStayOffTheProviderWire(t *testing.T) {
	msgs := []core.Message{core.User{
		Content: "look at this",
		Attachments: []core.Attachment{
			{Kind: "md", Name: "notes.md", Size: 12, Path: "/home/u/.omega/uploads/s1/notes.md"},
		},
	}}
	for _, stringArgs := range []bool{false, true} {
		wire := messagesToJSON(msgs, stringArgs)
		if _, leaked := wire[0]["attachments"]; leaked {
			t.Fatalf("attachments reached the provider wire (stringArgs=%v): %v", stringArgs, wire[0])
		}
		if wire[0]["content"] != "look at this" {
			t.Fatalf("content = %v, want the message text", wire[0]["content"])
		}
	}
}
