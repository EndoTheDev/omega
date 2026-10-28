package http_channel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// Submit-path expansion (card t_07aed3f9): the last incoming user
// message is the prompt; image tokens become ImageContent, text tokens
// inline, and relative paths root at the session workspace.
func TestExpandAtMentionsLastUserMessage(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	imgPath := filepath.Join(dir, "chart.png")
	if err := os.WriteFile(imgPath, png, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	txtPath := writeFile("notes.txt", "inlined body")

	s := NewServer(nil, nil, nil, nil, nil)
	incoming := []core.Message{
		core.NewUser("earlier question"),
		core.NewUser("see @" + imgPath + " and @" + txtPath),
	}
	out := s.expandAtMentions(incoming, dir)
	last, ok := out[len(out)-1].(core.User)
	if !ok {
		t.Fatalf("last message = %T, want User", out[len(out)-1])
	}
	if len(last.Images) != 1 || last.Images[0].MediaType != "image/png" {
		t.Fatalf("images = %+v, want one png", last.Images)
	}
	if !strings.Contains(last.Content, "inlined body") {
		t.Fatalf("prompt = %q, want the text file inlined", last.Content)
	}
	if strings.Contains(last.Content, "@"+imgPath) {
		t.Fatalf("prompt = %q, want the image token consumed", last.Content)
	}
	first, ok := out[0].(core.User)
	if !ok || first.Content != "earlier question" {
		t.Fatalf("earlier messages must ride through untouched, got %v", out[0])
	}
}

// A non-user last message (or no @, or empty input) rides through
// unchanged.
func TestExpandAtMentionsNoOp(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil)
	incoming := []core.Message{core.NewSystem("system preamble")}
	out := s.expandAtMentions(incoming, "")
	if len(out) != 1 || out[0] != incoming[0] {
		t.Fatalf("non-user last message must pass through, got %v", out)
	}
	empty := s.expandAtMentions(nil, "")
	if empty != nil {
		t.Fatalf("empty input must stay empty, got %v", empty)
	}
	plain := []core.Message{core.NewUser("no mention here")}
	out = s.expandAtMentions(plain, "")
	if len(out) != 1 || out[0].(core.User).Content != plain[0].(core.User).Content {
		t.Fatalf("plain input must pass through, got %v", out)
	}
}

// Unresolvable tokens stay in the text - the persisted prompt carries
// them, the way the TUI notice path left failing tokens.
func TestExpandAtMentionsUnresolvableStay(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil)
	incoming := []core.Message{core.NewUser("ghost @ghost.md stays, url user@x.io too")}
	out := s.expandAtMentions(incoming, "")
	got := out[0].(core.User).Content
	if got != incoming[0].(core.User).Content {
		t.Fatalf("content = %q, want unchanged %q", got, incoming[0].(core.User).Content)
	}
	if len(out[0].(core.User).Images) != 0 {
		t.Fatalf("images = %d, want 0", len(out[0].(core.User).Images))
	}
}

// Relative paths root at the dir argument - the workspace-scoped
// session case. Without dir they land on the process cwd.
func TestExpandAtMentionsDirWorkspace(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "spec.md"), []byte("the spec body"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := NewServer(nil, nil, nil, nil, nil)
	out := s.expandAtMentions([]core.Message{core.NewUser("read @spec.md")}, ws)
	got := out[0].(core.User).Content
	if !strings.Contains(got, "the spec body") {
		t.Fatalf("content = %q, want workspace-rooted inline", got)
	}
}