package observability

// Module-owned command surface (card t_0744eb8e): /logs belongs to this
// capsule (the logging half, since the 0.17.3 fold card t_ee3a968b) and
// rides the extension command seam - the same table the CLI loop
// dispatches through and the same data the webui's GET /logs reads. The
// old TUI follow mode (-f, a screen-tick redraw) was deleted with the
// TUI shell: following a file is a screen concern, and the loop law
// keeps the terminal one-dimensional.

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// defaultLogLines is the /logs line count; the read itself is the
// capsule's one tailer (routes.go's tailLines, bounded by logTailWindow -
// after the fold there is ONE reader for both surfaces).
const defaultLogLines = 200

// logsHandler tails the log file the logger carries. The logger seam
// only guarantees Path on the file logger - any other logger (including
// the no-op logger of a logging-disabled install) has none and /logs
// reports that. The closure captures the mount-time context so the
// handler reads the live logger seam.
//
// A foreign name is a chain MISS, not a /logs request: the command
// seam's wrapper falls through on an error, so answering every name
// here would swallow every command registered earlier in the chain
// (card t_d923b50a).
func logsHandler(ctx *core.Context) func(context.Context, string, string) (core.CommandResult, error) {
	return func(_ context.Context, name, args string) (core.CommandResult, error) {
		if name != "/logs" {
			return core.CommandResult{}, core.CommandMiss(name)
		}
		path := ""
		if ctx != nil {
			if p, ok := ctx.Services().Resolve("logger").(interface{ Path() string }); ok {
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
