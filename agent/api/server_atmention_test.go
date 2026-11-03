package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// Submit-path expansion (t_07aed3f9, re-ruled by card t_7f09bd0c): the
// last incoming user message is the prompt; the webui attaches PATHS -
// file tokens ride through as references, nothing inlines or attaches,
// and the pill'd token survives in the persisted text.
func TestExpandAtMentionsReferencesRide(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("inlined body"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(filepath.Join(dir, "chart.png"), png, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	s := NewServer(nil, nil, nil, nil, nil, nil)
	inner := "see @chart.png and @notes.txt and @missing.bin"
	incoming := []core.Message{
		core.NewUser("earlier question"),
		core.NewUser(inner),
	}
	out := s.expandAtMentions(incoming, dir)
	last, ok := out[len(out)-1].(core.User)
	if !ok {
		t.Fatalf("last message = %T, want User", out[len(out)-1])
	}
	if got := last.Content; got != inner {
		t.Fatalf("content = %q, want the tokens untouched %q", got, inner)
	}
	if len(last.Images) != 0 {
		t.Fatalf("images = %+v, want none - the path reference law attaches nothing", last.Images)
	}
	first, ok := out[0].(core.User)
	if !ok || first.Content != "earlier question" {
		t.Fatalf("earlier messages must ride through untouched, got %v", out[0])
	}
}

// A non-user last message (or no @, or empty input) rides through
// unchanged.
func TestExpandAtMentionsNoOp(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
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
// them, the way every submit surface leaves failing tokens.
func TestExpandAtMentionsUnresolvableStay(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
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
// session case. Under t_7f09bd0c the webui attaches PATHS: an
// unresolvable token stays exactly as written either way; the ride
// through is the same on the workspace-scoped path.
func TestExpandAtMentionsDirWorkspace(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "spec.md"), []byte("the spec body"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := NewServer(nil, nil, nil, nil, nil, nil)
	out := s.expandAtMentions([]core.Message{core.NewUser("read @spec.md")}, ws)
	got := out[0].(core.User).Content
	if got != "read @spec.md" {
		t.Fatalf("content = %q, want the token riding as a reference", got)
	}
}
