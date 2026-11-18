package observability

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// The capsule's REST face, read-only v1:
//
//	GET /observability/events?session=&level=&limit=
//	GET /observability/logs?lines=
//
// Both are pure reads: nothing here writes config, ring or disk. The
// events read is the primary view (the structured stream), the logs read
// is the secondary one (the on-disk tail). Free-text search is
// deliberately absent
// loud); session scope only when the parameter is present.
//
// The log tail is the shell's /logs reader's sibling (api/server_inspector.go).
// A capsule cannot import the shell ring (module-imports-shell, the tier
// law) and the shell's tailer is unexported, so the two are parallel by
// construction - the difference is ownership: /logs is this capsule's
// command surface (the logging half, since the 0.17.3 fold) while its
// file logger is what both read, and these two routes are the webui view.

// Page-size bounds for the two reads. The events page is capped at the ring's own page
// ceiling; the log tail mirrors the shell's reader (default 200 lines, hard ceiling
// 2000) so a tab cannot ask the server to materialize a gigabyte log.
const (
	eventsPageDefault = 200
	logTailDefault    = 200
	logTailMax        = 2000
	// logTailWindow bounds the bytes read from the END of the log file.
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

// eventsResponse is the GET /observability/events body.
type eventsResponse struct {
	Events   []Event `json:"events"`
	Returned int     `json:"returned"`
	Buffered int     `json:"buffered"`
	Capacity int     `json:"capacity"`
}

// handleEvents serves GET /observability/events. An unknown level value is a 400, never
// a silently empty page.
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

// logsResponse is the GET /observability/logs body.
type logsResponse struct {
	Path  string   `json:"path"`
	Lines []string `json:"lines"`
}

// logPather is this capsule's own consumer face for the "logger" need service.
type logPather interface {
	Path() string
}

// handleLogs serves GET /observability/logs.
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

// logPath resolves the logger seam per request.
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

// parseBounded reads an integer query parameter with a default and a ceiling.
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

// tailLines returns the last n lines of the file at path. A missing file is an error,
// never an empty tail - "there is no log" and "the log is empty" are different answers.
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
	}
	// Drop the POSIX trailing newline artifact: the last line of a file ending in "\n"
	// must not decode as a second, empty line.
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

// getOnly is the read-only guard.
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
