package logging

// Module-owned command surface (card t_0744eb8e): /logs belongs to the
// logging module and rides the extension command seam - the same table
// the CLI loop dispatches through and the same data the webui's
// GET /logs reads. The old TUI follow mode (-f, a screen-tick redraw)
// was deleted with the TUI shell: following a file is a screen
// concern, and the loop law keeps the terminal one-dimensional.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// defaultLogLines is the /logs line count.
const defaultLogLines = 200

// logTailWindow bounds one log read: 512 KiB covers far more than the
// default 200 lines of a timestamped omega.log line.
const logTailWindow = 512 << 10

// logsHandler tails the log file the logger carries. The logger seam
// only guarantees Path on the file logger - any other logger (including
// the no-op logger of a logging-disabled install) has none and /logs
// reports that. The closure captures the mount-time context so the
// handler reads the live logger seam.
func logsHandler(ctx *core.Context) func(context.Context, string, string) (core.CommandResult, error) {
	return func(_ context.Context, _ string, args string) (core.CommandResult, error) {
		path := ""
		if ctx != nil && ctx.Logger != nil {
			if p, ok := ctx.Logger.(interface{ Path() string }); ok {
				path = p.Path()
			}
		}
		if path == "" {
			return core.CommandResult{}, fmt.Errorf("log file not configured")
		}
		fields := strings.Fields(args)
		if len(fields) > 0 && fields[0] == "-f" {
			return core.CommandResult{}, fmt.Errorf("/logs -f is gone with the terminal shell - the webui tail follows live")
		}
		lines, err := tailLines(path, defaultLogLines)
		if err != nil {
			return core.CommandResult{}, fmt.Errorf("tail log: %w", err)
		}
		return core.CommandResult{Text: "[logs: " + path + "]\n" + strings.Join(lines, "\n")}, nil
	}
}

// tailLines returns the last n lines of path, no more than
// logTailWindow from the end. The final newline is dropped - trailing
// "" is not a log line.
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