package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"os/exec"
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

// buildFixture links a windows executable that carries the icon resource. The
// object is the one every sanctioned build generates from the committed asset,
// and it is linked in a module of the test's own: the real binary is not
// rebuilt on every gate run, and no object ever appears beside cmd/omega's
// sources, where a package building at the same moment would read it.
func buildFixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := writeSysoTo(filepath.Join(root, assetPath), filepath.Join(dir, "rsrc_windows_amd64.syso")); err != nil {
		t.Fatalf("write the icon object: %v", err)
	}
	for name, content := range map[string]string{
		"go.mod":  "module fixture\n\ngo 1.26.5\n",
		"main.go": "package main\n\nfunc main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	exe := filepath.Join(dir, "omega.exe")
	build := exec.Command("go", "build", "-o", exe, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build the fixture: %v\n%s", err, out)
	}
	return exe
}

func TestCheckAcceptsTheBuiltExecutable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the resource target is a windows PE")
	}
	if testing.Short() {
		t.Skip("links a fixture executable")
	}
	exe := buildFixture(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-check", exe}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("-check exited %d on %s; stderr: %s", code, exe, stderr.String())
	}
	if !strings.Contains(stdout.String(), "icon group 1: 16 24 32 48 64 128 256") {
		t.Fatalf("icon group report is %q", stdout.String())
	}
}

// writePEFixture writes the smallest image the header walk reads: a DOS stub
// pointing at a PE32+ optional header with the given subsystem, no sections
// and no code. The gate reads bytes, so the subsystem cases need neither a
// windows host nor a real build.
func writePEFixture(t *testing.T, subsystem uint16) string {
	t.Helper()
	const (
		dosSize  = 0x80
		signSize = 4
	)

	optional := pe.OptionalHeader64{Magic: 0x20b, Subsystem: subsystem}
	optional.NumberOfRvaAndSizes = uint32(len(optional.DataDirectory))
	var optionalBytes bytes.Buffer
	if err := binary.Write(&optionalBytes, binary.LittleEndian, &optional); err != nil {
		t.Fatalf("encode the optional header: %v", err)
	}
	fileHeader := pe.FileHeader{
		Machine:              pe.IMAGE_FILE_MACHINE_AMD64,
		SizeOfOptionalHeader: uint16(optionalBytes.Len()),
	}
	var fileHeaderBytes bytes.Buffer
	if err := binary.Write(&fileHeaderBytes, binary.LittleEndian, &fileHeader); err != nil {
		t.Fatalf("encode the file header: %v", err)
	}

	image := make([]byte, dosSize+signSize+fileHeaderBytes.Len()+optionalBytes.Len())
	copy(image, "MZ")
	binary.LittleEndian.PutUint32(image[0x3c:], dosSize)
	copy(image[dosSize:], "PE\x00\x00")
	copy(image[dosSize+signSize:], fileHeaderBytes.Bytes())
	copy(image[dosSize+signSize+fileHeaderBytes.Len():], optionalBytes.Bytes())

	path := filepath.Join(t.TempDir(), "omega.exe")
	if err := os.WriteFile(path, image, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSubsystemGateAcceptsTheWindowedImage(t *testing.T) {
	exe := writePEFixture(t, pe.IMAGE_SUBSYSTEM_WINDOWS_GUI)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-subsystem", exe}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("-subsystem exited %d on a windowed image; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "subsystem 2 (IMAGE_SUBSYSTEM_WINDOWS_GUI)") {
		t.Fatalf("subsystem report is %q", stdout.String())
	}
}

func TestSubsystemGateRefusesAConsoleImage(t *testing.T) {
	exe := writePEFixture(t, pe.IMAGE_SUBSYSTEM_WINDOWS_CUI)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-subsystem", exe}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("-subsystem exited %d on a console image, want 1; stderr: %s", code, stderr.String())
	}
	for _, want := range []string{"subsystem 3", "IMAGE_SUBSYSTEM_WINDOWS_CUI", exe, "-H=windowsgui"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr does not carry %q: %s", want, stderr.String())
		}
	}
}

func TestSubsystemGateRefusesANonPE(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omega.exe")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"-subsystem", path}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("-subsystem exited %d on a non-PE file, want 1; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not a PE image") {
		t.Fatalf("stderr does not name the missing PE: %s", stderr.String())
	}
}
