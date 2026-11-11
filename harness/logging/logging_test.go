package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
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

// TestFileLoggerWarnf pins the severity-marker convention (card
// t_221dc498): Warnf writes "WARN: " and Errorf "ERROR: " right after
// the log.LstdFlags timestamp. The logs tab's severity filter
// (api/http_channel/ui/severity.ts) classifies on exactly these
// shapes - this test is the Go-side half of that contract.
func TestFileLoggerWarnf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warn.log")
	l, err := NewFileLogger(path)
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}
	defer l.Close()

	l.Warnf("vision_model set but no caption sink")
	l.Errorf("hard down")

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "WARN: vision_model set but no caption sink") {
		t.Errorf("log missing WARN marker, got: %s", s)
	}
	if !strings.Contains(s, "ERROR: hard down") {
		t.Errorf("log missing ERROR marker, got: %s", s)
	}
}

func TestNopLoggerWarnf(t *testing.T) {
	l := NopLogger{}
	l.Warnf("ignored")
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
	if len(provides) != 1 || provides[0] != "logger" {
		t.Errorf("Provides() = %v, want [logger]", provides)
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
	ctx := &core.Context{Configs: map[string]any{"logging": cfg}}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	svc := ctx.Services().Resolve("logger")
	lg, ok := svc.(agent.LoggerProvider)
	if !ok || lg == nil {
		t.Fatalf("logger offer missing after Mount: %v", svc)
	}
	defer lg.Close()
}

func TestPluginMountDisabled(t *testing.T) {
	cfg := Default()
	cfg.Enabled = false

	p := NewPlugin()
	ctx := &core.Context{Configs: map[string]any{"logging": cfg}}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if ctx.Services().Resolve("logger") == nil {
		t.Fatal("ctx.Logger is nil after Mount")
	}
	if _, ok := ctx.Services().Resolve("logger").(NopLogger); !ok {
		t.Errorf("expected NopLogger when disabled, got %T", ctx.Services().Resolve("logger"))
	}
}

// Path reports the file the logger writes to (card t_db430fea - the
// /logs endpoint tails exactly this path against the exact open file).
func TestFileLoggerPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	l, err := NewFileLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if got := l.Path(); got != path {
		t.Fatalf("Path() = %q, want %q", got, path)
	}
}

// TestNewLoggerEmptyFileIsNop pins the no-cwd law (card t_0ccda501): an
// empty File (the construction default) yields a NopLogger and creates
// nothing in the process CWD.
func TestNewLoggerEmptyFileIsNop(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	l, err := NewLogger(Config{Enabled: true})
	if err != nil {
		t.Fatalf("NewLogger with empty File: %v", err)
	}
	defer l.Close()
	if _, ok := l.(NopLogger); !ok {
		t.Errorf("logger type %T, want NopLogger", l)
	}
	if entries, err := os.ReadDir(cwd); err != nil || len(entries) != 0 {
		t.Errorf("log file leaked into CWD: entries=%v err=%v", entries, err)
	}
}

// TestPluginMountDefaultNoCwdFile mounts the plugin with its own
// Default() config (empty File) and pins that no log file appears in
// the process CWD.
func TestPluginMountDefaultNoCwdFile(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	p := NewPlugin()
	ctx := &core.Context{Configs: map[string]any{}}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	defer ctx.Services().Resolve("logger").(agent.LoggerProvider).Close()
	if entries, err := os.ReadDir(cwd); err != nil || len(entries) != 0 {
		t.Errorf("log file leaked into CWD: entries=%v err=%v", entries, err)
	}
}

// TestLastErrorFact pins the health popover's last-error source (card
// t_5e2f3095): the newest ERROR-level entry with its time, the "ERROR: "
// marker stripped (the row already says what it is), and info/warn never
// moving it. A fresh logger reports ok=false - the popover's "none"
// rather than a stale entry from a previous run.
func TestLastErrorFact(t *testing.T) {
	l, err := NewFileLogger(filepath.Join(t.TempDir(), "last-error.log"))
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}
	defer l.Close()

	if msg, at, ok := l.LastError(); ok || msg != "" || !at.IsZero() {
		t.Fatalf("fresh logger = (%q, %v, %v), want none", msg, at, ok)
	}

	l.Printf("wired on port %d", 8098)
	l.Warnf("slow provider: %v", "12s")
	if _, _, ok := l.LastError(); ok {
		t.Fatal("an info/warn entry sourced a last error")
	}

	l.Errorf("stream died: %v", "eof")
	msg, at, ok := l.LastError()
	if !ok {
		t.Fatal("Errorf did not source a last error")
	}
	if msg != "stream died: eof" {
		t.Fatalf("message = %q, want the formatted text with no ERROR: marker", msg)
	}
	if at.IsZero() {
		t.Fatal("last error carries no timestamp")
	}

	l.Errorf("second failure")
	if msg, _, _ := l.LastError(); msg != "second failure" {
		t.Fatalf("LastError = %q, want the newest entry", msg)
	}
}

// TestNopLoggerHasNoLastError: the upgrade is optional - a
// logging-disabled install reports nothing and the popover degrades to
// "not reported" instead of inventing a "none".
func TestNopLoggerHasNoLastError(t *testing.T) {
	var l agent.LoggerProvider = NopLogger{}
	if _, ok := l.(agent.LoggerLastError); ok {
		t.Fatal("NopLogger claims the last-error upgrade")
	}
}
