package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
	agentapi "github.com/EndoTheDev/omega/agent/api"
)

// The inspector HTTP surface.

// handleCronJobs serves GET /cron/jobs: the cron runner's job set as a
// name/schedule/enabled/next array, sorted by name.
func (s *Server) handleCronJobs(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	rows := []json.RawMessage{}
	if svc, ok := s.LiveServices().Resolve("cron").(interface{ JobRows() []json.RawMessage }); ok {
		if got := svc.JobRows(); got != nil {
			rows = got
		}
	}
	writeJSON(w, http.StatusOK, rows)
}

// extensionListing is one GET /extensions row.
type extensionListing struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tools       int      `json:"tools"`
	Commands    int      `json:"commands"`
	Seams       []string `json:"seams"`
	// tool_names / command_names carry the EXPANSION detail. Names ride the wire;
	// descriptions do not (the inspector column is ~300px - a list of verbatim names fits,
	// prompt-quality prose does not).
	ToolNames    []string `json:"tool_names"`
	CommandNames []string `json:"command_names"`
}

// handleExtensions serves GET /extensions. Mount order is the dependency order
// MountAllAPI built, the same order the inspector renders; keep it, don't alphabetize
// away the seams-require-providers structure. THE LAW: this endpoint lists the MOUNTED
// capsules exactly - the Context's inventory, which the mount journal keeps exact across
// boot-disables (never mounted, never a row) and live toggles (dismount ctxUndo,
// remount ctxApply). A disabled module therefore cannot appear here while GET /modules
// reports it disabled, and a re-enabled one is back the moment it remounts.
func (s *Server) handleExtensions(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	info := s.Agent().ExtensionInfos()
	out := make([]extensionListing, 0, len(info))
	for _, e := range info {
		seams := e.Seams
		if seams == nil {
			seams = []string{}
		}
		toolNames := make([]string, 0, len(e.ToolList))
		for _, t := range e.ToolList {
			toolNames = append(toolNames, t.Name)
		}
		commandNames := e.CommandNames
		if commandNames == nil {
			commandNames = []string{}
		}
		out = append(out, extensionListing{
			Name:         e.Name,
			Description:  e.Description,
			Tools:        e.Tools,
			Commands:     e.Commands,
			Seams:        seams,
			ToolNames:    toolNames,
			CommandNames: commandNames,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// skillListing is one GET /skills row: name + description only. The
// inspector tab renders exactly this; skill content, directories and
// the prompt section never ride the wire.
type skillListing struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleSkills serves GET /skills: the loaded skill inventory as a name+description
// array, sorted by name.
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	// The consumer face: rows built server-side from the
	// std-typed inventory view - same name+description rows, same
	// name sort, same empty-list degradation for an unmounted module.
	out := []skillListing{}
	if svc, ok := s.LiveServices().Resolve("skills").(interface {
		SkillsEntries() (index map[string]string, contents map[string]string, err error)
	}); ok {
		index, _, listErr := svc.SkillsEntries()
		if listErr != nil {
			http.Error(w, "list skills: "+listErr.Error(), http.StatusInternalServerError)
			return
		}
		names := make([]string, 0, len(index))
		for n := range index {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			out = append(out, skillListing{Name: n, Description: index[n]})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// logTail bounds one GET /logs read.
const logTailWindow = 512 << 10

// handleLogs serves GET /logs?lines=N.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	lines := parseLogLines(r.URL.Query().Get("lines"))
	path := logPathOf(s.Logger())
	if path == "" {
		http.Error(w, "log file not configured", http.StatusNotFound)
		return
	}
	tail, err := tailLines(path, lines)
	if err != nil {
		http.Error(w, "tail log: "+err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "lines": tail})
}

// parseLogLines reads the ?lines= parameter with the endpoint's defaults.
func parseLogLines(raw string) int {
	n := 200
	if raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			n = v
		}
	}
	if n < 1 {
		n = 1
	}
	if n > 2000 {
		n = 2000
	}
	return n
}

// logPathOf extracts the log file path from the logger seam.
func logPathOf(logger agent.LoggerProvider) string {
	if p, ok := logger.(interface{ Path() string }); ok {
		return p.Path()
	}
	return ""
}

// tailLines returns the last n lines of the file at path.
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

	lines := splitLines(text)
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// splitLines splits on '\n' with no empty trailing entry.
func splitLines(s string) []string {
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// logPushInterval is the logs topic's cadence.
const logPushInterval = time.Second

// logPushWindow bounds one tick's read.
const logPushWindow = 256 << 10

// followLogs pushes log lines onto the logs topic (the delta, never the snapshot).
// Started by Serve, after the listener is up - the goroutine fence's law (mount never
// starts background work; the host does). Omega's only log-write site is
// harness/logging's FileLogger (logging.go: Printf/Errorf/Warnf -> log.Logger -> the
// file handle), and the module tier cannot reach this hub - a module importing the
// shell ring (api/, agent/api/) is the module-imports- shell violation. A write lands
// there once, and both readers - the REST tail and this push - read the same bytes, so
// the REST snapshot and the pushed lines can never disagree about what the log says.
// Subscription-gating is the hub's: while nobody holds the topic the offset still
// advances but no bytes are read and nothing is published, so a pane that mounts later
// starts from its own GET /logs snapshot and receives only what is appended after it -
// the tail never replays boot history at a new subscriber.
func (s *Server) followLogs(ctx context.Context) {
	path := logPathOf(s.Logger())
	if path == "" {
		return // logging disabled (NopLogger): there is no file to tail
	}
	seeded := s.logSeeded // test observability: closed once offset >= 0, nil in production
	offset := int64(-1)   // -1: no baseline yet, seed to the file's end
	carry := ""
	ticker := time.NewTicker(logPushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			offset, carry = s.pushLogDelta(path, offset, carry)
			if seeded != nil && offset >= 0 {
				close(seeded)
				seeded = nil
			}
		}
	}
}

// pushLogDelta publishes the complete lines appended to path since offset and returns.
// A first call, or a file that shrank (truncated, rotated), resyncs the offset to the
// end: history belongs to GET /logs, never to this topic.
func (s *Server) pushLogDelta(path string, offset int64, carry string) (int64, string) {
	f, err := os.Open(path)
	if err != nil {
		return offset, carry // transient: the next tick retries
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return offset, carry
	}
	size := st.Size()
	if offset < 0 || size < offset {
		return size, ""
	}
	if size == offset {
		return offset, carry
	}
	if !s.Sockets().Wants(agentapi.TopicLogs) {
		// nobody asked: skip the bytes, keep the offset at the end.
		return size, ""
	}
	n := size - offset
	if n > logPushWindow {
		n = logPushWindow
	}
	buf := make([]byte, n)
	read, _ := f.ReadAt(buf, offset)
	if read <= 0 {
		return offset, carry
	}
	offset += int64(read)
	text := carry + string(buf[:read])
	parts := strings.Split(text, "\n")
	carry = parts[len(parts)-1]
	lines := parts[:len(parts)-1]
	if len(lines) == 0 {
		return offset, carry
	}
	s.Sockets().Publish(agentapi.TopicLogs, "", agentapi.LogLines{Lines: lines})
	return offset, carry
}
