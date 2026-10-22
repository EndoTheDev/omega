package tools

// Screen tests for the module-owned /files browser (card t_88bb7dcf;
// behavior ported unchanged from the old modules/tui browser.go): the
// open-walk-preview-close state machine and the traversal defenses,
// driven through the agent.TUIProvider contract with a recording
// console.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// recordingConsole is an agent.TUIConsole that records output and
// renders styles unchanged.
type recordingConsole struct {
	labels []string
	bodies []string
	errs   []string
	cwd    string
}

func (c *recordingConsole) Labeled(label, body string) {
	c.labels = append(c.labels, label)
	c.bodies = append(c.bodies, body)
}
func (c *recordingConsole) Error(msg string) { c.errs = append(c.errs, msg) }
func (c *recordingConsole) ClearError()      { c.errs = nil }
func (c *recordingConsole) Style(string) agent.TUIStyle {
	return func(s string) string { return s }
}
func (c *recordingConsole) CWD() string         { return c.cwd }
func (c *recordingConsole) Ext() *agent.Context { return nil }

// plainFilesConsole builds a recording console rooted at dir.
func plainFilesConsole(t *testing.T, dir string) *recordingConsole {
	t.Helper()
	return &recordingConsole{cwd: dir}
}

// key wraps a tea.KeyMsg for screen driving.
func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

// TestFilesCommandDispatch covers /files through the provider
// contract: opening, walking down, previewing a file, leaving the
// preview, going back up and closing.
func TestFilesCommandDispatch(t *testing.T) {
	home := scratchFilesHome(t)
	c := plainFilesConsole(t, home)
	p := &tui{}

	res := p.HandleTUICommand("/files", "", c)
	if res.Overlay == nil {
		t.Fatal("/files left no browser open")
	}
	b := res.Overlay.(*filesBrowser)
	if b.rel != "" {
		t.Fatalf("rel = %q, want root", b.rel)
	}

	// move the cursor onto config/ and open it
	for b.entries[b.cursor].Name != "config" {
		b.Update(key(tea.KeyDown), c)
		if b.cursor >= len(b.entries) {
			t.Fatal("config/ not reachable in listing")
		}
	}
	b.Update(key(tea.KeyEnter), c)
	if b.rel != "config" {
		t.Fatalf("descend failed: rel = %q", b.rel)
	}
	if len(b.entries) == 0 || b.entries[0].Name != "omega.yaml" {
		t.Fatalf("config listing = %+v", b.entries)
	}

	// preview the file (enter): content, no binary, no truncation
	b.Update(key(tea.KeyEnter), c)
	if !b.preview || b.previewBin || b.previewTrunc {
		t.Fatalf("preview state = %+v", b)
	}
	if b.previewLabel != "config/omega.yaml" {
		t.Fatalf("previewLabel = %q", b.previewLabel)
	}
	if !strings.Contains(b.vp.View(), "provider: ollama") {
		t.Error("preview content missing file body")
	}

	// esc leaves the preview, esc again closes the browser
	b.Update(key(tea.KeyEsc), c)
	if b.preview {
		t.Fatal("esc did not leave the preview")
	}
	if b.rel != "config" {
		t.Fatalf("back to listing lost the dir: rel = %q", b.rel)
	}
	// backspace goes up to the root
	b.Update(key(tea.KeyBackspace), c)
	if b.rel != "" {
		t.Fatalf("backspace did not go up: rel = %q", b.rel)
	}
	// esc closes
	b.Update(key(tea.KeyEsc), c)
	if !b.Closed() {
		t.Fatal("esc did not close the browser")
	}
}

// TestFilesTraversalInBrowser the browser never opens on or descends
// into a path the validation rejects; a bad path keeps the browser
// closed (open) or the state intact (navigate) with the error shown.
func TestFilesTraversalInBrowser(t *testing.T) {
	home := scratchFilesHome(t)
	c := plainFilesConsole(t, home)
	p := &tui{}

	// absolute path rejected at open
	if res := p.HandleTUICommand("/files", "/", c); res.Overlay != nil || len(c.errs) == 0 {
		t.Fatalf("absolute arg opened a browser or silenced the error: screen=%v errs=%v", res.Overlay, c.errs)
	}

	// escape a directory the hard way: a symlink inside the tree
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}
	res := p.HandleTUICommand("/files", "", c)
	b := res.Overlay.(*filesBrowser)
	c.ClearError()
	for b.entries[b.cursor].Name != "link" {
		b.Update(key(tea.KeyDown), c)
		if b.cursor >= len(b.entries) {
			t.Fatal("link/ not reachable in listing")
		}
	}
	b.Update(key(tea.KeyEnter), c)
	if b.rel == "link" {
		t.Fatal("browser descended through a symlink escape")
	}
	if len(c.errs) == 0 {
		t.Fatal("symlink escape surfaced no error")
	}
	if b.Closed() {
		t.Fatal("a failed descent closed the browser")
	}
}

// TestFilesViewRendersListing pins the View output: the header, the
// cursor marker and the hint line, all through the identity styles.
func TestFilesViewRendersListing(t *testing.T) {
	home := scratchFilesHome(t)
	c := plainFilesConsole(t, home)
	res := (&tui{}).HandleTUICommand("/files", "", c)
	b := res.Overlay.(*filesBrowser)
	out := b.View(80, 24)
	for _, want := range []string{"/files:~  [1/", "NAME", "SIZE", "MODIFIED", "enter open", "config/", "> config/"} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q in:\n%s", want, out)
		}
	}
}