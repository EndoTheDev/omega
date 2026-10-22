package tools

// Walk-rule tests for the module-owned /files surface (card
// t_88bb7dcf; behavior ported unchanged from the old modules/tui
// browser.go): the validation rules shared with the webui file
// surface, the scratch-home walk, and the 64 KiB preview shapes.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateRelPathMirrorsWebUI pins the validation rules the TUI
// shares with the webui file surface: no absolute paths, no empty
// segments, no dot-prefixed segments - hidden files and .. traversal
// die under one rule.
func TestValidateRelPathMirrorsWebUI(t *testing.T) {
	cases := []struct {
		in    string
		want  string
		error bool
	}{
		{"", "", false},
		{"config", "config", false},
		{"config/themes", "config/themes", false},
		{"/etc/passwd", "", true},
		{"config//themes", "", true}, // empty segment
		{"./local", "", true},        // dot-prefixed
		{"config/../etc", "", true},  // dot-prefixed segment
		{"config/.hidden", "", true}, // hidden files rejected like the webui
	}
	for _, c := range cases {
		got, err := validateRelPath(c.in)
		if got != c.want || (err != nil) != c.error {
			t.Errorf("validateRelPath(%q) = (%q, %v), want (%q, error=%v)", c.in, got, err, c.want, c.error)
		}
	}
}

// scratchFilesHome builds a directory tree shaped like an omega home:
// config/, skills/, themes/ directories and files at the root.
func scratchFilesHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	mk := func(rel, body string) {
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk(filepath.Join("config", "omega.yaml"), "provider: ollama\n")
	mk(filepath.Join("skills", "main.md"), "# skill\n")
	mk(filepath.Join("themes", "dark.css"), "body{}\n")
	mk("README.md", "hello\n")
	return home
}

// TestListDirWalksScratchHome exercises the /files walk on a scratch
// home: directories first, one level at a time, traversal rejected.
func TestListDirWalksScratchHome(t *testing.T) {
	home := scratchFilesHome(t)
	entries, err := listDirUnderRoot(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries listed")
	}
	if !entries[0].IsDir {
		t.Fatalf("expected a directory first, got %q", entries[0].Name)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = e.IsDir
	}
	for _, want := range []string{"config", "skills", "themes", "README.md"} {
		if _, ok := names[want]; !ok {
			t.Errorf("%q missing from root listing", want)
		}
	}
	// descend one level into config
	sub, err := listDirUnderRoot(home, "config")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) == 0 || sub[0].Name != "omega.yaml" || sub[0].IsDir {
		t.Fatalf("config listing = %v", sub)
	}
	// traversal dies at validation, as in the webui
	if _, err := listDirUnderRoot(home, "../"); err == nil {
		t.Error("../ accepted")
	}
}

// TestListDirSymlinkEscape rejects a symlink pointing outside the root.
func TestListDirSymlinkEscape(t *testing.T) {
	home := scratchFilesHome(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := listDirUnderRoot(home, "link"); err == nil {
		t.Fatal("symlink escape accepted")
	}
}

// TestFilesPreviewCappedAndBinary checks the 64 KiB cut and the
// binary flag, mirroring the webui preview shapes.
func TestFilesPreviewCappedAndBinary(t *testing.T) {
	home := t.TempDir()
	big := strings.Repeat("x", filesPreviewCap+100)
	if err := os.WriteFile(filepath.Join(home, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	content, _, truncated, binary, err := readPreviewAt(home, "big.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || binary {
		t.Fatalf("truncated=%v binary=%v, want true/false", truncated, binary)
	}
	if len(content) != filesPreviewCap {
		t.Fatalf("preview len = %d, want %d", len(content), filesPreviewCap)
	}
	if err := os.WriteFile(filepath.Join(home, "blob.bin"), []byte{0xff, 0xfe, 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, _, binary, err = readPreviewAt(home, "blob.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !binary {
		t.Error("binary file not flagged")
	}
}