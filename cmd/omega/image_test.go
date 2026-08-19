package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
)

func TestDetectImagePNG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	// Minimal PNG header: \x89PNG\r\n\x1a\n + IHDR chunk
	pngHeader := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(path, pngHeader, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	img, err := detectImage(path)
	if err != nil {
		t.Fatalf("detectImage: %v", err)
	}
	if img == nil {
		t.Fatal("expected image, got nil")
	}
	if img.MediaType != "image/png" {
		t.Fatalf("mediaType = %q, want image/png", img.MediaType)
	}
	if img.Base64 == "" {
		t.Fatal("expected non-empty base64")
	}
}

func TestDetectImageJPEG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jpg")
	jpegHeader := []byte{0xff, 0xd8, 0xff, 0xe0}
	if err := os.WriteFile(path, jpegHeader, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	img, err := detectImage(path)
	if err != nil {
		t.Fatalf("detectImage: %v", err)
	}
	if img == nil {
		t.Fatal("expected image, got nil")
	}
	if img.MediaType != "image/jpeg" {
		t.Fatalf("mediaType = %q, want image/jpeg", img.MediaType)
	}
}

func TestDetectImageNotImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	img, err := detectImage(path)
	if err != nil {
		t.Fatalf("detectImage: %v", err)
	}
	if img != nil {
		t.Fatalf("expected nil for text file, got %+v", img)
	}
}

func TestParseFileArgsTextOnly(t *testing.T) {
	prompt, images, err := parseFileArgs([]string{"hello", "world"})
	if err != nil {
		t.Fatalf("parseFileArgs: %v", err)
	}
	if prompt != "hello world" {
		t.Fatalf("prompt = %q, want 'hello world'", prompt)
	}
	if len(images) != 0 {
		t.Fatalf("expected 0 images, got %d", len(images))
	}
}

func TestParseFileArgsWithImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	pngHeader := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(path, pngHeader, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	prompt, images, err := parseFileArgs([]string{"@" + path, "what is this?"})
	if err != nil {
		t.Fatalf("parseFileArgs: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MediaType != "image/png" {
		t.Fatalf("mediaType = %q, want image/png", images[0].MediaType)
	}
	if !strings.Contains(prompt, "what is this?") {
		t.Fatalf("prompt = %q, want it to contain 'what is this?'", prompt)
	}
}

func TestParseFileArgsWithTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("read this content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	prompt, images, err := parseFileArgs([]string{"@" + path, "summarize"})
	if err != nil {
		t.Fatalf("parseFileArgs: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("expected 0 images, got %d", len(images))
	}
	if !strings.Contains(prompt, "read this content") {
		t.Fatalf("prompt = %q, want it to contain file content", prompt)
	}
	if !strings.Contains(prompt, "summarize") {
		t.Fatalf("prompt = %q, want it to contain 'summarize'", prompt)
	}
}

// TestUserWithImagesRoundTrip verifies that a User message with Images
// survives JSON marshal/unmarshal (store round-trip).
func TestUserWithImagesRoundTrip(t *testing.T) {
	original := ai.NewUserWithImages("describe this", []ai.ImageContent{
		{MediaType: "image/png", Base64: "iVBORw0KGgo="},
	})
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ai.User
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

func TestExtractImagesWithImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	pngHeader := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(path, pngHeader, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	input := "what is @" + path + " showing?"
	prompt, images, err := extractImages(input)
	if err != nil {
		t.Fatalf("extractImages: %v", err)
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
	if !strings.Contains(prompt, "what is") || !strings.Contains(prompt, "showing?") {
		t.Fatalf("prompt = %q, want it to preserve surrounding text", prompt)
	}
}

func TestExtractImagesNoFile(t *testing.T) {
	input := "email me at user@gmail.com please"
	prompt, images, err := extractImages(input)
	if err != nil {
		t.Fatalf("extractImages: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("images = %d, want 0", len(images))
	}
	if prompt != input {
		t.Fatalf("prompt = %q, want unchanged %q", prompt, input)
	}
}

func TestExtractImagesWithTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("read this"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	input := "summarize @" + path
	prompt, images, err := extractImages(input)
	if err != nil {
		t.Fatalf("extractImages: %v", err)
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

func TestExtractImagesMultipleImages(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "a.png")
	path2 := filepath.Join(dir, "b.jpg")
	pngHeader := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	jpegHeader := []byte{0xff, 0xd8, 0xff, 0xe0}
	os.WriteFile(path1, pngHeader, 0o644)
	os.WriteFile(path2, jpegHeader, 0o644)

	input := "compare @" + path1 + " and @" + path2
	prompt, images, err := extractImages(input)
	if err != nil {
		t.Fatalf("extractImages: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("images = %d, want 2", len(images))
	}
	if !strings.Contains(prompt, "compare") || !strings.Contains(prompt, "and") {
		t.Fatalf("prompt = %q, want preserved text", prompt)
	}
}

func TestExtractImagesNoTokens(t *testing.T) {
	input := "just a regular message"
	prompt, images, err := extractImages(input)
	if err != nil {
		t.Fatalf("extractImages: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("images = %d, want 0", len(images))
	}
	if prompt != input {
		t.Fatalf("prompt = %q, want %q", prompt, input)
	}
}

func TestHighlightCodeGo(t *testing.T) {
	code := "func main() {\n    fmt.Println(\"hello\")\n}"
	result := highlightCode(code, "go", "dark")
	if result == code {
		t.Fatal("expected highlighted output to differ from input")
	}
	// Should contain ANSI escape codes.
	if !strings.Contains(result, "\x1b[") {
		t.Fatal("expected ANSI escape codes in highlighted output")
	}
}

func TestHighlightCodeEmptyLang(t *testing.T) {
	code := "plain text content"
	result := highlightCode(code, "", "dark")
	if result != code {
		t.Fatalf("expected unchanged output for empty lang, got %q", result)
	}
}

func TestHighlightCodeInvalidLang(t *testing.T) {
	code := "some text"
	result := highlightCode(code, "nonexistent-lang-xyz", "dark")
	// Should fall back to plain text.
	if result == "" {
		t.Fatal("expected non-empty fallback")
	}
}

func TestLangForTool(t *testing.T) {
	tests := []struct {
		toolName string
		args     map[string]any
		want     string
	}{
		{"files.read", map[string]any{"path": "main.go"}, "go"},
		{"files.read", map[string]any{"path": "app.py"}, "python"},
		{"files.read", map[string]any{"path": "index.ts"}, "typescript"},
		{"files.read", map[string]any{"path": "main.rs"}, "rust"},
		{"files.read", map[string]any{"path": "Dockerfile"}, "dockerfile"},
		{"files.read", map[string]any{"path": "Makefile"}, "makefile"},
		{"files.read", map[string]any{"path": "notes.txt"}, ""},
		{"files.read", map[string]any{"path": "noext"}, ""},
		{"shell.run", map[string]any{"command": "ls -la"}, "bash"},
		{"files.write", map[string]any{"path": "x.go", "content": "..."}, ""},
		{"unknown", nil, ""},
	}
	for _, tt := range tests {
		got := langForTool(tt.toolName, tt.args)
		if got != tt.want {
			t.Errorf("langForTool(%q, %v) = %q, want %q", tt.toolName, tt.args, got, tt.want)
		}
	}
}