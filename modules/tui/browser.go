package tui

// The TUI browse views (card t_5c280c68, read-only v1): /files, /cron
// and /logs - parity with the webui inspector tabs, terminal-shaped.
// Everything here reads through the same seams the webui surface
// reads; no storage is reimplemented and nothing writes:
//
//   - /cron lists agent.CronLister (the provider behind the webui's
//     GET /cron/jobs) via the Context seam.
//   - /logs tails the file the agent.LoggerProvider carries - the
//     FileLogger path behind the webui's GET /logs. Follow (/logs -f)
//     re-reads from a byte offset on a one-second tick.
//   - /files mirrors the webui file surface (modules/http_channel/
//     server_files.go): that surface is http-only, so the walk rules
//     are mirrored here against the same root - the process working
//     directory. Same validation, containment and 64 KiB preview cap.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/EndoTheDev/omega/agent"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// The walk/validation rules below mirror modules/http_channel
// server_files.go one to one (validateRelPath, resolveUnderRoot,
// ensureUnderRoot, sortFileEntries, the 64 KiB cap). Keeping them
// identical is the point: both frontends answer the same request the
// same way. The http surface is http-only and the TUI does not import
// it, so the mirror lives here.

// filesPreviewCap is the preview size cap, matching the webui's:
// contents are cut at 64 KiB and the truncated flag says so.
const filesPreviewCap = 64 * 1024

// validateRelPath checks a user-supplied relative path against the
// segment rules (mirroring the webui) and returns the cleaned relative
// path ("" = the root). Absolute paths, empty segments, and
// dot-prefixed segments are rejected - hidden files and .. traversal
// die under one rule.
func validateRelPath(req string) (string, error) {
	if req == "" {
		return "", nil
	}
	if filepath.IsAbs(req) || strings.HasPrefix(req, "/") {
		return "", errors.New("absolute paths are not allowed")
	}
	parts := strings.Split(req, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return "", errors.New("empty path segment")
		}
		if strings.HasPrefix(part, ".") {
			return "", errors.New("dot-prefixed path segment")
		}
		cleaned = append(cleaned, part)
	}
	return strings.Join(cleaned, "/"), nil
}

// resolveUnderRoot joins a validated relative path against root and
// resolves it through symlink evaluation, returning the real root
// (EvalSymlinks-resolved) alongside the resolved target. Containment
// is checked against realRoot, never the raw root: the working
// directory itself may sit behind a symlink. A missing leaf is not an
// error here - the resolved parent is returned and the caller's disk
// operation reports not-found.
func resolveUnderRoot(root, rel string) (realRoot, resolved string, err error) {
	realRoot, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	target := filepath.Join(realRoot, rel)
	resolved, err = filepath.EvalSymlinks(target)
	if err != nil {
		// EvalSymlinks fails for a nonexistent leaf; resolve the parent
		// so a missing file still gets the containment check
		realParent, perr := filepath.EvalSymlinks(filepath.Dir(target))
		if perr != nil {
			return "", "", perr
		}
		return realRoot, filepath.Join(realParent, filepath.Base(target)), nil
	}
	return realRoot, resolved, nil
}

// ensureUnderRoot verifies a resolved absolute path stays inside realRoot.
func ensureUnderRoot(realRoot, resolved string) error {
	if resolved == realRoot {
		return nil
	}
	if !strings.HasPrefix(resolved, realRoot+string(filepath.Separator)) {
		return errors.New("path escapes the working directory")
	}
	return nil
}

// fileEntry is one /files row (mirrors the webui's fileEntry).
type fileEntry struct {
	Name      string
	SizeBytes int64
	IsDir     bool
	ModTime   time.Time
}

