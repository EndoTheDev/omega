package tui

// Tests for the TUI browse views (card t_5c280c68): the /files walk
// rules mirrored from the webui surface, the interactive browser state
// machine, /cron rendering from a fixture, and /logs tail + follow.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// fakeCronLister satisfies agent.CronProvider + agent.CronLister for
// the /cron test, the same shape the webui handler test uses.
type fakeCronLister struct{ jobs []agent.CronJobInfo }

func (f fakeCronLister) Start(context.Context, agent.CronDeps) error { return nil }
func (f fakeCronLister) Stop() error                                 { return nil }
func (f fakeCronLister) Jobs() []agent.CronJobInfo                   { return f.jobs }

// fakeFileLogger satisfies agent.LoggerProvider with a path it carries
// for the /logs tests, mimicking modules/logging FileLogger.
type fakeFileLogger struct{ path string }

func (f fakeFileLogger) Printf(string, ...any) {}
func (f fakeFileLogger) Errorf(string, ...any) {}
func (f fakeFileLogger) Close() error          { return nil }
func (f fakeFileLogger) Path() string          { return f.path }

// browserModel builds a chat model rooted at a scratch working dir.
func browserModel(root string) model {
	m := newChatModel("ollama", "llama3", nil, "", nil, "", nil, nil, "dark", "", "bell", "dev", root)
	m.screenHeight = 40
	return m
}

// withCron attaches a fake cron provider to a model.
func withCron(m model, jobs []agent.CronJobInfo) model {
	m.extensions = &agent.Context{Cron: fakeCronLister{jobs: jobs}}
	return m
}

// withLogger attaches a fake file logger to a model.
func withLogger(m model, path string) model {
	m.extensions = &agent.Context{Logger: fakeFileLogger{path: path}}
	return m
}

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

// TestFilesCommandDispatch covers /files through handleCommand:
// opening, walking down, previewing a file, leaving the preview,
// going back up and closing.
func TestFilesCommandDispatch(t *testing.T) {
	home := scratchFilesHome(t)
	m := browserModel(home)

	// open at the root
	st, _ := m.handleCommand("/files")
	if st.(model).files == nil {
		t.Fatal("/files left no browser open")
	}
	m = st.(model)
	if m.files.rel != "" {
		t.Fatalf("rel = %q, want root", m.files.rel)
	}

	// move the cursor onto config/ and open it
	cursorTo(t, &m, "config")
	st, _ = m.updateFilesKeys(tea.KeyMsg{Type: tea.KeyEnter})
	m = st.(model)
	if m.files.rel != "config" {
		t.Fatalf("descend failed: rel = %q", m.files.rel)
	}
	if len(m.files.entries) == 0 || m.files.entries[0].Name != "omega.yaml" {
		t.Fatalf("config listing = %+v", m.files.entries)
	}

	// preview the file (enter): content, no binary, no truncation
	st, _ = m.updateFilesKeys(tea.KeyMsg{Type: tea.KeyEnter})
	m = st.(model)
	if !m.files.preview || m.files.previewBin || m.files.previewTrunc {
		t.Fatalf("preview state = %+v", m.files)
	}
	if m.files.previewLabel != "config/omega.yaml" {
		t.Fatalf("previewLabel = %q", m.files.previewLabel)
	}
	if !strings.Contains(m.files.vp.View(), "provider: ollama") {
		t.Error("preview content missing file body")
	}

	// esc leaves the preview, esc again closes the browser
	st, _ = m.updateFilesKeys(tea.KeyMsg{Type: tea.KeyEsc})
	m = st.(model)
	if m.files.preview {
		t.Fatal("esc did not leave the preview")
	}
	if m.files.rel != "config" {
		t.Fatalf("back to listing lost the dir: rel = %q", m.files.rel)
	}
	// backspace goes up to the root
	st, _ = m.updateFilesKeys(tea.KeyMsg{Type: tea.KeyBackspace})
	m = st.(model)
	if m.files.rel != "" {
		t.Fatalf("backspace did not go up: rel = %q", m.files.rel)
	}
	// esc closes
	st, _ = m.updateFilesKeys(tea.KeyMsg{Type: tea.KeyEsc})
	m = st.(model)
	if m.files != nil {
		t.Fatal("esc did not close the browser")
	}
}

