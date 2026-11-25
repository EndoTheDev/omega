package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// iconDirectory reads an ICO container's entries: size, bits per pixel and the
// first four bytes of each entry's payload.
type iconDirectoryEntry struct {
	size   int
	bits   uint16
	magic  string
	length uint32
}

func readIconDirectory(t *testing.T, path string) []iconDirectoryEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(data) < 6 {
		t.Fatalf("%s is too short to be an ICO", path)
	}
	if kind := binary.LittleEndian.Uint16(data[2:]); kind != 1 {
		t.Fatalf("%s carries type %d, want 1 (icon)", path, kind)
	}
	count := int(binary.LittleEndian.Uint16(data[4:]))
	if len(data) < 6+16*count {
		t.Fatalf("%s has a truncated directory", path)
	}
	entries := make([]iconDirectoryEntry, count)
	for i := 0; i < count; i++ {
		entry := data[6+i*16:]
		size := int(entry[0])
		if size == 0 {
			size = 256
		}
		length := binary.LittleEndian.Uint32(entry[8:])
		offset := binary.LittleEndian.Uint32(entry[12:])
		if uint64(offset)+uint64(length) > uint64(len(data)) {
			t.Fatalf("%s entry %d runs past the file", path, i)
		}
		entries[i] = iconDirectoryEntry{
			size:   size,
			bits:   binary.LittleEndian.Uint16(entry[6:]),
			magic:  string(data[offset : offset+4]),
			length: length,
		}
	}
	return entries
}

func TestIconSetCarriesTheSevenWindowsSizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omega.ico")
	if err := writeIconFile(path); err != nil {
		t.Fatalf("writeIconFile: %v", err)
	}

	entries := readIconDirectory(t, path)
	var sizes []int
	for _, entry := range entries {
		sizes = append(sizes, entry.size)
		if entry.bits != 32 {
			t.Errorf("%dpx entry is %d bits per pixel, want 32", entry.size, entry.bits)
		}
	}
	if want := []int{16, 24, 32, 48, 64, 128, 256}; !reflect.DeepEqual(sizes, want) {
		t.Fatalf("icon sizes are %v, want %v", sizes, want)
	}
	for _, entry := range entries {
		switch {
		case entry.size == 256 && entry.magic != "\x89PNG":
			t.Errorf("256px entry is not PNG-compressed (magic %q)", entry.magic)
		case entry.size < 256 && binary.LittleEndian.Uint32([]byte(entry.magic)) != 40:
			t.Errorf("%dpx entry is not a 40-byte BITMAPINFOHEADER DIB", entry.size)
		}
	}
}

func TestIconSetIsByteIdenticalAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.ico")
	second := filepath.Join(dir, "second.ico")
	if err := writeIconFile(first); err != nil {
		t.Fatalf("writeIconFile first: %v", err)
	}
	if err := writeIconFile(second); err != nil {
		t.Fatalf("writeIconFile second: %v", err)
	}

	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("two renders differ: %d and %d bytes", len(a), len(b))
	}
}

// The test binary is itself a PE with no icon group, so it stands in for a
// faceless executable.
func TestCheckFailsOnExecutableWithoutIconGroup(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the stand-in executable is only a PE on windows")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"-check", self}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("-check exited %d, want 1; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "no icon group") {
		t.Fatalf("stderr does not name the missing icon group: %s", stderr.String())
	}
}

func TestCheckAcceptsTheBuiltExecutable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the built executable is windows-only")
	}
	exe := filepath.Join("..", "..", "bin", "omega.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Skip("bin/omega.exe is not built")
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"-check", exe}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("-check exited %d on %s; stderr: %s", code, exe, stderr.String())
	}
	if !strings.Contains(stdout.String(), "icon group 1: 16 24 32 48 64 128 256") {
		t.Fatalf("icon group report is %q", stdout.String())
	}
}