// sortFileEntries returns the entries ordered: directories first, then
// names, case-insensitive. Pure - sorts a copy, never the caller's slice.
func sortFileEntries(entries []fileEntry) []fileEntry {
	out := append([]fileEntry(nil), entries...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// filesRoot returns the directory /files walks: the process working
// directory as passed from the host, falling back to os.Getwd (the
// same root the webui file surface walks).
func filesRoot(cwd string) (string, error) {
	if cwd != "" {
		return cwd, nil
	}
	return os.Getwd()
}

// listDirUnderRoot lists one directory of root, applying the same
// validation/containment as the webui. Returns the sorted row set.
func listDirUnderRoot(root, rel string) ([]fileEntry, error) {
	rel, err := validateRelPath(rel)
	if err != nil {
		return nil, err
	}
	realRoot, resolved, rerr := resolveUnderRoot(root, rel)
	if rerr != nil {
		return nil, rerr
	}
	if err := ensureUnderRoot(realRoot, resolved); err != nil {
		return nil, err
	}
	info, serr := os.Stat(resolved)
	if serr != nil {
		return nil, serr
	}
	if !info.IsDir() {
		return nil, errors.New("not a directory: " + rel)
	}
	dirEntries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}
	entries := make([]fileEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		entry := fileEntry{Name: de.Name(), IsDir: de.IsDir()}
		if info, ierr := de.Info(); ierr == nil {
			entry.SizeBytes = info.Size()
			entry.ModTime = info.ModTime()
		}
		entries = append(entries, entry)
	}
	return sortFileEntries(entries), nil
}

// readPreviewAt reads up to 64 KiB of the file at rel under root,
// mirroring the webui's /files/read: non-UTF-8 content comes back
// binary=true with empty content, and the truncated flag says when
// the 64 KiB cap cut the read.
func readPreviewAt(root, rel string) (content, realRel string, truncated, binary bool, err error) {
	realRel, err = validateRelPath(rel)
	if err != nil {
		return "", "", false, false, err
	}
	realRoot, resolved, rerr := resolveUnderRoot(root, realRel)
	if rerr != nil {
		return "", "", false, false, rerr
	}
	if err := ensureUnderRoot(realRoot, resolved); err != nil {
		return "", "", false, false, err
	}
	info, serr := os.Stat(resolved)
	if serr != nil {
		return "", "", false, false, serr
	}
	if info.IsDir() {
		return "", "", false, false, errors.New("not a file: " + realRel)
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", "", false, false, err
	}
	defer f.Close()
	// one byte over the cap, so a cut is detectable without a second stat
	data := make([]byte, filesPreviewCap+1)
	n, rerr := f.Read(data)
	if rerr != nil && n == 0 && !errors.Is(rerr, io.EOF) {
		return "", "", false, false, rerr
	}
	data = data[:n]
	if len(data) > filesPreviewCap {
		data = data[:filesPreviewCap]
		truncated = true
	}
	if !utf8.Valid(data) {
		return "", realRel, truncated, true, nil
	}
	return string(data), realRel, truncated, false, nil
}

// logTailWindow bounds one log read: 512 KiB covers far more than the
// default 200 lines of a timestamped omega.log line (mirrors the
// webui's logTailWindow).
const logTailWindow = 512 << 10

// defaultLogLines is the /logs line count: the webui's /logs default.
const defaultLogLines = 200

// tailLines returns the last n lines of the file at path, reading at
// most logTailWindow from the end (mirrors the webui's tailLines).
// The final newline is dropped - trailing "" is not a log line.
func tailLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	if size == 0 {
		return []string{}, nil
	}

	window := int64(logTailWindow)
	if size < window {
		window = size
	}
	buf := make([]byte, window)
	if _, err := f.ReadAt(buf, size-window); err != nil {
		return nil, err
	}

	text := string(buf)
	if window < size {
		// the window starts mid-file: the first fragment is the cut-off
		// tail of an earlier line, not a line of its own
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		} else {
			text = ""
		}
	} else {
		// whole file: only strip the POSIX trailing newline artifact
		if len(text) > 0 && text[len(text)-1] == '\n' {
			text = text[:len(text)-1]
		}
	}
	if text == "" {
		return []string{}, nil
	}

	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// readLogIncrement reads the bytes appended to path since offset and
// returns them with the new offset. A file smaller than the offset is
// treated as rotated: the read restarts from zero and everything so
// far comes back as one chunk.
func readLogIncrement(path string, offset int64) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", offset, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", offset, err
	}
	size := st.Size()
	if size < offset {
		// rotated or truncated: restart from the top
		offset = 0
	}
	if size == offset {
		return "", offset, nil
	}
	buf := make([]byte, size-offset)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return "", offset, err
	}
	return string(buf), size, nil
}

