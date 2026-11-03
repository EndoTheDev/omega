package api

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// The inspector HTTP surface (card t_db430fea, read-only v1):
// GET /cron/jobs, GET /skills and GET /logs - the data behind the
// inspector's Cron | Skills | Tools | Logs tabs. Every handler here is
// a pure read: no config writes, no state mutation, nothing the API
// could use to change cron, skills or the log. The tool inventory
// behind the Tools tab already lives in server.go (GET /tools).

// handleCronJobs serves GET /cron/jobs: the cron runner's job set as a
// name/schedule/enabled/next array, sorted by name. When the cron
// module is unmounted (or a mounted CronProvider predates the
// listing), the answer is an EMPTY LIST, not 501 - an unconfigured
// scheduler is a normal state of a running omega, not a missing
// feature.
func (s *Server) handleCronJobs(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	// The consumer face (part 3 seam constitution): the route is the
	// shell's, the job rows pre-marshaled by the cron capsule - std
	// types only, the wire bytes identical to what the shared row type
	// produced. Unmounted module: an empty list, the same normal state
	// the empty-list law ever served.
	rows := []json.RawMessage{}
	if svc, ok := s.LiveServices().Resolve("cron").(interface{ JobRows() []json.RawMessage }); ok {
		if got := svc.JobRows(); got != nil {
			rows = got
		}
	}
	writeJSON(w, http.StatusOK, rows)
}

// extensionListing is one GET /extensions row: the same facts the
// /extensions command renders (name, tools, commands, seams) plus the
// module's own one-line description (agent.Describer, omitted when the
// module doesn't implement it). has-ui does NOT ride this wire: the
// webui derives it at build time from the composition manifest
// (harness/<name>/ui/dockable.json - compose.ts), the runtime doesn't
// ship module source. Extensions carry no versions.
type extensionListing struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tools       int      `json:"tools"`
	Commands    int      `json:"commands"`
	Seams       []string `json:"seams"`
	// tool_names / command_names carry the EXPANSION detail (card
	// t_210a0d19): the webui extensions tab lists this module's tools
	// and slash commands inside its expanded row. Names ride the wire;
	// descriptions do not (the inspector column is ~300px - a list of
	// verbatim names fits, prompt-quality prose does not).
	ToolNames    []string `json:"tool_names"`
	CommandNames []string `json:"command_names"`
}

// handleExtensions serves GET /extensions: the loaded-extension
// inventory in mount order - the webui parity of the /extensions
// command. Mount order is the dependency order MountAllAPI built, the
// same order the inspector renders; keep it, don't alphabetize away the
// seams-require-providers structure. A no-extension run lists as an
// empty array, like the other inspector reads.
//
// THE LAW (card t_b2654d6b): this endpoint lists the MOUNTED capsules
// exactly - ctx.Infos, the inventory the mount journal keeps exact
// across boot-disables (never mounted, never a row) and live toggles
// (dismount ctxUndo, remount ctxApply). The read goes through
// agent.ExtensionInfos, whose serving copy core.LiveSeams refreshes
// after every successful toggle (rewireSeams →
// agent.RefreshExtensionInfos, the same law as /tools). A disabled
// module therefore cannot appear here while GET /modules reports it
// disabled, and a re-enabled one is back the moment it remounts.
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

// handleSkills serves GET /skills: the loaded skill inventory as a
// name+description array, sorted by name. An unmounted skills module
// lists as empty, like cron.
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	// The consumer face (part 3): rows built server-side from the
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

// logTail bounds one GET /logs read. 512 KiB covers far more than the
// default 200 lines of a timestamped omega.log line, and caps memory
// if someone asks for a huge N or leaves a huge log.
const logTailWindow = 512 << 10

// handleLogs serves GET /logs?lines=N: the tail of the omega log the
// server itself writes, default 200 lines. 404 when there is no log
// file - logging disabled, or a logger without a readable path. The
// read is positional (tail window, no file rewrite), so it is safe
// against the logger's own appends.
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

// parseLogLines reads the ?lines= parameter with the endpoint's
// defaults: 200 when absent, clamped to 1..2000 so neither empty-file
// reads nor unbounded tails are possible.
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

// logPathOf extracts the log file path from the logger seam. The
// FileLogger carries its own path (harness/logging); any other logger
// implementation, including NopLogger for logging-disabled installs,
// has none and the /logs endpoint 404s.
func logPathOf(logger core.LoggerProvider) string {
	if p, ok := logger.(interface{ Path() string }); ok {
		return p.Path()
	}
	return ""
}

// tailLines returns the last n lines of the file at path. It reads at
// most logTailWindow from the end: seek to the window, split on
// newlines, drop the cut-off head line (unless the window covers the
// whole file), then keep the last n. The final newline is dropped -
// trailing "" is not a log line. Line contents are returned raw
// (timestamps included, as written).
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
		// TAIL of an earlier line, not a line of its own
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
