package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockStore is a minimal StoreProvider for testing @session: injection.
// Only GetActiveMessages has configurable behavior; the rest are no-ops.
type mockStore struct {
	messages []Message
	err      error
}

func (m *mockStore) Open(string) error                                           { return nil }
func (m *mockStore) Close() error                                                { return nil }
func (m *mockStore) CreateSession(context.Context, string, string, string) error { return nil }
func (m *mockStore) GetSession(context.Context, string) (Session, error) {
	return Session{}, nil
}
func (m *mockStore) ListSessions(context.Context) ([]Session, error)        { return nil, nil }
func (m *mockStore) DeleteSession(context.Context, string) error            { return nil }
func (m *mockStore) UpdateSession(context.Context, string, string) error    { return nil }
func (m *mockStore) AppendMessage(context.Context, string, Message) error   { return nil }
func (m *mockStore) GetMessages(context.Context, string) ([]Message, error) { return nil, nil }
func (m *mockStore) GetMessagesWithTimes(context.Context, string) ([]StoredMessage, error) {
	return nil, nil
}

var _ StoreProvider = (*mockStore)(nil)

func (m *mockStore) GetActiveView(context.Context, string) (ActiveView, error) {
	return ActiveView{}, m.err
}

func (m *mockStore) GetActiveMessages(context.Context, string) ([]Message, error) {
	return m.messages, m.err
}
func (m *mockStore) GetSessionTree(context.Context) ([]*SessionNode, error)  { return nil, nil }
func (m *mockStore) GetAncestorMessages(context.Context, string) ([]Message, error) {
	return nil, nil
}
func (m *mockStore) SearchMessages(context.Context, string) ([]SearchResult, error) {
	return nil, nil
}
func (m *mockStore) SearchSessions(context.Context, string) ([]SearchResult, error) {
	return nil, nil
}
func (m *mockStore) ComputeInsights(context.Context, int) (*Insights, error) { return nil, nil }
func (m *mockStore) CountMessages(context.Context, string) (int, error)      { return 0, nil }
func (m *mockStore) SessionCounts(context.Context) (map[string]SessionCounts, error) {
	return nil, nil
}
func (m *mockStore) AddSessionTokens(context.Context, string, int, int) error { return nil }
func (m *mockStore) SetSessionTitle(context.Context, string, string, bool) error {
	return nil
}
func (m *mockStore) SetSessionLastRun(context.Context, string, *LastRunStats) error {
	return nil
}
func (m *mockStore) SetSessionPinned(context.Context, string, bool) error { return nil }
func (m *mockStore) SetSessionRead(context.Context, string) error          { return nil }
func (m *mockStore) SetSessionArchived(context.Context, string, bool) error {
	return nil
}
func (m *mockStore) SessionModels(context.Context) (map[string]string, error) {
	return nil, nil
}

// testMessageText is the MessageText seam the tests supply (core is
// stdlib-only, so it cannot import the agent package's real one).
func testMessageText(m Message) string {
	switch v := m.(type) {
	case System:
		return v.Content
	case User:
		return v.Content
	case Assistant:
		return v.Content
	case ToolResult:
		return v.Content
	}
	return ""
}

// testDetectImage is the DetectImage seam: magic-byte PNG/JPEG sniffing
// with no ai import (core tests are stdlib-only like core itself).
func testDetectImage(path string) (*ImageContent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	media := ""
	if len(data) >= 4 && string(data[:4]) == "\x89PNG" {
		media = "image/png"
	} else if len(data) >= 3 && string(data[:3]) == "\xff\xd8\xff" {
		media = "image/jpeg"
	}
	if media == "" {
		return nil, nil // not an image
	}
	return &ImageContent{MediaType: media, Base64: base64.StdEncoding.EncodeToString(data)}, nil
}

func env(store StoreProvider, skills []Skill) AtMentionEnv {
	return AtMentionEnv{Store: store, Skills: skills, DetectImage: testDetectImage, MessageText: testMessageText}
}