// cursorTo moves the browser cursor onto the named entry in the open
// listing and fails the test if it is not there.
func cursorTo(t *testing.T, m *model, name string) {
	t.Helper()
	for i, e := range m.files.entries {
		if e.Name == name {
			for i != m.files.cursor {
				st, _ := m.updateFilesKeys(tea.KeyMsg{Type: tea.KeyDown})
				*m = st.(model)
			}
			if m.files.cursor != i {
				t.Fatalf("cursor = %d, want on %q (%d)", m.files.cursor, name, i)
			}
			return
		}
	}
	t.Fatalf("%q not in listing %v", name, m.files.entries)
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

// TestFilesTraversalInBrowser the browser never opens on or descends
// into a path the validation rejects; a bad path keeps the browser
// closed (open) or the state intact (navigate) with the error shown.
func TestFilesTraversalInBrowser(t *testing.T) {
	home := scratchFilesHome(t)
	m := browserModel(home)

	// absolute path rejected at open
	st, _ := m.handleCommand("/files /")
	if st.(model).files != nil || st.(model).err == "" {
		t.Fatalf("absolute arg opened a browser or silenced the error: files=%v err=%q", st.(model).files, st.(model).err)
	}

	// escape a directory the hard way: a symlink inside the tree
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}
	st, _ = m.handleCommand("/files")
	m = st.(model)
	cursorTo(t, &m, "link")
	st, _ = m.updateFilesKeys(tea.KeyMsg{Type: tea.KeyEnter})
	m = st.(model)
	if m.files.rel == "link" {
		t.Fatal("browser descended through a symlink escape")
	}
	if m.err == "" {
		t.Fatal("symlink escape surfaced no error")
	}
}

// TestCronCommandDispatch renders the fixture through /cron: rows
// sorted by name, the disabled/empty variants handled, the webui's
// empty-list semantics (unconfigured scheduler, not an error).
func TestCronCommandDispatch(t *testing.T) {
	jobs := []agent.CronJobInfo{
		{Name: "nightly", Schedule: "0 3 * * *", Enabled: true, Next: "2026-10-22T03:00:00+07:00"},
		{Name: "alpha", Schedule: "5 * * * *", Enabled: false},
	}
	m := withCron(browserModel(""), jobs)
	st, _ := m.handleCommand("/cron")
	m = st.(model)
	out := ansiStrip(m.transcript)
	if !strings.Contains(out, "[cron]") {
		t.Fatalf("transcript missing [cron] header: %q", out)
	}
	// sorted by name: alpha row before nightly row
	alpha, nightly := strings.Index(out, "alpha"), strings.Index(out, "nightly")
	if alpha < 0 || nightly < 0 || alpha > nightly {
		t.Fatalf("rows not sorted by name: %q", out)
	}
	if !strings.Contains(out, "0 3 * * *") || !strings.Contains(out, "2026-10-22T03:00:00+07:00") {
		t.Errorf("schedule/next missing: %q", out)
	}

	// empty set reads as a configured-but-empty scheduler
	m = withCron(browserModel(""), nil)
	st, _ = m.handleCommand("/cron")
	if !strings.Contains(ansiStrip(st.(model).transcript), "no cron jobs configured") {
		t.Fatalf("empty set not handled: %q", st.(model).transcript)
	}

	// a CronProvider without the listing reads as empty, like the webui
	m.extensions = &agent.Context{Cron: noListerCron{}}
	st, _ = m.handleCommand("/cron")
	if !strings.Contains(ansiStrip(st.(model).transcript), "no cron jobs configured") {
		t.Fatalf("no-lister provider not handled: %q", st.(model).transcript)
	}
}

// noListerCron is a CronProvider that is not a CronLister - the shape
// the inspector must treat as an empty listing.
type noListerCron struct{}

func (noListerCron) Start(context.Context, agent.CronDeps) error { return nil }
func (noListerCron) Stop() error                                 { return nil }

