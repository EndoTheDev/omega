package logging

// Module-owned TUI surface (card t_88bb7dcf): /logs belongs to the
// logging module. Everything here reads through the same seam the
// webui's GET /logs reads - the file the FileLogger carries. Follow
// (/logs -f, card t_5c280c68 behavior preserved) re-reads from a byte
// offset on a one-second tick that the TUI shell drives.

import (
	"os"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// defaultLogLines is the /logs line count: the webui's /logs default.
const defaultLogLines = 200

// logTailWindow bounds one log read: 512 KiB covers far more than the
// default 200 lines of a timestamped omega.log line (mirrors the
// webui's logTailWindow).
const logTailWindow = 512 << 10

// tui implements core.TUIProvider for the /logs command.
type tui struct{}

var _ core.TUIProvider = (*tui)(nil)

// TUICommands declares the /logs command.
func (t *tui) TUICommands() []core.TUICommand {
	return []core.TUICommand{
		{Name: "/logs", Help: "tail omega.log (-f: keep following)", Args: []string{"-f"}},
	}
}

// HandleTUICommand tails the log file the logger carries. The logger
// seam only guarantees Path on the file logger - any other logger
// (including the no-op logger of a logging-disabled install) has none
// and /logs reports that.
func (t *tui) HandleTUICommand(name, args string, c core.TUIConsole) core.TUIResult {
	path := ""
	if pctx := c.Ext(); pctx != nil && pctx.Logger != nil {
		if p, ok := pctx.Logger.(interface{ Path() string }); ok {
			path = p.Path()
		}
	}
	if path == "" {
		c.Error("log file not configured")
		return core.TUIResult{}
	}
	fields := strings.Fields(args)
	lines, err := tailLines(path, defaultLogLines)
	if err != nil {
		c.Error("tail log: " + err.Error())
		return core.TUIResult{}
	}
	c.Labeled("[logs: "+path+"]", "\n"+strings.Join(lines, "\n")+"\n")
	if len(fields) == 1 && fields[0] == "-f" {
		return core.TUIResult{Tick: &core.TUITick{
			Interval: time.Second,
			Step:     logFollowStep(path),
		}}
	}
	return core.TUIResult{}
}

// logFollowStep returns the follow-loop step for path: append what the
// logger wrote since the last read - only complete lines render; a
// chunk not ending on a line break is held back until the logger
// finishes the line. Rotation (the file shrinking) restarts the read
// from zero; a read error stops follow.
func logFollowStep(path string) func(c core.TUIConsole) (string, bool) {
	offset := int64(0)
	if st, err := os.Stat(path); err == nil {
		offset = st.Size()
	}
	return func(c core.TUIConsole) (string, bool) {
		chunk, off, err := readLogIncrement(path, offset)
		if err != nil {
			c.Error("log follow: " + err.Error())
			return "", false
		}
		offset = off
		if tail := strings.LastIndexByte(chunk, '\n'); tail < 0 {
			return "", true
		} else if complete := chunk[:tail]; complete != "" {
			return complete + "\n", true
		}
		return "", true
	}
}

// tailLines returns the last n lines of path, no more than
// logTailWindow from the end (mirrors the webui's tailLines).
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