func TestUserWithImagesRoundTrip(t *testing.T) {
	original := NewUserWithImages("describe this", []ImageContent{
		{MediaType: "image/png", Base64: "iVBORw0KGgo="},
	})
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded User
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Content != "describe this" {
		t.Fatalf("content = %q, want 'describe this'", decoded.Content)
	}
	if len(decoded.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(decoded.Images))
	}
	if decoded.Images[0].MediaType != "image/png" {
		t.Fatalf("mediaType = %q, want image/png", decoded.Images[0].MediaType)
	}
	if decoded.Images[0].Base64 != "iVBORw0KGgo=" {
		t.Fatalf("base64 = %q, want iVBORw0KGgo=", decoded.Images[0].Base64)
	}
}

func writePNG(t *testing.T, path string) {
	t.Helper()
	pngHeader := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(path, pngHeader, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestExpandAtMentionsWithImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	writePNG(t, path)
	input := "what is @" + path + " showing?"
	prompt, images, _, err := ExpandAtMentions(input, env(nil, nil))
	if err != nil {
		t.Fatalf("ExpandAtMentions: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("images = %d, want 1", len(images))
	}
	if images[0].MediaType != "image/png" {
		t.Fatalf("mediaType = %q, want image/png", images[0].MediaType)
	}
	if strings.Contains(prompt, "@"+path) {
		t.Fatalf("prompt should not contain @path, got %q", prompt)
	}
	if !strings.Contains(prompt, "[image/png: ") {
		t.Fatalf("prompt = %q, want the image placeholder", prompt)
	}
}

func TestExpandAtMentionsNoFile(t *testing.T) {
	input := "email me at user@gmail.com please"
	prompt, images, _, err := ExpandAtMentions(input, env(nil, nil))
	if err != nil {
		t.Fatalf("ExpandAtMentions: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("images = %d, want 0", len(images))
	}
	if prompt != input {
		t.Fatalf("prompt = %q, want unchanged %q", prompt, input)
	}
}

func TestExpandAtMentionsWithTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("read this"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	input := "summarize @" + path
	prompt, images, _, err := ExpandAtMentions(input, env(nil, nil))
	if err != nil {
		t.Fatalf("ExpandAtMentions: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("images = %d, want 0", len(images))
	}
	if !strings.Contains(prompt, "read this") {
		t.Fatalf("prompt = %q, want inlined file content", prompt)
	}
	if !strings.Contains(prompt, "summarize") {
		t.Fatalf("prompt = %q, want 'summarize'", prompt)
	}
}

func TestExpandAtMentionsMultipleImages(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "a.png")
	path2 := filepath.Join(dir, "b.jpg")
	writePNG(t, path1)
	if err := os.WriteFile(path2, []byte{0xff, 0xd8, 0xff, 0xe0}, 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}
	input := "compare @" + path1 + " and @" + path2
	prompt, images, _, err := ExpandAtMentions(input, env(nil, nil))
	if err != nil {
		t.Fatalf("ExpandAtMentions: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("images = %d, want 2", len(images))
	}
	if !strings.Contains(prompt, "compare") || !strings.Contains(prompt, "and") {
		t.Fatalf("prompt = %q, want preserved text", prompt)
	}
}

func TestExpandAtMentionsNoTokens(t *testing.T) {
	input := "just a regular message"
	prompt, images, _, err := ExpandAtMentions(input, env(nil, nil))
	if err != nil {
		t.Fatalf("ExpandAtMentions: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("images = %d, want 0", len(images))
	}
	if prompt != input {
		t.Fatalf("prompt = %q, want %q", prompt, input)
	}
}

// --- branch coverage tests ---

func TestExpandAtMentionsSkillInjection(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		skills []Skill
		want   string
	}{
		{
			name:   "skill found",
			input:  "use @skill:foo here",
			skills: []Skill{{Name: "foo", Content: "skill body text"}},
			want:   "use skill body text here",
		},
		{
			name:   "skill not found leaves token",
			input:  "use @skill:bar here",
			skills: []Skill{{Name: "foo", Content: "skill body text"}},
			want:   "use @skill:bar here",
		},
		{
			name:   "skill not found with empty skills",
			input:  "use @skill:foo here",
			skills: nil,
			want:   "use @skill:foo here",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt, images, _, err := ExpandAtMentions(tt.input, env(nil, tt.skills))
			if err != nil {
				t.Fatalf("ExpandAtMentions: %v", err)
			}
			if len(images) != 0 {
				t.Fatalf("images = %d, want 0", len(images))
			}
			if prompt != tt.want {
				t.Fatalf("prompt = %q, want %q", prompt, tt.want)
			}
		})
	}
}

func TestExpandAtMentionsSessionInjection(t *testing.T) {
	t.Run("messages returned", func(t *testing.T) {
		store := &mockStore{
			messages: []Message{
				NewUser("hello there"),
				NewAssistant("hi back"),
			},
		}
		input := "ctx @session:abc please"
		prompt, images, _, err := ExpandAtMentions(input, env(store, nil))
		if err != nil {
			t.Fatalf("ExpandAtMentions: %v", err)
		}
		if len(images) != 0 {
			t.Fatalf("images = %d, want 0", len(images))
		}
		if !strings.Contains(prompt, "[user] hello there") {
			t.Fatalf("prompt %q missing [user] hello there", prompt)
		}
		if !strings.Contains(prompt, "[assistant] hi back") {
			t.Fatalf("prompt %q missing [assistant] hi back", prompt)
		}
		if !strings.Contains(prompt, "ctx") || !strings.Contains(prompt, "please") {
			t.Fatalf("prompt %q should preserve surrounding text", prompt)
		}
	})

	t.Run("store error leaves token", func(t *testing.T) {
		store := &mockStore{err: errors.New("db down")}
		input := "ctx @session:abc please"
		prompt, images, _, err := ExpandAtMentions(input, env(store, nil))
		if err != nil {
			t.Fatalf("ExpandAtMentions: %v", err)
		}
		if len(images) != 0 {
			t.Fatalf("images = %d, want 0", len(images))
		}
		if !strings.Contains(prompt, "@session:abc") {
			t.Fatalf("prompt %q should contain @session:abc token", prompt)
		}
	})

	t.Run("nil store leaves token", func(t *testing.T) {
		input := "ctx @session:abc please"
		prompt, images, _, err := ExpandAtMentions(input, env(nil, nil))
		if err != nil {
			t.Fatalf("ExpandAtMentions: %v", err)
		}
		if len(images) != 0 {
			t.Fatalf("images = %d, want 0", len(images))
		}
		if prompt != input {
			t.Fatalf("prompt = %q, want %q (nil store)", prompt, input)
		}
	})
}

func TestExpandAtMentionsGlobExpansion(t *testing.T) {
	t.Run("glob matches inlined", func(t *testing.T) {
		dir := t.TempDir()
		a := filepath.Join(dir, "a.go")
		b := filepath.Join(dir, "b.go")
		if err := os.WriteFile(a, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write a: %v", err)
		}
		if err := os.WriteFile(b, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write b: %v", err)
		}
		pattern := filepath.Join(dir, "*.go")
		input := "review @" + pattern + " now"
		prompt, images, _, err := ExpandAtMentions(input, env(nil, nil))
		if err != nil {
			t.Fatalf("ExpandAtMentions: %v", err)
		}
		if len(images) != 0 {
			t.Fatalf("images = %d, want 0", len(images))
		}
		if !strings.Contains(prompt, "--- ") || !strings.Contains(prompt, "a.go") || !strings.Contains(prompt, "b.go") {
			t.Fatalf("prompt %q should inline glob matches", prompt)
		}
		if !strings.Contains(prompt, "review") || !strings.Contains(prompt, "now") {
			t.Fatalf("prompt %q should preserve surrounding text", prompt)
		}
	})

	t.Run("glob no matches leaves token", func(t *testing.T) {
		dir := t.TempDir()
		pattern := filepath.Join(dir, "*.nomatch")
		input := "review @" + pattern + " now"
		prompt, images, _, err := ExpandAtMentions(input, env(nil, nil))
		if err != nil {
			t.Fatalf("ExpandAtMentions: %v", err)
		}
		if len(images) != 0 {
			t.Fatalf("images = %d, want 0", len(images))
		}
		if !strings.Contains(prompt, "@"+pattern) {
			t.Fatalf("prompt %q should contain original token", prompt)
		}
	})
}

func TestExpandAtMentionsDirectoryToken(t *testing.T) {
	dir := t.TempDir()
	input := "read @" + dir + " now"
	prompt, images, _, err := ExpandAtMentions(input, env(nil, nil))
	if err != nil {
		t.Fatalf("ExpandAtMentions: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("images = %d, want 0", len(images))
	}
	if !strings.Contains(prompt, "@"+dir) {
		t.Fatalf("prompt %q should leave directory token as-is", prompt)
	}
}

// Dir roots relative paths and globs - the webui's workspace-scoping
// seam. Absolute tokens ignore Dir; relative ones resolve against it.
func TestExpandAtMentionsDirBase(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "ws")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "notes.txt"), []byte("inlined body"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	writePNG(t, filepath.Join(root, "outside.png"))

	t.Run("relative text file roots at Dir", func(t *testing.T) {
		e := env(nil, nil)
		e.Dir = sub
		prompt, images, _, err := ExpandAtMentions("see @notes.txt", e)
		if err != nil {
			t.Fatalf("ExpandAtMentions: %v", err)
		}
		if len(images) != 0 {
			t.Fatalf("images = %d, want 0", len(images))
		}
		if !strings.Contains(prompt, "inlined body") {
			t.Fatalf("prompt = %q, want inlined content", prompt)
		}
	})

	t.Run("relative image roots at Dir, token stays relative", func(t *testing.T) {
		e := env(nil, nil)
		e.Dir = sub
		if err := os.WriteFile(filepath.Join(sub, "pic.png"), []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		prompt, images, _, err := ExpandAtMentions("look at @pic.png", e)
		if err != nil {
			t.Fatalf("ExpandAtMentions: %v", err)
		}
		if len(images) != 1 || images[0].MediaType != "image/png" {
			t.Fatalf("images = %+v, want one png", images)
		}
		if !strings.Contains(prompt, "[image/png: pic.png]") {
			t.Fatalf("prompt = %q, want relative placeholder", prompt)
		}
	})

	t.Run("relative glob roots at Dir", func(t *testing.T) {
		e := env(nil, nil)
		e.Dir = sub
		if err := os.WriteFile(filepath.Join(sub, "a.md"), []byte("alpha"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sub, "b.md"), []byte("beta"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		prompt, images, _, err := ExpandAtMentions("read @*.md", e)
		if err != nil {
			t.Fatalf("ExpandAtMentions: %v", err)
		}
		if len(images) != 0 {
			t.Fatalf("images = %d, want 0", len(images))
		}
		if !strings.Contains(prompt, "alpha") || !strings.Contains(prompt, "beta") {
			t.Fatalf("prompt = %q, want glob contents", prompt)
		}
	})

	t.Run("absolute token ignores Dir", func(t *testing.T) {
		e := env(nil, nil)
		e.Dir = sub
		input := "abs @" + filepath.Join(root, "outside.png")
		prompt, images, _, err := ExpandAtMentions(input, e)
		if err != nil {
			t.Fatalf("ExpandAtMentions: %v", err)
		}
		if len(images) != 1 {
			t.Fatalf("images = %d, want 1 (Dir must not block absolute paths)", len(images))
		}
		if !strings.Contains(prompt, "[image/png: ") {
			t.Fatalf("prompt = %q, want image placeholder", prompt)
		}
	})

	t.Run("unresolvable relative token stays", func(t *testing.T) {
		e := env(nil, nil)
		e.Dir = sub
		input := "ghost @ghost.md stays"
		prompt, images, _, err := ExpandAtMentions(input, e)
		if err != nil {
			t.Fatalf("ExpandAtMentions: %v", err)
		}
		if len(images) != 0 || prompt != input {
			t.Fatalf("prompt = %q, want unchanged %q", prompt, input)
		}
	})
}

// DetectImage read failure surfaces as the error return, token as-is -
// the same notice the TUI rendered.
func TestExpandAtMentionsDetectImageError(t *testing.T) {
	e := env(nil, nil)
	called := false
	e.DetectImage = func(path string) (*ImageContent, error) {
		called = true
		return nil, fmt.Errorf("read %s: boom", path)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	writePNG(t, path)
	input := "see @" + path
	_, images, _, err := ExpandAtMentions(input, e)
	if !called {
		t.Fatal("DetectImage was not consulted")
	}
	if err == nil {
		t.Fatal("want the detect error surfaced")
	}
	if len(images) != 0 {
		t.Fatalf("images = %d, want 0", len(images))
	}
}
// --- unresolved reporting (the TUI notice law; ported from the TUI's
// image_test.go when the expansion law moved into core, card t_07aed3f9) ---

// TestExpandAtMentionsUnresolvedReports verifies a standalone unresolvable
// token rides along literally AND is reported so the caller can notice it.
func TestExpandAtMentionsUnresolvedReports(t *testing.T) {
	input := "check @nonexistent.txt please"
	prompt, images, unresolved, err := ExpandAtMentions(input, AtMentionEnv{})
	if err != nil {
		t.Fatalf("ExpandAtMentions: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("images = %d, want 0", len(images))
	}
	if prompt != input {
		t.Fatalf("prompt = %q, want unchanged %q", prompt, input)
	}
	if len(unresolved) != 1 || unresolved[0] != "@nonexistent.txt" {
		t.Fatalf("unresolved = %v, want [@nonexistent.txt]", unresolved)
	}
}

// TestExpandAtMentionsMidWordNotUnresolved verifies a mid-word @ (email
// address, handle) resolves nothing and is never reported: it is a
// name, not a file reference.
func TestExpandAtMentionsMidWordNotUnresolved(t *testing.T) {
	input := "email me at user@gmail.com please"
	_, _, unresolved, err := ExpandAtMentions(input, AtMentionEnv{})
	if err != nil {
		t.Fatalf("ExpandAtMentions: %v", err)
	}
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %v, want none for a mid-word @", unresolved)
	}
}

// TestExpandAtMentionsUnresolvedPrefixedTokens verifies the @-namespaced
// species report their misses like plain paths do.
func TestExpandAtMentionsUnresolvedPrefixedTokens(t *testing.T) {
	input := "@skill:nope and @session:gone and @*.nomatch"
	_, _, unresolved, err := ExpandAtMentions(input, AtMentionEnv{})
	if err != nil {
		t.Fatalf("ExpandAtMentions: %v", err)
	}
	want := map[string]bool{"@skill:nope": false, "@session:gone": false, "@*.nomatch": false}
	for _, tok := range unresolved {
		if _, ok := want[tok]; !ok {
			t.Fatalf("unexpected unresolved token %q (full list %v)", tok, unresolved)
		}
		want[tok] = true
	}
	for tok, seen := range want {
		if !seen {
			t.Fatalf("missing unresolved token %q (full list %v)", tok, unresolved)
		}
	}
}

// TestExpandAtMentionsResolvedNotUnresolved verifies resolved tokens never
// land in the unresolved list alongside literal ones.
func TestExpandAtMentionsResolvedNotUnresolved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	input := "@" + path + " vs @missing.txt"
	prompt, _, unresolved, err := ExpandAtMentions(input, AtMentionEnv{})
	if err != nil {
		t.Fatalf("ExpandAtMentions: %v", err)
	}
	if len(unresolved) != 1 || unresolved[0] != "@missing.txt" {
		t.Fatalf("unresolved = %v, want [@missing.txt]", unresolved)
	}
	if !strings.Contains(prompt, "content") {
		t.Fatalf("prompt = %q, want resolved @%s inlined", prompt, path)
	}
	if !strings.Contains(prompt, "@missing.txt") {
		t.Fatalf("prompt = %q, want the missing token left literal", prompt)
	}
}
