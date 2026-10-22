package tools

// The interactive /files browser (ported from the old modules/tui
// browser.go, card t_88bb7dcf): the full-screen view moves to the
// module that owns the files domain, as an agent.TUIScreen the TUI
// shell routes keys and renders. Read-only view state only.

import (
	"fmt"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// tui implements agent.TUIProvider for the /files command.
type tui struct{}

var _ agent.TUIProvider = (*tui)(nil)

// TUICommands declares the /files command.
func (t *tui) TUICommands() []agent.TUICommand {
	return []agent.TUICommand{
		{Name: "/files", Help: "browse the working directory (enter opens, esc/q closes)"},
	}
}

// HandleTUICommand opens the interactive file browser rooted at the
// working directory. [path] selects the starting directory (same rules
// as the webui: relative, no dot segments); a bad path leaves no
// browser open.
func (t *tui) HandleTUICommand(name, args string, c agent.TUIConsole) agent.TUIResult {
	root, err := filesRoot(c.CWD())
	if err != nil {
		c.Error("resolve working directory: " + err.Error())
		return agent.TUIResult{}
	}
	// The shell splits on whitespace; directory segments rejoin here.
	rel := strings.Join(strings.Fields(args), "/")
	b, err := openFilesBrowser(c, root, rel)
	if err != nil {
		c.Error("/files: " + err.Error())
		return agent.TUIResult{}
	}
	return agent.TUIResult{Overlay: b}
}

// filesBrowser is the open /files view state. rel is the relative path
// under the root ("" = root); the listing is dirs-first, one level at
// a time, and enter opens a directory or previews a file.
type filesBrowser struct {
	c            agent.TUIConsole // shell console, for theme styles + errors
	root         string
	rel          string // relative path of the open directory; "" = root
	entries      []fileEntry
	cursor       int
	preview      bool   // a file preview is shown instead of the list
	previewLabel string // relative path of the previewed file
	previewTrunc bool   // preview content was cut at 64 KiB
	previewBin   bool   // preview content is not text
	closed       bool
	vp           viewport.Model
}

// openFilesBrowser lists rel under root and returns the ready browser.
// An empty arg or a directory error is reported as err, not a
// half-opened browser.
func openFilesBrowser(c agent.TUIConsole, root, rel string) (*filesBrowser, error) {
	rel, err := validateRelPath(rel)
	if err != nil {
		return nil, err
	}
	entries, err := listDirUnderRoot(root, rel)
	if err != nil {
		return nil, err
	}
	return &filesBrowser{c: c, root: root, rel: rel, entries: entries, vp: viewport.New(80, 20)}, nil
}

// Update consumes one shell message. While the browser is open it
// takes every key; non-key messages are inert (the layout sizes from
// the View call, the shell's own resize cycle).
func (b *filesBrowser) Update(msg any, c agent.TUIConsole) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return
	}
	switch key.String() {
	case "q", "esc":
		if b.preview {
			b.preview = false
			b.vp.GotoTop()
		} else {
			b.closed = true
		}
	case "backspace", "h", "left":
		if b.preview {
			b.preview = false
			b.vp.GotoTop()
			return
		}
		b.openFilesRel(b.upDir(), c)
	case "up", "k":
		if b.preview {
			b.vp.LineUp(1)
			return
		}
		if b.cursor > 0 {
			b.cursor--
		}
	case "down", "j":
		if b.preview {
			b.vp.LineDown(1)
			return
		}
		if b.cursor < len(b.entries)-1 {
			b.cursor++
		}
	case "pgup", "ctrl+u":
		b.vp.HalfPageUp()
	case "pgdown", "ctrl+d":
		b.vp.HalfPageDown()
	case "g", "home":
		b.vp.GotoTop()
	case "G", "end":
		b.vp.GotoBottom()
	case "enter", "right", "l":
		if b.preview || len(b.entries) == 0 {
			return
		}
		child := b.rel
		if child == "" {
			child = b.entries[b.cursor].Name
		} else {
			child = child + "/" + b.entries[b.cursor].Name
		}
		if b.entries[b.cursor].IsDir {
			b.openFilesRel(child, c)
			return
		}
		// file: load the preview
		content, realRel, truncated, binary, err := readPreviewAt(b.root, child)
		if err != nil {
			c.Error(err.Error())
			return
		}
		b.preview = true
		b.previewLabel = realRel
		b.previewTrunc = truncated
		b.previewBin = binary
		b.vp.SetContent(content)
		b.vp.GotoTop()
	}
}