// renderCronTable renders the cron job list the way the webui Cron tab
// orders and renders it: sorted by name, disabled jobs dimmed, empty
// Next shown as "-". Pure on jobs (the renderer's copy is sorted).
func renderCronTable(jobs []agent.CronJobInfo, t Theme) string {
	out := append([]agent.CronJobInfo(nil), jobs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	nameW, schedW := len("NAME"), len("SCHEDULE")
	for _, j := range out {
		if len(j.Name) > nameW {
			nameW = len(j.Name)
		}
		if len(j.Schedule) > schedW {
			schedW = len(j.Schedule)
		}
	}
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(t.Info.Render(fmt.Sprintf("  %-*s  %-*s  %-3s  %s", nameW, "NAME", schedW, "SCHEDULE", "ON", "NEXT")))
	sb.WriteString("\n")
	for _, j := range out {
		next := j.Next
		if next == "" {
			next = "-"
		}
		state := "yes"
		if !j.Enabled {
			state = "no"
		}
		row := fmt.Sprintf("  %-*s  %-*s  %-3s  %s", nameW, j.Name, schedW, j.Schedule, state, next)
		if !j.Enabled {
			row = t.Thinking.Render(row)
		}
		sb.WriteString(row)
		sb.WriteString("\n")
	}
	return sb.String()
}

// --- interactive /files browser ---

// filesBrowser is the open /files view state. rel is the relative path
// under the root ("" = root); the listing is dirs-first, one level at
// a time, and enter opens a directory or previews a file.
type filesBrowser struct {
	rel          string // relative path of the open directory; "" = root
	entries      []fileEntry
	cursor       int
	preview      bool   // a file preview is shown instead of the list
	previewLabel string // relative path of the previewed file
	previewTrunc bool   // preview content was cut at 64 KiB
	previewBin   bool   // preview content is not text
	vp           viewport.Model
}

// openFilesBrowser lists arg under root and returns the ready browser.
// An empty arg or a directory error is reported as err, not a half-
// opened browser.
func openFilesBrowser(root, arg string) (*filesBrowser, error) {
	rel, err := validateRelPath(arg)
	if err != nil {
		return nil, err
	}
	entries, err := listDirUnderRoot(root, rel)
	if err != nil {
		return nil, err
	}
	b := &filesBrowser{rel: rel, entries: entries, vp: viewport.New(80, 20)}
	return b, nil
}

// upDir strips the last segment of the current directory ("" = root).
// Pure on b - the caller assigns the result.
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

// current returns the highlighted entry.
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

// resize sets the browser viewport size: full width, and the height
// left by the window minus header, hint and status lines.
func (b *filesBrowser) resize(width, height int) {
	if height < 1 {
		height = 1
	}
	b.vp.Width = width
	b.vp.Height = height
}

// render paints the browser full-screen view: header (path, position),
// the listing or the preview, and a key hint footer.
func (m model) filesView() string {
	b := m.files
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
	sb.WriteString(m.theme.Info.Render(head))
	sb.WriteString("\n")
	if !b.preview {
		b.vp.SetContent(m.filesListContent())
	} else if b.previewBin {
		b.vp.SetContent(m.theme.Thinking.Render("(binary file - content not shown)"))
	}
	sb.WriteString(b.vp.View())
	sb.WriteString("\n")
	hint := "enter open · backspace parent · q/esc close"
	if b.preview {
		hint = "esc back · j/k scroll · q close"
	}
	sb.WriteString(m.theme.Status.Render(hint))
	return sb.String()
}

// filesListContent renders the directory rows for the listing pane.
func (m model) filesListContent() string {
	b := m.files
	if len(b.entries) == 0 {
		return m.theme.Thinking.Render("(empty directory)")
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
	sb.WriteString(m.theme.Info.Render(fmt.Sprintf("%-*s  %8s  %s", nameW, "NAME", "SIZE", "MODIFIED")))
	sb.WriteString("\n")
	for i, e := range b.entries {
		name := e.Name
		if e.IsDir {
			name += "/"
		}
		row := fmt.Sprintf("%-*s  %8s  %s", nameW, name, formatBytes(e.SizeBytes), formatModTime(e.ModTime))
		styled := m.theme.Thinking.Render // dimmed fallback (mtime)
		if e.IsDir {
			styled = m.theme.Tool.Render
		}
		if i == b.cursor {
			sb.WriteString(m.theme.Match.Render("> " + row))
		} else {
			sb.WriteString("  " + styled(row))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// filesBrowserHeight is rows consumed outside the listing viewport:
// header + hint + status + newlines between them.
const filesBrowserHeight = 4

// layoutFiles sizes the open browser to the current window. A closed
// browser is a no-op.
func (m model) layoutFiles() {
	if m.files == nil {
		return
	}
	h := m.screenHeight - filesBrowserHeight
	if h < 1 {
		h = 1
	}
	m.files.resize(m.viewport.Width, h)
}

// updateFilesKeys runs while the file browser is open; it consumes
// every key (the browser is a full-screen view).
func (m model) updateFilesKeys(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		// non-key messages: only window size matters
		if _, isSize := msg.(tea.WindowSizeMsg); isSize {
			m.layoutFiles()
		}
		return m, nil
	}
	b := m.files
	switch key.String() {
	case "ctrl+c":
		m.files = nil
		return m, tea.Quit
	case "q", "esc":
		if b.preview {
			b.preview = false
			b.vp.GotoTop()
		} else {
			m.files = nil
			m.refresh()
		}
		return m, nil
	case "backspace", "h", "left":
		if b.preview {
			b.preview = false
			b.vp.GotoTop()
			return m, nil
		}
		return m.openFilesRel(b.upDir())
	case "up", "k":
		if b.preview {
			b.vp.LineUp(1)
			return m, nil
		}
		if b.cursor > 0 {
			b.cursor--
		}
		return m, nil
	case "down", "j":
		if b.preview {
			b.vp.LineDown(1)
			return m, nil
		}
		if b.cursor < len(b.entries)-1 {
			b.cursor++
		}
		return m, nil
	case "pgup", "ctrl+u":
		b.vp.HalfPageUp()
		return m, nil
	case "pgdown", "ctrl+d":
		b.vp.HalfPageDown()
		return m, nil
	case "g", "home":
		b.vp.GotoTop()
		return m, nil
	case "G", "end":
		b.vp.GotoBottom()
		return m, nil
	case "enter", "right", "l":
		if b.preview || len(b.entries) == 0 {
			return m, nil
		}
		child := b.rel
		if child == "" {
			child = b.entries[b.cursor].Name
		} else {
			child = child + "/" + b.entries[b.cursor].Name
		}
		if b.entries[b.cursor].IsDir {
			return m.openFilesRel(child)
		}
		// file: load the preview
		content, realRel, truncated, binary, err := readPreviewAt(m.filesRoot(), child)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		b.preview = true
		b.previewLabel = realRel
		b.previewTrunc = truncated
		b.previewBin = binary
		b.vp.SetContent(content)
		b.vp.GotoTop()
		return m, nil
	}
	return m, nil
}

// openFilesRel lists rel under the root and reopens the listing. On a
// disk error the old browser state is kept and the error surfaces in
// the status line.
func (m model) openFilesRel(rel string) (tea.Model, tea.Cmd) {
	entries, err := listDirUnderRoot(m.filesRoot(), rel)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.files = &filesBrowser{rel: rel, entries: entries, vp: m.files.vp}
	m.files.vp.GotoTop()
	m.err = ""
	m.layoutFiles()
	return m, nil
}

// filesRoot returns the /files root: the host's working directory.
func (m model) filesRoot() string {
	if m.cwd != "" {
		return m.cwd
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
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

// --- slash command handlers ---

// handleFiles opens the interactive file browser rooted at the working
// directory. [path] selects the starting directory (same rules as the
// webui: relative, no dot segments); a bad path leaves no browser open.
func (m model) handleFiles(args []string) (tea.Model, tea.Cmd) {
	root, err := filesRoot(m.cwd)
	if err != nil {
		m.err = "resolve working directory: " + err.Error()
		return m, nil
	}
	if m.logFollow {
		m.stopLogFollow()
	}
	b, err := openFilesBrowser(root, strings.Join(args, "/"))
	if err != nil {
		m.err = "/files: " + err.Error()
		return m, nil
	}
	m.files = b
	m.layoutFiles()
	return m, nil
}

// handleCronCommand lists the cron jobs through the same seam the
// webui Cron tab reads (agent.CronLister over Context.Cron). A
// missing cron module or a provider without the listing reads as an
// empty job set - an unconfigured scheduler is a normal state, not a
// missing feature.
func (m model) handleCronCommand() (tea.Model, tea.Cmd) {
	if m.extensions == nil || m.extensions.Cron == nil {
		m.transcript += "\n" + m.theme.Info.Render("[cron] no cron jobs configured") + "\n"
		m.refresh()
		return m, nil
	}
	jobs := []agent.CronJobInfo{}
	if lister, ok := m.extensions.Cron.(agent.CronLister); ok {
		if got := lister.Jobs(); got != nil {
			jobs = got
		}
	}
	if len(jobs) == 0 {
		m.transcript += "\n" + m.theme.Info.Render("[cron] no cron jobs configured") + "\n"
		m.refresh()
		return m, nil
	}
	m.transcript += "\n" + m.theme.Info.Render("[cron]") + renderCronTable(jobs, m.theme)
	m.refresh()
	return m, nil
}

// handleLogs tails the log file the logger carries (the same path the
// webui's GET /logs reads). /logs -f keeps reading: a one-second tick
// appends whatever the logger wrote since the last read, until any
// keypress stops it.
func (m model) handleLogs(args []string) (tea.Model, tea.Cmd) {
	var lines []string
	path, err := m.logPath()
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	lines, err = tailLines(path, defaultLogLines)
	if err != nil {
		m.err = "tail log: " + err.Error()
		return m, nil
	}
	m.transcript += "\n" + m.theme.Info.Render("[logs: "+path+"]") + "\n"
	m.transcript += strings.Join(lines, "\n") + "\n"
	m.refresh()
	if len(args) == 1 && args[0] == "-f" {
		m.startLogFollow(path)
		return m, logFollowSchedule()
	}
	return m, nil
}

// logPath resolves the log file path through the logging seam - the
// FileLogger carries it; any other logger (including the no-op logger
// of a logging-disabled install) has none and /logs reports that.
func (m model) logPath() (string, error) {
	if m.extensions == nil || m.extensions.Logger == nil {
		return "", errors.New("log file not configured")
	}
	if p, ok := m.extensions.Logger.(interface{ Path() string }); ok {
		return p.Path(), nil
	}
	return "", errors.New("log file not configured")
}

// logFollowTickMsg drives the /logs -f loop: one per second while
// follow is active.
type logFollowTickMsg struct{}

// logFollowSchedule returns the command for the next follow tick.
func logFollowSchedule() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return logFollowTickMsg{} })
}

// startLogFollow arms the follow mode: the next incremental read
// starts at the current end of file, so follow shows only new lines.
func (m *model) startLogFollow(path string) {
	m.logFollow = true
	m.logFile = path
	m.logOffset = 0
	if st, err := os.Stat(path); err == nil {
		m.logOffset = st.Size()
	}
}

// stopLogFollow disarms follow mode.
func (m *model) stopLogFollow() {
	m.logFollow = false
	m.logFile = ""
}

// handleLogFollowTick appends the lines written since the last tick
// and reschedules. Rotation (the file shrinking) restarts the read
// from zero; a read error stops follow.
func (m model) handleLogFollowTick() (tea.Model, tea.Cmd) {
	if !m.logFollow {
		return m, nil
	}
	chunk, off, err := readLogIncrement(m.logFile, m.logOffset)
	if err != nil {
		m.stopLogFollow()
		m.err = "log follow: " + err.Error()
		return m, nil
	}
	m.logOffset = off
	if chunk != "" {
		// a chunk not ending on a line break is held back until the
		// logger finishes the line - partial lines never render
		if tail := strings.LastIndexByte(chunk, '\n'); tail < 0 {
			return m, logFollowSchedule()
		} else if complete := chunk[:tail]; complete != "" {
			m.transcript += complete + "\n"
			m.refresh()
		}
	}
	return m, logFollowSchedule()
}