// TestRenderCronTable is pure: fixtures in, table out.
func TestRenderCronTable(t *testing.T) {
	jobs := []agent.CronJobInfo{
		{Name: "zeta", Schedule: "0 3 * * *", Enabled: true, Next: "tomorrow"},
		{Name: "beta", Schedule: "*/5 * * * *", Enabled: true, Next: "soon"},
		{Name: "off", Schedule: "garbage", Enabled: false},
	}
	out := ansiStrip(renderCronTable(jobs, themes["dark"]))
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "SCHEDULE") {
		t.Fatalf("table header missing: %q", out)
	}
	if strings.Index(out, "beta") > strings.Index(out, "zeta") {
		t.Fatalf("rows not sorted: %q", out)
	}
	// the disabled job's empty Next shows as the dash placeholder
	offRow := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "off") {
			offRow = line
		}
	}
	if offRow == "" || strings.Contains(offRow, "tomorrow") {
		t.Fatalf("disabled row wrong: %q", offRow)
	}
	if !strings.Contains(offRow, "no") || strings.HasSuffix(strings.TrimSpace(offRow), "yes") {
		t.Fatalf("enabled column wrong on %q", offRow)
	}
}

// TestLogsCommandDispatch tails a fixture file through /logs and
// exercises follow: -f arms the follow state and reschedules, a tick
// appends the lines written since, rotation restarts, stop works.
func TestLogsCommandDispatch(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "omega.log")
	log := "first\nsecond\nthird\n"
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	m := withLogger(browserModel(home), path)

	// plain /logs renders the tail
	st, _ := m.handleCommand("/logs")
	m = st.(model)
	out := ansiStrip(m.transcript)
	if !strings.Contains(out, "[logs: "+path+"]") {
		t.Fatalf("transcript missing log header: %q", out)
	}
	if !strings.Contains(out, "second\nthird") {
		t.Fatalf("tail lines missing: %q", out)
	}
	if m.logFollow {
		t.Error("plain /logs must not arm follow")
	}

	// /logs -f arms follow
	st, cmd := m.handleCommand("/logs -f")
	m = st.(model)
	if !m.logFollow {
		t.Fatal("-f did not arm follow")
	}
	if cmd == nil {
		t.Fatal("-f returned no tick command")
	}

	// the logger writes a new line; the next tick appends it
	if err := os.WriteFile(path, []byte(log+"fourth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.logOffset = int64(len(log)) // what the initial tail read left behind
	st, cmd = m.handleLogFollowTick()
	m = st.(model)
	if !m.logFollow {
		t.Fatal("follow dropped after a successful tick")
	}
	if cmd == nil {
		t.Fatal("tick did not reschedule")
	}
	if !containsLogLine(m.transcript, "fourth") {
		t.Fatalf("appended line missing: %q", ansiStrip(m.transcript))
	}

	// an unreadable log stops follow with an error
	gone := filepath.Join(home, "gone.log")
	m.logFile = gone
	st, cmd = m.handleLogFollowTick()
	m = st.(model)
	if m.logFollow || m.err == "" {
		t.Fatalf("follow kept running on error: follow=%v err=%q", m.logFollow, m.err)
	}
	if cmd != nil {
		t.Fatal("error tick still rescheduled")
	}

	// rotation: the file shrinks below the offset and the read restarts
	m.logFollow = true
	m.logFile = path
	m.logOffset = int64(len(log) + len("fourth\n") + 10000)
	if err := os.WriteFile(path, []byte("rotated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ = m.handleLogFollowTick()
	m = st.(model)
	if !containsLogLine(m.transcript, "rotated") {
		t.Fatalf("rotation restart missed: %q", ansiStrip(m.transcript))
	}
}

// containsLogLine reports whether the transcript carries a log line.
func containsLogLine(transcript, line string) bool {
	return strings.Contains(ansiStrip(transcript), line)
}

// TestLogsMissingLogger - a nil logger (logging disabled) reports
// "log file not configured" on /logs and renders nothing.
func TestLogsMissingLogger(t *testing.T) {
	m := browserModel("")
	st, _ := m.handleCommand("/logs")
	m = st.(model)
	if m.err != "log file not configured" {
		t.Fatalf("err = %q", m.err)
	}
	if strings.Contains(m.transcript, "[logs:") {
		t.Error("nothing may render without a logger path")
	}
}

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
