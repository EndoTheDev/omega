package loop

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/EndoTheDev/omega/core"
)

// Non-image attachment context: the composer's upload arrives as a reference on the
// user message (core.User.Attachments), never as bytes, and the provider wire carries
// no attachment field. Small plain-text files are ALSO inlined - the one exception to
// reference-only - because a tool round trip for a 200-byte CSV is pure latency.

// inlineAttachmentMaxBytes caps the content that is inlined into the message body.
const inlineAttachmentMaxBytes = 8 * 1024

// inlineAttachmentKinds are the media labels whose content is inlined when small.
var inlineAttachmentKinds = map[string]bool{
	"md":   true,
	"txt":  true,
	"csv":  true,
	"json": true,
}

// AttachmentContext returns the model-facing copy of a run's messages. The input is
// never mutated - the returned slice is fresh and only the user messages with
// attachments are replaced, so the loop's own history and any store-bound slice stay
// reference-only.
func AttachmentContext(messages []core.Message) []core.Message {
	has := false
	for _, msg := range messages {
		if u, ok := msg.(core.User); ok && len(u.Attachments) > 0 {
			has = true
			break
		}
	}
	if !has {
		return messages
	}
	out := make([]core.Message, len(messages))
	copy(out, messages)
	for i := range out {
		u, ok := out[i].(core.User)
		if !ok || len(u.Attachments) == 0 {
			continue
		}
		u.Content = withAttachmentContext(u.Content, u.Attachments)
		out[i] = u
	}
	return out
}

// withAttachmentContext appends one structured block per attachment to the message.
func withAttachmentContext(content string, atts []core.Attachment) string {
	var b strings.Builder
	b.WriteString(content)
	for _, att := range atts {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		writeAttachmentEntry(&b, att)
	}
	return b.String()
}

// writeAttachmentEntry renders one reference.
func writeAttachmentEntry(b *strings.Builder, att core.Attachment) {
	name := att.Name
	if name == "" {
		name = filepath.Base(att.Path)
	}
	kind := att.Kind
	if kind == "" {
		kind = attachmentKind(name)
	}
	b.WriteString("--- attached file ---\n")
	fmt.Fprintf(b, "name: %s\n", name)
	fmt.Fprintf(b, "type: %s\n", kind)
	fmt.Fprintf(b, "size: %d bytes\n", att.Size)
	fmt.Fprintf(b, "path: %s\n", att.Path)

	text, ok := inlineContent(att, kind)
	if !ok {
		if inlineKind(kind) {
			b.WriteString("content: not inlined (over 8 KB or not plain UTF-8) - read it with files.read\n")
		} else {
			b.WriteString("content: not inlined - read it with files.read\n")
		}
		return
	}
	b.WriteString("content:\n")
	b.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("--- end attached file content ---\n")
}

// inlineContent returns the file's text when it is one of the inlined kinds and fits.
func inlineContent(att core.Attachment, kind string) (string, bool) {
	if !inlineKind(kind) {
		return "", false
	}
	if att.Path == "" || att.Size > inlineAttachmentMaxBytes {
		return "", false
	}
	data, err := os.ReadFile(att.Path)
	if err != nil || len(data) > inlineAttachmentMaxBytes {
		return "", false
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", false
	}
	return string(data), true
}

// inlineKind reports whether the media label is inlined when small.
func inlineKind(kind string) bool {
	return inlineAttachmentKinds[strings.ToLower(kind)]
}

// attachmentKind derives the media label from a filename.
func attachmentKind(name string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if ext == "" {
		return "file"
	}
	return ext
}
