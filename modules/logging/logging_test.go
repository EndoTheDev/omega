package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func TestFileLoggerWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	l, err := NewFileLogger(path)
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}
	defer l.Close()

	l.Printf("hello %s", "world")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "hello world") {
		t.Errorf("log missing content, got: %s", s)
	}
	// Should have a timestamp prefix from log.LstdFlags
	if len(s) < 20 {
		t.Errorf("log too short, expected timestamp prefix, got: %s", s)
	}
}

func TestFileLoggerErrorf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "error.log")
	l, err := NewFileLogger(path)
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}
	defer l.Close()

	l.Errorf("connection failed: %v", "timeout")

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "ERROR:") {
		t.Errorf("log missing ERROR prefix, got: %s", s)
	}
	if !strings.Contains(s, "connection failed: timeout") {
		t.Errorf("log missing content, got: %s", s)
	}
}

func TestFileLoggerAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "append.log")

	// Write first entry
	l1, err := NewFileLogger(path)
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}
	l1.Printf("first entry")
	l1.Close()

	// Write second entry - should append, not truncate
	l2, err := NewFileLogger(path)
	if err != nil {
		t.Fatalf("NewFileLogger 2: %v", err)
	}
	l2.Printf("second entry")
	l2.Close()

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "first entry") {
		t.Errorf("log missing first entry, got: %s", s)
	}
	if !strings.Contains(s, "second entry") {
		t.Errorf("log missing second entry, got: %s", s)
	}
}

func TestNopLogger(t *testing.T) {
	l := NopLogger{}
	l.Printf("ignored")
	l.Errorf("ignored")
	if err := l.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestNewLoggerEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "enabled.log")
	cfg := Config{Enabled: true, File: path}

	l, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer l.Close()

	if _, ok := l.(*FileLogger); !ok {
		t.Errorf("expected *FileLogger, got %T", l)
	}

	l.Printf("test entry")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "test entry") {
		t.Error("FileLogger did not write to file")
	}
}

func TestNewLoggerDisabled(t *testing.T) {
	cfg := Config{Enabled: false, File: "ignored.log"}

	l, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	if _, ok := l.(NopLogger); !ok {
		t.Errorf("expected NopLogger, got %T", l)
	}

	// Should not create any file
	l.Printf("should not write")
	if _, err := os.Stat("ignored.log"); err == nil {
		t.Error("NopLogger created a file")
		_ = os.Remove("ignored.log")
	}
}

func TestPluginImplementsInterface(t *testing.T) {
	var _ agent.Plugin = (*Plugin)(nil)
}

func TestPluginMetadata(t *testing.T) {
	p := NewPlugin()
	if p.Name() != "logging" {
		t.Errorf("Name() = %q, want %q", p.Name(), "logging")
	}
	provides := p.Provides()
	if len(provides) != 1 || provides[0] != "logging" {
		t.Errorf("Provides() = %v, want [logging]", provides)
	}
	if len(p.Requires()) != 0 {
		t.Errorf("Requires() = %v, want empty", p.Requires())
	}
}

func TestPluginMount(t *testing.T) {
	dir := t.TempDir()
	cfg := Default()
	cfg.File = filepath.Join(dir, "mount.log")

	p := NewPlugin()
	ctx := &agent.Context{Configs: map[string]any{"logging": cfg}}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if ctx.Logger == nil {
		t.Fatal("ctx.Logger is nil after Mount")
	}
	defer ctx.Logger.Close()
}

func TestPluginMountDisabled(t *testing.T) {
	cfg := Default()
	cfg.Enabled = false

	p := NewPlugin()
	ctx := &agent.Context{Configs: map[string]any{"logging": cfg}}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if ctx.Logger == nil {
		t.Fatal("ctx.Logger is nil after Mount")
	}
	if _, ok := ctx.Logger.(NopLogger); !ok {
		t.Errorf("expected NopLogger when disabled, got %T", ctx.Logger)
	}
}