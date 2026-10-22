package logging

// Tests for the module-owned /logs TUI command (card t_88bb7dcf;
// behavior ported unchanged from the old modules/tui browser.go): the
// tail helper, the tail+follow command contract over a recording
// console, and the disabled-logging report.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// consoleSpy is an agent.TUIConsole that records output and renders
// styles unchanged.
type consoleSpy struct {
	labels []string
	bodies []string
	errs   []string
	ext    *agent.Context
}

func (c *consoleSpy) Labeled(label, body string) {
	c.labels = append(c.labels, label)
	c.bodies = append(c.bodies, body)
}
func (c *consoleSpy) Error(msg string)        { c.errs = append(c.errs, msg) }
func (c *consoleSpy) ClearError()             {}
func (c *consoleSpy) Style(string) agent.TUIStyle {
	return func(s string) string { return s }
}
func (c *consoleSpy) CWD() string         { return "" }
func (c *consoleSpy) Ext() *agent.Context { return c.ext }
func (c *consoleSpy) transcript() string {
	var sb strings.Builder
	for i, body := range c.bodies {
		if i < len(c.labels) {
			sb.WriteString(c.labels[i])
		}
		sb.WriteString(body)
	}
	return sb.String()
}

// fakeFileLogger satisfies agent.LoggerProvider with a path it
// carries, mimicking FileLogger.
type fakeFileLogger struct{ path string }

func (f fakeFileLogger) Printf(string, ...any) {}
func (f fakeFileLogger) Errorf(string, ...any) {}
func (f fakeFileLogger) Close() error          { return nil }
func (f fakeFileLogger) Path() string          { return f.path }

// TestTailLines pins the tail helper: an empty file yields no lines,
// fewer lines than n come back whole, more than n returns the last n,
// the trailing newline is not a line, and a missing file errors.
func TestTailLines(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "omega.log")

	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := tailLines(path, 10)
	if err != nil || len(lines) != 0 {
		t.Fatalf("empty file tail = %v, %v", lines, err)
	}

	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err = tailLines(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(lines, ","); got != "a,b,c" {
		t.Fatalf("tail = %q", got)
	}

	if err := os.WriteFile(path, []byte(strings.Repeat("x\n", 1000)), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err = tailLines(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 || lines[2] != "x" {
		t.Fatalf("tail of 1000 = %d lines: %v", len(lines), lines)
	}

	if _, err := tailLines(filepath.Join(home, "missing.log"), 3); err == nil {
		t.Fatal("missing file did not error")
	}
}

// TestLogsCommand tails a fixture file through the provider contract
// and exercises follow: -f returns a follow loop, a step appends the
// lines written since, rotation restarts, an unreadable log stops.
func TestLogsCommand(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "omega.log")
	log := "first\nsecond\nthird\n"
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &tui{}
	c := &consoleSpy{ext: &agent.Context{Logger: fakeFileLogger{path: path}}}

	// plain /logs renders the tail
	if res := p.HandleTUICommand("/logs", "", c); res.Tick != nil {
		t.Fatal("plain /logs must not arm follow")
	}
	out := c.transcript()
	if !strings.Contains(out, "[logs: "+path+"]") {
		t.Fatalf("output missing log header: %v", c.labels)
	}
	if !strings.Contains(out, "second\nthird") {
		t.Fatalf("tail lines missing: %q", out)
	}

	// /logs -f arms follow
	c2 := &consoleSpy{ext: c.ext}
	res := p.HandleTUICommand("/logs", "-f", c2)
	if res.Tick == nil {
		t.Fatal("-f did not arm follow")
	}

	// the logger writes a new line; the next step appends it
	if err := os.WriteFile(path, []byte(log+"fourth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, keep := res.Tick.Step(c2)
	if !keep {
		t.Fatal("follow stopped after a successful step")
	}
	if !strings.Contains(lines, "fourth\n") {
		t.Fatalf("appended line missing: %q", lines)
	}

	// steps emit only complete lines: a partial write is held back
	if err := os.WriteFile(path, []byte(log+"fourth\npartial"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, keep = res.Tick.Step(c2)
	if !keep || lines != "" {
		t.Fatalf("partial line leaked: %q keep=%v", lines, keep)
	}

	// rotation: the file shrinks below the follow offset and the read
	// restarts from the top
	if err := os.WriteFile(path, []byte(strings.Repeat("big\n", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	c3 := &consoleSpy{ext: c.ext}
	follow := p.HandleTUICommand("/logs", "-f", c3)
	if err := os.WriteFile(path, []byte("rotated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if lines, keep := follow.Tick.Step(c3); !keep || !strings.Contains(lines, "rotated\n") {
		t.Fatalf("rotation restart missed: %q keep=%v", lines, keep)
	}

	// an unreadable log stops the loop with an error
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	lines, keep = follow.Tick.Step(c3)
	if keep || lines != "" || len(c3.errs) == 0 {
		t.Fatalf("follow kept running on error: keep=%v lines=%q errs=%v", keep, lines, c3.errs)
	}
}

// TestLogsMissingLogger - a nil logger (logging disabled) reports
// "log file not configured" on /logs and renders nothing.
func TestLogsMissingLogger(t *testing.T) {
	c := &consoleSpy{}
	if res := (&tui{}).HandleTUICommand("/logs", "", c); res.Tick != nil || res.Overlay != nil {
		t.Fatal("no interactive instructions without a logger")
	}
	if len(c.errs) != 1 || c.errs[0] != "log file not configured" {
		t.Fatalf("errs = %q", c.errs)
	}
	if len(c.bodies) != 0 {
		t.Errorf("nothing may render without a logger path: %v", c.bodies)
	}
}

// TestLogsTUIRegistration pins the law: Mount registers the provider,
// so a logging plugin absent from a mount never surfaces /logs in a
// TUI command table.
func TestLogsTUIRegistration(t *testing.T) {
	cfg := Config{Enabled: true, File: filepath.Join(t.TempDir(), "omega.log")}
	ctx := &agent.Context{Configs: map[string]any{"logging": cfg}}
	if err := (&Plugin{}).Mount(ctx); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range ctx.TUIProviders {
		for _, cmd := range p.TUICommands() {
			got = append(got, cmd.Name)
		}
	}
	if len(got) != 1 || got[0] != "/logs" {
		t.Fatalf("mounted TUI commands = %v, want [/logs]", got)
	}
}