// Closed reports the browser is closed (its q/esc path).
func (b *filesBrowser) Closed() bool { return b.closed }

// upDir strips the last segment of the current directory ("" = root).
// Pure on b - the caller re-opens the listing.
func (b filesBrowser) upDir() string {
	if b.rel == "" {
		return ""
	}
	i := strings.LastIndexByte(b.rel, '/')
	if i < 0 {
		return ""
	}
	return b.rel[:i]
}

// openFilesRel lists rel under the root and reopens the listing. On a
// disk error the old browser state is kept and the error surfaces in
// the status line.
func (b *filesBrowser) openFilesRel(rel string, c agent.TUIConsole) {
	entries, err := listDirUnderRoot(b.root, rel)
	if err != nil {
		c.Error(err.Error())
		return
	}
	b.rel = rel
	b.entries = entries
	b.cursor = 0
	b.vp.GotoTop()
	c.ClearError()
}

// filesBrowserHeight is rows consumed outside the listing viewport:
// header + hint + status + newlines between them.
const filesBrowserHeight = 4

// resize sets the browser viewport size: full width, and the height
// left by the window minus header, hint and status lines.
func (b *filesBrowser) resize(width, height int) {
	if height < 1 {
		height = 1
	}
	b.vp.Width = width
	b.vp.Height = height
}

// View paints the browser full-screen view: header (path, position),
// the listing or the preview, and a key hint footer. The height covers
// the full screen - the shell's status line rides filesBrowserHeight.
func (b *filesBrowser) View(width, height int) string {
	b.resize(width, height-filesBrowserHeight)
	c := b.c
	dir := b.rel
	if dir == "" {
		dir = "~"
	}
	var sb strings.Builder
	head := fmt.Sprintf("/files:%s  [%d/%d]", dir, b.cursor+1, len(b.entries))
	if b.preview {
		head = "/files: " + b.previewLabel
		if b.previewBin {
			head += "  [binary - no preview]"
		} else if b.previewTrunc {
			head += "  [truncated at 64 KiB]"
		}
	}
	sb.WriteString(c.Style("info")(head))
	sb.WriteString("\n")
	if !b.preview {
		b.vp.SetContent(b.listContent())
	} else if b.previewBin {
		b.vp.SetContent(c.Style("dim")("(binary file - content not shown)"))
	}
	sb.WriteString(b.vp.View())
	sb.WriteString("\n")
	hint := "enter open · backspace parent · q/esc close"
	if b.preview {
		hint = "esc back · j/k scroll · q close"
	}
	sb.WriteString(c.Style("status")(hint))
	return sb.String()
}

// listContent renders the directory rows for the listing pane.
func (b filesBrowser) listContent() string {
	c := b.c
	if len(b.entries) == 0 {
		return c.Style("dim")("(empty directory)")
	}
	nameW := len("NAME")
	for _, e := range b.entries {
		if l := len(e.Name) + 1; e.IsDir && l > nameW {
			nameW = l
		} else if len(e.Name) > nameW {
			nameW = len(e.Name)
		}
	}
	var sb strings.Builder
	sb.WriteString(c.Style("info")(fmt.Sprintf("%-*s  %8s  %s", nameW, "NAME", "SIZE", "MODIFIED")))
	sb.WriteString("\n")
	for i, e := range b.entries {
		name := e.Name
		if e.IsDir {
			name += "/"
		}
		row := fmt.Sprintf("%-*s  %8s  %s", nameW, name, formatBytes(e.SizeBytes), formatModTime(e.ModTime))
		styled := c.Style("dim") // dimmed fallback (mtime)
		if e.IsDir {
			styled = c.Style("tool")
		}
		if i == b.cursor {
			sb.WriteString(c.Style("match")("> " + row))
		} else {
			sb.WriteString("  " + styled(row))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// formatBytes renders a file size for the listing column.
func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fK", float64(n)/(1024))
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1fG", float64(n)/(1024*1024*1024))
	}
}

// formatModTime renders a modification time for the listing column.
func formatModTime(tm time.Time) string {
	if tm.IsZero() {
		return "-"
	}
	return tm.UTC().Format("2006-01-02 15:04")
}

// current returns the highlighted entry (tests).
func (b filesBrowser) current() (fileEntry, bool) {
	if len(b.entries) == 0 {
		return fileEntry{}, false
	}
	if b.cursor < 0 {
		b.cursor = 0
	}
	if b.cursor >= len(b.entries) {
		b.cursor = len(b.entries) - 1
	}
	return b.entries[b.cursor], true
}

var _ agent.TUIScreen = (*filesBrowser)(nil)