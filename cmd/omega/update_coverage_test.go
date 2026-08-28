package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0B"},
		{512, "512B"},
		{1023, "1023B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
		{1572864, "1.5MB"},
		{5242880, "5.0MB"},
	}
	for _, tt := range tests {
		got := humanBytes(tt.input)
		if got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFindInArchiveRoot(t *testing.T) {
	tmp := t.TempDir()
	// Create a file named "omega" at the root.
	binPath := filepath.Join(tmp, "omega")
	if err := os.WriteFile(binPath, []byte("fake binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := findInArchive(tmp, "omega")
	if got == "" {
		t.Fatalf("findInArchive returned empty, expected %s", binPath)
	}
	// On Windows, findInArchive checks for .exe first. If the file is
	// "omega" (no .exe), the loop tries "omega.exe" (stat fails), then
	// "omega" (stat succeeds). Verify we got something pointing to the
	// right file.
	if filepath.Base(got) != "omega" && filepath.Base(got) != "omega.exe" {
		t.Fatalf("findInArchive returned %q, expected base name omega or omega.exe", got)
	}
}

func TestFindInArchiveNested(t *testing.T) {
	tmp := t.TempDir()
	// Create a versioned directory with omega inside.
	subDir := filepath.Join(tmp, "omega-v0.3.0")
	os.MkdirAll(subDir, 0o755)
	binPath := filepath.Join(subDir, "omega")
	if err := os.WriteFile(binPath, []byte("fake binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := findInArchive(tmp, "omega")
	if got == "" {
		t.Fatalf("findInArchive returned empty for nested binary")
	}
}

func TestFindInArchiveNotFound(t *testing.T) {
	tmp := t.TempDir()
	got := findInArchive(tmp, "omega")
	if got != "" {
		t.Fatalf("findInArchive returned %q, expected empty", got)
	}
}

func TestCopyFile(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	content := "hello world"
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("copyFile content = %q, want %q", string(data), content)
	}
}

func TestCopyFileMissingSrc(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "dst.txt")
	err := copyFile(filepath.Join(tmp, "nonexistent"), dst)
	if err == nil {
		t.Fatal("copyFile with missing source should return error")
	}
}

func TestCopyIfMissingCreates(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	copyIfMissing(src, dst)
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("copyIfMissing did not create dst: %v", err)
	}
	if string(data) != "data" {
		t.Errorf("copyIfMissing content = %q, want %q", string(data), "data")
	}
}

func TestCopyIfMissingPreservesExisting(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	os.WriteFile(src, []byte("new"), 0o644)
	os.WriteFile(dst, []byte("old"), 0o644)
	copyIfMissing(src, dst)
	data, _ := os.ReadFile(dst)
	if string(data) != "old" {
		t.Errorf("copyIfMissing overwrote existing file: got %q, want %q", string(data), "old")
	}
}

func TestProgressReaderRead(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 500)
	pr := &progressReader{r: bytes.NewReader(data), total: int64(len(data))}
	buf := make([]byte, 250)
	n, err := pr.Read(buf)
	if err != nil {
		t.Fatalf("progressReader.Read error: %v", err)
	}
	if n != 250 {
		t.Errorf("progressReader.Read n = %d, want 250", n)
	}
	if string(buf) != strings.Repeat("x", 250) {
		t.Errorf("progressReader.Read data mismatch")
	}
	// Read rest
	n2, err := pr.Read(buf)
	if err != nil {
		t.Fatalf("progressReader.Read second call error: %v", err)
	}
	if n2 != 250 {
		t.Errorf("progressReader.Read second n = %d, want 250", n2)
	}
}

func TestExtractZipBasic(t *testing.T) {
	tmp := t.TempDir()
	// Create a zip in memory with one file.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	fw, err := w.Create("test.txt")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte("hello"))
	w.Close()

	if err := extractZip(bytes.NewReader(buf.Bytes()), tmp); err != nil {
		t.Fatalf("extractZip failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, "test.txt"))
	if err != nil {
		t.Fatalf("extracted file not found: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("extractZip content = %q, want %q", string(data), "hello")
	}
}

func TestExtractZipTraversalBlocked(t *testing.T) {
	tmp := t.TempDir()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	fw, err := w.Create("../../../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte("evil"))
	w.Close()

	err = extractZip(bytes.NewReader(buf.Bytes()), tmp)
	if err == nil {
		t.Fatal("extractZip should reject path traversal")
	}
	if !strings.Contains(err.Error(), "traversal") {
		t.Errorf("extractZip error = %q, expected traversal", err.Error())
	}
}

func TestExtractTarGzBasic(t *testing.T) {
	tmp := t.TempDir()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{
		Name: "test.txt",
		Mode: 0o644,
		Size: int64(len("hello")),
	}
	tw.WriteHeader(hdr)
	tw.Write([]byte("hello"))
	tw.Close()
	gw.Close()

	if err := extractTarGz(bytes.NewReader(buf.Bytes()), tmp); err != nil {
		t.Fatalf("extractTarGz failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, "test.txt"))
	if err != nil {
		t.Fatalf("extracted file not found: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("extractTarGz content = %q, want %q", string(data), "hello")
	}
}

func TestExtractTarGzInvalidGzip(t *testing.T) {
	tmp := t.TempDir()
	err := extractTarGz(bytes.NewReader([]byte("not gzip")), tmp)
	if err == nil {
		t.Fatal("extractTarGz should fail on invalid gzip")
	}
}

func TestReplaceBinarySameDir(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "new_omega")
	dst := filepath.Join(tmp, "omega")
	os.WriteFile(src, []byte("new binary"), 0o755)
	os.WriteFile(dst, []byte("old binary"), 0o755)

	if err := replaceBinary(src, dst); err != nil {
		t.Fatalf("replaceBinary failed: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new binary" {
		t.Errorf("replaceBinary content = %q, want %q", string(data), "new binary")
	}
	// Verify src is still intact (copyFile reads from src)
	srcData, _ := os.ReadFile(src)
	if string(srcData) != "new binary" {
		t.Errorf("replaceBinary modified source file")
	}
}

func TestCopyFileEmptyReader(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "empty.txt")
	dst := filepath.Join(tmp, "copy.txt")
	if err := os.WriteFile(src, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile empty failed: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Errorf("copyFile empty size = %d, want 0", info.Size())
	}
}