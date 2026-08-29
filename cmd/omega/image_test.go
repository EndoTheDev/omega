package main

import (
	"os"
	"path/filepath"
	"testing"

)

func TestParseFileArgsTextOnly(t *testing.T) {
	prompt, images, err := parseFileArgs([]string{"hello", "world"})
	if err != nil {
		t.Fatalf("parseFileArgs: %v", err)
	}
	if prompt != "hello world" {
		t.Fatalf("prompt = %q, want \"hello world\"", prompt)
	}
	if len(images) != 0 {
		t.Fatalf("images = %v, want empty", images)
	}
}

func TestParseFileArgsWithImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	pngHeader := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(path, pngHeader, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	prompt, images, err := parseFileArgs([]string{"@" + path, "what is this?"})
	if err != nil {
		t.Fatalf("parseFileArgs: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("images = %d, want 1", len(images))
	}
	if images[0].MediaType != "image/png" {
		t.Fatalf("mediaType = %q, want image/png", images[0].MediaType)
	}
	if !contains(prompt, "image/png") {
		t.Fatalf("prompt missing image reference: %q", prompt)
	}
}

func TestParseFileArgsWithTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("remember this"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	prompt, images, err := parseFileArgs([]string{"@" + path, "summarize"})
	if err != nil {
		t.Fatalf("parseFileArgs: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("images = %d, want 0", len(images))
	}
	if !contains(prompt, "remember this") {
		t.Fatalf("prompt missing file content: %q", prompt)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}