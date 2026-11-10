package observability

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// The capsule's REST face, read-only v1 (card t_8de82cff):
//
//	GET /observability/events?session=&level=&limit=
//	GET /observability/logs?lines=
//
// Both are pure reads: nothing here writes config, ring or disk. The
// events read is the primary view (the structured stream), the logs read
// is the secondary one (the on-disk tail). Free-text search is
// deliberately absent (ruling 4, deferred until the stream proves too
// loud); session scope only when the parameter is present.
//
// The log tail is the module's own copy of the shell's /logs reader
// (api/server_inspector.go). A capsule cannot import the shell ring
// (module-imports-shell, the tier law) and the shell's tailer is
// unexported, so the two are parallel by construction - the difference
// is ownership: /logs belongs to the shell's Logs tab, these two routes
// mount and die with this capsule, which is what lets the whole
// observability view be removed whole.

// Page-size bounds for the two reads. The events page is capped at the
// ring's own page ceiling; the log tail mirrors the shell's reader
// (default 200 lines, hard ceiling 2000) so a tab cannot ask the server
// to materialize a gigabyte log.
const (
	eventsPageDefault = 200
	logTailDefault    = 200
	logTailMax        = 2000
	// logTailWindow bounds the bytes read from the END of the log file:
	// 512 KiB covers far more than logTailMax timestamped lines, and caps
	// memory when the file has grown for weeks.
	logTailWindow = 512 << 10
)

// Routes is the capsule's REST face over the ring and the logger.
type Routes struct {
	ctx  *core.Context
	ring *Ring
}

func newRoutes(ctx *core.Context, ring *Ring) *Routes {
	return &Routes{ctx: ctx, ring: ring}
}

// eventsResponse is the GET /observability/events body. buffered and
// capacity describe the ring itself, so a client can tell "no events
// yet" apart from "the ring rolled over while you were away".
type eventsResponse struct {
	Events   []Event `json:"events"`
	Returned int     `json:"returned"`
	Buffered int     `json:"buffered"`
	Capacity int     `json:"capacity"`
}

// handleEvents serves GET /observability/events: the structured stream,
// newest page first-class. An unknown level value is a 400, never a
// silently empty page.
func (rt *Routes) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	q := r.URL.Query()
	level := strings.TrimSpace(q.Get("level"))
	switch level {
	case "", "all", LevelInfo, LevelWarn, LevelError:
	default:
		http.Error(w, "level must be one of all, info, warn, error", http.StatusBadRequest)
		return
	}
	events := rt.ring.Page(q.Get("session"), level, parseBounded(q.Get("limit"), eventsPageDefault, PageMax))
	if events == nil {
		events = []Event{}
	}
	writeJSON(w, http.StatusOK, eventsResponse{
		Events:   events,
		Returned: len(events),
		Buffered: rt.ring.Len(),
		Capacity: rt.ring.Capacity(),
	})
}

// logsResponse is the GET /observability/logs body: the file the tail
// came from (so a reader can tell WHICH log it is looking at) and the
// verbatim lines.
type logsResponse struct {
	Path  string   `json:"path"`
	Lines []string `json:"lines"`
}

// logPather is this capsule's own consumer face for the "logger" need
// service: the file logger exposes its path (harness/logging.Path), a
// NopLogger does not - which is the honest "logging is disabled" answer.
type logPather interface {
	Path() string
}

// handleLogs serves GET /observability/logs: the tail of the log the
// running process writes. 404 when there is no log file (logging
// disabled, or a logger without a path) - the same degradation the
// shell's /logs documents.
func (rt *Routes) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	path := rt.logPath()
	if path == "" {
		http.Error(w, "log file not configured", http.StatusNotFound)
		return
	}
	lines, err := tailLines(path, parseBounded(r.URL.Query().Get("lines"), logTailDefault, logTailMax))
	if err != nil {
		http.Error(w, "tail log: "+err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, logsResponse{Path: path, Lines: lines})
}

// logPath resolves the logger seam per request (a live toggle can swap
// the logger under a running server) through this capsule's own face.
func (rt *Routes) logPath() string {
	if rt.ctx == nil {
		return ""
	}
	svc := rt.ctx.Services().Resolve("logger")
	if svc == nil {
		return ""
	}
	if p, ok := svc.(logPather); ok {
		return p.Path()
	}
	return ""
}

// parseBounded reads an integer query parameter with a default and a
// ceiling: absent or unparsable keeps the default, anything else clamps
// to 1..max so neither an empty page nor an unbounded read is possible.
func parseBounded(raw string, def, max int) int {
	n := def
	if raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			n = v
		}
	}
	if n < 1 {
		n = 1
	}
	if n > max {
		n = max
	}
	return n
}

// tailLines returns the last n lines of the file at path. It reads at
// most logTailWindow from the end: seek into the window, split on
// newlines, drop the cut-off head fragment (unless the window covers the
// whole file), then keep the last n. A missing file is an error, never
// an empty tail - "there is no log" and "the log is empty" are different
// answers.
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
		// The window starts mid-file: the first fragment is the cut-off
		// TAIL of an earlier line, not a line of its own.
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		} else {
			text = ""
		}
	}
	// Drop the POSIX trailing newline artifact: the last line of a file
	// ending in "\n" must not decode as a second, empty line. (A tail
	// window sees this in both branches - the whole-file read and the
	// mid-file read - so the strip lives here, once.)
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{}, nil
	}

	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// getOnly is the read-only guard (the shell's GetOnly equivalent; a
// capsule answers with its own copy rather than importing the shell).
func getOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// writeJSON serves one JSON body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
